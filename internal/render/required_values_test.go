package render

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// These tests pin what each render path says about values that do not
// satisfy the module's #config. The kernel is the one check on both paths
// (it refuses an unset required value since library v1.0.0-beta.7): the
// ModuleInstance path reports the kernel's synthesis error with every finding
// and its positions written out, the ModulePackage path the kernel's package
// load error as it is.
//
// Both build a module in a temporary directory, so they need opmodel.dev/core
// and the opm catalog from CUE_REGISTRY (GHCR under `task dev:test`).

// helloFixtureDir is the hello module fixture, the base of both test modules.
var helloFixtureDir = filepath.Join("..", "..", "test", "fixtures", "modules", "hello")

// requiredValueRegistry returns the CUE_REGISTRY mapping, skipping the test
// when there is none. Under OPM_TEST_REGISTRY_FORCE=1 (PR CI) a missing
// mapping fails instead.
func requiredValueRegistry(t *testing.T) string {
	t.Helper()
	reg := os.Getenv("CUE_REGISTRY")
	if reg != "" {
		return reg
	}
	if os.Getenv("OPM_TEST_REGISTRY_FORCE") == "1" {
		t.Fatal("OPM_TEST_REGISTRY_FORCE=1 but CUE_REGISTRY is not set")
	}
	t.Skip("CUE_REGISTRY not set: core and the catalog cannot resolve")
	return ""
}

// rawValues returns doc as a ModuleInstance's spec.values.
func rawValues(doc string) *releasesv1alpha1.RawValues {
	v := &releasesv1alpha1.RawValues{}
	v.Raw = []byte(doc)
	return v
}

// A ModuleInstance whose spec.values do not satisfy the module's #config is
// refused by instance synthesis, the only check of the values. The message is
// the kernel's error with every finding and its positions written out: no
// finding is shortened to a count, and a finding caused by a value names its
// position in spec.values. The failure is not an acquisition failure, so the
// Ready reason stays RenderFailed.
func TestKernelModuleRenderer_ValuesFailureIsTheKernels(t *testing.T) {
	registry := requiredValueRegistry(t)

	// The hello fixture with two more #config fields: required, without a
	// default, and read by no component.
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(helloFixtureDir)))
	extra := filepath.Join(dir, "extra.cue")
	require.NoError(t, os.WriteFile(extra,
		[]byte("package hello\n\n#config: note: string\n#config: other: int\n"), 0o644))

	k := kernel.New(kernel.WithRegistry(registry))
	mod, err := k.AcquireModuleFromDir(context.Background(), dir)
	require.NoError(t, err)
	r := &KernelModuleRenderer{Kernel: k, Store: platformstore.NewStore(), RuntimeName: "opm-controller"}

	const frame = `synthesizing release: Kernel.SynthesizeInstance: instance "needy": `
	noteUnset := "values.note: incomplete value string (" + extra + ":3:16)"
	otherUnset := "values.other: incomplete value int (" + extra + ":4:17)"

	for _, tc := range []struct {
		name   string
		values *releasesv1alpha1.RawValues
		// want is the whole message; contains are substrings, for a message
		// whose positions inside the fixture's own files are not pinned here.
		want     string
		contains []string
	}{
		{
			name:   "no spec.values",
			values: nil,
			want:   frame + "not fully concrete: " + noteUnset + "; " + otherUnset,
		},
		{
			name:   "one required value unset",
			values: rawValues(`{"other": 1}`),
			want:   frame + "not fully concrete: " + noteUnset,
		},
		{
			name:   "two required values unset",
			values: rawValues(`{"message": "set"}`),
			want:   frame + "not fully concrete: " + noteUnset + "; " + otherUnset,
		},
		{
			name:   "wrong type no component reads",
			values: rawValues(`{"note":7,"other":1}`),
			want: frame + "#module.#config.note: conflicting values string and 7 " +
				"(mismatched types string and int) (" + extra + ":3:16, spec.values:1:1, spec.values:1:9)",
		},
		{
			name:   "two wrong types",
			values: rawValues(`{"note":7,"other":"x"}`),
			want: frame + "#module.#config.note: conflicting values string and 7 " +
				"(mismatched types string and int) (" + extra + ":3:16, spec.values:1:1, spec.values:1:9); " +
				`#module.#config.other: conflicting values int and "x" ` +
				"(mismatched types int and string) (" + extra + ":4:17, spec.values:1:1, spec.values:1:19)",
		},
		{
			name:   "wrong type a component reads",
			values: rawValues(`{"note":"n","other":1,"message":42}`),
			contains: []string{
				frame + "#module.#config.message: ",
				"conflicting values 42 and string (mismatched types int and string)",
				"spec.values:1:33",
			},
		},
		{
			name:   "field not allowed",
			values: rawValues(`{"note":"n","other":1,"bogus":true}`),
			want:   frame + "field not allowed (spec.values:1:23)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", tc.values)
			require.Error(t, err)
			assert.Nil(t, inst)
			if tc.want != "" {
				assert.Equal(t, tc.want, err.Error())
			}
			for _, sub := range tc.contains {
				assert.Contains(t, err.Error(), sub)
			}
			assert.NotContains(t, err.Error(), "more errors", "every finding is written out")
			assert.NotErrorIs(t, err, ErrAcquire, "a values failure is not an acquisition failure")
			_, isFetch := errors.AsType[*oerrors.FetchError](err)
			assert.False(t, isFetch, "a values failure must not retry as a registry fetch failure")
		})
	}

	t.Run("every required value set", func(t *testing.T) {
		inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", rawValues(`{"note":"n","other":1}`))
		require.NoError(t, err)
		assert.NotNil(t, inst)
	})
}

// needyInstancePackage writes an authored #ModuleInstance package whose
// module declares two #config values that no component reads, one required
// (note) and one with a default (tier), and whose values are valuesCUE. Its dependencies are the hello fixture's, so
// the core and catalog pins follow the fixtures.
func needyInstancePackage(t *testing.T, valuesCUE string) string {
	t.Helper()
	dir := t.TempDir()

	modFile, err := os.ReadFile(filepath.Join(helloFixtureDir, "cue.mod", "module.cue"))
	require.NoError(t, err)
	first, rest, ok := strings.Cut(string(modFile), "\n")
	require.True(t, ok && strings.HasPrefix(first, "module: "), "the fixture's module file starts with its module line")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte("module: \"test.example/releases/needy@v0\"\n"+rest), 0o644))

	pkg := `package instance

import (
	core "opmodel.dev/core@v2"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
)

core.#ModuleInstance

metadata: {
	name:      "needy"
	namespace: "default"
}

#module: core.#Module & {
	metadata: {
		name:       "needy"
		modulePath: "test.example/modules/needy@v0"
		version:    "0.0.1"
	}
	#config: {
		message: string | *"hello"
		note:    string
		tier:    string | *"a"
	}
	#components: hello: {
		res.#ConfigMaps
		metadata: name: "hello"
		spec: configMaps: hello: data: message: #config.message
	}
}

values: ` + valuesCUE + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instance.cue"), []byte(pkg), 0o644))
	return dir
}

// A ModulePackage whose values leave a required #config value unset is
// refused when the package loads, as an acquisition failure that no retry
// cures: the Ready reason is ResolutionFailed and the object stalls. Library
// v1.0.0-beta.6 loaded such a package and rendered it.
func TestKernelPackageRenderer_UnsetRequiredValueIsRefused(t *testing.T) {
	registry := requiredValueRegistry(t)
	r := &KernelPackageRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(registry)),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}

	kind, result, err := r.Render(context.Background(), needyInstancePackage(t, `message: "set"`))
	require.Error(t, err)
	assert.Equal(t, KindModuleInstance, kind)
	assert.Nil(t, result)
	assert.Equal(t, `loading package: Kernel.AcquireInstanceFromDir: instance "needy": `+
		`not fully concrete: values.note: incomplete value string`, err.Error())
	assert.ErrorIs(t, err, ErrAcquire)
	_, isFetch := errors.AsType[*oerrors.FetchError](err)
	assert.False(t, isFetch, "an unset value must not retry as a registry fetch failure")

	// A value no component reads that carries a default other than the
	// #config default is not concrete once the two unify, so the kernel
	// refuses it the same way.
	_, _, err = r.Render(context.Background(),
		needyInstancePackage(t, `{note: "set", tier: string | *"b"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `loading package: Kernel.AcquireInstanceFromDir: instance "needy": `+
		`not fully concrete: values.tier: incomplete value `)
	assert.ErrorIs(t, err, ErrAcquire)

	// With the value set the package loads; the empty store then stops the
	// render at the platform gate, after the load.
	_, _, err = r.Render(context.Background(), needyInstancePackage(t, `note: "set"`))
	assert.ErrorIs(t, err, ErrPlatformNotReady)
}

// A package that imports a path no module of its build provides fails its
// load with the library's typed author-defect resolution failure
// (*oerrors.ResolutionError, library v1.0.0-beta.7). It is not a registry
// fetch failure, so it does not retry on the backoff, and its message is the
// cause's, unchanged.
func TestKernelPackageRenderer_UnprovidedImportIsNotAFetchFailure(t *testing.T) {
	dir := writeInstancePackage(t, KindModuleInstance)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "imports.cue"),
		[]byte("package instance\n\nimport \"test.example/absent/pkg\"\n\nabsent: pkg.x\n"), 0o644))

	_, result, err := newPackageRenderer().Render(context.Background(), dir)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, ErrAcquire)
	_, isFetch := errors.AsType[*oerrors.FetchError](err)
	assert.False(t, isFetch, "an unprovided import must not classify as a registry fetch failure: %v", err)
	re, isResolution := errors.AsType[*oerrors.ResolutionError](err)
	require.True(t, isResolution, "want a *ResolutionError, got %T: %v", err, err)
	assert.Equal(t, oerrors.ResolutionImportUnprovided, re.Kind)
	assert.True(t, strings.HasPrefix(err.Error(), "loading package: "), "message: %s", err)
	assert.Contains(t, err.Error(), "cannot find module providing package test.example/absent/pkg")
}
