package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
)

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

func TestLayout_WriteCreatesGenerationDirectory(t *testing.T) {
	l := Layout{Root: filepath.Join(t.TempDir(), "platform")}
	dir, err := l.Write(3, sampleFiles("gen3"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if dir != l.Dir(3) {
		t.Fatalf("Write returned %s, want %s", dir, l.Dir(3))
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

func TestLayout_WriteSameGenerationSwaps(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	if _, err := l.Write(1, sampleFiles("first")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dir, err := l.Write(1, sampleFiles("second"))
	if err != nil {
		t.Fatalf("Write (again): %v", err)
	}
	if got := readMarker(t, dir); got != "second" {
		t.Fatalf("marker %q after re-write, want second", got)
	}
	if names := listRoot(t, l.Root); len(names) != 1 {
		t.Fatalf("root holds %v after a same-generation swap, want only gen-1", names)
	}
}

func TestLayout_FailedWriteLeavesNoGenerationDirectory(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	// A path escaping the module directory is refused by the helper after
	// the staging directory exists, so the failure path must clean it up.
	_, err := l.Write(2, platformmodule.Files{
		platformmodule.PlatformFileName: []byte("package platform\n"),
		"../escape.cue":                 []byte("nope"),
	})
	if err == nil {
		t.Fatal("expected Write to refuse an escaping path")
	}
	if _, statErr := os.Stat(l.Dir(2)); !os.IsNotExist(statErr) {
		t.Fatalf("gen-2 exists after a failed write (stat: %v)", statErr)
	}
	for _, name := range listRoot(t, l.Root) {
		if strings.HasPrefix(name, generationPrefix) {
			t.Fatalf("a generation directory %s survived a failed write", name)
		}
	}
}

func TestLayout_PruneKeepsOnlyTheKeepSet(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	for _, g := range []int64{1, 2, 3} {
		if _, err := l.Write(g, sampleFiles("x")); err != nil {
			t.Fatalf("Write %d: %v", g, err)
		}
	}
	// Staging and aside leftovers from an interrupted run.
	for _, stale := range []string{stagingPrefix + "4-deadbeef", asidePrefix + "2-cafebabe"} {
		if err := os.MkdirAll(filepath.Join(l.Root, stale), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Prune(3, 2); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	gens, err := l.Generations()
	if err != nil {
		t.Fatalf("Generations: %v", err)
	}
	if len(gens) != 2 || gens[0] != 2 || gens[1] != 3 {
		t.Fatalf("generations after prune %v, want [2 3]", gens)
	}
	if names := listRoot(t, l.Root); len(names) != 2 {
		t.Fatalf("root holds %v after prune, want only gen-2 and gen-3", names)
	}
}

func TestLayout_ResetEmptiesRoot(t *testing.T) {
	l := Layout{Root: filepath.Join(t.TempDir(), "nested", "platform")}
	// Reset on a missing root creates it.
	if err := l.Reset(); err != nil {
		t.Fatalf("Reset (missing root): %v", err)
	}
	if _, err := l.Write(7, sampleFiles("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := l.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if names := listRoot(t, l.Root); len(names) != 0 {
		t.Fatalf("root holds %v after reset, want empty", names)
	}
	gens, err := l.Generations()
	if err != nil {
		t.Fatalf("Generations: %v", err)
	}
	if len(gens) != 0 {
		t.Fatalf("generations after reset %v, want none", gens)
	}
}

func TestLayout_Refusals(t *testing.T) {
	if _, err := (Layout{}).Write(1, sampleFiles("x")); err == nil {
		t.Fatal("expected Write on an empty root to fail")
	}
	if _, err := (Layout{Root: t.TempDir()}).Write(1, nil); err == nil {
		t.Fatal("expected Write with no files to fail")
	}
	if err := (Layout{}).Reset(); err == nil {
		t.Fatal("expected Reset on an empty root to fail")
	}
	if err := (Layout{Root: filepath.Join(t.TempDir(), "absent")}).Prune(); err != nil {
		t.Fatalf("Prune on a missing root should succeed, got %v", err)
	}
}
