package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cueerrors "cuelang.org/go/cue/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// These tests pin what each render path says about values that do not
// satisfy the module's #config. The kernel is the one check on both paths:
// the ModuleInstance path reports the kernel's synthesis error, the
// ModulePackage path the kernel's package load error, and both write every
// finding out with its positions, where the kernel's own text holds the first
// finding and a count.
//
// Since library v1.0.0-beta.8 the kernel names every unset required value as
// values.<field>, a value a component reads included.
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

	// The hello fixture with three more #config fields that no component
	// reads: two required, without a default, and one constrained.
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(helloFixtureDir)))
	extra := filepath.Join(dir, "extra.cue")
	require.NoError(t, os.WriteFile(extra,
		[]byte("package hello\n\n#config: note: string\n#config: other: int\n#config: port: int & >0 | *80\n"), 0o644))

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
			name:   "constraint violated",
			values: rawValues(`{"note":"n","other":1,"port":-1}`),
			want: frame + "#module.#config.port: 2 errors in empty disjunction:; " +
				"#module.#config.port: conflicting values 80 and -1 (" + extra + ":5:28, spec.values:1:1, spec.values:1:30); " +
				"#module.#config.port: invalid value -1 (out of bound >0) (" + extra + ":5:22, spec.values:1:30)",
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
			var ce cueerrors.Error
			assert.ErrorAs(t, err, &ce, "the wording keeps the kernel's error chain")
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
		`not fully concrete: values.note: incomplete value string (instance.cue:23:12)`, err.Error())
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

// A ModuleInstance that leaves several required #config values unset is told
// every one of them in one message, each as values.<field> with the position
// of its #config declaration: a nested value by its full path, and a value a
// component reads like one that none reads. The kernel's refusal is the only
// report (library v1.0.0-beta.8 names every unset value); its own text names
// the first and counts the rest, so the message is worded from its findings.
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

	const frame = `synthesizing release: Kernel.SynthesizeInstance: instance "needy": not fully concrete: `
	unset := func(field, position string) string {
		return "values." + field + ": incomplete value string (" + extra + ":" + position + ")"
	}
	note, host, greeting := unset("note", "4:8"), unset("db.host", "5:12"), unset("greeting", "6:12")

	cases := []struct {
		name   string
		values string
		unset  []string
	}{
		{name: "nothing set", values: `{}`, unset: []string{note, host, greeting}},
		{name: "one value set", values: `{"note": "set"}`, unset: []string{host, greeting}},
		{name: "only the read value unset", values: `{"note": "set", "db": {"host": "h"}}`, unset: []string{greeting}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.synthesizeFrom(context.Background(), mod, "needy", "default", rawValues(tc.values))
			require.Error(t, err)
			assert.Equal(t, frame+strings.Join(tc.unset, "; "), err.Error())
			assert.NotContains(t, err.Error(), "components.", "no finding names the place that reads a value")
			// The message fits an event note, so the note is the message.
			assert.Equal(t, err.Error(), EventNote(err))
			assert.NotErrorIs(t, err, ErrAcquire)
		})
	}

	t.Run("every value set", func(t *testing.T) {
		inst, err := r.synthesizeFrom(context.Background(), mod, "needy", "default",
			rawValues(`{"note": "set", "db": {"host": "h"}, "greeting": "hi"}`))
		require.NoError(t, err)
		assert.NotNil(t, inst)
	})
}

// Twenty unset required values on a real module: the message, which is the
// condition message, names every one; the event note stays inside the note
// limit, names whole findings from the first and counts the rest.
func TestKernelModuleRenderer_TwentyUnsetValuesFitTheEventNote(t *testing.T) {
	registry := requiredValueRegistry(t)

	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(helloFixtureDir)))
	var fields strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&fields, "#config: required%02d: string\n", i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.cue"),
		[]byte("package hello\n\n"+fields.String()), 0o644))

	k := kernel.New(kernel.WithRegistry(registry))
	mod, err := k.AcquireModuleFromDir(context.Background(), dir)
	require.NoError(t, err)
	r := &KernelModuleRenderer{Kernel: k, Store: platformstore.NewStore(), RuntimeName: "opm-controller"}

	_, err = r.synthesizeFrom(context.Background(), mod, "needy", "default", nil)
	require.Error(t, err)
	message := err.Error()
	for i := 1; i <= 20; i++ {
		assert.Contains(t, message, fmt.Sprintf("values.required%02d: incomplete value string (", i))
	}
	assert.NotContains(t, message, " more ")
	assert.Less(t, len(message), conditionMessageLimit)
	require.Greater(t, len(message), eventNoteLimit, "the case must need the cut")

	note := EventNote(err)
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	named := strings.Count(note, ": incomplete value string (")
	left := countedTail(t, note)
	assert.Equal(t, 20, named+left, "every value is named or counted: %s", note)
	assert.Positive(t, named)
	assert.True(t, strings.HasPrefix(message, strings.TrimSuffix(note, moreFindings(left))+"; "),
		"the cut is between two findings: %s", note)
}

// readingInstancePackage is needyInstancePackage with a module whose two
// required #config values have no default: greeting, which the component
// reads, and note, which nothing reads.
func readingInstancePackage(t *testing.T, valuesCUE string) string {
	t.Helper()
	dir := needyInstancePackage(t, valuesCUE)
	file := filepath.Join(dir, "instance.cue")
	pkg, err := os.ReadFile(file)
	require.NoError(t, err)
	edited := strings.NewReplacer(
		"message: string | *\"hello\"", "greeting: string",
		"data: message: #config.message", "data: message: #config.greeting",
	).Replace(string(pkg))
	require.NotEqual(t, string(pkg), edited, "the package template changed under this helper")
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644))
	return dir
}

// A ModulePackage that leaves unset a required #config value a component
// reads is told the value's name, and every other unset value with it, each
// as values.<field> with the position of its #config declaration in the
// package. Until library v1.0.0-beta.8 the kernel named the place that read
// the value instead (components.hello.spec.configMaps.hello.data.message).
// The kernel's own text names the first unset value and counts the rest, so
// the message is worded from its findings, as on the ModuleInstance path.
func TestKernelPackageRenderer_EveryUnsetRequiredValueIsNamed(t *testing.T) {
	registry := requiredValueRegistry(t)
	r := &KernelPackageRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(registry)),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}
	const frame = `loading package: Kernel.AcquireInstanceFromDir: instance "needy": not fully concrete: `

	cases := []struct {
		name   string
		values string
		want   string
	}{
		{name: "only the read value unset", values: `note: "set"`,
			want: frame + `values.greeting: incomplete value string (instance.cue:22:13)`},
		{name: "both unset", values: `{}`,
			want: frame + `values.greeting: incomplete value string (instance.cue:22:13); ` +
				`values.note: incomplete value string (instance.cue:23:12)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, result, err := r.Render(context.Background(), readingInstancePackage(t, tc.values))
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Equal(t, tc.want, err.Error())
			assert.Equal(t, tc.want, EventNote(err))
			assert.ErrorIs(t, err, ErrAcquire)
			_, isFetch := errors.AsType[*oerrors.FetchError](err)
			assert.False(t, isFetch, "an unset value must not retry as a registry fetch failure")
		})
	}

	_, _, err := r.Render(context.Background(), readingInstancePackage(t, `{greeting: "hi", note: "set"}`))
	assert.ErrorIs(t, err, ErrPlatformNotReady)
}

// constrainedInstancePackage is needyInstancePackage with a module whose
// #config holds greeting (required, read by the component), db.host
// (required, nested), port (constrained, with a default) and note (required).
func constrainedInstancePackage(t *testing.T, valuesCUE string) string {
	t.Helper()
	dir := needyInstancePackage(t, valuesCUE)
	file := filepath.Join(dir, "instance.cue")
	pkg, err := os.ReadFile(file)
	require.NoError(t, err)
	edited := strings.NewReplacer(
		"message: string | *\"hello\"", "greeting: string\n\t\tdb: host: string\n\t\tport: int & >0 | *80",
		"data: message: #config.message", "data: message: #config.greeting",
	).Replace(string(pkg))
	require.NotEqual(t, string(pkg), edited, "the package template changed under this helper")
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644))
	return dir
}

// A ModulePackage whose values do not satisfy the module's #config is told
// every finding with its positions, where the kernel's own text holds the
// first finding, a count and no position. A position in the package is
// relative to the package's CUE module root: the operator extracts a package
// to a new temporary directory on every reconcile, and the message of an
// unchanged package must not change with it.
func TestKernelPackageRenderer_ValuesFailureListsEveryFinding(t *testing.T) {
	registry := requiredValueRegistry(t)
	r := &KernelPackageRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(registry)),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}
	const frame = `loading package: Kernel.AcquireInstanceFromDir: instance "needy": `

	for _, tc := range []struct {
		name   string
		values string
		want   string
	}{
		{
			name:   "three unset, one read, one nested",
			values: `{}`,
			want: frame + "not fully concrete: values.greeting: incomplete value string (instance.cue:22:13); " +
				"values.db.host: incomplete value string (instance.cue:23:13); " +
				"values.note: incomplete value string (instance.cue:25:12)",
		},
		{
			name:   "wrong type",
			values: `{note: 7, db: host: "h", greeting: "g"}`,
			want: frame + "#module.#config.note: conflicting values string and 7 " +
				"(mismatched types string and int) (instance.cue:25:12, instance.cue:35:16)",
		},
		{
			name:   "constraint violated",
			values: `{note: "n", db: host: "h", greeting: "g", port: -1}`,
			want: frame + "#module.#config.port: 2 errors in empty disjunction:; " +
				"#module.#config.port: conflicting values 80 and -1 (instance.cue:24:21, instance.cue:35:57); " +
				"#module.#config.port: invalid value -1 (out of bound >0) (instance.cue:24:15, instance.cue:35:57)",
		},
		{
			name:   "field not allowed",
			values: `{note: "n", db: host: "h", greeting: "g", bogus: true}`,
			want:   frame + "field not allowed (instance.cue:35:51)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := constrainedInstancePackage(t, tc.values)
			_, result, err := r.Render(context.Background(), first)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Equal(t, tc.want, err.Error())
			assert.NotContains(t, err.Error(), "more errors", "every finding is written out")
			assert.NotContains(t, err.Error(), first, "no position names the extraction directory")
			var ce cueerrors.Error
			assert.ErrorAs(t, err, &ce, "the wording keeps the kernel's error chain")
			assert.ErrorIs(t, err, ErrAcquire)

			// The same package in another directory reads the same.
			_, _, again := r.Render(context.Background(), constrainedInstancePackage(t, tc.values))
			require.Error(t, again)
			assert.Equal(t, err.Error(), again.Error())
		})
	}
}

// Twenty-five unset required values through the real package load: the
// message names every one, and the event note of the error Render returns is
// cut to whole findings and a count. Nothing between the kernel and the
// returned error may hide the findings from EventNote.
func TestKernelPackageRenderer_ManyUnsetValuesFitTheEventNote(t *testing.T) {
	registry := requiredValueRegistry(t)
	r := &KernelPackageRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(registry)),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}

	dir := needyInstancePackage(t, `{}`)
	file := filepath.Join(dir, "instance.cue")
	pkg, err := os.ReadFile(file)
	require.NoError(t, err)
	var fields strings.Builder
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&fields, "\n\t\trequiredValueNumber%02d: string", i)
	}
	edited := strings.Replace(string(pkg), "note:    string\n", "note:    string"+fields.String()+"\n", 1)
	require.NotEqual(t, string(pkg), edited, "the package template changed under this test")
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644))

	_, _, err = r.Render(context.Background(), dir)
	require.Error(t, err)
	message := err.Error()
	for i := 1; i <= 25; i++ {
		assert.Contains(t, message, fmt.Sprintf("values.requiredValueNumber%02d: incomplete value string (instance.cue:", i))
	}
	assert.NotContains(t, message, " more ")
	require.Greater(t, len(message), eventNoteLimit, "the case must need the cut")

	note := EventNote(err)
	assert.LessOrEqual(t, len(note), eventNoteLimit)
	named := strings.Count(note, ": incomplete value string (")
	left := countedTail(t, note)
	assert.Equal(t, 26, named+left, "note and the twenty-five are named or counted: %s", note)
	assert.Positive(t, named)
	assert.True(t, strings.HasPrefix(message, strings.TrimSuffix(note, moreFindings(left))+"; "),
		"the cut is between two findings: %s", note)
}
