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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// These tests exercise the Platform CRD's API-server-level validation
// (singleton CEL rule, cluster scope, required fields). No controller
// reconciles Platform in this slice, so they assert apply-time behavior only.
var _ = Describe("Platform CRD", func() {
	AfterEach(func() {
		// Clean up any Platform left behind; ignore not-found.
		platforms := &releasesv1alpha1.PlatformList{}
		Expect(k8sClient.List(ctx, platforms)).To(Succeed())
		for i := range platforms.Items {
			Expect(client.IgnoreNotFound(
				k8sClient.Delete(ctx, &platforms.Items[i]),
			)).To(Succeed())
		}
	})

	Context("singleton enforcement", func() {
		It("accepts a Platform named cluster", func() {
			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/opm@v2": {
							Version: "2.0.0-alpha.3",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, platform)).To(Succeed())
			// Cluster-scoped: object carries no namespace.
			Expect(platform.Namespace).To(BeEmpty())
		})

		It("rejects a Platform with any other name via the CEL rule", func() {
			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
				Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
			}
			err := k8sClient.Create(ctx, platform)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("singleton"))
		})
	})

	Context("spec projection of core #Platform", func() {
		It("accepts a minimal spec with type and a versioned registry entry, no enable", func() {
			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/opm@v2": {Version: "2.0.0-alpha.3"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, platform)).To(Succeed())

			// The omitted enable round-trips as nil (deferred to schema default).
			fetched := &releasesv1alpha1.Platform{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(platform), fetched)).To(Succeed())
			Expect(fetched.Spec.Registry["opmodel.dev/catalogs/opm@v2"].Enable).To(BeNil())
		})

		It("rejects a registry entry missing the required version at admission", func() {
			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: releasesv1alpha1.PlatformSpec{
					Type: "kubernetes",
					Registry: map[string]releasesv1alpha1.Subscription{
						"opmodel.dev/catalogs/opm@v2": {},
					},
				},
			}
			err := k8sClient.Create(ctx, platform)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version"))
		})

		It("prunes a legacy filter field at admission so it never reaches the reconciler", func() {
			// The reshaped structural schema no longer declares filter, so the
			// API server prunes it on write. Applied as unstructured because
			// the Go type cannot express the retired field.
			platform := &unstructured.Unstructured{}
			platform.SetGroupVersionKind(releasesv1alpha1.GroupVersion.WithKind("Platform"))
			platform.SetName("cluster")
			Expect(unstructured.SetNestedField(platform.Object, "kubernetes", "spec", "type")).To(Succeed())
			Expect(unstructured.SetNestedMap(platform.Object,
				map[string]any{
					"version": "2.0.0-alpha.3",
					"filter":  map[string]any{"range": ">=1.0.0-alpha"},
				},
				"spec", "registry", "opmodel.dev/catalogs/opm@v2")).To(Succeed())
			Expect(k8sClient.Create(ctx, platform)).To(Succeed())

			fetched := &unstructured.Unstructured{}
			fetched.SetGroupVersionKind(releasesv1alpha1.GroupVersion.WithKind("Platform"))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(platform), fetched)).To(Succeed())
			sub, found, err := unstructured.NestedMap(fetched.Object,
				"spec", "registry", "opmodel.dev/catalogs/opm@v2")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(sub).To(HaveKeyWithValue("version", "2.0.0-alpha.3"))
			Expect(sub).NotTo(HaveKey("filter"), "the structural schema must prune the retired filter field")
		})

		It("rejects a spec missing the required type", func() {
			platform := &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       releasesv1alpha1.PlatformSpec{},
			}
			err := k8sClient.Create(ctx, platform)
			Expect(err).To(HaveOccurred())
		})
	})
})
