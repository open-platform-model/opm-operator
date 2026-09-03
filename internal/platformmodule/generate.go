package platformmodule

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"cuelang.org/go/mod/modfile"
)

const (
	// ModulePath is the generated module's own identity: the reserved,
	// never-published platform namespace (0019 D6). Fixed rather than derived
	// per generation so generated files are byte-stable across generations
	// of the same spec, and distinct from every instance module path the
	// render build could pair it with.
	ModulePath = "opmodel.dev/platforms/cluster@v0"

	// CorePath is the major-qualified module path of the core schema the
	// generated module embeds.
	CorePath = "opmodel.dev/core@v2"

	// CoreVersion is the core build the generated module pins. It is the
	// operator's compiled-in answer to "which core does the platform run"
	// (design: the core pin comes from an operator constant): deterministic
	// per operator build and stated in the module for the render build's
	// promotion to read (0019 D13). Must name a D5-shaped core (registry
	// entries carrying the catalog by import: 2.0.0-alpha.7 or later).
	// Bumped by the workspace-root pin tooling (`.tasks/deps/platform-pins.sh`
	// beside this repo, run by `task deps:update` there) alongside the other
	// pins that live outside a cue.mod.
	CoreVersion = "v2.0.0-alpha.7"

	// LanguageVersion is the generated module's declared CUE language
	// version: the floor every published first-party module declares and the
	// render build requires for cue.mod/local-module.cue.
	LanguageVersion = "v0.17.0"

	// ModuleFileName and PlatformFileName are the two files a generated
	// module consists of, relative to the module directory.
	ModuleFileName   = "cue.mod/module.cue"
	PlatformFileName = "platform.cue"
)

// Entry is one catalog subscription of the CR, in the shape Generate
// consumes: the major-qualified catalog path (the CR's registry key), the
// bare SemVer build the CR names (the value of spec.registry[path].version)
// and whether the subscription is enabled (the CR's nil pointer resolved to
// the schema default, true).
type Entry struct {
	Path    string
	Version string
	Enable  bool
}

// Dep is one pinned dependency of the generated cue.mod: a major-qualified
// module path and a canonical "v"-prefixed version.
type Dep struct {
	Path    string
	Version string
}

// Input is everything Generate needs. Name and Type come from the CR
// (metadata.name and spec.type); Entries from spec.registry; Deps is the
// resolved dependency closure (see Closure), which MUST contain a pin for
// core and for every entry's catalog.
type Input struct {
	Name    string
	Type    string
	Entries []Entry
	Deps    []Dep
}

// Files maps a path relative to the module directory to the file's bytes.
type Files map[string][]byte

// Roots returns the dependency roots the closure is derived from: the core
// pin plus every entry's catalog, disabled entries included (a disabled
// entry still imports its catalog). Versions are canonicalised with the "v"
// prefix cue.mod requires; the CR stores bare SemVer.
func Roots(entries []Entry) []Dep {
	roots := make([]Dep, 0, len(entries)+1)
	roots = append(roots, Dep{Path: CorePath, Version: CoreVersion})
	for _, e := range entries {
		roots = append(roots, Dep{Path: e.Path, Version: canonicalVersion(e.Version)})
	}
	sortDeps(roots)
	return roots
}

// Generate renders the module's two files from in. It is pure and
// deterministic: entries and dependencies are emitted in sorted path order
// whatever order they arrive in, so the same CR generation always produces
// byte-identical content. Each registry entry stamps the CR's version as the
// entry's expected `version`, which unifies with the schema's readout of the
// imported catalog so wrong bytes are a build conflict naming the entry
// (0019 D13 tripwire).
func Generate(in Input) (Files, error) {
	if in.Name == "" {
		return nil, errors.New("platform name is required")
	}
	if in.Type == "" {
		return nil, errors.New("platform type is required")
	}

	entries := append([]Entry(nil), in.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	for i := 1; i < len(entries); i++ {
		if entries[i].Path == entries[i-1].Path {
			return nil, fmt.Errorf("duplicate registry entry %q", entries[i].Path)
		}
	}

	pinned := make(map[string]string, len(in.Deps))
	for _, d := range in.Deps {
		if d.Path == "" || d.Version == "" {
			return nil, fmt.Errorf("dependency %q has an empty path or version", d.Path)
		}
		if prev, dup := pinned[d.Path]; dup && prev != d.Version {
			return nil, fmt.Errorf("dependency %q pinned twice (%s and %s)", d.Path, prev, d.Version)
		}
		pinned[d.Path] = d.Version
	}
	if _, ok := pinned[CorePath]; !ok {
		return nil, fmt.Errorf("dependency closure does not pin %s", CorePath)
	}
	for _, e := range entries {
		if e.Path == "" || e.Version == "" {
			return nil, fmt.Errorf("registry entry %q has an empty path or version", e.Path)
		}
		if _, ok := pinned[e.Path]; !ok {
			return nil, fmt.Errorf("dependency closure does not pin registry entry %s", e.Path)
		}
	}

	moduleFile, err := renderModuleFile(pinned)
	if err != nil {
		return nil, err
	}
	return Files{
		ModuleFileName:   moduleFile,
		PlatformFileName: renderPlatformFile(in.Name, in.Type, entries),
	}, nil
}

// renderModuleFile emits cue.mod/module.cue in modfile's canonical format,
// which sorts dependencies by path. No dependency carries a default-major
// marker: the platform imports nothing unqualified, and this matches what
// `cue mod tidy` writes for the same roots (measured, design § closure).
func renderModuleFile(pinned map[string]string) ([]byte, error) {
	f := &modfile.File{
		Module:   ModulePath,
		Language: &modfile.Language{Version: LanguageVersion},
		Deps:     make(map[string]*modfile.Dep, len(pinned)),
	}
	for path, version := range pinned {
		f.Deps[path] = &modfile.Dep{Version: version}
	}
	data, err := modfile.Format(f)
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", ModuleFileName, err)
	}
	return data, nil
}

// renderPlatformFile emits platform.cue: the core.#Platform embedding, one
// unqualified catalog import per entry under a positional alias (cat<N>, so
// two catalogs sharing a last path element cannot collide), and one
// #registry entry per subscription carrying enable, the stamped expected
// version and the imported catalog. CUE names an unqualified import after
// the path's last element, the convention both first-party catalogs follow;
// a catalog whose root package deviates fails the build naming the import.
func renderPlatformFile(name, typ string, entries []Entry) []byte {
	var b bytes.Buffer
	b.WriteString("// Generated by opm-operator from the cluster Platform. Never edit, never publish.\n")
	b.WriteString("package platform\n\n")
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\tcore %s\n", quote(CorePath))
	for i, e := range entries {
		fmt.Fprintf(&b, "\tcat%d %s\n", i, quote(e.Path))
	}
	b.WriteString(")\n\n")
	b.WriteString("core.#Platform\n\n")
	fmt.Fprintf(&b, "metadata: name: %s\n", quote(name))
	fmt.Fprintf(&b, "type: %s\n\n", quote(typ))
	b.WriteString("#registry: {")
	if len(entries) == 0 {
		b.WriteString("}\n")
		return b.Bytes()
	}
	b.WriteString("\n")
	for i, e := range entries {
		fmt.Fprintf(&b, "\t%s: {\n", quote(e.Path))
		fmt.Fprintf(&b, "\t\tenable:   %t\n", e.Enable)
		fmt.Fprintf(&b, "\t\tversion:  %s\n", quote(e.Version))
		fmt.Fprintf(&b, "\t\t#catalog: cat%d\n", i)
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// quote renders s as a CUE string literal. Every value quoted here is a
// module path, a SemVer string or a DNS-style name, none of which contains
// a quote or a backslash; the escape is defensive.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// canonicalVersion adds the "v" prefix cue.mod requires to a bare SemVer
// string; an already-prefixed version is returned unchanged.
func canonicalVersion(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func sortDeps(deps []Dep) {
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })
}
