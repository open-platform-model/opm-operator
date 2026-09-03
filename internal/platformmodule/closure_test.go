package platformmodule

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// fakeSource is a fixture module graph: module version string -> the paths
// and versions that module requires. A version absent from the map is an
// unpublished build.
type fakeSource struct {
	graph map[string][]Dep
	calls []string
}

func (f *fakeSource) ModFile(_ context.Context, mv module.Version) (*modfile.File, error) {
	f.calls = append(f.calls, mv.String())
	deps, ok := f.graph[mv.String()]
	if !ok {
		return nil, fmt.Errorf("module %s: module not found", mv)
	}
	mf := &modfile.File{Module: mv.Path(), Deps: map[string]*modfile.Dep{}}
	for _, d := range deps {
		mf.Deps[d.Path] = &modfile.Dep{Version: d.Version}
	}
	if err := mf.Init(); err != nil {
		return nil, err
	}
	return mf, nil
}

func fixtureGraph() *fakeSource {
	return &fakeSource{graph: map[string][]Dep{
		"opmodel.dev/core@v2.0.0-alpha.7": nil,
		"opmodel.dev/core@v2.0.0-alpha.6": nil,
		"opmodel.dev/catalogs/opm@v4.0.1": {
			{Path: "cue.dev/x/k8s.io@v0", Version: "v0.10.0"},
			{Path: CorePath, Version: "v2.0.0-alpha.6"},
		},
		"opmodel.dev/catalogs/k8s@v1.0.0-alpha.2": {
			{Path: CorePath, Version: "v2.0.0-alpha.6"},
		},
		"cue.dev/x/k8s.io@v0.10.0": nil,
	}}
}

func assertDeps(t *testing.T, got, want []Dep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("closure %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("closure[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestClosure_TransitiveDependencyIsPinned(t *testing.T) {
	src := fixtureGraph()
	roots := Roots([]Entry{
		{Path: opmPath, Version: "4.0.1", Enable: true},
		{Path: k8sPath, Version: "1.0.0-alpha.2", Enable: false},
	})
	got, err := Closure(context.Background(), src, roots)
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	// Sorted by path; core resolves to the root's alpha.7 (the maximum over
	// the catalogs' alpha.6 requirement); k8s.io joins from the opm catalog.
	assertDeps(t, got, []Dep{
		{Path: "cue.dev/x/k8s.io@v0", Version: "v0.10.0"},
		{Path: k8sPath, Version: "v1.0.0-alpha.2"},
		{Path: opmPath, Version: "v4.0.1"},
		{Path: CorePath, Version: "v2.0.0-alpha.7"},
	})
}

func TestClosure_RootsParticipateInTheMaximum(t *testing.T) {
	src := fixtureGraph()
	src.graph["opmodel.dev/core@v2.0.0-alpha.8"] = nil
	src.graph["opmodel.dev/catalogs/opm@v4.0.1"] = []Dep{
		{Path: CorePath, Version: "v2.0.0-alpha.8"},
	}
	got, err := Closure(context.Background(), src, Roots([]Entry{{Path: opmPath, Version: "4.0.1", Enable: true}}))
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	// A catalog requiring a newer core than the operator's constant raises the
	// stated pin, exactly as `cue mod tidy` would.
	assertDeps(t, got, []Dep{
		{Path: opmPath, Version: "v4.0.1"},
		{Path: CorePath, Version: "v2.0.0-alpha.8"},
	})
}

func TestClosure_UnpublishedRootNamesPathAndVersion(t *testing.T) {
	src := fixtureGraph()
	_, err := Closure(context.Background(), src, Roots([]Entry{{Path: opmPath, Version: "4.9.9", Enable: true}}))
	if err == nil {
		t.Fatal("expected an error for an unpublished pin")
	}
	for _, want := range []string{"opmodel.dev/catalogs/opm@v4.9.9", "module not found"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

func TestClosure_EachVersionFetchedOnce(t *testing.T) {
	src := fixtureGraph()
	_, err := Closure(context.Background(), src, Roots([]Entry{
		{Path: opmPath, Version: "4.0.1", Enable: true},
		{Path: k8sPath, Version: "1.0.0-alpha.2", Enable: true},
	}))
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	seen := map[string]int{}
	for _, c := range src.calls {
		seen[c]++
	}
	for mv, n := range seen {
		if n != 1 {
			t.Fatalf("%s fetched %d times", mv, n)
		}
	}
	// Both catalogs require core alpha.6 and the root names alpha.7: both
	// versions are walked (their requirements count), each once.
	for _, want := range []string{"opmodel.dev/core@v2.0.0-alpha.6", "opmodel.dev/core@v2.0.0-alpha.7"} {
		if seen[want] != 1 {
			t.Fatalf("%s not walked exactly once: %v", want, src.calls)
		}
	}
}

func TestClosure_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Closure(ctx, fixtureGraph(), Roots(nil))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClosure_NilSource(t *testing.T) {
	if _, err := Closure(context.Background(), nil, Roots(nil)); err == nil {
		t.Fatal("expected an error for a nil source")
	}
}
