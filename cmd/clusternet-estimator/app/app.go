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

package app

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/estimator"
	_ "github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/version"
)

var (
	// the command name
	cmdName = "clusternet-estimator"
)

// NewClusternetEstimatorCmd creates a *cobra.Command object with default parameters
func NewClusternetEstimatorCmd(ctx context.Context) *cobra.Command {
	opts, err := NewOptions()
	if err != nil {
		klog.Fatalf("unable to initialize command options: %v", err)
	}

	cmd := &cobra.Command{
		Use:  cmdName,
		Long: `Running in child cluster, responsible for replicas estimator, etc`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := version.PrintAndExitIfRequested(cmdName); err != nil {
				klog.Exit(err)
			}

			if err := opts.Complete(); err != nil {
				klog.Exit(err)
			}
			if err := opts.Validate(); err != nil {
				klog.Exit(err)
			}

			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				klog.V(1).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
			})

			agentCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			estimator, err := estimator.NewEstimatorServer(agentCtx, opts.ControllerOptions)
			if err != nil {
				klog.Exit(err)
			}
			if err = estimator.Run(); err != nil {
				klog.Exit(err)
			}

		},
	}

	version.AddVersionFlag(cmd.Flags())
	opts.AddFlags(cmd.Flags())
	utilfeature.DefaultMutableFeatureGate.AddFlag(cmd.Flags())

	return cmd
}
