package render

import (
	"context"
	"errors"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	"github.com/open-platform-model/poc-controller/pkg/core"
	"github.com/open-platform-model/poc-controller/pkg/loader"
	"github.com/open-platform-model/poc-controller/pkg/module"
	"github.com/open-platform-model/poc-controller/pkg/provider"
	pkgrender "github.com/open-platform-model/poc-controller/pkg/render"
)

// Kind constants surface the CUE `kind` field so the reconciler can dispatch
// to the appropriate pipeline without re-evaluating the value.
const (
	KindModuleRelease = "ModuleRelease"
	KindBundleRelease = "BundleRelease"
)

// ErrUnsupportedKind indicates the loaded CUE value has a `kind` field that
// this controller cannot render (e.g., BundleRelease, which is not yet
// implemented).
var ErrUnsupportedKind = errors.New("unsupported release kind")

// LoadReleaseFromPath evaluates the CUE package at packageDir and returns
// the raw CUE value along with the detected `kind` field. CUE module
// dependencies are resolved from the registry set via CUE_REGISTRY.
func LoadReleaseFromPath(packageDir string) (cue.Value, string, error) {
	cueCtx := cuecontext.New()
	raw, err := loader.LoadModulePackage(cueCtx, packageDir)
	if err != nil {
		return cue.Value{}, "", fmt.Errorf("loading release package: %w", err)
	}

	kindVal := raw.LookupPath(cue.ParsePath("kind"))
	if !kindVal.Exists() {
		return cue.Value{}, "", fmt.Errorf("release package at %q has no kind field", packageDir)
	}
	kind, err := kindVal.String()
	if err != nil {
		return cue.Value{}, "", fmt.Errorf("reading kind field: %w", err)
	}
	return raw, kind, nil
}

// RenderLoadedModuleRelease runs the ModuleRelease render pipeline on a
// pre-loaded CUE value (evaluated via LoadReleaseFromPath). The value must
// have kind == "ModuleRelease". Used by the Release reconciler where the
// CUE package is delivered via a Flux artifact.
func RenderLoadedModuleRelease(
	ctx context.Context,
	raw cue.Value,
	packageDir string,
	prov *provider.Provider,
) (*RenderResult, error) {
	mod, err := extractModuleFromRelease(raw, packageDir)
	if err != nil {
		return nil, fmt.Errorf("extracting module from release: %w", err)
	}

	cueCtx := raw.Context()

	// Inject #runtimeLabels so component transformers pick up the managed-by
	// label consistent with the synthesized-release path.
	runtimeLabels := cueCtx.CompileString(fmt.Sprintf(`{
	%q: %q
}`, core.LabelManagedBy, core.LabelManagedByControllerValue), cue.Filename("runtimeLabels"))
	if runtimeLabels.Err() != nil {
		return nil, fmt.Errorf("compiling runtime labels: %w", runtimeLabels.Err())
	}
	raw = raw.FillPath(cue.ParsePath("#runtimeLabels"), runtimeLabels)

	// Release CRDs carry no `values` — the CUE package already specifies them.
	// Pass nil so ParseModuleRelease falls back to the module #config defaults.
	rel, err := module.ParseModuleRelease(ctx, raw, mod, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing module release: %w", err)
	}

	controllerLabels := map[string]string{
		core.LabelManagedBy:              core.LabelManagedByControllerValue,
		core.LabelModuleReleaseNamespace: rel.Metadata.Namespace,
	}
	result, err := pkgrender.ProcessModuleRelease(ctx, rel, prov, controllerLabels)
	if err != nil {
		return nil, fmt.Errorf("processing module release: %w", err)
	}
	if len(result.Resources) == 0 {
		return nil, fmt.Errorf("release %q: no resources rendered", rel.Metadata.Name)
	}

	entries, err := buildInventoryEntries(result.Resources)
	if err != nil {
		return nil, fmt.Errorf("building inventory entries: %w", err)
	}

	return &RenderResult{
		Resources:        result.Resources,
		InventoryEntries: entries,
		Warnings:         result.Warnings,
	}, nil
}
