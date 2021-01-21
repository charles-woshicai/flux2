/*
Copyright 2020 The Flux authors

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

package main

import (
	"context"
	"fmt"

	"github.com/fluxcd/flux2/internal/utils"
	"github.com/fluxcd/pkg/apis/meta"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta1"
	"github.com/spf13/cobra"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var resumeKsCmd = &cobra.Command{
	Use:     "kustomization [name]",
	Aliases: []string{"ks"},
	Short:   "Resume a suspended Kustomization",
	Long: `The resume command marks a previously suspended Kustomization resource for reconciliation and waits for it to
finish the apply.`,
	Example: `  # Resume reconciliation for an existing Kustomization
  flux resume ks podinfo
`,
	RunE: resumeKsCmdRun,
}

func init() {
	resumeCmd.AddCommand(resumeKsCmd)
}

func resumeKsCmdRun(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("Kustomization name is required")
	}
	name := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), rootArgs.timeout)
	defer cancel()

	kubeClient, err := utils.KubeClient(rootArgs.kubeconfig, rootArgs.kubecontext)
	if err != nil {
		return err
	}

	namespacedName := types.NamespacedName{
		Namespace: rootArgs.namespace,
		Name:      name,
	}
	var kustomization kustomizev1.Kustomization
	err = kubeClient.Get(ctx, namespacedName, &kustomization)
	if err != nil {
		return err
	}

	logger.Actionf("resuming Kustomization %s in %s namespace", name, rootArgs.namespace)
	kustomization.Spec.Suspend = false
	if err := kubeClient.Update(ctx, &kustomization); err != nil {
		return err
	}
	logger.Successf("Kustomization resumed")

	logger.Waitingf("waiting for Kustomization reconciliation")
	if err := wait.PollImmediate(rootArgs.pollInterval, rootArgs.timeout,
		isKustomizationResumed(ctx, kubeClient, namespacedName, &kustomization)); err != nil {
		return err
	}
	logger.Successf("Kustomization reconciliation completed")

	logger.Successf("applied revision %s", kustomization.Status.LastAppliedRevision)
	return nil
}

func isKustomizationResumed(ctx context.Context, kubeClient client.Client,
	namespacedName types.NamespacedName, kustomization *kustomizev1.Kustomization) wait.ConditionFunc {
	return func() (bool, error) {
		err := kubeClient.Get(ctx, namespacedName, kustomization)
		if err != nil {
			return false, err
		}

		// Confirm the state we are observing is for the current generation
		if kustomization.Generation != kustomization.Status.ObservedGeneration {
			return false, nil
		}

		if c := apimeta.FindStatusCondition(kustomization.Status.Conditions, meta.ReadyCondition); c != nil {
			switch c.Status {
			case metav1.ConditionTrue:
				return true, nil
			case metav1.ConditionFalse:
				return false, fmt.Errorf(c.Message)
			}
		}
		return false, nil
	}
}
