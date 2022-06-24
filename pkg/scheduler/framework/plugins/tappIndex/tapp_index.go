package tappIndex

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
	"github.com/clusternet/clusternet/pkg/utils"
)

// TAppIndex binds subscriptions to clusters using a clusternet client.
type TAppIndex struct {
	handle framework.Handle
}

var _ framework.PreBindPlugin = &TAppIndex{}

// New creates a DefaultBinder.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &TAppIndex{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *TAppIndex) Name() string {
	return names.TAppIndex
}

// PreBind binds tappIndex of clusters with subscriptions
func (pl *TAppIndex) PreBind(ctx context.Context, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return nil
	}

	indexSetMap := generateIndex(sub, targetClusters)
	if len(indexSetMap) != 0 {
		subCopy := sub.DeepCopy()
		indexSetMapStr, err := json.Marshal(indexSetMap)
		if err != nil {
			return framework.AsStatus(err)
		}
		subCopy.Labels[utils.TAppIndexLabel] = string(indexSetMapStr)
		_, err = pl.handle.ClientSet().AppsV1alpha1().Subscriptions(sub.Namespace).Update(ctx, subCopy, metav1.UpdateOptions{})
		if err != nil {
			return framework.AsStatus(err)
		}
	}

	return nil

}

func generateIndex(sub *appsapi.Subscription, targetClusters framework.TargetClusters) map[string]map[string][]utils.TAppIndexList {
	var indexSetMap map[string]map[string][]utils.TAppIndexList
	for _, feedOrder := range sub.Spec.Feeds {
		if feedOrder.APIVersion == utils.TAppAPIVersion && feedOrder.Kind == utils.TAppKind {
			feedKey := utils.GetFeedKey(feedOrder)
			replicas, ok := targetClusters.Replicas[feedKey]
			if !ok {
				continue
			}
			if len(replicas) == 0 {
				continue
			}
			var indexSet map[string][]utils.TAppIndexList
			assignedIndex := int32(0)
			for clusterIndex, namespacedName := range targetClusters.BindingClusters {
				if len(replicas) < clusterIndex {
					break
				}
				value := replicas[clusterIndex]
				indexList := make([]utils.TAppIndexList, 1)
				indexList[0].Start = assignedIndex
				indexList[0].End = assignedIndex + value
				indexSet[namespacedName] = indexList
				assignedIndex += value
			}
			indexSetMap[feedKey] = indexSet
		}
	}

	return indexSetMap
}
