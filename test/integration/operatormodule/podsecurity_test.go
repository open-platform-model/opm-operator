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

package operatormodule_test

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The envtest API server enforces Pod Security admission (a default-enabled
// plugin; envtest disables only ServiceAccount admission), so these specs
// prove admission, not field presence.
var _ = Describe("The operator module's pods under Pod Security restricted", Ordered, func() {
	var namespace string

	BeforeAll(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "restricted-",
			Labels:       map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
		}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed()) })
	})

	// podFrom builds a Pod from the rendered Deployment's pod template.
	podFrom := func(objs []obj) *corev1.Pod {
		GinkgoHelper()
		d := find(objs, "Deployment", "opm-operator-controller-manager")
		b, err := json.Marshal(lookup(d.Object, "spec", "template"))
		Expect(err).NotTo(HaveOccurred())
		var tmpl corev1.PodTemplateSpec
		Expect(json.Unmarshal(b, &tmpl)).To(Succeed())
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "manager-", Namespace: namespace, Labels: tmpl.Labels},
			Spec:       tmpl.Spec,
		}
	}

	It("admits the pod rendered with default values", func() {
		Expect(k8sClient.Create(ctx, podFrom(mustRender(moduleDir)), client.DryRunAll)).To(Succeed())
	})

	It("admits the pod rendered with every value set", func() {
		Expect(k8sClient.Create(ctx, podFrom(mustRenderWith(moduleDir, everyValue)), client.DryRunAll)).To(Succeed())
	})

	It("refuses the pod without its pod-level seccomp profile, so enforcement is on", func() {
		pod := podFrom(mustRender(moduleDir))
		Expect(pod.Spec.SecurityContext).NotTo(BeNil())
		Expect(pod.Spec.SecurityContext.SeccompProfile).NotTo(BeNil())
		pod.Spec.SecurityContext.SeccompProfile = nil
		err := k8sClient.Create(ctx, pod, client.DryRunAll)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected a Pod Security refusal, got %v", err)
		Expect(strings.Contains(err.Error(), "seccompProfile")).To(BeTrue(), err.Error())
	})
})
