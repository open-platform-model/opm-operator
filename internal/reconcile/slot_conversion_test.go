package reconcile

import (
	"context"
	"errors"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// The render slot is held through the export of the rendered set for apply,
// because the export is where the heap peaks. These tests pin that: on a pool
// of one slot, a conversion inside the slot sees one slot held, and one
// moved out of it sees none. No timing is involved.

// recordingConvert returns a convert that records how many slots of pool are
// held when it runs, then hands over to next.
func recordingConvert(
	pool *render.Slots,
	held *int,
	next func(*render.RenderResult) (*convertedRender, error),
) func(*render.RenderResult) (*convertedRender, error) {
	*held = -1
	return func(result *render.RenderResult) (*convertedRender, error) {
		*held = pool.Held()
		return next(result)
	}
}

// instanceSlotParams wires a ModuleInstance reconcile over a fake client with
// a pool of one slot and the given convert.
func instanceSlotParams(
	t *testing.T,
	pool *render.Slots,
	convert func(*render.RenderResult) (*convertedRender, error),
) *ModuleInstanceParams {
	t.Helper()
	return &ModuleInstanceParams{
		Client:        loopTestClient(t, operatorInstance(nil)),
		EventRecorder: events.NewFakeRecorder(32),
		Renderer:      moduleRendererFunc(func() (*render.RenderResult, error) { return configMapRenderResult(t), nil }),
		RenderSlots:   pool,
		convert:       convert,
	}
}

func storedInstanceReady(t *testing.T, params *ModuleInstanceParams) *metav1.Condition {
	t.Helper()
	var got releasesv1alpha1.ModuleInstance
	if err := params.Client.Get(context.Background(), loopTestRequest.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	return apimeta.FindStatusCondition(got.Status.Conditions, status.ReadyCondition)
}

func TestSlotConversion_ModuleInstance(t *testing.T) {
	pool := render.NewSlots(1)
	var held int
	// The conversion fails on purpose, so the reconcile stalls before apply
	// and needs no server-side apply from the fake client.
	stop := &conversionError{reason: status.ApplyFailedReason, step: "converting resources", err: errors.New("stopped by the test")}
	params := instanceSlotParams(t, pool, recordingConvert(pool, &held, func(*render.RenderResult) (*convertedRender, error) {
		return nil, stop
	}))

	if _, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if held != 1 {
		t.Fatalf("Held() during conversion = %d, want 1: the conversion ran outside the render slot", held)
	}
	if got := pool.Held(); got != 0 {
		t.Fatalf("Held() after the reconcile = %d, want 0", got)
	}
	ready := storedInstanceReady(t, params)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.ApplyFailedReason {
		t.Fatalf("Ready = %+v, want False with reason %s from the conversion error", ready, status.ApplyFailedReason)
	}
}

func TestSlotConversion_ModulePackage(t *testing.T) {
	pool := render.NewSlots(1)
	var held int
	params := &ModulePackageParams{
		EventRecorder: events.NewFakeRecorder(32),
		Renderer: packageRendererFunc(func() (string, *render.RenderResult, error) {
			return render.KindModuleInstance, configMapRenderResult(t), nil
		}),
		RenderSlots: pool,
		convert:     recordingConvert(pool, &held, convertRender),
	}

	converted, fail, err := renderModulePackage(context.Background(), params, operatorPackage(nil), t.TempDir(), DefaultModulePackageInterval)
	if err != nil || fail != nil {
		t.Fatalf("renderModulePackage: fail=%+v err=%v", fail, err)
	}
	if converted == nil || len(converted.resources) != 1 {
		t.Fatalf("converted = %+v, want one resource", converted)
	}

	if held != 1 {
		t.Fatalf("Held() during conversion = %d, want 1: the conversion ran outside the render slot", held)
	}
	if got := pool.Held(); got != 0 {
		t.Fatalf("Held() after the render = %d, want 0", got)
	}
}

// A panic during conversion is recorded and frees the slot.
func TestSlotConversion_PanicIsRecordedAndFreesTheSlot(t *testing.T) {
	pool := render.NewSlots(1)
	params := instanceSlotParams(t, pool, func(*render.RenderResult) (*convertedRender, error) {
		panic("conversion exploded")
	})

	got := panicValue(func() { _, _ = ReconcileModuleInstance(context.Background(), params, loopTestRequest) })

	if got != "conversion exploded" {
		t.Fatalf("panicked with %#v, want the conversion's own value", got)
	}
	if held := pool.Held(); held != 0 {
		t.Fatalf("Held() after the panic = %d, want 0", held)
	}
	ready := storedInstanceReady(t, params)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.ReconcilePanicReason {
		t.Fatalf("Ready = %+v, want False with reason %s", ready, status.ReconcilePanicReason)
	}
	if want := "reconcile panicked: conversion exploded"; ready.Message != want {
		t.Fatalf("Ready message = %q, want %q", ready.Message, want)
	}
}
