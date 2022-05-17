/*
Copyright 2021 The Clusternet Authors.

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

package defaultaggregate

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/estimator/framework/interfaces"
	names2 "github.com/clusternet/clusternet/pkg/estimator/framework/plugins/names"
)

// DefaultAggregator aggregate replicas all.
type DefaultAggregator struct {
	handle framework.Handle
}

var _ framework.AggregatePlugin = &DefaultAggregator{}

// New creates a DefaultAssigner.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &DefaultAggregator{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *DefaultAggregator) Name() string {
	return names2.DefaultAggregator
}

// Aggregate aggregate acceptable replicas for all filtered node
func (pl *DefaultAggregator) Aggregate(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores framework.NodeScoreList) (framework.AcceptableReplicas, *framework.Status) {
	klog.V(8).InfoS("Attempting to Aggregate replicas")
	result := make(framework.AcceptableReplicas)
	maxAcceptableReplicas := int32(0)
	for i := range scores {
		maxAcceptableReplicas += scores[i].MaxAvailableReplicas
	}
	result[framework.DefaultAcceptableReplicasKey] = maxAcceptableReplicas

	return result, nil
}
