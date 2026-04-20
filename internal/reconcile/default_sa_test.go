/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile

import (
	"context"
	"reflect"
	"strings"
	"testing"

	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// TestResolveEffectiveSA covers the pure precedence logic: explicit spec wins
// over flag default, flag default fills in when spec is empty, both empty
// returns empty so the caller falls back to the controller's own client.
func TestResolveEffectiveSA(t *testing.T) {
	tests := []struct {
		name       string
		specSA     string
		defaultSA  string
		wantSA     string
		wantSource string
	}{
		{name: "both empty returns empty", specSA: "", defaultSA: "", wantSA: "", wantSource: ""},
		{name: "spec wins over flag", specSA: "custom", defaultSA: "opm-deployer", wantSA: "custom", wantSource: "spec"},
		{name: "flag fills in when spec empty", specSA: "", defaultSA: "opm-deployer", wantSA: "opm-deployer", wantSource: "default"},
		{name: "spec set, flag empty", specSA: "custom", defaultSA: "", wantSA: "custom", wantSource: "spec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSA, gotSource := resolveEffectiveSA(tt.specSA, tt.defaultSA)
			if gotSA != tt.wantSA || gotSource != tt.wantSource {
				t.Fatalf("resolveEffectiveSA(%q, %q) = (%q, %q), want (%q, %q)",
					tt.specSA, tt.defaultSA, gotSA, gotSource, tt.wantSA, tt.wantSource)
			}
		})
	}
}

// saTestScheme returns a scheme registered with the core and release types
// used across the buildApplyClient tests.
func saTestScheme(t *testing.T) *runtime.Scheme {
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

func saFixture(namespace, name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func mrFixture(namespace, specSA string) *releasesv1alpha1.ModuleRelease {
	return &releasesv1alpha1.ModuleRelease{
		ObjectMeta: metav1.ObjectMeta{Name: "mr", Namespace: namespace},
		Spec: releasesv1alpha1.ModuleReleaseSpec{
			ServiceAccountName: specSA,
		},
	}
}

// paramsForBuildApplyClient wires the minimal ModuleReleaseParams needed to
// exercise buildApplyClient: a fake client/reader for SA existence checks
// and a placeholder RestConfig + ResourceManager so the non-impersonated
// branch has distinct pointer identity for assertions.
func paramsForBuildApplyClient(c client.Client, defaultSA string) (*ModuleReleaseParams, *fluxssa.ResourceManager) {
	rm := apply.NewResourceManager(c, "opm-controller")
	return &ModuleReleaseParams{
		Client:                c,
		APIReader:             c,
		RestConfig:            &rest.Config{Host: "https://localhost:6443"},
		ResourceManager:       rm,
		DefaultServiceAccount: defaultSA,
	}, rm
}

// TestBuildApplyClient_NoImpersonation covers the "both empty" path: no SA
// from spec, no --default-service-account flag set → the controller's own
// ResourceManager/Client are returned so apply uses the controller identity.
func TestBuildApplyClient_NoImpersonation(t *testing.T) {
	scheme := saTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	mr := mrFixture("team-a", "")
	params, controllerRM := paramsForBuildApplyClient(c, "")

	gotRM, gotClient, err := buildApplyClient(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(gotRM, controllerRM) {
		t.Fatalf("expected controller ResourceManager, got impersonated")
	}
	if gotClient != c {
		t.Fatalf("expected controller client, got impersonated")
	}
}

// TestBuildApplyClient_DefaultSAExists covers the "empty spec + non-empty
// flag + SA present in release namespace" path: buildApplyClient must build
// an impersonated client distinct from the controller's.
func TestBuildApplyClient_DefaultSAExists(t *testing.T) {
	scheme := saTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(saFixture("team-a", "opm-deployer")).
		Build()

	mr := mrFixture("team-a", "")
	params, controllerRM := paramsForBuildApplyClient(c, "opm-deployer")

	gotRM, gotClient, err := buildApplyClient(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.DeepEqual(gotRM, controllerRM) {
		t.Fatalf("expected impersonated ResourceManager, got controller's")
	}
	if gotClient == c {
		t.Fatalf("expected impersonated client, got controller client")
	}
}

// TestBuildApplyClient_DefaultSAMissing covers the "empty spec + non-empty
// flag + SA absent" path: caller receives an error so the reconcile can
// stall with ImpersonationFailed.
func TestBuildApplyClient_DefaultSAMissing(t *testing.T) {
	scheme := saTestScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	mr := mrFixture("team-a", "")
	params, _ := paramsForBuildApplyClient(c, "opm-deployer")

	_, _, err := buildApplyClient(context.Background(), params, mr)
	if err == nil {
		t.Fatal("expected error when flag-defaulted SA is missing, got nil")
	}
	// Error must name the release namespace + SA so the stall message is
	// attributable to the tenant-scoped identity the operator expected.
	msg := err.Error()
	if !strings.Contains(msg, "team-a") || !strings.Contains(msg, "opm-deployer") {
		t.Fatalf("error %q should mention namespace and SA name", msg)
	}
}

// TestBuildApplyClient_SpecWinsOverDefault covers the precedence case:
// non-empty spec + non-empty flag → the spec value is the impersonation
// target and the flag's SA is ignored (not even looked up).
func TestBuildApplyClient_SpecWinsOverDefault(t *testing.T) {
	scheme := saTestScheme(t)
	// Only the spec SA exists; if the flag's "opm-deployer" were looked up
	// this would fail NotFound. Success proves the flag was ignored.
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(saFixture("team-a", "custom-sa")).
		Build()

	mr := mrFixture("team-a", "custom-sa")
	params, controllerRM := paramsForBuildApplyClient(c, "opm-deployer")

	gotRM, gotClient, err := buildApplyClient(context.Background(), params, mr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.DeepEqual(gotRM, controllerRM) {
		t.Fatalf("expected impersonated ResourceManager, got controller's")
	}
	if gotClient == c {
		t.Fatalf("expected impersonated client, got controller client")
	}
}

// TestDeletion_FlagResolution documents the deletion cleanup contract:
// handleDeletion / handleReleaseDeletion resolve the effective SA by passing
// (spec.ServiceAccountName, params.DefaultServiceAccount) through
// resolveEffectiveSA — the same call shape used in apply/prune. An empty spec
// combined with a non-empty flag must yield the flag value so deletion
// cleanup impersonates the same identity that apply used. Best-effort fallback
// on impersonation failure is covered by the deletion integration paths.
func TestDeletion_FlagResolution(t *testing.T) {
	mr := mrFixture("team-a", "")
	params := &ModuleReleaseParams{DefaultServiceAccount: "opm-deployer"}

	gotSA, gotSource := resolveEffectiveSA(mr.Spec.ServiceAccountName, params.DefaultServiceAccount)
	if gotSA != "opm-deployer" {
		t.Fatalf("deletion effective SA = %q, want %q", gotSA, "opm-deployer")
	}
	if gotSource != "default" {
		t.Fatalf("deletion source = %q, want %q", gotSource, "default")
	}
}

// TestBuildApplyClient_DefaultSANotCrossNamespace covers the namespace-scope
// invariant: the flag's SA must exist in the release's namespace, not the
// controller's. An SA with the flag name in a different namespace does not
// satisfy the lookup and the reconcile stalls.
func TestBuildApplyClient_DefaultSANotCrossNamespace(t *testing.T) {
	scheme := saTestScheme(t)
	// SA exists in "opm-system" only. Release lives in "team-b".
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(saFixture("opm-system", "opm-deployer")).
		Build()

	mr := mrFixture("team-b", "")
	params, _ := paramsForBuildApplyClient(c, "opm-deployer")

	_, _, err := buildApplyClient(context.Background(), params, mr)
	if err == nil {
		t.Fatal("expected error: flag default must not fall back to controller namespace")
	}
	msg := err.Error()
	if !strings.Contains(msg, "team-b") {
		t.Fatalf("error %q should mention release namespace team-b", msg)
	}
}
