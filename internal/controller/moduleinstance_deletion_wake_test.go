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
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// wakeNamespace is the namespace of the instances in the trigger specs.
const wakeNamespace = "team-a"

// wakeInstance is a ModuleInstance for the trigger specs; deleting gives it
// the deletion timestamp and the finalizer a Terminating object carries.
func wakeInstance(deleting bool, mutate ...func(*releasesv1alpha1.ModuleInstance)) *releasesv1alpha1.ModuleInstance {
	mi := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "mi", Namespace: wakeNamespace, Generation: 2},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
		},
	}
	if deleting {
		now := metav1.NewTime(time.Unix(1700000000, 0))
		mi.DeletionTimestamp = &now
		mi.Finalizers = []string{opmreconcile.FinalizerName}
	}
	for _, m := range mutate {
		m(mi)
	}
	return mi
}

func withAnnotation(key, value string) func(*releasesv1alpha1.ModuleInstance) {
	return func(mi *releasesv1alpha1.ModuleInstance) {
		if mi.Annotations == nil {
			mi.Annotations = map[string]string{}
		}
		mi.Annotations[key] = value
	}
}

var _ = Describe("ModuleInstance primary watch predicate", func() {
	const orphan = releasesv1alpha1.AnnotationForceDeleteOrphan

	// The predicate the controller installs on For().
	primary := func() predicate.Predicate {
		return predicate.Or(predicate.GenerationChangedPredicate{}, orphanAnnotationSet())
	}

	DescribeTable("passes an update",
		func(old, cur *releasesv1alpha1.ModuleInstance, want bool) {
			Expect(primary().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: cur})).To(Equal(want))
		},
		Entry("when the orphan annotation is set on a deleting instance",
			wakeInstance(true), wakeInstance(true, withAnnotation(orphan, "true")), true),
		Entry("when the orphan annotation moves from another value to true on a deleting instance",
			wakeInstance(true, withAnnotation(orphan, "yes")), wakeInstance(true, withAnnotation(orphan, "true")), true),
		Entry("not when the orphan annotation is set to a value other than true",
			wakeInstance(true), wakeInstance(true, withAnnotation(orphan, "yes")), false),
		Entry("not when the orphan annotation was true already (a status write on the stalled instance)",
			wakeInstance(true, withAnnotation(orphan, "true")), wakeInstance(true, withAnnotation(orphan, "true")), false),
		Entry("not when the orphan annotation is removed",
			wakeInstance(true, withAnnotation(orphan, "true")), wakeInstance(true), false),
		Entry("not when the orphan annotation is set on a live instance",
			wakeInstance(false), wakeInstance(false, withAnnotation(orphan, "true")), false),
		Entry("not when another annotation changes on a deleting instance",
			wakeInstance(true), wakeInstance(true, withAnnotation("example.com/note", "true")), false),
		Entry("when the generation changes, as before",
			wakeInstance(false),
			wakeInstance(false, func(mi *releasesv1alpha1.ModuleInstance) { mi.Generation = 3 }), true),
	)

	It("passes create, delete and generic events", func() {
		mi := wakeInstance(false)
		Expect(primary().Create(event.CreateEvent{Object: mi})).To(BeTrue())
		Expect(primary().Delete(event.DeleteEvent{Object: mi})).To(BeTrue())
		Expect(primary().Generic(event.GenericEvent{Object: mi})).To(BeTrue())
	})

	It("does not pass an update with a missing object", func() {
		Expect(orphanAnnotationSet().Update(event.UpdateEvent{ObjectNew: wakeInstance(true)})).To(BeFalse())
		Expect(orphanAnnotationSet().Update(event.UpdateEvent{ObjectOld: wakeInstance(true)})).To(BeFalse())
	})
})

var _ = Describe("ServiceAccount watch", func() {
	named := func(name string) func(*releasesv1alpha1.ModuleInstance) {
		return func(mi *releasesv1alpha1.ModuleInstance) { mi.Name = name }
	}
	inNamespace := func(ns string) func(*releasesv1alpha1.ModuleInstance) {
		return func(mi *releasesv1alpha1.ModuleInstance) { mi.Namespace = ns }
	}
	impersonating := func(sa string) func(*releasesv1alpha1.ModuleInstance) {
		return func(mi *releasesv1alpha1.ModuleInstance) { mi.Spec.ServiceAccountName = sa }
	}
	cliOwned := func(mi *releasesv1alpha1.ModuleInstance) { mi.Spec.Owner = releasesv1alpha1.OwnerCLI }
	suspended := func(mi *releasesv1alpha1.ModuleInstance) { mi.Spec.Suspend = true }

	// The watch is metadata-only, so the mapper receives this type. Every
	// ServiceAccount of these specs is created in wakeNamespace.
	created := func(name string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: wakeNamespace},
		}
	}
	request := func(name string) reconcile.Request {
		return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: wakeNamespace, Name: name}}
	}

	It("passes only the creation of a ServiceAccount", func() {
		p := serviceAccountCreated()
		sa := created("deploy-sa")
		Expect(p.Create(event.CreateEvent{Object: sa})).To(BeTrue())
		Expect(p.Update(event.UpdateEvent{ObjectOld: sa, ObjectNew: sa})).To(BeFalse())
		Expect(p.Delete(event.DeleteEvent{Object: sa})).To(BeFalse())
		Expect(p.Generic(event.GenericEvent{Object: sa})).To(BeFalse())
	})

	// stalledOn is the Ready condition a reconcile leaves when it cannot act
	// as the instance's ServiceAccount.
	stalledOn := func(reason string) func(*releasesv1alpha1.ModuleInstance) {
		return func(mi *releasesv1alpha1.ModuleInstance) {
			apimeta.SetStatusCondition(&mi.Status.Conditions, metav1.Condition{
				Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: reason, Message: "stalled",
			})
		}
	}
	applyStalled := stalledOn(status.ImpersonationFailedReason)
	deleteStalled := stalledOn(status.DeletionSAMissingReason)
	healthy := func(mi *releasesv1alpha1.ModuleInstance) {
		apimeta.SetStatusCondition(&mi.Status.Conditions, metav1.Condition{
			Type: status.ReadyCondition, Status: metav1.ConditionTrue, Reason: status.ReconciliationSucceededReason, Message: "ok",
		})
	}

	It("enqueues the instances of the namespace that are stalled on the created ServiceAccount", func() {
		c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
			wakeInstance(false, named("live"), impersonating("deploy-sa"), applyStalled),
			wakeInstance(true, named("deleting"), impersonating("deploy-sa"), deleteStalled),
			wakeInstance(true, named("deleting-forbidden"), impersonating("deploy-sa"), applyStalled),
			wakeInstance(true, named("deleting-suspended"), impersonating("deploy-sa"), suspended, deleteStalled),
			wakeInstance(false, named("live-suspended"), impersonating("deploy-sa"), suspended, applyStalled),
			wakeInstance(false, named("cli-owned"), impersonating("deploy-sa"), cliOwned, applyStalled),
			wakeInstance(true, named("cli-owned-deleting"), impersonating("deploy-sa"), cliOwned, deleteStalled),
			wakeInstance(false, named("other-sa"), impersonating("other-sa"), applyStalled),
			wakeInstance(false, named("no-sa"), applyStalled),
			wakeInstance(false, named("other-namespace"), inNamespace("team-b"), impersonating("deploy-sa"), applyStalled),
		).Build()
		r := &ModuleInstanceReconciler{Client: c, Scheme: scheme.Scheme}

		Expect(r.mapServiceAccountToModuleInstances(context.Background(), created("deploy-sa"))).To(ConsistOf(
			request("live"),
			request("deleting"),
			request("deleting-forbidden"),
			request("deleting-suspended"),
		))
		Expect(r.mapServiceAccountToModuleInstances(context.Background(), created("unused-sa"))).To(BeEmpty())
	})

	// A mapped request skips the rate limiter and a live instance renders
	// before it reads its ServiceAccount: a create must cost nothing unless
	// an instance waits for it.
	It("enqueues nothing for an instance that is not stalled on its ServiceAccount", func() {
		c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
			wakeInstance(false, named("healthy"), impersonating("deploy-sa"), healthy),
			wakeInstance(false, named("never-reconciled"), impersonating("deploy-sa")),
			wakeInstance(false, named("render-failed"), impersonating("deploy-sa"), stalledOn(status.RenderFailedReason)),
			wakeInstance(true, named("deleting-not-stalled"), impersonating("deploy-sa"), healthy),
			wakeInstance(false, named("stalled"), impersonating("deploy-sa"), applyStalled),
		).Build()
		r := &ModuleInstanceReconciler{Client: c, Scheme: scheme.Scheme}

		Expect(r.mapServiceAccountToModuleInstances(context.Background(), created("deploy-sa"))).To(ConsistOf(
			request("stalled"),
		))
	})

	It("counts the manager's default ServiceAccount as the effective one", func() {
		c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
			wakeInstance(false, named("defaulted"), applyStalled),
			wakeInstance(false, named("explicit"), impersonating("deploy-sa"), applyStalled),
		).Build()
		r := &ModuleInstanceReconciler{Client: c, Scheme: scheme.Scheme, DefaultServiceAccount: "opm-deployer"}

		Expect(r.mapServiceAccountToModuleInstances(context.Background(), created("opm-deployer"))).To(ConsistOf(
			request("defaulted"),
		))
		Expect(r.mapServiceAccountToModuleInstances(context.Background(), created("deploy-sa"))).To(ConsistOf(
			request("explicit"),
		))
	})
})
