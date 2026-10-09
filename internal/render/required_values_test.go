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

// These tests pin what each render path says about a required #config value
// the values leave unset and no component reads. Since library v1.0.0-beta.7
// the kernel refuses it in both instance verbs. On the ModuleInstance path
// the renderer's own check of spec.values runs first, so its message is the
// one a user reads; on the ModulePackage path the kernel's refusal is the
// only one.
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

// A ModuleInstance whose spec.values leave a required #config value unset is
// refused by the renderer's check of spec.values against #config, before
// synthesis: the message keeps its prefix and names the #config field and
// its position, and the failure is not an acquisition failure, so the Ready
// reason stays RenderFailed.
func TestKernelModuleRenderer_UnsetRequiredValueKeepsItsMessage(t *testing.T) {
	registry := requiredValueRegistry(t)

	// The hello fixture with one more #config field: required, without a
	// default, and read by no component.
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(helloFixtureDir)))
	extra := filepath.Join(dir, "extra.cue")
	require.NoError(t, os.WriteFile(extra, []byte("package hello\n\n#config: note: string\n"), 0o644))

	k := kernel.New(kernel.WithRegistry(registry))
	mod, err := k.AcquireModuleFromDir(context.Background(), dir)
	require.NoError(t, err)
	r := &KernelModuleRenderer{Kernel: k, Store: platformstore.NewStore(), RuntimeName: "opm-controller"}

	want := "validating values against the module's #config: " +
		"#config.note: incomplete value string (" + extra + ":3:16)"

	for name, values := range map[string]*releasesv1alpha1.RawValues{
		"no spec.values":            nil,
		"spec.values without field": rawValues(`{"message": "set"}`),
	} {
		t.Run(name, func(t *testing.T) {
			inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", values)
			require.Error(t, err)
			assert.Nil(t, inst)
			assert.Equal(t, want, err.Error())
			assert.NotErrorIs(t, err, ErrAcquire, "a values failure is not an acquisition failure")
			_, isFetch := errors.AsType[*oerrors.FetchError](err)
			assert.False(t, isFetch, "a values failure must not retry as a registry fetch failure")
		})
	}

	t.Run("spec.values with the field set", func(t *testing.T) {
		inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", rawValues(`{"note": "set"}`))
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

// eventNoteLimit is the longest note events.k8s.io/v1 accepts. A render
// failure's event note is the error text, with no cut.
const eventNoteLimit = 1024

// A ModuleInstance that leaves several required #config values unset is told
// every one of them in one message: a nested value by its full path, and a
// value a component reads like one that none reads. Since library
// v1.0.0-beta.8 the kernel's own refusal names the same values, at the same
// positions, as values.<field> where the renderer's check writes
// #config.<field>. The change that removes the renderer's check relies on
// that, so it is pinned here: a library change that makes the two reports
// name different values fails on the bump.
func TestKernelModuleRenderer_EveryUnsetRequiredValueIsNamed(t *testing.T) {
	registry := requiredValueRegistry(t)

	// The hello fixture with three more #config fields, all required and
	// without a default. Only greeting is read by a component.
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(helloFixtureDir)))
	extra := filepath.Join(dir, "extra.cue")
	require.NoError(t, os.WriteFile(extra, []byte("package hello\n\n"+
		"#config: {\n\tnote: string\n\tdb: host: string\n\tgreeting: string\n}\n"+
		"#components: hello: spec: configMaps: hello: data: greeting: #config.greeting\n"), 0o644))

	k := kernel.New(kernel.WithRegistry(registry))
	mod, err := k.AcquireModuleFromDir(context.Background(), dir)
	require.NoError(t, err)
	r := &KernelModuleRenderer{Kernel: k, Store: platformstore.NewStore(), RuntimeName: "opm-controller"}

	const prefix = "validating values against the module's #config: "
	finding := func(root, field, position string) string {
		return root + field + ": incomplete value string (" + extra + ":" + position + ")"
	}
	findings := func(root string, fields ...[2]string) string {
		parts := make([]string, 0, len(fields))
		for _, f := range fields {
			parts = append(parts, finding(root, f[0], f[1]))
		}
		return strings.Join(parts, "; ")
	}
	note, host, greeting := [2]string{"note", "4:8"}, [2]string{"db.host", "5:12"}, [2]string{"greeting", "6:12"}

	cases := []struct {
		name   string
		values string
		unset  [][2]string
	}{
		{name: "nothing set", values: `{}`, unset: [][2]string{note, host, greeting}},
		{name: "one value set", values: `{"note": "set"}`, unset: [][2]string{host, greeting}},
		{name: "only the read value unset", values: `{"note": "set", "db": {"host": "h"}}`, unset: [][2]string{greeting}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// What a user reads: the renderer's check, which runs first.
			_, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", rawValues(tc.values))
			require.Error(t, err)
			assert.Equal(t, prefix+findings("#config.", tc.unset...), err.Error())
			assert.Less(t, len(err.Error()), eventNoteLimit, "the message is the event note")
			assert.NotErrorIs(t, err, ErrAcquire)

			// What the kernel says for the same values without that check.
			src, err := k.LoadSourceFromBytes(valuesOrigin, []byte(tc.values))
			require.NoError(t, err)
			_, err = k.SynthesizeInstance(context.Background(), kernel.InstanceInput{
				Module: mod, Name: "needy", Namespace: "default", Values: []kernel.Source{src},
			})
			require.Error(t, err)
			assert.Equal(t, findings("values.", tc.unset...), cueFindings(err),
				"the kernel names the values the renderer's check names")
			assert.True(t, strings.HasPrefix(err.Error(),
				`Kernel.SynthesizeInstance: instance "needy": not fully concrete: values.`+tc.unset[0][0]+`: incomplete value string`),
				"the kernel's own text names the first value only: %s", err)
		})
	}

	t.Run("every value set", func(t *testing.T) {
		inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default",
			rawValues(`{"note": "set", "db": {"host": "h"}, "greeting": "hi"}`))
		require.NoError(t, err)
		assert.NotNil(t, inst)
	})
}
