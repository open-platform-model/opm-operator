/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package reconcile

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// deletionTestScheme registers types needed by handleDeletion and
// handleModulePackageDeletion tests. Kept narrow to the resources the fake client
// actually serves (ModuleInstance, ModulePackage, ServiceAccount).
func deletionTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := releasesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add releasesv1alpha1: %v", err)
	}
	return s
}

const deletionTestNamespace = "team-a"

// deletingMR returns a ModuleInstance fixture with finalizer + DeletionTimestamp
// set so handleDeletion exercises the deletion branch. Optional SA name,
// annotation value, and an inventory with a single entry simulate a release
// stuck on cleanup. Name + namespace are fixed because tests look up the
// object by them.
func deletingMR(saName, orphanAnnotation string) *releasesv1alpha1.ModuleInstance {
	now := metav1.Now()
	mr := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "mr",
			Namespace:         deletionTestNamespace,
			Finalizers:        []string{FinalizerName},
			DeletionTimestamp: &now,
		},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Prune:              true,
			ServiceAccountName: saName,
		},
		Status: releasesv1alpha1.ModuleInstanceStatus{
			Inventory: &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Group: "", Version: "v1", Kind: "ConfigMap", Namespace: deletionTestNamespace, Name: "cm-1"},
				},
			},
		},
	}
	if orphanAnnotation != "" {
		mr.Annotations = map[string]string{releasesv1alpha1.AnnotationForceDeleteOrphan: orphanAnnotation}
	}
	return mr
}

func deletingModulePackage(saName, orphanAnnotation string) *releasesv1alpha1.ModulePackage {
	now := metav1.Now()
	rel := &releasesv1alpha1.ModulePackage{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "rel",
			Namespace:         deletionTestNamespace,
			Finalizers:        []string{FinalizerName},
			DeletionTimestamp: &now,
		},
		Spec: releasesv1alpha1.ModulePackageSpec{
			Prune:              true,
			ServiceAccountName: saName,
		},
		Status: releasesv1alpha1.ModulePackageStatus{
			Inventory: &releasesv1alpha1.Inventory{
				Revision: 1,
				Count:    1,
				Entries: []releasesv1alpha1.InventoryEntry{
					{Group: "", Version: "v1", Kind: "ConfigMap", Namespace: deletionTestNamespace, Name: "cm-1"},
				},
			},
		},
	}
	if orphanAnnotation != "" {
		rel.Annotations = map[string]string{releasesv1alpha1.AnnotationForceDeleteOrphan: orphanAnnotation}
	}
	return rel
}

// drainEvents collects every Event sent to a FakeRecorder's channel. The
// events API FakeRecorder exposes its buffered channel as .Events; we read
// until empty so each test can assert the exact sequence it produced.
func drainEvents(rec *events.FakeRecorder) []string {
	out := []string{}
	for {
		select {
		case e := <-rec.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func countEventsWithReason(es []string, reason string) int {
	n := 0
	for _, e := range es {
		if strings.Contains(e, reason) {
			n++
		}
	}
	return n
}

// TestHandleDeletion_SAMissing_NoAnnotation covers Task 5.1: when the
// impersonation SA is gone and no orphan annotation is set, handleDeletion
// stalls with DeletionSAMissingReason, emits the Warning event, retains the
// finalizer, and leaves inventory untouched so recovery can still prune.
func TestHandleDeletion_SAMissing_NoAnnotation(t *testing.T) {
	scheme := deletionTestScheme(t)
	mr := deletingMR("missing-sa", "")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	rec := events.NewFakeRecorder(4)

	params := &ModuleInstanceParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	result, err := handleDeletion(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("handleDeletion returned error: %v", err)
	}
	if result.RequeueAfter != StalledRecheckInterval {
		t.Fatalf("RequeueAfter = %v, want %v", result.RequeueAfter, StalledRecheckInterval)
	}

	var updated releasesv1alpha1.ModuleInstance
	if err := c.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &updated); err != nil {
		t.Fatalf("get MR: %v", err)
	}

	ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
	if ready == nil {
		t.Fatal("Ready condition missing")
	}
	if ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready = %s, want False", ready.Status)
	}
	if ready.Reason != status.DeletionSAMissingReason {
		t.Fatalf("Ready.Reason = %q, want %q", ready.Reason, status.DeletionSAMissingReason)
	}
	if !strings.Contains(ready.Message, "team-a/missing-sa") {
		t.Fatalf("Ready.Message %q should name team-a/missing-sa", ready.Message)
	}
	if !strings.Contains(ready.Message, releasesv1alpha1.AnnotationForceDeleteOrphan) {
		t.Fatalf("Ready.Message %q should mention orphan annotation key", ready.Message)
	}

	if !hasCleanupFinalizer(updated.Finalizers) {
		t.Fatal("finalizer missing on stalled MR; must be retained until recovery")
	}
	if updated.Status.Inventory == nil || updated.Status.Inventory.Count != 1 {
		t.Fatal("inventory must remain untouched while stalled on DeletionSAMissing")
	}

	evs := drainEvents(rec)
	if got := countEventsWithReason(evs, status.DeletionSAMissingReason); got != 1 {
		t.Fatalf("DeletionSAMissing event count = %d, want 1, events=%v", got, evs)
	}
}

// TestHandleDeletion_SAMissing_OrphanAnnotation covers Task 5.2: with the
// orphan annotation set to "true", the finalizer is removed in a single
// reconcile, inventory is cleared in the status patch, and the
// OrphanedOnDeletion Warning event is emitted with the orphan count.
func TestHandleDeletion_SAMissing_OrphanAnnotation(t *testing.T) {
	scheme := deletionTestScheme(t)
	mr := deletingMR("missing-sa", "true")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	rec := events.NewFakeRecorder(4)

	params := &ModuleInstanceParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	result, err := handleDeletion(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("handleDeletion returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %v, want 0 (orphan-exit is terminal)", result.RequeueAfter)
	}

	// Fake client garbage-collects a resource once the last finalizer is
	// dropped while DeletionTimestamp is set. A NotFound here confirms the
	// finalizer was actually removed, which is the observable behavior.
	var updated releasesv1alpha1.ModuleInstance
	err = c.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &updated)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected MR to be gone after orphan-exit, got err=%v, updated=%+v", err, updated)
	}
	// In-memory mr should have been mutated to clear inventory before the
	// finalizer patch; asserts on the in-memory state are the only place we
	// can read the pre-delete status delta after garbage collection.
	if mr.Status.Inventory != nil {
		t.Fatalf("inventory must be cleared on orphan-exit, got %+v", mr.Status.Inventory)
	}

	evs := drainEvents(rec)
	if got := countEventsWithReason(evs, status.OrphanedOnDeletionReason); got != 1 {
		t.Fatalf("OrphanedOnDeletion event count = %d, want 1, events=%v", got, evs)
	}
	joined := strings.Join(evs, "|")
	if !strings.Contains(joined, "Orphaned 1") {
		t.Fatalf("OrphanedOnDeletion event should reference count=1, got %v", evs)
	}
}

// TestHandleDeletion_SAMissing_AnnotationNotTrue covers Task 5.3: only the
// literal string "true" triggers orphan-exit. Any other value (here "yes")
// is treated as absent so the operator cannot accidentally release the
// finalizer by typo or a legacy YAML value.
func TestHandleDeletion_SAMissing_AnnotationNotTrue(t *testing.T) {
	scheme := deletionTestScheme(t)
	mr := deletingMR("missing-sa", "yes")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	rec := events.NewFakeRecorder(4)

	params := &ModuleInstanceParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	result, err := handleDeletion(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("handleDeletion returned error: %v", err)
	}
	if result.RequeueAfter != StalledRecheckInterval {
		t.Fatalf("RequeueAfter = %v, want %v (must stall with non-literal annotation)", result.RequeueAfter, StalledRecheckInterval)
	}

	var updated releasesv1alpha1.ModuleInstance
	if err := c.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &updated); err != nil {
		t.Fatalf("get MR: %v", err)
	}
	if !hasCleanupFinalizer(updated.Finalizers) {
		t.Fatal("finalizer must be retained when annotation is not the literal 'true'")
	}
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
	if ready == nil || ready.Reason != status.DeletionSAMissingReason {
		t.Fatalf("Ready.Reason = %v, want %q", ready, status.DeletionSAMissingReason)
	}
}

// TestHandleDeletion_SAPresent_PruneSucceeds covers Task 5.5: the happy path
// is unchanged — with prune enabled, a valid SA, and inventory entries the
// impersonated client prunes successfully and the finalizer is removed.
func TestHandleDeletion_SAPresent_PruneSucceeds(t *testing.T) {
	scheme := deletionTestScheme(t)
	sa := saFixture("team-a", "deploy-sa")
	mr := deletingMR("deploy-sa", "")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr, sa).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	rec := events.NewFakeRecorder(4)

	// Use nil RestConfig so handleDeletion skips building the impersonated
	// client and goes straight to the controller-client prune path. This
	// keeps the unit test focused on the deletion flow without exercising
	// client.New(*rest.Config) which fails on a stub URL.
	params := &ModuleInstanceParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    nil,
		EventRecorder: rec,
	}

	result, err := handleDeletion(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("handleDeletion returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %v, want 0 (clean deletion)", result.RequeueAfter)
	}

	var updated releasesv1alpha1.ModuleInstance
	err = c.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &updated)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected MR to be gone after clean prune, got err=%v, updated=%+v", err, updated)
	}
}

// TestHandleDeletion_SAPresent_PruneForbidden covers Task 5.4: when the SA
// exists but the impersonation client is denied permissions (Forbidden on
// prune), handleDeletion must stall with the generic ImpersonationFailed
// reason, not DeletionSAMissing. Exercised by intercepting Get on the
// impersonated client with an apiserver Forbidden status error. The orphan
// annotation must be ignored in this branch — it is a SA-missing lever only.
func TestHandleDeletion_SAPresent_PruneForbidden(t *testing.T) {
	scheme := deletionTestScheme(t)
	sa := saFixture("team-a", "deploy-sa")
	// Annotation present but irrelevant: Forbidden on prune must stall
	// with ImpersonationFailedReason regardless of orphan annotation value.
	mr := deletingMR("deploy-sa", "true")

	base := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr, sa).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	// Intercept Get for ConfigMaps (the inventory entry kind) with a
	// Forbidden status error so apply.Prune returns a wrapped Forbidden —
	// the same shape produced by apiserver when impersonation is denied.
	denied := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, key.Name, errors.New("impersonate denied"))
			}
			if u, ok := obj.(interface {
				GroupVersionKind() schema.GroupVersionKind
			}); ok {
				if u.GroupVersionKind().Kind == "ConfigMap" {
					return apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, key.Name, errors.New("impersonate denied"))
				}
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	rec := events.NewFakeRecorder(4)
	// RestConfig nil so the controller's own (intercepted) client is used
	// as the prune client; otherwise NewImpersonatedClient would try to
	// contact the stub host. The Forbidden still arrives via the
	// interceptor, exercising the same error classification.
	params := &ModuleInstanceParams{
		Client:        denied,
		APIReader:     denied,
		RestConfig:    nil,
		EventRecorder: rec,
	}

	result, err := handleDeletion(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("handleDeletion returned error: %v", err)
	}
	if result.RequeueAfter != StalledRecheckInterval {
		t.Fatalf("RequeueAfter = %v, want %v", result.RequeueAfter, StalledRecheckInterval)
	}

	var updated releasesv1alpha1.ModuleInstance
	if err := denied.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &updated); err != nil {
		t.Fatalf("get MR: %v", err)
	}
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
	if ready == nil {
		t.Fatal("Ready condition missing")
	}
	if ready.Reason != status.ImpersonationFailedReason {
		t.Fatalf("Ready.Reason = %q, want %q (orphan annotation must NOT short-circuit Forbidden)", ready.Reason, status.ImpersonationFailedReason)
	}
	if !hasCleanupFinalizer(updated.Finalizers) {
		t.Fatal("finalizer must be retained on Forbidden stall")
	}
}

// TestHandleDeletion_EventDedup covers Task 5.7: if handleDeletion is called
// twice in a row on the same stalled MR, only one DeletionSAMissing event is
// emitted (one per Ready transition, not per reconcile), so a long-stalled
// release does not spam the event feed.
func TestHandleDeletion_EventDedup(t *testing.T) {
	scheme := deletionTestScheme(t)
	mr := deletingMR("missing-sa", "")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(mr).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}).
		Build()
	rec := events.NewFakeRecorder(8)

	params := &ModuleInstanceParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	for i := range 3 {
		var current releasesv1alpha1.ModuleInstance
		if err := c.Get(context.Background(), types.NamespacedName{Name: "mr", Namespace: "team-a"}, &current); err != nil {
			t.Fatalf("get MR (iter %d): %v", i, err)
		}
		if _, err := handleDeletion(context.Background(), params, &current); err != nil {
			t.Fatalf("handleDeletion iter %d: %v", i, err)
		}
	}

	evs := drainEvents(rec)
	if got := countEventsWithReason(evs, status.DeletionSAMissingReason); got != 1 {
		t.Fatalf("DeletionSAMissing emitted %d times across 3 reconciles, want 1; events=%v", got, evs)
	}
}

// TestHandleModulePackageDeletion_SAMissing_NoAnnotation covers Task 5.6 for the
// ModulePackage CR. Deletion-path stall behavior must be symmetric between
// ModuleInstance and ModulePackage.
func TestHandleModulePackageDeletion_SAMissing_NoAnnotation(t *testing.T) {
	scheme := deletionTestScheme(t)
	rel := deletingModulePackage("missing-sa", "")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rel).
		WithStatusSubresource(&releasesv1alpha1.ModulePackage{}).
		Build()
	rec := events.NewFakeRecorder(4)

	params := &ModulePackageParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	result, err := handleModulePackageDeletion(context.Background(), params, rel)
	if err != nil {
		t.Fatalf("handleModulePackageDeletion returned error: %v", err)
	}
	if result.RequeueAfter != StalledRecheckInterval {
		t.Fatalf("RequeueAfter = %v, want %v", result.RequeueAfter, StalledRecheckInterval)
	}

	var updated releasesv1alpha1.ModulePackage
	if err := c.Get(context.Background(), types.NamespacedName{Name: "rel", Namespace: "team-a"}, &updated); err != nil {
		t.Fatalf("get ModulePackage: %v", err)
	}
	ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
	if ready == nil || ready.Reason != status.DeletionSAMissingReason {
		t.Fatalf("Ready = %v, want reason %q", ready, status.DeletionSAMissingReason)
	}
	if !hasCleanupFinalizer(updated.Finalizers) {
		t.Fatal("finalizer must be retained on ModulePackage stalled with DeletionSAMissing")
	}
	evs := drainEvents(rec)
	if got := countEventsWithReason(evs, status.DeletionSAMissingReason); got != 1 {
		t.Fatalf("DeletionSAMissing event count = %d, want 1, events=%v", got, evs)
	}
}

// TestHandleModulePackageDeletion_SAMissing_OrphanAnnotation covers Task 5.6 for
// the orphan-exit path on ModulePackage, mirroring the ModuleInstance assertions.
func TestHandleModulePackageDeletion_SAMissing_OrphanAnnotation(t *testing.T) {
	scheme := deletionTestScheme(t)
	rel := deletingModulePackage("missing-sa", "true")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rel).
		WithStatusSubresource(&releasesv1alpha1.ModulePackage{}).
		Build()
	rec := events.NewFakeRecorder(4)

	params := &ModulePackageParams{
		Client:        c,
		APIReader:     c,
		RestConfig:    &rest.Config{Host: "https://localhost:6443"},
		EventRecorder: rec,
	}

	if _, err := handleModulePackageDeletion(context.Background(), params, rel); err != nil {
		t.Fatalf("handleModulePackageDeletion returned error: %v", err)
	}

	var updated releasesv1alpha1.ModulePackage
	err := c.Get(context.Background(), types.NamespacedName{Name: "rel", Namespace: "team-a"}, &updated)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected ModulePackage to be gone after orphan-exit, got err=%v, updated=%+v", err, updated)
	}
	if rel.Status.Inventory != nil {
		t.Fatalf("ModulePackage inventory must be cleared on orphan-exit, got %+v", rel.Status.Inventory)
	}
	evs := drainEvents(rec)
	if got := countEventsWithReason(evs, status.OrphanedOnDeletionReason); got != 1 {
		t.Fatalf("OrphanedOnDeletion event count = %d, want 1, events=%v", got, evs)
	}
}

// TestReadyAlreadyStalledWith covers the tiny helper directly so the event-
// dedup invariant is cheap to verify independent of the fake client: the
// function returns true only when Ready is False with the target reason.
func TestReadyAlreadyStalledWith(t *testing.T) {
	cases := []struct {
		name  string
		conds []metav1.Condition
		want  bool
	}{
		{"no conditions", nil, false},
		{
			"ready true",
			[]metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionTrue, Reason: "ReconciliationSucceeded"}},
			false,
		},
		{
			"stalled with different reason",
			[]metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: status.ImpersonationFailedReason}},
			false,
		},
		{
			"stalled with target reason",
			[]metav1.Condition{{Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: status.DeletionSAMissingReason}},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readyAlreadyStalledWith(tc.conds, status.DeletionSAMissingReason)
			if got != tc.want {
				t.Fatalf("readyAlreadyStalledWith = %v, want %v", got, tc.want)
			}
		})
	}
}

// hasCleanupFinalizer reports whether the deletion-cleanup finalizer is still
// present on the object's metadata. Narrower than controllerutil so tests
// don't pull the whole controllerutil dep.
func hasCleanupFinalizer(fs []string) bool {
	return slices.Contains(fs, FinalizerName)
}
