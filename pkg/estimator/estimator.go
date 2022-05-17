package estimator

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/estimator/framework/interfaces"
	frameworkruntime "github.com/clusternet/clusternet/pkg/estimator/framework/runtime"
	"github.com/clusternet/clusternet/pkg/estimator/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
)

// findNodesThatFitRequirements filters the nodes to find the ones that fit the requirements based on the framework filter plugins.
func findNodesThatFitRequirements(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo,
) ([]*framework.NodeInfo, error) {
	// Run "prefilter" plugins.
	s := fwk.RunPreFilterPlugins(ctx, requirements)
	if !s.IsSuccess() {
		if !s.IsUnschedulable() {
			return nil, s.AsError()
		}
		return nil, s.AsError()
	}

	feasibleNodes, err := findNodesThatPassFilters(ctx, fwk, requirements, nodes)
	if err != nil {
		return nil, err
	}

	return feasibleNodes, nil
}

// findNodesThatPassFilters finds the nodes that fit the filter plugins.
func findNodesThatPassFilters(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo) ([]*framework.NodeInfo, error) {

	// Create feasible list with enough space to avoid growing it
	// and allow assigning.
	feasibleNodes := make([]*framework.NodeInfo, len(nodes))

	if !fwk.HasFilterPlugins() {
		return nodes, nil
	}

	errCh := parallelize.NewErrorChannel()
	var feasibleNodesLen int32
	ctx, cancel := context.WithCancel(ctx)
	checkNode := func(i int) {
		// We check the nodes starting from where we left off in the previous scheduling cycle,
		// this is to make sure all nodes have the same chance of being examined across pods.
		nodeInfo := nodes[i]
		status := fwk.RunFilterPlugins(ctx, requirements, nodeInfo).Merge()
		if status.Code() == framework.Error {
			errCh.SendErrorWithCancel(status.AsError(), cancel)
			return
		}
		if status.IsSuccess() {
			length := atomic.AddInt32(&feasibleNodesLen, 1)
			feasibleNodes[length-1] = nodeInfo
		}
	}

	beginCheckNode := time.Now()
	statusCode := framework.Success
	defer func() {
		// We record Filter extension point latency here instead of in framework.go because framework.RunFilterPlugins
		// function is called for each node, whereas we want to have an overall latency for all nodes per scheduling cycle.
		// Note that this latency also includes latency for `addNominatedPods`, which calls framework.RunPreFilterAddPod.
		metrics.FrameworkExtensionPointDuration.WithLabelValues(frameworkruntime.Filter, statusCode.String(), fwk.ProfileName()).Observe(metrics.SinceInSeconds(beginCheckNode))
	}()

	// Stops searching for more nodes once the configured number of feasible nodes
	// are found.
	fwk.Parallelizer().Until(ctx, len(nodes), checkNode)

	feasibleNodes = feasibleNodes[:feasibleNodesLen]
	if err := errCh.ReceiveError(); err != nil {
		statusCode = framework.Error
		return nil, err
	}
	return feasibleNodes, nil
}

// calculateReplicas calculate max accept replicas in each node  based on the framework calculate plugins.
func calculateReplicas(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo,
) (framework.NodeScoreList, error) {
	availableList := make(framework.NodeScoreList, len(nodes))
	for i := range nodes {
		availableList[i] = framework.NodeScore{
			Name: klog.KObj(nodes[i].Node()).String(),
		}
	}

	// Initialize.
	for i := range availableList {
		availableList[i].MaxAvailableReplicas = -1
	}

	if !fwk.HasCalculatePlugins() {
		return nil, fmt.Errorf("no calculate plugin is registry")
	}

	// Run PreCalculate plugins.
	preCalculateStatus := fwk.RunPreCalculatePlugins(ctx, requirements, nodes)
	if !preCalculateStatus.IsSuccess() {
		return nil, preCalculateStatus.AsError()
	}

	// Run the Calculate plugins.
	availableList, calculate := fwk.RunCalculatePlugins(ctx, requirements, nodes, availableList)
	if !calculate.IsSuccess() {
		return nil, calculate.AsError()
	}

	if klog.V(10).Enabled() {
		for i := range availableList {
			klog.V(10).InfoS("calculate node's max available replicas", "nodes", availableList[i].Name, "max available replicas", availableList[i].MaxAvailableReplicas)
		}
	}
	return availableList, nil
}

// prioritizeNodes prioritizes the nodes by running the score plugins,
// which return a score for each node from the call to RunScorePlugins().
// The scores from each plugin are added together to make the score for that node, then
// any extenders are run as well.
// All scores are finally combined (added) to get the total weighted scores of all nodes
func prioritizeNodes(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*corev1.Node,
	result framework.NodeScoreList,
) (framework.NodeScoreList, error) {
	// If no priority configs are provided, then all nodes will have a score of one.
	// This is required to generate the priority list in the required format
	if !fwk.HasScorePlugins() {
		for i := range result {
			result[i].Score = 1
		}
		return result, nil
	}

	// Run PreScore plugins.
	preScoreStatus := fwk.RunPreScorePlugins(ctx, requirements, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	// Run the Score plugins.
	scoresMap, scoreStatus := fwk.RunScorePlugins(ctx, requirements, nodes)
	if !scoreStatus.IsSuccess() {
		return nil, scoreStatus.AsError()
	}

	if klog.V(10).Enabled() {
		for plugin, nodeScoreList := range scoresMap {
			for _, nodeScore := range nodeScoreList {
				klog.V(10).InfoS("Plugin scored node", "plugin", plugin, "node", nodeScore.Name, "score", nodeScore.Score)
			}
		}
	}

	// Summarize all scores.

	for i := range nodes {
		for j := range scoresMap {
			result[i].Score += scoresMap[j][i].Score
		}
	}

	if klog.V(10).Enabled() {
		for i := range result {
			klog.V(10).InfoS("Calculated node's final score", "node", result[i].Name, "score", result[i].Score)
		}
	}
	return result, nil
}

// aggregateReplicas aggregate the  max accept replicas in all selected nodes based on the framework aggregate plugins.
func aggregateReplicas(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	result framework.NodeScoreList,
) (framework.AcceptableReplicas, error) {
	if !fwk.HasAggregatePlugins() {
		return nil, fmt.Errorf("no calculate plugin is registry")
	}

	// Run PreAggregate plugins.
	preAggregateStatus := fwk.RunPreAggregatePlugins(ctx, requirements, result)
	if !preAggregateStatus.IsSuccess() {
		return nil, preAggregateStatus.AsError()
	}

	// Run the Aggregate plugins.
	acceptableReplicas, aggregateStatus := fwk.RunAggregatePlugins(ctx, requirements, result)
	if !aggregateStatus.IsSuccess() {
		return nil, aggregateStatus.AsError()
	}

	return acceptableReplicas, nil
}
