package platform

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
)

// gen is the identity of a package generated for CR generation n with no
// active claims: the shape every platform had before claims existed.
func gen(n int64) PackageIdentity { return NewPackageIdentity(n, nil) }

// genWith is the identity of a package generated for CR generation n with one
// active claim, so a test can hold two identities for one generation.
func genWith(n int64, catalog, version string) PackageIdentity {
	return NewPackageIdentity(n, []ClaimCoordinate{{Catalog: catalog, Version: version}})
}

func sampleFiles(marker string) platformmodule.Files {
	return platformmodule.Files{
		platformmodule.ModuleFileName:   []byte("module: \"opmodel.dev/platforms/cluster@v0\"\n// " + marker + "\n"),
		platformmodule.PlatformFileName: []byte("package platform\n// " + marker + "\n"),
	}
}

func readMarker(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, platformmodule.PlatformFileName))
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return strings.TrimPrefix(lines[len(lines)-1], "// ")
}

func listRoot(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("listing %s: %v", root, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestLayout_WriteCreatesPackageDirectory(t *testing.T) {
	l := Layout{Root: filepath.Join(t.TempDir(), "platform")}
	dir, err := l.Write(gen(3), sampleFiles("gen3"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if dir != l.Dir(gen(3)) {
		t.Fatalf("Write returned %s, want %s", dir, l.Dir(gen(3)))
	}
	if got := readMarker(t, dir); got != "gen3" {
		t.Fatalf("marker %q, want gen3", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "cue.mod", "module.cue")); err != nil {
		t.Fatalf("cue.mod/module.cue missing: %v", err)
	}
	if names := listRoot(t, l.Root); len(names) != 1 || names[0] != "gen-3" {
		t.Fatalf("root holds %v, want only gen-3 (no staging leftovers)", names)
	}
}

// Two identities for one CR generation — the case 0015:D17 exists for — must not
// share a directory, or the second write would overwrite the module a render
// holding the first is still reading.
func TestLayout_TwoIdentitiesOneGenerationGetSeparateDirectories(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	bare, err := l.Write(gen(5), sampleFiles("no-claims"))
	if err != nil {
		t.Fatalf("Write (no claims): %v", err)
	}
	claimed, err := l.Write(genWith(5, "opmodel.dev/catalogs/k8up@v1", "1.2.0"), sampleFiles("one-claim"))
	if err != nil {
		t.Fatalf("Write (one claim): %v", err)
	}
	if bare == claimed {
		t.Fatalf("both identities wrote to %s", bare)
	}
	if got := readMarker(t, bare); got != "no-claims" {
		t.Fatalf("the claimless package was overwritten, marker %q", got)
	}
	if got := readMarker(t, claimed); got != "one-claim" {
		t.Fatalf("marker %q in the claimed package", got)
	}
	pkgs, err := l.Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("Packages = %v, want both identities", pkgs)
	}
}

func TestLayout_WriteSameIdentitySwaps(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	if _, err := l.Write(gen(1), sampleFiles("first")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dir, err := l.Write(gen(1), sampleFiles("second"))
	if err != nil {
		t.Fatalf("Write (again): %v", err)
	}
	if got := readMarker(t, dir); got != "second" {
		t.Fatalf("marker %q after re-write, want second", got)
	}
	if names := listRoot(t, l.Root); len(names) != 1 {
		t.Fatalf("root holds %v after a same-identity swap, want only gen-1", names)
	}
}

func TestLayout_FailedWriteLeavesNoPackageDirectory(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	// A path escaping the module directory is refused by the helper after
	// the staging directory exists, so the failure path must clean it up.
	_, err := l.Write(gen(2), platformmodule.Files{
		platformmodule.PlatformFileName: []byte("package platform\n"),
		"../escape.cue":                 []byte("nope"),
	})
	if err == nil {
		t.Fatal("expected Write to refuse an escaping path")
	}
	if _, statErr := os.Stat(l.Dir(gen(2))); !os.IsNotExist(statErr) {
		t.Fatalf("gen-2 exists after a failed write (stat: %v)", statErr)
	}
	for _, name := range listRoot(t, l.Root) {
		if strings.HasPrefix(name, packagePrefix) {
			t.Fatalf("a package directory %s survived a failed write", name)
		}
	}
}

func TestLayout_PruneKeepsOnlyTheKeepSet(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	ids := []PackageIdentity{gen(1), gen(2), gen(3)}
	for _, id := range ids {
		if _, err := l.Write(id, sampleFiles("x")); err != nil {
			t.Fatalf("Write %s: %v", id, err)
		}
	}
	// Staging and aside leftovers from an interrupted run.
	for _, stale := range []string{stagingPrefix + "gen-4-deadbeef", asidePrefix + "gen-2-cafebabe"} {
		if err := os.MkdirAll(filepath.Join(l.Root, stale), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Prune(gen(3), gen(2)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pkgs, err := l.Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if want := []string{"gen-2", "gen-3"}; !slices.Equal(pkgs, want) {
		t.Fatalf("packages after prune %v, want %v", pkgs, want)
	}
	if names := listRoot(t, l.Root); len(names) != 2 {
		t.Fatalf("root holds %v after prune, want only gen-2 and gen-3", names)
	}
}

// A superseded package's directory survives prune while its identity is in
// the keep set, which is how a render in flight keeps reading the module it
// started against.
func TestLayout_PruneKeepsASupersededIdentity(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	held := gen(4)
	current := genWith(4, "opmodel.dev/catalogs/k8up@v1", "1.2.0")
	if _, err := l.Write(held, sampleFiles("held")); err != nil {
		t.Fatalf("Write (held): %v", err)
	}
	if _, err := l.Write(current, sampleFiles("current")); err != nil {
		t.Fatalf("Write (current): %v", err)
	}
	if err := l.Prune(current, held); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if got := readMarker(t, l.Dir(held)); got != "held" {
		t.Fatalf("the leased package should survive prune, marker %q", got)
	}

	// Released: the next prune drops it.
	if err := l.Prune(current); err != nil {
		t.Fatalf("Prune (released): %v", err)
	}
	if _, err := os.Stat(l.Dir(held)); !os.IsNotExist(err) {
		t.Fatalf("a released superseded package should be reclaimed (stat: %v)", err)
	}
}

func TestLayout_ResetEmptiesRoot(t *testing.T) {
	l := Layout{Root: filepath.Join(t.TempDir(), "nested", "platform")}
	// Reset on a missing root creates it.
	if err := l.Reset(); err != nil {
		t.Fatalf("Reset (missing root): %v", err)
	}
	if _, err := l.Write(gen(7), sampleFiles("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := l.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if names := listRoot(t, l.Root); len(names) != 0 {
		t.Fatalf("root holds %v after reset, want empty", names)
	}
	pkgs, err := l.Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("packages after reset %v, want none", pkgs)
	}
}

func TestLayout_Refusals(t *testing.T) {
	if _, err := (Layout{}).Write(gen(1), sampleFiles("x")); err == nil {
		t.Fatal("expected Write on an empty root to fail")
	}
	if _, err := (Layout{Root: t.TempDir()}).Write(gen(1), nil); err == nil {
		t.Fatal("expected Write with no files to fail")
	}
	if err := (Layout{}).Reset(); err == nil {
		t.Fatal("expected Reset on an empty root to fail")
	}
	if err := (Layout{Root: filepath.Join(t.TempDir(), "absent")}).Prune(); err != nil {
		t.Fatalf("Prune on a missing root should succeed, got %v", err)
	}
}
