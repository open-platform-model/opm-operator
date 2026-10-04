/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile

import (
	"fmt"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// TestMarkApplyFailure_NoMatchIsApplyFailed covers the scenario "Discovery
// never serves the kind": the no-match error Apply returns when the discovery
// retry gives up is reported as ApplyFailed and retried, with or without an
// impersonated identity.
func TestMarkApplyFailure_NoMatchIsApplyFailed(t *testing.T) {
	noMatch := fmt.Errorf("failed to apply resources: %w", &apimeta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "never.example.com", Kind: "Doohickey"},
		SearchedVersions: []string{"v1"},
	})
	for _, sa := range []string{"", "tenant-sa"} {
		t.Run(fmt.Sprintf("service account %q", sa), func(t *testing.T) {
			mi := &releasesv1alpha1.ModuleInstance{}
			outcome := markApplyFailure(mi, noMatch, sa)
			if outcome != FailedTransient {
				t.Fatalf("outcome = %v, want %v", outcome, FailedTransient)
			}
			ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
			if ready == nil {
				t.Fatal("Ready condition not set")
			}
			if ready.Reason != status.ApplyFailedReason {
				t.Fatalf("Ready reason = %q, want %q", ready.Reason, status.ApplyFailedReason)
			}
		})
	}
}
