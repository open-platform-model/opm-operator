package platformmodule

import (
	"bytes"
	"strings"
	"testing"

	"cuelang.org/go/mod/modfile"
)

const (
	opmPath = "opmodel.dev/catalogs/opm@v4"
	k8sPath = "opmodel.dev/catalogs/k8s@v1"
)

func twoCatalogInput() Input {
	return Input{
		Name: "cluster",
		Type: "kubernetes",
		Entries: []Entry{
			{Path: opmPath, Version: "4.0.1", Enable: true},
			{Path: k8sPath, Version: "1.0.0-alpha.2", Enable: false},
		},
		Deps: []Dep{
			{Path: CorePath, Version: CoreVersion},
			{Path: opmPath, Version: "v4.0.1"},
			{Path: k8sPath, Version: "v1.0.0-alpha.2"},
			{Path: "cue.dev/x/k8s.io@v0", Version: "v0.10.0"},
		},
	}
}

func TestGenerate_TwoCatalogs(t *testing.T) {
	files, err := Generate(twoCatalogInput())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected two files, got %d: %v", len(files), keys(files))
	}

	mf, err := modfile.Parse(files[ModuleFileName], ModuleFileName)
	if err != nil {
		t.Fatalf("generated module.cue does not parse: %v", err)
	}
	if mf.QualifiedModule() != ModulePath {
		t.Fatalf("module path %q, want %q", mf.QualifiedModule(), ModulePath)
	}
	if mf.Language == nil || mf.Language.Version != LanguageVersion {
		t.Fatalf("language version %+v, want %s", mf.Language, LanguageVersion)
	}
	want := map[string]string{
		CorePath:              CoreVersion,
		opmPath:               "v4.0.1",
		k8sPath:               "v1.0.0-alpha.2",
		"cue.dev/x/k8s.io@v0": "v0.10.0",
	}
	if len(mf.Deps) != len(want) {
		t.Fatalf("deps %v, want %v", mf.Deps, want)
	}
	for path, version := range want {
		dep, ok := mf.Deps[path]
		if !ok {
			t.Fatalf("module.cue does not pin %s", path)
		}
		if dep.Version != version {
			t.Fatalf("%s pinned at %s, want %s", path, dep.Version, version)
		}
		if dep.Default {
			t.Fatalf("%s carries a default-major marker; tidy writes none for a platform", path)
		}
	}

	plat := string(files[PlatformFileName])
	for _, want := range []string{
		"package platform\n",
		"\tcore \"opmodel.dev/core@v2\"\n",
		"\tcat0 \"opmodel.dev/catalogs/k8s@v1\"\n",
		"\tcat1 \"opmodel.dev/catalogs/opm@v4\"\n",
		"core.#Platform\n",
		"metadata: name: \"cluster\"\n",
		"type: \"kubernetes\"\n",
		"\t\"opmodel.dev/catalogs/k8s@v1\": {\n\t\tenable:   false\n\t\tversion:  \"1.0.0-alpha.2\"\n\t\t#catalog: cat0\n\t}\n",
		"\t\"opmodel.dev/catalogs/opm@v4\": {\n\t\tenable:   true\n\t\tversion:  \"4.0.1\"\n\t\t#catalog: cat1\n\t}\n",
	} {
		if !strings.Contains(plat, want) {
			t.Fatalf("platform.cue lacks %q:\n%s", want, plat)
		}
	}
	if n := strings.Count(plat, "#catalog:"); n != 2 {
		t.Fatalf("expected exactly two registry entries, found %d:\n%s", n, plat)
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	a, err := Generate(twoCatalogInput())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Same content, reversed input order.
	in := twoCatalogInput()
	for i, j := 0, len(in.Entries)-1; i < j; i, j = i+1, j-1 {
		in.Entries[i], in.Entries[j] = in.Entries[j], in.Entries[i]
	}
	for i, j := 0, len(in.Deps)-1; i < j; i, j = i+1, j-1 {
		in.Deps[i], in.Deps[j] = in.Deps[j], in.Deps[i]
	}
	b, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate (reordered): %v", err)
	}

	for _, name := range []string{ModuleFileName, PlatformFileName} {
		if !bytes.Equal(a[name], b[name]) {
			t.Fatalf("%s differs across input orderings:\n--- a\n%s\n--- b\n%s", name, a[name], b[name])
		}
	}
}

func TestGenerate_DisabledEntryIsKept(t *testing.T) {
	in := twoCatalogInput()
	files, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plat := string(files[PlatformFileName])
	if !strings.Contains(plat, "\tcat0 \"opmodel.dev/catalogs/k8s@v1\"\n") {
		t.Fatalf("disabled catalog is not imported:\n%s", plat)
	}
	if !strings.Contains(plat, "\t\tenable:   false\n") {
		t.Fatalf("disabled entry is not emitted with enable: false:\n%s", plat)
	}
	mf, err := modfile.Parse(files[ModuleFileName], ModuleFileName)
	if err != nil {
		t.Fatalf("module.cue: %v", err)
	}
	if _, ok := mf.Deps[k8sPath]; !ok {
		t.Fatalf("disabled catalog is not pinned in module.cue")
	}
}

func TestGenerate_StampsExpectedVersion(t *testing.T) {
	in := twoCatalogInput()
	in.Entries = in.Entries[:1]
	in.Entries[0].Version = "4.0.1"
	files, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plat := string(files[PlatformFileName])
	// The stamp is the CR's bare SemVer, never the "v"-prefixed cue.mod form:
	// it unifies with #Catalog.metadata.version, which is bare.
	if !strings.Contains(plat, "\t\tversion:  \"4.0.1\"\n") {
		t.Fatalf("entry does not stamp the CR version:\n%s", plat)
	}
	if strings.Contains(plat, "\"v4.0.1\"") {
		t.Fatalf("entry stamps the cue.mod form of the version:\n%s", plat)
	}
}

func TestGenerate_EmptyRegistry(t *testing.T) {
	files, err := Generate(Input{
		Name: "cluster",
		Type: "kubernetes",
		Deps: []Dep{{Path: CorePath, Version: CoreVersion}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	plat := string(files[PlatformFileName])
	if !strings.Contains(plat, "#registry: {}\n") {
		t.Fatalf("empty registry not emitted as an empty struct:\n%s", plat)
	}
	if strings.Contains(plat, "cat0") {
		t.Fatalf("empty registry emitted an import:\n%s", plat)
	}
}

func TestGenerate_Refusals(t *testing.T) {
	cases := map[string]func(*Input){
		"missing name":        func(in *Input) { in.Name = "" },
		"missing type":        func(in *Input) { in.Type = "" },
		"duplicate entry":     func(in *Input) { in.Entries = append(in.Entries, in.Entries[0]) },
		"entry not pinned":    func(in *Input) { in.Deps = in.Deps[:1] },
		"core not pinned":     func(in *Input) { in.Deps = in.Deps[1:] },
		"dep pinned twice":    func(in *Input) { in.Deps = append(in.Deps, Dep{Path: opmPath, Version: "v4.0.2"}) },
		"empty entry version": func(in *Input) { in.Entries[0].Version = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := twoCatalogInput()
			mutate(&in)
			if _, err := Generate(in); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

func TestRoots(t *testing.T) {
	roots := Roots([]Entry{
		{Path: opmPath, Version: "4.0.1", Enable: true},
		{Path: k8sPath, Version: "1.0.0-alpha.2", Enable: false},
	})
	want := []Dep{
		{Path: k8sPath, Version: "v1.0.0-alpha.2"},
		{Path: opmPath, Version: "v4.0.1"},
		{Path: CorePath, Version: CoreVersion},
	}
	if len(roots) != len(want) {
		t.Fatalf("roots %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots[%d] = %v, want %v (full: %v)", i, roots[i], want[i], roots)
		}
	}
}

func keys(files Files) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	return out
}
