package estimator

import (
	"context"
	"k8s.io/apimachinery/pkg/labels"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	framework "github.com/clusternet/clusternet/pkg/estimator/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/estimator/framework/plugins"
	frameworkruntime "github.com/clusternet/clusternet/pkg/estimator/framework/runtime"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/options"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/utils"
)

type EstimatorServer struct {
	kubeClient kubernetes.Interface

	informerFactory informers.SharedInformerFactory
	nodeInformer    informerv1.NodeInformer
	podInformer     informerv1.PodInformer
	nodeLister      listerv1.NodeLister
	podLister       listerv1.PodLister

	// default in-tree registry
	registry  frameworkruntime.Registry
	framework framework.Framework
}

func NewEstimatorServer(schedulerOptions *options.SchedulerOptions) (*EstimatorServer, error) {
	clientConfig, err := utils.LoadsKubeConfig(&schedulerOptions.ClientConnection)
	if err != nil {
		return nil, err
	}
	clientConfig.QPS = schedulerOptions.ClientConnection.QPS
	clientConfig.Burst = int(schedulerOptions.ClientConnection.Burst)

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
		kubeClient:      kubeClient,
		informerFactory: informerFactory,
		nodeInformer:    informerFactory.Core().V1().Nodes(),
		podInformer:     informerFactory.Core().V1().Pods(),
		nodeLister:      informerFactory.Core().V1().Nodes().Lister(),
		podLister:       informerFactory.Core().V1().Pods().Lister(),
		registry:        plugins.NewInTreeRegistry(),
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
	return es, nil
}

func (es *EstimatorServer) Start(ctx context.Context) {
	stopCh := ctx.Done()
	klog.Infof("Starting clusternet estimator")
	defer klog.Infof("Shutting down clusternet estimator")

	es.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(ctx.Done(), es.podInformer.Informer().HasSynced, es.nodeInformer.Informer().HasSynced) {
		klog.Errorf("failed to wait for caches to sync")
		return
	}
	// TODO: add communication interface to scheduler
}

func (es *EstimatorServer) MaxAcceptableReplicas(ctx context.Context, requirements appsapi.ReplicaRequirements) (map[string]int32, error) {

	nodes, err := es.nodeLister.List(labels.SelectorFromSet(requirements.NodeSelector))

}
