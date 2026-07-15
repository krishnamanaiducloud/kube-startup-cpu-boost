// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webhook

import (
	"context"
	"fmt"

	autoscaling "github.com/google/kube-startup-cpu-boost/api/v1alpha1"
	"github.com/google/kube-startup-cpu-boost/internal/boost"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodBoostResolver resolves the boost policy to apply to an incoming Pod.
// Implementations must be safe to use on non-leader webhook replicas.
type PodBoostResolver interface {
	Resolve(context.Context, *corev1.Pod) (boost.StartupCPUBoost, bool, error)
}

type apiPodBoostResolver struct {
	client client.Client
}

// NewAPIPodBoostResolver creates a resolver backed by the controller-runtime
// client. Reads use the manager's shared, synchronized cache by default, so
// every webhook replica has the same source of truth.
func NewAPIPodBoostResolver(k8sClient client.Client) PodBoostResolver {
	return &apiPodBoostResolver{client: k8sClient}
}

func (r *apiPodBoostResolver) Resolve(ctx context.Context,
	pod *corev1.Pod) (boost.StartupCPUBoost, bool, error) {
	var boostList autoscaling.StartupCPUBoostList
	if err := r.client.List(ctx, &boostList, client.InNamespace(pod.Namespace)); err != nil {
		return nil, false, fmt.Errorf("listing StartupCPUBoost resources in namespace %q: %w",
			pod.Namespace, err)
	}

	candidates := make([]boost.StartupCPUBoost, 0, len(boostList.Items))
	for i := range boostList.Items {
		candidate, err := boost.NewStartupCPUBoost(r.client, &boostList.Items[i], false)
		if err != nil {
			return nil, false, fmt.Errorf("building StartupCPUBoost %s/%s: %w",
				boostList.Items[i].Namespace, boostList.Items[i].Name, err)
		}
		candidates = append(candidates, candidate)
	}

	selected, found := boost.SelectCPUBoostForPod(pod, candidates)
	return selected, found, nil
}

type managerPodBoostResolver struct {
	manager boost.Manager
}

func (r *managerPodBoostResolver) Resolve(ctx context.Context,
	pod *corev1.Pod) (boost.StartupCPUBoost, bool, error) {
	selected, found := r.manager.GetCPUBoostForPod(ctx, pod)
	return selected, found, nil
}
