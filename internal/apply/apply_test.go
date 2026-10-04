package apply

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fluxcd/cli-utils/pkg/object"
	fluxssa "github.com/fluxcd/pkg/ssa"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var gadgetGK = schema.GroupKind{Group: "lag.example.com", Kind: "Gadget"}

// testCRD returns a CustomResourceDefinition object for group and kind; an
// empty kind leaves spec.names.kind out.
func testCRD(group, kind string) *unstructured.Unstructured {
	names := map[string]any{"plural": "things"}
	if kind != "" {
		names["kind"] = kind
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"group": group, "names": names},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition",
	})
	obj.SetName("things." + group)
	return obj
}

// testObject returns a group/v1 kind object named name in the default namespace.
func testObject(group, kind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind})
	obj.SetNamespace("default")
	obj.SetName(name)
	return obj
}

// gadgetNoMatch is the error the CI run saw, wrapped as a dry-run error is.
func gadgetNoMatch() error {
	return fmt.Errorf("Gadget/default/g dry-run failed: %w",
		&meta.NoKindMatchError{GroupKind: gadgetGK, SearchedVersions: []string{"v1"}})
}

func TestPendingCRDKind(t *testing.T) {
	gadgetCRD := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")
	discoveryNotFound := &apiutil.ErrResourceDiscoveryFailed{
		schema.GroupVersion{Group: gadgetGK.Group, Version: "v1"}: apierrors.NewNotFound(
			schema.GroupResource{Group: gadgetGK.Group, Resource: "gadgets"}, ""),
	}

	tests := []struct {
		name      string
		err       error
		resources []*unstructured.Unstructured
		want      bool
	}{
		{
			name:      "wrapped no-match for a kind a CRD in the set defines",
			err:       gadgetNoMatch(),
			resources: []*unstructured.Unstructured{gadget, gadgetCRD},
			want:      true,
		},
		{
			name:      "discovery NotFound for a group a CRD in the set defines",
			err:       fmt.Errorf("dry-run failed: %w", discoveryNotFound),
			resources: []*unstructured.Unstructured{gadget, gadgetCRD},
			want:      true,
		},
		{
			name: "no-match for a kind no CRD in the set defines",
			err: &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "other.example.com", Kind: "Gizmo"}, SearchedVersions: []string{"v1"},
			},
			resources: []*unstructured.Unstructured{gadgetCRD},
			want:      false,
		},
		{
			name: "no-match for another kind of a group the set defines",
			err: &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: gadgetGK.Group, Kind: "Gizmo"}, SearchedVersions: []string{"v1"},
			},
			resources: []*unstructured.Unstructured{gadgetCRD},
			want:      false,
		},
		{
			name:      "no CRD in the set",
			err:       gadgetNoMatch(),
			resources: []*unstructured.Unstructured{gadget},
			want:      false,
		},
		{
			name:      "not a no-match error",
			err:       apierrors.NewConflict(schema.GroupResource{Group: gadgetGK.Group, Resource: "gadgets"}, "g", errors.New("conflict")),
			resources: []*unstructured.Unstructured{gadget, gadgetCRD},
			want:      false,
		},
		{
			name:      "CRD without spec.names.kind",
			err:       gadgetNoMatch(),
			resources: []*unstructured.Unstructured{gadget, testCRD(gadgetGK.Group, "")},
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pendingCRDKind(tt.err, tt.resources); got != tt.want {
				t.Fatalf("pendingCRDKind() = %v, want %v (err: %v)", got, tt.want, tt.err)
			}
		})
	}
}

// changeSet returns a change set with one entry per object and action pair.
func changeSet(entries ...any) *fluxssa.ChangeSet {
	cs := fluxssa.NewChangeSet()
	for i := 0; i < len(entries); i += 2 {
		obj := entries[i].(*unstructured.Unstructured)
		cs.Add(fluxssa.ChangeSetEntry{
			ObjMetadata: object.UnstructuredToObjMetadata(obj),
			Action:      entries[i+1].(fluxssa.Action),
		})
	}
	return cs
}

func TestActionLedger_KeepsTheFirstCreatedAction(t *testing.T) {
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")
	cm := testObject("", "ConfigMap", "cm")

	ledger := actionLedger{}
	ledger.record(changeSet(crd, fluxssa.CreatedAction, cm, fluxssa.UnchangedAction))
	ledger.record(nil)
	final := changeSet(crd, fluxssa.UnchangedAction, cm, fluxssa.UnchangedAction, gadget, fluxssa.CreatedAction)
	ledger.record(final)

	got := ledger.result(final)
	want := ApplyResult{Created: 2, Unchanged: 1}
	if *got != want {
		t.Fatalf("result = %+v, want %+v", *got, want)
	}
}

// shortenDiscoveryRetry sets the retry pacing for one test.
func shortenDiscoveryRetry(t *testing.T, interval, timeout time.Duration) {
	t.Helper()
	oldInterval, oldTimeout := discoveryRetryInterval, discoveryRetryTimeout
	discoveryRetryInterval, discoveryRetryTimeout = interval, timeout
	t.Cleanup(func() { discoveryRetryInterval, discoveryRetryTimeout = oldInterval, oldTimeout })
}

func TestDiscoveryRetry_AttemptsKeepTheCallersContext(t *testing.T) {
	shortenDiscoveryRetry(t, time.Millisecond, time.Minute)
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")

	attempts := 0
	result, err := applyWithDiscoveryRetry(context.Background(), func(ctx context.Context) (*fluxssa.ChangeSet, error) {
		attempts++
		if _, has := ctx.Deadline(); has {
			t.Errorf("attempt %d: context has a deadline the caller did not set", attempts)
		}
		if attempts < 3 {
			return changeSet(crd, fluxssa.CreatedAction), gadgetNoMatch()
		}
		return changeSet(crd, fluxssa.UnchangedAction, gadget, fluxssa.CreatedAction), nil
	}, []*unstructured.Unstructured{gadget, crd})
	if err != nil {
		t.Fatalf("applyWithDiscoveryRetry() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if result.Created != 2 {
		t.Fatalf("Created = %d, want 2", result.Created)
	}
}

func TestDiscoveryRetry_BoundEndsTheRetry(t *testing.T) {
	shortenDiscoveryRetry(t, 5*time.Millisecond, 50*time.Millisecond)
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	attempts := 0
	start := time.Now()
	_, err := applyWithDiscoveryRetry(ctx, func(context.Context) (*fluxssa.ChangeSet, error) {
		attempts++
		return changeSet(crd, fluxssa.CreatedAction), gadgetNoMatch()
	}, []*unstructured.Unstructured{gadget, crd})
	if !meta.IsNoMatchError(err) {
		t.Fatalf("error = %v, want a no-match error", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("the caller's context ended; the bound should have ended the retry")
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want a retry", attempts)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("retry ran %v, past its 50ms bound", elapsed)
	}
}

func TestDiscoveryRetry_ContextEndsTheRetry(t *testing.T) {
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")

	for _, inFlight := range []bool{false, true} {
		t.Run(fmt.Sprintf("cut short in flight %v", inFlight), func(t *testing.T) {
			shortenDiscoveryRetry(t, 5*time.Millisecond, time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attempts := 0
			_, err := applyWithDiscoveryRetry(ctx, func(ctx context.Context) (*fluxssa.ChangeSet, error) {
				attempts++
				if attempts == 3 {
					cancel()
					if inFlight {
						return nil, ctx.Err()
					}
				}
				return changeSet(crd, fluxssa.CreatedAction), gadgetNoMatch()
			}, []*unstructured.Unstructured{gadget, crd})
			if _, ok := errors.AsType[*meta.NoKindMatchError](err); !ok {
				t.Fatalf("error = %v, want it to wrap the no-match", err)
			}
			if attempts != 3 {
				t.Fatalf("attempts = %d, want 3", attempts)
			}
		})
	}
}

func TestDiscoveryRetry_ContextEndKeepsARealError(t *testing.T) {
	shortenDiscoveryRetry(t, 5*time.Millisecond, time.Minute)
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")
	conflict := apierrors.NewConflict(schema.GroupResource{Resource: "gadgets"}, "g", errors.New("conflict"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	_, err := applyWithDiscoveryRetry(ctx, func(context.Context) (*fluxssa.ChangeSet, error) {
		attempts++
		if attempts == 2 {
			cancel()
			return nil, conflict
		}
		return changeSet(crd, fluxssa.CreatedAction), gadgetNoMatch()
	}, []*unstructured.Unstructured{gadget, crd})
	if !errors.Is(err, conflict) {
		t.Fatalf("error = %v, want it to wrap the conflict", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestDiscoveryRetry_OtherErrorsFailAtOnce(t *testing.T) {
	shortenDiscoveryRetry(t, time.Millisecond, time.Minute)
	crd := testCRD(gadgetGK.Group, gadgetGK.Kind)
	gadget := testObject(gadgetGK.Group, gadgetGK.Kind, "g")
	conflict := apierrors.NewConflict(schema.GroupResource{Resource: "gadgets"}, "g", errors.New("conflict"))
	foreign := &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{Group: "other.example.com", Kind: "Gizmo"}, SearchedVersions: []string{"v1"},
	}

	for _, want := range []error{conflict, foreign} {
		t.Run(want.Error(), func(t *testing.T) {
			attempts := 0
			_, err := applyWithDiscoveryRetry(context.Background(), func(context.Context) (*fluxssa.ChangeSet, error) {
				attempts++
				return nil, want
			}, []*unstructured.Unstructured{gadget, crd})
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if attempts != 1 {
				t.Fatalf("attempts = %d, want 1", attempts)
			}
		})
	}
}
