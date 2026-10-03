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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// watchedPlatform is a generated, Ready cluster Platform carrying every field
// the Platform watch predicate reads, so each spec moves exactly one of them.
func watchedPlatform() *releasesv1alpha1.Platform {
	p := &releasesv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName, Generation: 3},
		Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
		Status: releasesv1alpha1.PlatformStatus{
			ObservedGeneration: 3,
			PackageIdentity:    "g3",
			OperatorVersion:    "v1.0.0-beta.1",
			Registry: []releasesv1alpha1.ResolvedRegistryEntry{
				{Catalog: "opmodel.dev/catalogs/opm@v1", Version: "4.3.0"},
			},
		},
	}
	setPlatformCondition(p, status.ReadyCondition, metav1.ConditionTrue, status.GeneratedReason, "generated")
	setPlatformCondition(p, status.ContractsFulfilledCondition, metav1.ConditionTrue, status.ContractsFulfilledReason, "every contract has a provider")
	return p
}

func setPlatformCondition(p *releasesv1alpha1.Platform, condType string, s metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type: condType, Status: s, Reason: reason, Message: msg,
	})
}

var _ = Describe("Platform watch predicate", func() {
	p := platformConsumedFieldsChanged()

	update := func(mutate func(*releasesv1alpha1.Platform)) bool {
		old := watchedPlatform()
		cur := old.DeepCopy()
		mutate(cur)
		return p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: cur})
	}
	transition := func(from, to func(*releasesv1alpha1.Platform)) bool {
		old := watchedPlatform()
		from(old)
		cur := old.DeepCopy()
		to(cur)
		return p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: cur})
	}
	ready := func(s metav1.ConditionStatus, reason string) func(*releasesv1alpha1.Platform) {
		return func(p *releasesv1alpha1.Platform) {
			setPlatformCondition(p, status.ReadyCondition, s, reason, "reason "+reason)
		}
	}
	noReady := func(p *releasesv1alpha1.Platform) {
		apimeta.RemoveStatusCondition(&p.Status.Conditions, status.ReadyCondition)
	}

	DescribeTable("passes an update that moves a field instances consume",
		func(from, to func(*releasesv1alpha1.Platform)) {
			Expect(transition(from, to)).To(BeTrue())
		},
		Entry("Ready absent to True/Generated", noReady, ready(metav1.ConditionTrue, status.GeneratedReason)),
		Entry("Ready False/BuildFailed to True/Generated",
			ready(metav1.ConditionFalse, status.BuildFailedReason), ready(metav1.ConditionTrue, status.GeneratedReason)),
		Entry("Ready True to False/BuildFailed",
			ready(metav1.ConditionTrue, status.GeneratedReason), ready(metav1.ConditionFalse, status.BuildFailedReason)),
		Entry("packageIdentity changed", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) { p.Status.PackageIdentity = "g3+claims" }),
		Entry("a registry entry's version changed", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) { p.Status.Registry[0].Version = "4.3.1" }),
		Entry("skewPolicy nil to Refuse", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) {
				refuse := releasesv1alpha1.SkewPolicyRefuse
				p.Spec.SkewPolicy = &refuse
			}),
		Entry("generation bumped", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) { p.Generation++ }),
		Entry("observedGeneration bumped", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) { p.Status.ObservedGeneration++ }),
		Entry("operatorVersion changed (the upgrade recovery edge)", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) { p.Status.OperatorVersion = "v1.0.0-beta.2" }),
	)

	DescribeTable("drops an update that changes nothing instances consume",
		func(from, to func(*releasesv1alpha1.Platform)) {
			Expect(transition(from, to)).To(BeFalse())
		},
		Entry("Ready message only", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) {
				setPlatformCondition(p, status.ReadyCondition, metav1.ConditionTrue, status.GeneratedReason, "another message")
			}),
		Entry("Ready False reason only",
			ready(metav1.ConditionFalse, status.BuildFailedReason), ready(metav1.ConditionFalse, status.GenerateFailedReason)),
		Entry("Ready lastTransitionTime only", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) {
				c := apimeta.FindStatusCondition(p.Status.Conditions, status.ReadyCondition)
				c.LastTransitionTime = metav1.NewTime(c.LastTransitionTime.Add(42))
			}),
		Entry("ContractsFulfilled only", func(*releasesv1alpha1.Platform) {},
			func(p *releasesv1alpha1.Platform) {
				setPlatformCondition(p, status.ContractsFulfilledCondition, metav1.ConditionFalse, status.UnfulfilledContractsReason, "one contract has no provider")
			}),
		Entry("identical objects", func(*releasesv1alpha1.Platform) {}, func(*releasesv1alpha1.Platform) {}),
	)

	It("passes create, delete and generic events", func() {
		Expect(p.Create(event.CreateEvent{Object: watchedPlatform()})).To(BeTrue())
		Expect(p.Delete(event.DeleteEvent{Object: watchedPlatform()})).To(BeTrue())
		Expect(p.Generic(event.GenericEvent{Object: watchedPlatform()})).To(BeTrue())
	})

	It("passes an update whose objects are not Platforms (fails open)", func() {
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: &releasesv1alpha1.ModuleInstance{},
			ObjectNew: &releasesv1alpha1.ModuleInstance{},
		})).To(BeTrue())
		Expect(update(func(*releasesv1alpha1.Platform) {})).To(BeFalse())
	})
})
