/*
Copyright 2026.

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

package apply_test

import (
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

func newUnstructuredConfigMap(name string, data map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})
	obj.SetNamespace("default")
	obj.SetName(name)
	if data != nil {
		dataMap := make(map[string]any, len(data))
		for k, v := range data {
			dataMap[k] = v
		}
		_ = unstructured.SetNestedMap(obj.Object, dataMap, "data")
	}
	return obj
}

var _ = Describe("Apply", func() {
	var rm *fluxssa.ResourceManager

	BeforeEach(func() {
		rm = apply.NewResourceManager(k8sClient, "test-owner")
	})

	Context("When applying ConfigMaps", func() {
		It("should create resources and verify they exist in cluster", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("apply-test-cm1", map[string]string{"key": "value1"}),
				newUnstructuredConfigMap("apply-test-cm2", map[string]string{"key": "value2"}),
			}

			result, err := apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Created).To(Equal(2))
			Expect(result.Updated).To(Equal(0))
			Expect(result.Unchanged).To(Equal(0))

			By("verifying ConfigMaps exist in cluster")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "apply-test-cm1",
			}, cm)).To(Succeed())
			Expect(cm.Data["key"]).To(Equal("value1"))

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "apply-test-cm2",
			}, cm)).To(Succeed())
			Expect(cm.Data["key"]).To(Equal("value2"))
		})
	})

	Context("When re-applying unchanged resources", func() {
		It("should return unchanged counts on idempotent re-apply", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("idempotent-test-cm", map[string]string{"key": "value"}),
			}

			By("applying the first time")
			result, err := apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Created).To(Equal(1))

			By("re-applying the same resources")
			result, err = apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Unchanged).To(Equal(1))
			Expect(result.Created).To(Equal(0))
			Expect(result.Updated).To(Equal(0))
		})
	})

	// NOTE on SSA force-conflicts: Flux's ResourceManager.apply() always uses
	// client.ForceOwnership, so SSA field-ownership conflicts never surface through
	// this layer. The spec scenario "Force conflicts disabled (default)" assumes
	// raw SSA semantics, but Flux abstracts that away. The `force` parameter in
	// ApplyOptions controls immutable field recreation (delete-and-recreate), not
	// SSA ownership. A different field manager can always overwrite fields.
	//
	// Spec divergence: openspec/changes/08-ssa-apply/specs/ssa-apply/spec.md
	//   "Scenario: Force conflicts disabled (default)" — not applicable with Flux SSA.
	Context("When using force-conflicts", func() {
		It("should allow a different field manager to overwrite fields (Flux always forces ownership)", func() {
			cm := newUnstructuredConfigMap("ownership-test-cm", map[string]string{"key": "original"})

			By("applying with the default field manager")
			result, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{cm}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Created).To(Equal(1))

			By("applying the same field with a different field manager and force=false")
			conflictRM := apply.NewResourceManager(k8sClient, "conflict-owner")
			cmUpdated := newUnstructuredConfigMap("ownership-test-cm", map[string]string{"key": "overwritten"})

			result, err = apply.Apply(ctx, conflictRM, []*unstructured.Unstructured{cmUpdated}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Updated).To(Equal(1))

			By("verifying the overwritten value")
			fetched := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "ownership-test-cm",
			}, fetched)).To(Succeed())
			Expect(fetched.Data["key"]).To(Equal("overwritten"))
		})

		It("should take ownership of conflicting fields when force is true", func() {
			cm := newUnstructuredConfigMap("force-test-cm", map[string]string{"key": "original"})

			By("applying with the default field manager")
			result, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{cm}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Created).To(Equal(1))

			By("applying with a different field manager to create a conflict")
			conflictRM := apply.NewResourceManager(k8sClient, "conflict-owner")
			conflictRM.SetOwnerLabels([]*unstructured.Unstructured{cm}, "conflict-owner", "default")

			cmUpdated := newUnstructuredConfigMap("force-test-cm", map[string]string{"key": "conflicting"})

			By("applying with force=true to resolve conflict")
			result, err = apply.Apply(ctx, conflictRM, []*unstructured.Unstructured{cmUpdated}, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Updated).To(Equal(1))

			By("verifying the updated value")
			fetched := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "force-test-cm",
			}, fetched)).To(Succeed())
			Expect(fetched.Data["key"]).To(Equal("conflicting"))
		})
	})

	// Spec reference: openspec/changes/08-ssa-apply/specs/ssa-apply/spec.md
	//   "Scenario: CRD applied before custom resource"
	Context("When applying a CRD and an instance of it together", func() {
		It("should apply the CRD before the custom resource regardless of input order", func() {
			crd := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinition",
				"metadata": map[string]any{
					"name": "widgets.test.example.com",
				},
				"spec": map[string]any{
					"group": "test.example.com",
					"names": map[string]any{
						"plural":   "widgets",
						"singular": "widget",
						"kind":     "Widget",
						"listKind": "WidgetList",
					},
					"scope": "Namespaced",
					"versions": []any{map[string]any{
						"name":    "v1",
						"served":  true,
						"storage": true,
						"schema": map[string]any{
							"openAPIV3Schema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"spec": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"size": map[string]any{"type": "integer"},
										},
									},
								},
							},
						},
					}},
				},
			}}

			widget := &unstructured.Unstructured{}
			widget.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "test.example.com",
				Version: "v1",
				Kind:    "Widget",
			})
			widget.SetNamespace("default")
			widget.SetName("apply-order-widget")
			_ = unstructured.SetNestedField(widget.Object, int64(3), "spec", "size")

			By("applying with the custom resource listed before its CRD")
			result, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{widget, crd}, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Created).To(Equal(2))

			By("verifying the custom resource exists in the cluster")
			fetched := &unstructured.Unstructured{}
			fetched.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "test.example.com",
				Version: "v1",
				Kind:    "Widget",
			})
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Namespace: "default",
					Name:      "apply-order-widget",
				}, fetched)
			}, 10*time.Second, 100*time.Millisecond).Should(Succeed())

			By("cleaning up the custom resource and CRD")
			Expect(k8sClient.Delete(ctx, fetched)).To(Succeed())
			Expect(k8sClient.Delete(ctx, crd)).To(Succeed())
		})
	})
})
