package render

import (
	"context"
	"fmt"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/synthesis"
	"github.com/open-platform-model/opm-operator/pkg/core"
	"github.com/open-platform-model/opm-operator/pkg/loader"
	"github.com/open-platform-model/opm-operator/pkg/module"
	"github.com/open-platform-model/opm-operator/pkg/provider"
	pkgrender "github.com/open-platform-model/opm-operator/pkg/render"
)

// RenderResult holds the output of a successful RenderModule call.
// Contains both the rendered resources and their inventory entries, giving the
// caller everything needed for apply + inventory in one call.
type RenderResult struct {
	// Resources is the ordered list of rendered Kubernetes resources.
	Resources []*core.Resource

	// InventoryEntries are the CRD-typed inventory entries built from Resources.
	InventoryEntries []releasesv1alpha1.InventoryEntry

	// Warnings are non-fatal render warnings (e.g., unhandled traits).
	Warnings []string
}

// RenderModuleFromRegistry synthesizes a #ModuleRelease CUE package that
// imports the target module from an OCI registry via CUE's native module
// system. This is the primary render path for the controller.
//
// The flow:
//  1. Synthesize a temporary CUE module with module.cue + release.cue.
//  2. Load the package (CUE resolves dependencies from OCI registry).
//  3. Extract module metadata and config schema from the loaded release.
//  4. Inject values.
//  5. Build release via ParseModuleRelease → ProcessModuleRelease.
//     The controller's runtime identity is injected into each transformer's
//     #context.#runtimeName via ProcessModuleRelease.
//  6. Build inventory entries from rendered resources.
func RenderModuleFromRegistry(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
	prov *provider.Provider,
) (*RenderResult, error) {
	// Synthesize the release CUE package.
	dir, err := synthesis.SynthesizeRelease(synthesis.ReleaseParams{
		Name:          name,
		Namespace:     namespace,
		ModulePath:    modulePath,
		ModuleVersion: moduleVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesizing release: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	cueCtx := cuecontext.New()

	// Load the synthesized package. CUE resolves module dependencies from the
	// OCI registry (respects CUE_REGISTRY env var set by the controller).
	raw, err := loader.LoadModulePackage(cueCtx, dir)
	if err != nil {
		return nil, fmt.Errorf("loading synthesized release: %w", err)
	}

	// Extract module info from the loaded release value.
	mod, err := extractModuleFromRelease(raw, dir)
	if err != nil {
		return nil, fmt.Errorf("extracting module from release: %w", err)
	}

	// Convert CRD values to cue.Value slice.
	// When no user values are provided, use the module's #config defaults
	// so that values: _ in the #ModuleRelease schema becomes concrete.
	var cueValues []cue.Value
	if values != nil && values.Raw != nil {
		compiled := cueCtx.CompileBytes(values.Raw, cue.Filename("values"))
		if compiled.Err() != nil {
			return nil, fmt.Errorf("compiling values: %w", compiled.Err())
		}
		cueValues = append(cueValues, compiled)
	} else if mod.Config.Exists() {
		cueValues = append(cueValues, mod.Config)
	}

	// Build the release: validate values, fill, ensure concrete.
	rel, err := module.ParseModuleRelease(ctx, raw, mod, cueValues)
	if err != nil {
		return nil, fmt.Errorf("parsing module release: %w", err)
	}

	// Render with the caller-supplied provider. ProcessModuleRelease injects
	// the controller's runtime identity into each transformer's #context.
	result, err := pkgrender.ProcessModuleRelease(ctx, rel, prov, core.LabelManagedByControllerValue)
	if err != nil {
		return nil, fmt.Errorf("processing module release: %w", err)
	}

	if len(result.Resources) == 0 {
		return nil, fmt.Errorf("module %q: no resources rendered", name)
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

// extractModuleFromRelease extracts module information from a loaded
// #ModuleRelease CUE value. The module metadata and config are accessed
// via the #module definition that was bound in the synthesized release.
// Mirrors the CLI's approach in releasefile.bareModuleRelease.
func extractModuleFromRelease(raw cue.Value, moduleDir string) (module.Module, error) {
	// The synthesized release binds #module to the imported module package.
	// Extract metadata from the release's top-level metadata (which includes
	// module-level fields through #ModuleRelease schema unification).
	metaVal := raw.LookupPath(cue.ParsePath("metadata"))
	if !metaVal.Exists() {
		return module.Module{}, fmt.Errorf("release missing metadata field")
	}

	meta := &module.ModuleMetadata{}
	if err := metaVal.Decode(meta); err != nil {
		return module.Module{}, fmt.Errorf("decoding module metadata from release: %w", err)
	}

	// Extract #module and #config from the release, matching the CLI's
	// two-step lookup (LookupPath for #module, then #config on the result).
	moduleVal := raw.LookupPath(cue.MakePath(cue.Def("module")))
	config := moduleVal.LookupPath(cue.MakePath(cue.Def("config")))

	return module.Module{
		Metadata:   meta,
		Config:     config,
		Raw:        raw,
		ModulePath: moduleDir,
	}, nil
}

// buildInventoryEntries converts rendered resources to inventory entries.
func buildInventoryEntries(resources []*core.Resource) ([]releasesv1alpha1.InventoryEntry, error) {
	entries := make([]releasesv1alpha1.InventoryEntry, 0, len(resources))
	for _, r := range resources {
		u, err := r.ToUnstructured()
		if err != nil {
			return nil, fmt.Errorf("converting resource %s to unstructured: %w", r, err)
		}
		entries = append(entries, inventory.NewEntryFromResource(u))
	}
	return entries, nil
}
