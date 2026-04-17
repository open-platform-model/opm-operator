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
	fluxssa "github.com/fluxcd/pkg/ssa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/open-platform-model/poc-controller/internal/apply"
)

var _ = Describe("DetectDrift", func() {
	var rm *fluxssa.ResourceManager

	BeforeEach(func() {
		rm = apply.NewResourceManager(k8sClient, "test-owner")
	})

	Context("When cluster state matches desired state", func() {
		It("should return empty drift result", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("drift-no-drift-cm", map[string]string{"key": "value"}),
			}

			By("applying resources to establish desired state")
			_, err := apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())

			By("running drift detection")
			result, err := apply.DetectDrift(ctx, rm, resources)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Drifted).To(BeFalse())
			Expect(result.Resources).To(BeEmpty())
		})
	})

	Context("When a resource has been modified on the cluster", func() {
		It("should identify the drifted resource", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("drift-modified-cm", map[string]string{"key": "original"}),
			}

			By("applying resources to establish desired state")
			_, err := apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())

			By("modifying the resource directly on the cluster")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default", Name: "drift-modified-cm",
			}, cm)).To(Succeed())
			cm.Data["key"] = "modified-by-hand"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("running drift detection")
			result, err := apply.DetectDrift(ctx, rm, resources)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Drifted).To(BeTrue())
			Expect(result.Resources).To(HaveLen(1))
			Expect(result.Resources[0].Kind).To(Equal("ConfigMap"))
			Expect(result.Resources[0].Name).To(Equal("drift-modified-cm"))
			Expect(result.Resources[0].Namespace).To(Equal("default"))
		})
	})

	Context("When resource does not yet exist on cluster", func() {
		It("should not report drift for new resources", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("drift-new-cm", map[string]string{"key": "value"}),
			}

			By("running drift detection without prior apply")
			result, err := apply.DetectDrift(ctx, rm, resources)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Drifted).To(BeFalse())
			Expect(result.Resources).To(BeEmpty())
		})
	})

	Context("When multiple resources have mixed drift", func() {
		It("should identify only the drifted resources", func() {
			resources := []*unstructured.Unstructured{
				newUnstructuredConfigMap("drift-mixed-ok", map[string]string{"key": "stable"}),
				newUnstructuredConfigMap("drift-mixed-changed", map[string]string{"key": "original"}),
			}

			By("applying resources to establish desired state")
			_, err := apply.Apply(ctx, rm, resources, false)
			Expect(err).NotTo(HaveOccurred())

			By("modifying only one resource on the cluster")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default", Name: "drift-mixed-changed",
			}, cm)).To(Succeed())
			cm.Data["key"] = "drifted"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("running drift detection")
			result, err := apply.DetectDrift(ctx, rm, resources)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Drifted).To(BeTrue())
			Expect(result.Resources).To(HaveLen(1))
			Expect(result.Resources[0].Name).To(Equal("drift-mixed-changed"))
		})
	})
})
