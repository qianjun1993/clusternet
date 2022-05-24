/*
Copyright 2022 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package estimator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	framework "github.com/clusternet/clusternet/pkg/estimator/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/estimator/framework/plugins"
	frameworkruntime "github.com/clusternet/clusternet/pkg/estimator/framework/runtime"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	nodeNameKeyIndex = "spec.nodeName"
)

// GetPodsAssignedToNodeFunc is a function which accept a node name input
// and returns the pods that assigned to the node.
type GetPodsAssignedToNodeFunc func(string) ([]*corev1.Pod, error)

type EstimatorServer struct {
	ctx context.Context

	// controllerOptions for leader election and client connection
	controllerOptions *utils.ControllerOptions

	kubeClient kubernetes.Interface

	informerFactory informers.SharedInformerFactory
	nodeInformer    informerv1.NodeInformer
	podInformer     informerv1.PodInformer
	nodeLister      listerv1.NodeLister
	podLister       listerv1.PodLister

	getPodFunc GetPodsAssignedToNodeFunc

	// default in-tree registry
	registry  frameworkruntime.Registry
	framework framework.Framework

	httpServer *http.Server
}

func NewEstimatorServer(
	ctx context.Context,
	opts *utils.ControllerOptions,
) (*EstimatorServer, error) {
	clientConfig, err := utils.LoadsKubeConfig(&opts.ClientConnection)
	if err != nil {
		return nil, err
	}
	clientConfig.QPS = opts.ClientConnection.QPS
	clientConfig.Burst = int(opts.ClientConnection.Burst)

	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: clientConfig,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-estimator"))
	informerFactory := informers.NewSharedInformerFactory(kubeClient, known.DefaultResync)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-estimator"})
	es := &EstimatorServer{
		ctx:               ctx,
		controllerOptions: opts,
		kubeClient:        kubeClient,
		informerFactory:   informerFactory,
		nodeInformer:      informerFactory.Core().V1().Nodes(),
		podInformer:       informerFactory.Core().V1().Pods(),
		nodeLister:        informerFactory.Core().V1().Nodes().Lister(),
		podLister:         informerFactory.Core().V1().Pods().Lister(),
		registry:          plugins.NewInTreeRegistry(),
		httpServer:        &http.Server{Addr: fmt.Sprintf("%s:%d", "0.0.0.0", 10353)},
	}

	framework, err := frameworkruntime.NewFramework(es.registry, getDefaultPlugins(),
		frameworkruntime.WithEventRecorder(recorder),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithClientSet(kubeClient),
		frameworkruntime.WithKubeConfig(clientConfig),
		frameworkruntime.WithParallelism(parallelize.DefaultParallelism),
		frameworkruntime.WithRunAllFilters(false),
	)
	if err != nil {
		return nil, err
	}
	es.framework = framework

	getPodFunc, err := es.BuildGetPodsAssignedToNodeFunc(es.podInformer)
	if err != nil {
		return nil, err
	}
	es.getPodFunc = getPodFunc

	return es, nil
}

var _ schedulerapi.EstimatorProvider = &EstimatorServer{}

func (es *EstimatorServer) Run() error {
	// Start all informers.
	es.informerFactory.Start(es.ctx.Done())
	// Wait for all caches to sync before scheduling.
	es.informerFactory.WaitForCacheSync(es.ctx.Done())

	// if leader election is disabled, so runCommand inline until done.
	if !es.controllerOptions.LeaderElection.LeaderElect {
		es.run(es.ctx)
		klog.Warning("finished without leader elect")
		return nil
	}

	// leader election is enabled, runCommand via LeaderElector until done and exit.
	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}
	le, err := leaderelection.NewLeaderElector(*utils.NewLeaderElectionConfigWithDefaultValue(
		curIdentity,
		es.controllerOptions.LeaderElection.ResourceName,
		es.controllerOptions.LeaderElection.ResourceNamespace,
		es.controllerOptions.LeaderElection.LeaseDuration.Duration,
		es.controllerOptions.LeaderElection.RenewDeadline.Duration,
		es.controllerOptions.LeaderElection.RetryPeriod.Duration,
		es.kubeClient,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				es.run(ctx)
			},
			OnStoppedLeading: func() {
				klog.Error("leader election got lost")
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == curIdentity {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	))
	if err != nil {
		return err
	}
	le.Run(es.ctx)
	return nil
}

func (es *EstimatorServer) run(ctx context.Context) {
	stopCh := ctx.Done()
	klog.Infof("Starting clusternet estimator")
	defer klog.Infof("Shutting down clusternet estimator")

	es.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(ctx.Done(), es.podInformer.Informer().HasSynced, es.nodeInformer.Informer().HasSynced) {
		klog.Errorf("failed to wait for caches to sync")
		return
	}

	es.serveHTTP()

	// TODO: add communication interface to scheduler
}

// BuildGetPodsAssignedToNodeFunc establishes an indexer to map the pods and their assigned nodes.
// It returns a function to help us get all the pods that assigned to a node based on the indexer.
func (es *EstimatorServer) BuildGetPodsAssignedToNodeFunc(podInformer informerv1.PodInformer) (GetPodsAssignedToNodeFunc, error) {
	// Establish an indexer to map the pods and their assigned nodes.
	err := podInformer.Informer().AddIndexers(cache.Indexers{
		nodeNameKeyIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return []string{}, nil
			}
			if len(pod.Spec.NodeName) == 0 {
				return []string{}, nil
			}
			return []string{pod.Spec.NodeName}, nil
		},
	})
	if err != nil {
		return nil, err
	}

	// The indexer helps us get all the pods that assigned to a node.
	podIndexer := podInformer.Informer().GetIndexer()
	getPodsAssignedToNode := func(nodeName string) ([]*corev1.Pod, error) {
		objs, err := podIndexer.ByIndex(nodeNameKeyIndex, nodeName)
		if err != nil {
			return nil, err
		}
		pods := make([]*corev1.Pod, 0, len(objs))
		for _, obj := range objs {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				continue
			}
			pods = append(pods, pod)
		}
		return pods, nil
	}
	return getPodsAssignedToNode, nil
}

func (es *EstimatorServer) MaxAcceptableReplicas(ctx context.Context, requirements appsapi.ReplicaRequirements) (map[string]int32, error) {

	defaultAcceptableReplicas := map[string]int32{
		framework.DefaultAcceptableReplicasKey: 0,
	}

	klog.V(4).Infof("node selector: %v", requirements.NodeSelector)
	nodes, err := es.nodeLister.List(labels.SelectorFromSet(requirements.NodeSelector))
	if err != nil {
		return defaultAcceptableReplicas, fmt.Errorf("failed to get nodes that match node selector, err: %v", err)
	}
	klog.V(4).Infof("find %v nodes", len(nodes))
	nodeInfoList := make([]*framework.NodeInfo, len(nodes))
	var nodesLen int32
	getNodeInfo := func(i int) {
		pods, err := es.getPodFunc(nodes[i].Name)
		if err != nil {
			klog.V(4).InfoS("failed to get pods in nodes", "nodes", nodes[i].Name)
		} else {
			nodeInfo := framework.NewNodeInfo(nodes[i], pods)
			length := atomic.AddInt32(&nodesLen, 1)
			nodeInfoList[length-1] = nodeInfo
		}
	}
	es.framework.Parallelizer().Until(ctx, len(nodes), getNodeInfo)

	klog.V(4).Infof("find %v nodes", len(nodes))

	// Step 1: Filter Nodes.
	feasibleNodes, err := findNodesThatFitRequirements(ctx, es.framework, &requirements, nodeInfoList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	if len(feasibleNodes) == 0 {
		return defaultAcceptableReplicas, fmt.Errorf("no feasible nodes found")
	}

	// Step 2: cal max available replicas for each feasibleNodes.
	nodeScoreList, err := computeReplicas(ctx, es.framework, &requirements, feasibleNodes)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	// Step 3: Prioritize clusters.
	priorityList, err := prioritizeNodes(ctx, es.framework, &requirements, feasibleNodes, nodeScoreList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	// step4 aggregate the max available replicas
	result, err := aggregateReplicas(ctx, es.framework, &requirements, priorityList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}
	return result, nil
}

func (es *EstimatorServer) UnschedulableReplicas(ctx context.Context, gvk metav1.GroupVersionKind, namespacedName string,
	labelSelector map[string]string) (int32, error) {
	// TODO: add real logic
	return 0, nil
}

func (es *EstimatorServer) serveHTTP() {
	handler := http.NewServeMux()

	handler.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler.Handle("/metrics", promhttp.Handler())

	handler.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			type DebugInfo struct {
				Requirements appsapi.ReplicaRequirements `json:"requirements"`
			}

			klog.V(5).Infof("request: %v", r.Body)

			var request DebugInfo
			// Try to decode the request body into the struct. If there is an error,
			// respond to the client with the error message and a 400 status code.
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			klog.V(5).Infof("request: %v", request)

			type response struct {
				maxReplicas map[string]int32 `json:"maxReplicas"`
			}

			maxReplicas, err := es.MaxAcceptableReplicas(es.ctx, request.Requirements)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			res := response{
				maxReplicas: maxReplicas,
			}
			klog.V(6).InfoS("maxReplicas", "num", maxReplicas)
			if err := json.NewEncoder(w).Encode(res); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		default:
			http.Error(w, "Only post method is supported", http.StatusBadRequest)
		}
	})
	es.httpServer.Handler = handler
	if err := es.httpServer.ListenAndServe(); err != nil {
		klog.Error(err)
	}
}
