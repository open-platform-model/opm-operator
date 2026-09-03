package platformmodule

import (
	"context"
	"fmt"

	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// ModFileSource yields a published module's cue.mod/module.cue. It is the
// one method of [modconfig.Registry] the closure needs, kept as a narrow
// interface so tests can supply a fixture graph without a registry.
type ModFileSource interface {
	ModFile(ctx context.Context, mv module.Version) (*modfile.File, error)
}

// NewRegistry returns a module-file source resolving through the given CUE
// registry mapping (the operator's --registry value; empty falls back to the
// process CUE_REGISTRY) and the process CUE cache directory (CUE_CACHE_DIR,
// set at manager start). Module files it fetches are the same artifacts the
// validation build fetches, so a closure derivation never adds an artifact
// class to the reconcile path.
func NewRegistry(registry string) (ModFileSource, error) {
	reg, err := modconfig.NewRegistry(&modconfig.Config{
		CUERegistry: registry,
		ClientType:  "opm-operator",
	})
	if err != nil {
		return nil, fmt.Errorf("configuring module registry: %w", err)
	}
	return reg, nil
}

// Closure derives the generated module's full dependency list from roots: a
// breadth-first walk over each reachable module version's published module
// file, selecting the maximum version per major-qualified path, the roots
// participating in the maximum. This is minimum version selection computed
// the way `cue mod tidy` computes it (0019 D13: tidying happens once, at
// platform-package generation), minus the prune of modules no import
// reaches, which pins a path nothing evaluates and is harmless. Derived
// entries carry no default-major marker; `cue mod tidy` writes none for a
// platform either, because the platform imports nothing unqualified
// (measured, design § closure).
//
// A root or transitive requirement naming an unpublished build fails with
// an error naming the module path and version, the same wording the CUE
// resolver uses for a missing pin.
func Closure(ctx context.Context, src ModFileSource, roots []Dep) ([]Dep, error) {
	if src == nil {
		return nil, fmt.Errorf("closure needs a module-file source")
	}
	selected := make(map[string]module.Version, len(roots))
	visited := make(map[string]bool, len(roots))
	var queue []module.Version

	push := func(mv module.Version) {
		if cur, ok := selected[mv.Path()]; !ok || mv.Compare(cur) > 0 {
			selected[mv.Path()] = mv
		}
		if !visited[mv.String()] {
			visited[mv.String()] = true
			queue = append(queue, mv)
		}
	}

	for _, r := range roots {
		mv, err := module.NewVersion(r.Path, r.Version)
		if err != nil {
			return nil, fmt.Errorf("dependency root %s@%s: %w", r.Path, r.Version, err)
		}
		push(mv)
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mv := queue[0]
		queue = queue[1:]
		mf, err := src.ModFile(ctx, mv)
		if err != nil {
			return nil, fmt.Errorf("resolving dependency %s: %w", mv, err)
		}
		for _, dep := range mf.DepVersions() {
			if dep.IsLocal() {
				continue
			}
			push(dep)
		}
	}

	out := make([]Dep, 0, len(selected))
	for path, mv := range selected {
		out = append(out, Dep{Path: path, Version: mv.Version()})
	}
	sortDeps(out)
	return out, nil
}
