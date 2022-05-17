package defaultcalculate

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/estimator/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/estimator/framework/plugins/names"
)

// ResourceCalculator calculate replicas according to the resource.
type ResourceCalculator struct {
	handle framework.Handle
}

var _ framework.CalculatePlugin = &ResourceCalculator{}

// New creates a DefaultAssigner.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &ResourceCalculator{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *ResourceCalculator) Name() string {
	return names.ResourceCalculator
}

// Calculate calculate acceptable replicas for each filtered node
func (pl *ResourceCalculator) Calculate(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeInfo *framework.NodeInfo) (int32, *framework.Status) {
	klog.V(3).InfoS("Attempting to calculate replicas in nodes", nodeInfo.Node().Name)

	return 0, nil
}
