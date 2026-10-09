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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// What a Foreground delete does under envtest, which runs no garbage
// collector, and what the suite's collector helper adds.
var _ = Describe("Foreground delete under envtest", func() {
	newNamespace := func(prefix string) string {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: prefix}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		return ns.Name
	}
	newDeployment := func(namespace string) *appsv1.Deployment {
		lbls := map[string]string{"app": "foreground"}
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "foreground", Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: lbls},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: lbls},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name: "app", Image: "registry.invalid/app:1",
					}}},
				},
			},
		}
	}
	foreground := client.PropagationPolicy(metav1.DeletePropagationForeground)

	It("leaves the object terminating with the foregroundDeletion finalizer while nothing collects it", func() {
		namespace := newNamespace("fg-held-")
		resume := gc.Pause(namespace)
		defer resume()

		deployment := newDeployment(namespace)
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "foreground", Namespace: namespace}}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		Expect(k8sClient.Delete(ctx, deployment, foreground)).To(Succeed())
		Expect(k8sClient.Delete(ctx, configMap, foreground)).To(Succeed())

		// The API server sets the finalizer on every kind, with or without
		// dependents, and nothing in envtest removes it.
		Consistently(func(g Gomega) {
			for _, obj := range []client.Object{deployment, configMap} {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
				g.Expect(obj.GetDeletionTimestamp().IsZero()).To(BeFalse())
				g.Expect(obj.GetFinalizers()).To(ConsistOf(metav1.FinalizerDeleteDependents))
			}
		}, time.Second, 100*time.Millisecond).Should(Succeed())

		// A second Foreground delete of the terminating object is accepted
		// and changes nothing.
		Expect(k8sClient.Delete(ctx, deployment, foreground)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		Expect(deployment.GetFinalizers()).To(ConsistOf(metav1.FinalizerDeleteDependents))

		By("resuming the collector")
		resume()
		Eventually(func() bool {
			depErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
			cmErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
			return apierrors.IsNotFound(depErr) && apierrors.IsNotFound(cmErr)
		}, 10*time.Second, 50*time.Millisecond).Should(BeTrue())
	})

	It("ends in NotFound while the collector helper runs", func() {
		namespace := newNamespace("fg-collected-")
		deployment := newDeployment(namespace)
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

		Expect(k8sClient.Delete(ctx, deployment, foreground)).To(Succeed())

		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(deployment), deployment))
		}, 10*time.Second, 50*time.Millisecond).Should(BeTrue())
	})

	It("keeps a finalizer another writer set", func() {
		namespace := newNamespace("fg-foreign-")
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "held", Namespace: namespace, Finalizers: []string{"example.com/hold"},
		}}
		Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

		Expect(k8sClient.Delete(ctx, configMap, foreground)).To(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).To(Succeed())
			g.Expect(configMap.GetFinalizers()).To(ConsistOf("example.com/hold"))
		}, 10*time.Second, 50*time.Millisecond).Should(Succeed())

		configMap.SetFinalizers(nil)
		Expect(k8sClient.Update(ctx, configMap)).To(Succeed())
	})
})
