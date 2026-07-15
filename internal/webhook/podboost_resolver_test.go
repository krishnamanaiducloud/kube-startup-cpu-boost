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

package webhook_test

import (
	"context"
	"encoding/json"
	"errors"

	autoscaling "github.com/google/kube-startup-cpu-boost/api/v1alpha1"
	"github.com/google/kube-startup-cpu-boost/internal/boost"
	bwebhook "github.com/google/kube-startup-cpu-boost/internal/webhook"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type resolverFunc func(context.Context, *corev1.Pod) (boost.StartupCPUBoost, bool, error)

func (f resolverFunc) Resolve(ctx context.Context,
	pod *corev1.Pod) (boost.StartupCPUBoost, bool, error) {
	return f(ctx, pod)
}

type listErrorClient struct {
	client.Client
	err error
}

func (c *listErrorClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return c.err
}

var _ = Describe("API-backed Pod boost resolver", func() {
	var (
		scheme *runtime.Scheme
		pod    *corev1.Pod
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(autoscaling.AddToScheme(scheme)).To(Succeed())
		pod = oneContainerBurstablePodTemplate.DeepCopy()
		pod.Labels = map[string]string{"app": "demo", "tier": "api"}
	})

	It("resolves from the shared API cache without a populated leader manager", func() {
		apiBoost := resolverBoost("api-boost", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiBoost).Build()
		resolver := bwebhook.NewAPIPodBoostResolver(k8sClient)

		selected, found, err := resolver.Resolve(context.Background(), pod)

		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(selected.Name()).To(Equal("api-boost"))
	})

	It("chooses the most specific matching selector independent of API list order", func() {
		general := resolverBoost("a-general", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		specific := resolverBoost("z-specific", "demo", metav1.LabelSelector{MatchLabels: map[string]string{
			"app": "demo", "tier": "api",
		}}, 130)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(specific, general).Build()

		selected, found, err := bwebhook.NewAPIPodBoostResolver(k8sClient).
			Resolve(context.Background(), pod)

		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(selected.Name()).To(Equal("z-specific"))
	})

	It("breaks equally-specific matching selector ties by name", func() {
		zeta := resolverBoost("zeta", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		alpha := resolverBoost("alpha", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 130)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(zeta, alpha).Build()

		selected, found, err := bwebhook.NewAPIPodBoostResolver(k8sClient).
			Resolve(context.Background(), pod)

		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(selected.Name()).To(Equal("alpha"))
	})

	It("selects the same policy as the elected in-memory manager", func() {
		general := resolverBoost("a-general", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		specific := resolverBoost("z-specific", "demo", metav1.LabelSelector{MatchLabels: map[string]string{
			"app": "demo", "tier": "api",
		}}, 130)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(specific, general).Build()
		manager := boost.NewManager(k8sClient)
		for _, spec := range []*autoscaling.StartupCPUBoost{general, specific} {
			candidate, err := boost.NewStartupCPUBoost(k8sClient, spec, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(manager.AddRegularCPUBoost(context.Background(), candidate)).To(Succeed())
		}

		cachedSelection, cachedFound, err := bwebhook.NewAPIPodBoostResolver(k8sClient).
			Resolve(context.Background(), pod)
		managerSelection, managerFound := manager.GetCPUBoostForPod(context.Background(), pod)

		Expect(err).NotTo(HaveOccurred())
		Expect(cachedFound).To(BeTrue())
		Expect(managerFound).To(BeTrue())
		Expect(cachedSelection.Name()).To(Equal(managerSelection.Name()))
		Expect(cachedSelection.Name()).To(Equal("z-specific"))
	})

	It("only considers boost resources in the incoming Pod namespace", func() {
		otherNamespace := resolverBoost("other", "other",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(otherNamespace).Build()

		selected, found, err := bwebhook.NewAPIPodBoostResolver(k8sClient).
			Resolve(context.Background(), pod)

		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
		Expect(selected).To(BeNil())
	})

	It("returns API list errors to the admission handler", func() {
		sentinel := errors.New("cache unavailable")
		baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		resolver := bwebhook.NewAPIPodBoostResolver(&listErrorClient{Client: baseClient, err: sentinel})

		selected, found, err := resolver.Resolve(context.Background(), pod)

		Expect(err).To(MatchError(ContainSubstring("cache unavailable")))
		Expect(found).To(BeFalse())
		Expect(selected).To(BeNil())
	})

	It("mutates a Pod on a follower using only the cache-backed resolver", func() {
		apiBoost := resolverBoost("api-boost", "demo",
			metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}}, 120)
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(apiBoost).Build()
		resolver := bwebhook.NewAPIPodBoostResolver(k8sClient)
		pod.Namespace = ""
		podJSON, err := json.Marshal(pod)
		Expect(err).NotTo(HaveOccurred())
		hook := bwebhook.NewPodCPUBoostWebHookWithResolver(resolver, clientgoscheme.Scheme,
			false, false)

		response := hook.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "demo",
			Object:    runtime.RawExtension{Raw: podJSON},
		}})

		Expect(response.Allowed).To(BeTrue())
		Expect(response.Patches).To(ContainElement(jsonpatch.Operation{
			Operation: "add",
			Path:      "/metadata/labels/autoscaling.x-k8s.io~1startup-cpu-boost",
			Value:     "api-boost",
		}))
	})

	It("returns an admission error so the configured Ignore failure policy can apply", func() {
		sentinel := errors.New("cache unavailable")
		resolver := resolverFunc(func(context.Context,
			*corev1.Pod) (boost.StartupCPUBoost, bool, error) {
			return nil, false, sentinel
		})
		podJSON, err := json.Marshal(pod)
		Expect(err).NotTo(HaveOccurred())
		hook := bwebhook.NewPodCPUBoostWebHookWithResolver(resolver, clientgoscheme.Scheme,
			false, false)

		response := hook.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: "demo",
			Object:    runtime.RawExtension{Raw: podJSON},
		}})

		Expect(response.Allowed).To(BeFalse())
		Expect(response.Result).NotTo(BeNil())
		Expect(response.Result.Code).To(Equal(int32(500)))
		Expect(response.Result.Message).To(ContainSubstring("cache unavailable"))
	})
})

func resolverBoost(name, namespace string, selector metav1.LabelSelector,
	percentage int64) *autoscaling.StartupCPUBoost {
	return &autoscaling.StartupCPUBoost{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Selector:   selector,
		Spec: autoscaling.StartupCPUBoostSpec{
			ResourcePolicy: autoscaling.ResourcePolicy{ContainerPolicies: []autoscaling.ContainerPolicy{{
				ContainerName:      "container-one",
				PercentageIncrease: &autoscaling.PercentageIncrease{Value: percentage},
			}}},
			DurationPolicy: autoscaling.DurationPolicy{Fixed: &autoscaling.FixedDurationPolicy{
				Unit: autoscaling.FixedDurationPolicyUnitSec, Value: 30,
			}},
		},
	}
}
