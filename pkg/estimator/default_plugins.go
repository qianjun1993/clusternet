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
	estimatorapis "github.com/clusternet/clusternet/pkg/estimator/apis"
	"github.com/clusternet/clusternet/pkg/estimator/framework/plugins/names"
)

// getDefaultPlugins returns the default set of plugins.
func getDefaultPlugins() *estimatorapis.Plugins {
	return &estimatorapis.Plugins{
		PreFilter: estimatorapis.PluginSet{},
		Filter: estimatorapis.PluginSet{
			Enabled: []estimatorapis.Plugin{
				{Name: names.TaintToleration},
			},
		},
		PostFilter: estimatorapis.PluginSet{},
		PreCompute: estimatorapis.PluginSet{},
		Compute: estimatorapis.PluginSet{
			Enabled: []estimatorapis.Plugin{
				{Name: names.DefaultComputer},
			},
		},
		PreScore: estimatorapis.PluginSet{},
		Score: estimatorapis.PluginSet{
			Enabled: []estimatorapis.Plugin{
				{Name: names.TaintToleration, Weight: 3},
			},
		},
		PreAggregate: estimatorapis.PluginSet{},
		Aggregate: estimatorapis.PluginSet{
			Enabled: []estimatorapis.Plugin{
				{Name: names.DefaultAggregator},
			},
		},
	}
}
