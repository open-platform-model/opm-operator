// Package synthesis generates temporary CUE modules that synthesize a
// #ModuleRelease from a target module. The synthesized package imports the
// target module via CUE's native OCI module resolution, producing the same
// result as a hand-written release.cue in the CLI workflow.
package synthesis

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// CatalogVersion is the pinned catalog version the controller was built against.
// Update when releasing a new controller version.
const CatalogVersion = "v1.3.2"

// CUELanguageVersion is the CUE language version for synthesized modules.
const CUELanguageVersion = "v0.16.1"

// SynthesisModule is the CUE module path for synthesized release packages.
const SynthesisModule = "opmodel.dev/controller/release@v0"

// ReleaseParams holds the inputs for synthesizing a release CUE package.
type ReleaseParams struct {
	// Name is the ModuleRelease CR metadata.name.
	Name string

	// Namespace is the ModuleRelease CR metadata.namespace.
	Namespace string

	// ModulePath is the CUE module import path (e.g. "opmodel.dev/modules/cert_manager@v0").
	ModulePath string

	// ModuleVersion is the pinned version (e.g. "v0.2.1").
	ModuleVersion string
}

// SynthesizeRelease creates a temporary directory containing a CUE module
// that synthesizes a #ModuleRelease for the given parameters. The caller
// must remove the directory when done (typically via defer os.RemoveAll).
//
// The synthesized module contains:
//   - cue.mod/module.cue: declares dependencies on the catalog and target module
//   - release.cue: imports #ModuleRelease and the target module, sets metadata
func SynthesizeRelease(params ReleaseParams) (string, error) {
	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if params.Namespace == "" {
		return "", fmt.Errorf("namespace is required")
	}
	if params.ModulePath == "" {
		return "", fmt.Errorf("module path is required")
	}
	if params.ModuleVersion == "" {
		return "", fmt.Errorf("module version is required")
	}

	tmpDir, err := os.MkdirTemp("", "opm-release-*")
	if err != nil {
		return "", fmt.Errorf("creating temp directory: %w", err)
	}

	if err := writeModuleFile(tmpDir, params); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing module file: %w", err)
	}

	if err := writeReleaseFile(tmpDir, params); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing release file: %w", err)
	}

	return tmpDir, nil
}

var moduleTmpl = template.Must(template.New("module.cue").Parse(`module: "{{ .SynthesisModule }}"
language: version: "{{ .CUELanguageVersion }}"
deps: {
	"opmodel.dev/core/v1alpha1@v1": v: "{{ .CatalogVersion }}"
	"{{ .ModulePath }}": v: "{{ .ModuleVersion }}"
}
`))

type moduleTemplateData struct {
	SynthesisModule    string
	CUELanguageVersion string
	CatalogVersion     string
	ModulePath         string
	ModuleVersion      string
}

func writeModuleFile(dir string, params ReleaseParams) error {
	modDir := filepath.Join(dir, "cue.mod")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(modDir, "module.cue"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return moduleTmpl.Execute(f, moduleTemplateData{
		SynthesisModule:    SynthesisModule,
		CUELanguageVersion: CUELanguageVersion,
		CatalogVersion:     CatalogVersion,
		ModulePath:         params.ModulePath,
		ModuleVersion:      params.ModuleVersion,
	})
}

var releaseTmpl = template.Must(template.New("release.cue").Parse(`package release

import (
	mr "opmodel.dev/core/v1alpha1/modulerelease@v1"
	mod "{{ .ModulePath }}"
)

mr.#ModuleRelease

metadata: {
	name:      "{{ .Name }}"
	namespace: "{{ .Namespace }}"
}

#module: mod
`))

type releaseTemplateData struct {
	ModulePath string
	Name       string
	Namespace  string
}

func writeReleaseFile(dir string, params ReleaseParams) error {
	f, err := os.Create(filepath.Join(dir, "release.cue"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return releaseTmpl.Execute(f, releaseTemplateData{
		ModulePath: params.ModulePath,
		Name:       params.Name,
		Namespace:  params.Namespace,
	})
}
