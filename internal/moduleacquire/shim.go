// Package moduleacquire loads a ModuleRelease's target module from the OCI
// registry into a library *module.Module. A ModuleRelease references its module
// by registry path and version, but the kernel's only module loader
// (Kernel.LoadModulePackage) requires a local directory. This package bridges
// the two: it writes a minimal shim CUE package whose sole dependency is the
// target module and whose single .cue file imports and embeds that module at
// the package root, loads it through the kernel, and decodes the result.
//
// Unlike internal/synthesis (which pins a catalog version and scaffolds a
// #ModuleRelease), the shim depends only on the target module — the OPM core
// schema resolves through the kernel's schema cache and catalog transformers
// are not required to acquire a module.
package moduleacquire

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// cueLanguageVersion is the CUE language version stamped into the shim module.
// It mirrors the value the published module fixtures and the legacy synthesis
// path use, and loads cleanly under the operator's CUE toolchain.
const cueLanguageVersion = "v0.16.1"

// shimModule is the CUE module path for the generated shim package. It is the
// shim's own identity and never references the catalog.
const shimModule = "opmodel.dev/controller/acquire@v0"

// shimPackage is the package name of the generated .cue file.
const shimPackage = "acquire"

// writeShim creates a temporary directory containing a CUE module that imports
// and embeds the target module at the package root. The caller must remove the
// directory when done (typically via defer os.RemoveAll).
//
// The shim contains:
//   - cue.mod/module.cue: declares exactly one dependency — the target module
//     at the given version (no catalog dependency, no catalog version pin)
//   - acquire.cue: imports the target module and embeds it at the root
func writeShim(path, version string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("module path is required")
	}
	if version == "" {
		return "", fmt.Errorf("module version is required")
	}

	tmpDir, err := os.MkdirTemp("", "opm-acquire-*")
	if err != nil {
		return "", fmt.Errorf("creating temp directory: %w", err)
	}

	if err := writeModuleFile(tmpDir, path, version); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing module file: %w", err)
	}

	if err := writePackageFile(tmpDir, path); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("writing package file: %w", err)
	}

	return tmpDir, nil
}

var moduleTmpl = template.Must(template.New("module.cue").Parse(`module: "{{ .ShimModule }}"
language: version: "{{ .CUELanguageVersion }}"
deps: {
	"{{ .ModulePath }}": v: "{{ .ModuleVersion }}"
}
`))

type moduleTemplateData struct {
	ShimModule         string
	CUELanguageVersion string
	ModulePath         string
	ModuleVersion      string
}

func writeModuleFile(dir, path, version string) error {
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
		ShimModule:         shimModule,
		CUELanguageVersion: cueLanguageVersion,
		ModulePath:         path,
		ModuleVersion:      version,
	})
}

var packageTmpl = template.Must(template.New("acquire.cue").Parse(`package {{ .Package }}

import mod "{{ .ModulePath }}"

mod
`))

type packageTemplateData struct {
	Package    string
	ModulePath string
}

func writePackageFile(dir, path string) error {
	f, err := os.Create(filepath.Join(dir, "acquire.cue"))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return packageTmpl.Execute(f, packageTemplateData{
		Package:    shimPackage,
		ModulePath: path,
	})
}
