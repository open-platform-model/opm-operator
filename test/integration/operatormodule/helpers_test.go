/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package operatormodule_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/mod/semver"
	"sigs.k8s.io/yaml"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"

	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
)

const (
	// repoRoot is the repository root, relative to this package.
	repoRoot = "../../.."

	// The only instance coordinates the module renders for.
	instanceName      = "opm-operator"
	instanceNamespace = "opm-operator-system"

	// catalogPath is the catalog the module pins; the generated platform
	// subscribes to exactly the build the module's cue.mod names.
	catalogPath = "opmodel.dev/catalogs/opm@v4"

	runtimeName = "operator-module-test"
)

var (
	moduleDir      = filepath.Join(repoRoot, "modules", "opm_operator")
	minVersionFile = filepath.Join(repoRoot, "hack", "operator-module", "min-operator-version")
)

// registrySkip skips the current spec for a missing prerequisite, unless
// OPM_TEST_REGISTRY_FORCE=1 (PR CI), where it fails instead.
func registrySkip(msg string) {
	if os.Getenv("OPM_TEST_REGISTRY_FORCE") == "1" {
		Fail("OPM_TEST_REGISTRY_FORCE=1 but a prerequisite is missing: " + msg)
	}
	Skip(msg)
}

// renderEnv is the kernel and the platform generated from the module's own
// catalog pin, built once for the suite.
type renderEnv struct {
	kernel   *kernel.Kernel
	platform *platform.Platform
}

var (
	envOnce    sync.Once
	sharedEnv  *renderEnv
	envSkipMsg string
)

// moduleRenderEnv returns the shared render environment, skipping the spec
// (failing under OPM_TEST_REGISTRY_FORCE=1) when the registry cannot serve the
// module's pins.
func moduleRenderEnv() *renderEnv {
	GinkgoHelper()
	envOnce.Do(func() {
		reg := os.Getenv("CUE_REGISTRY")
		if reg == "" || !strings.Contains(reg, "opmodel.dev") {
			envSkipMsg = "CUE_REGISTRY maps no opmodel.dev domain; the module's core and catalog pins cannot resolve"
			return
		}
		catalogVersion, err := moduleCatalogPin(moduleDir)
		if err != nil {
			envSkipMsg = err.Error()
			return
		}
		src, err := platformmodule.NewRegistry(platformmodule.RegistryConfig{Registry: reg, Env: os.Environ()})
		if err != nil {
			envSkipMsg = "configuring the registry module source: " + err.Error()
			return
		}
		entries := []platformmodule.Entry{{Path: catalogPath, Version: catalogVersion, Enable: true}}
		deps, err := platformmodule.Closure(ctx, src, platformmodule.Roots(entries))
		if err != nil {
			envSkipMsg = "the module's catalog pin is not resolvable from CUE_REGISTRY: " + err.Error()
			return
		}
		files, err := platformmodule.Generate(platformmodule.Input{
			Name:       "cluster",
			Type:       "kubernetes",
			ModulePath: opmcontroller.PlatformModulePath,
			Entries:    entries,
			Deps:       deps,
		})
		if err != nil {
			envSkipMsg = "generating the platform module: " + err.Error()
			return
		}
		dir, err := os.MkdirTemp("", "operatormodule-platform-")
		if err != nil {
			envSkipMsg = err.Error()
			return
		}
		if err := files.WriteTo(dir); err != nil {
			envSkipMsg = "writing the platform module: " + err.Error()
			return
		}
		k := kernel.New(kernel.WithRegistry(reg))
		plat, err := k.AcquirePlatformFromDir(ctx, dir)
		if err != nil {
			envSkipMsg = "building the generated platform module: " + err.Error()
			return
		}
		sharedEnv = &renderEnv{kernel: k, platform: plat}
	})
	if sharedEnv == nil {
		registrySkip(envSkipMsg)
	}
	return sharedEnv
}

// moduleCatalogPin reads the catalog build the module's cue.mod pins.
func moduleCatalogPin(dir string) (string, error) {
	v, err := compileFile(filepath.Join(dir, "cue.mod", "module.cue"))
	if err != nil {
		return "", err
	}
	pin, err := v.LookupPath(cue.MakePath(cue.Str("deps"), cue.Str(catalogPath), cue.Str("v"))).String()
	if err != nil {
		return "", fmt.Errorf("the module's cue.mod pins no %s: %w", catalogPath, err)
	}
	return pin, nil
}

// compileFile evaluates one self-contained CUE file (no imports), the way
// `cue eval` reads it.
func compileFile(path string) (cue.Value, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return cue.Value{}, err
	}
	v := cuecontext.New().CompileBytes(b, cue.Filename(path))
	return v, v.Err()
}

// obj is one rendered object with the component and transformer that
// produced it.
type obj struct {
	Component   string
	Transformer string
	Object      map[string]any
}

func (o obj) kind() string      { return str(o.Object, "kind") }
func (o obj) name() string      { return str(o.Object, "metadata", "name") }
func (o obj) namespace() string { return str(o.Object, "metadata", "namespace") }

// render synthesizes the module at dir as the instance name/namespace with
// the given values (a CUE struct; "{}" for the defaults) and renders it. A
// refusal is returned as the error, for the refusal specs.
func render(dir, name, namespace, values string) ([]obj, error) {
	GinkgoHelper()
	env := moduleRenderEnv()
	abs, err := filepath.Abs(dir)
	Expect(err).NotTo(HaveOccurred())
	mod, err := env.kernel.AcquireModuleFromDir(ctx, abs)
	if err != nil {
		return nil, err
	}
	vals, err := env.kernel.LoadSourceFromBytes("values.cue", []byte(values+"\n"))
	if err != nil {
		return nil, err
	}
	inst, err := env.kernel.SynthesizeInstance(ctx, kernel.InstanceInput{
		Module: mod, Name: name, Namespace: namespace, Values: []kernel.Source{vals},
	})
	if err != nil {
		return nil, err
	}
	res, err := env.kernel.Render(ctx, kernel.RenderInput{
		Instance: inst, Platform: env.platform, RuntimeName: runtimeName,
	})
	if err != nil {
		return nil, err
	}
	out := make([]obj, 0, len(res.Compiled))
	for _, c := range res.Compiled {
		b, err := c.Value.MarshalJSON()
		Expect(err).NotTo(HaveOccurred())
		var m map[string]any
		Expect(json.Unmarshal(b, &m)).To(Succeed())
		out = append(out, obj{Component: c.Component, Transformer: c.Transformer, Object: m})
	}
	return out, nil
}

// mustRender renders the module with default values for its fixed instance
// and fails the spec on any error.
func mustRender(dir string) []obj {
	GinkgoHelper()
	objs, err := render(dir, instanceName, instanceNamespace, "{}")
	Expect(err).NotTo(HaveOccurred())
	return objs
}

// find returns the one rendered object of kind and name, failing otherwise.
func find(objs []obj, kind, name string) obj {
	GinkgoHelper()
	var found []obj
	for _, o := range objs {
		if o.kind() == kind && o.name() == name {
			found = append(found, o)
		}
	}
	Expect(found).To(HaveLen(1), "exactly one %s %s is rendered", kind, name)
	return found[0]
}

// managerContainer returns the Deployment's manager container.
func managerContainer(objs []obj) map[string]any {
	GinkgoHelper()
	d := find(objs, "Deployment", "opm-operator-controller-manager")
	for _, c := range list(d.Object, "spec", "template", "spec", "containers") {
		if m, ok := c.(map[string]any); ok && m["name"] == "manager" {
			return m
		}
	}
	Fail("the Deployment has no manager container")
	return nil
}

// moduleOperator is the operator release the module names, read from
// operator/operator.cue without a render.
type moduleOperator struct {
	Version    string
	Repository string
	Tag        string
	Digest     string
}

func readModuleOperator(dir string) (moduleOperator, error) {
	v, err := compileFile(filepath.Join(dir, "operator", "operator.cue"))
	if err != nil {
		return moduleOperator{}, err
	}
	var out moduleOperator
	for field, dst := range map[string]*string{
		"Version":          &out.Version,
		"Image.repository": &out.Repository,
		"Image.tag":        &out.Tag,
		"Image.digest":     &out.Digest,
	} {
		s, err := v.LookupPath(cue.ParsePath(field)).String()
		if err != nil {
			return moduleOperator{}, fmt.Errorf("operator/operator.cue %s: %w", field, err)
		}
		*dst = s
	}
	return out, nil
}

// checkMinOperatorVersion refuses a module whose operator release is below
// the repository's minimum operator version (the first release that refuses
// to reconcile its own instance), naming both.
func checkMinOperatorVersion(dir, minFile string) error {
	op, err := readModuleOperator(dir)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(minFile)
	if err != nil {
		return err
	}
	minTag := strings.TrimSpace(string(b))
	if !semver.IsValid(minTag) {
		return fmt.Errorf("%s holds %q, not a v-prefixed operator release tag", minFile, minTag)
	}
	got := "v" + op.Version
	if !semver.IsValid(got) {
		return fmt.Errorf("operator/operator.cue Version %q is not SemVer", op.Version)
	}
	if semver.Compare(got, minTag) < 0 {
		return fmt.Errorf("the module names operator %s, below the minimum operator version %s (%s): "+
			"an older operator reconciles its own instance", got, minTag, filepath.Base(minFile))
	}
	return nil
}

// rbacSourceNames returns the role names the module's generated RBAC data
// carry, read from zz_generated_rbac.cue without a render.
func rbacSourceNames(dir string) []string {
	GinkgoHelper()
	v, err := compileFile(filepath.Join(dir, "zz_generated_rbac.cue"))
	Expect(err).NotTo(HaveOccurred())
	it, err := v.LookupPath(cue.ParsePath("#rbacSource")).Fields(cue.Definitions(true))
	Expect(err).NotTo(HaveOccurred())
	var names []string
	for it.Next() {
		names = append(names, it.Selector().Unquoted())
	}
	sort.Strings(names)
	return names
}

// configRoles reads every Role and ClusterRole the rbac kustomization lists
// in the config tree at configDir, keyed by its unprefixed name.
func configRoles(configDir string) map[string]map[string]any {
	GinkgoHelper()
	var kust struct {
		Resources []string `json:"resources"`
	}
	readYAML(filepath.Join(configDir, "rbac", "kustomization.yaml"), &kust)
	out := map[string]map[string]any{}
	for _, r := range kust.Resources {
		var m map[string]any
		readYAML(filepath.Join(configDir, "rbac", r), &m)
		if k := str(m, "kind"); k == "Role" || k == "ClusterRole" {
			out[str(m, "metadata", "name")] = m
		}
	}
	return out
}

// configCRDs reads the generated CRDs of the config tree, keyed by name.
func configCRDs(configDir string) map[string]map[string]any {
	GinkgoHelper()
	files, err := filepath.Glob(filepath.Join(configDir, "crd", "bases", "*.yaml"))
	Expect(err).NotTo(HaveOccurred())
	out := map[string]map[string]any{}
	for _, f := range files {
		var m map[string]any
		readYAML(f, &m)
		out[str(m, "metadata", "name")] = m
	}
	return out
}

func readYAML(path string, into any) {
	GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	Expect(yaml.Unmarshal(b, into)).To(Succeed(), "parsing %s", path)
}

// yamlDocs splits a multi-document YAML stream into objects.
func yamlDocs(b []byte) []map[string]any {
	GinkgoHelper()
	var out []map[string]any
	for _, doc := range regexp.MustCompile(`(?m)^---\s*$`).Split(string(b), -1) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var m map[string]any
		Expect(yaml.Unmarshal([]byte(doc), &m)).To(Succeed())
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}

// copyTree copies the directory src to dst, which must not exist.
func copyTree(src, dst string) {
	GinkgoHelper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	Expect(err).NotTo(HaveOccurred())
}

// scratchModule copies the module tree into a temporary directory.
func scratchModule() string {
	GinkgoHelper()
	dst := filepath.Join(GinkgoT().TempDir(), "opm_operator")
	copyTree(moduleDir, dst)
	return dst
}

// requireTool returns the path of a CLI the spec shells out to, skipping
// (failing under OPM_TEST_REGISTRY_FORCE=1) when it is not on PATH.
func requireTool(name string) string {
	GinkgoHelper()
	p, err := exec.LookPath(name)
	if err != nil {
		registrySkip(name + " is not on PATH")
	}
	return p
}

// runScript runs a repository script with extra environment, failing the
// spec with its output on error.
func runScript(dir string, env []string, script string, args ...string) []byte {
	GinkgoHelper()
	abs, err := filepath.Abs(filepath.Join(repoRoot, script))
	Expect(err).NotTo(HaveOccurred())
	cmd := exec.Command("bash", append([]string{abs}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		Fail(fmt.Sprintf("%s failed: %v\n%s", script, err, stderr.String()))
	}
	return out
}

// normalize round-trips v through JSON so values from YAML and from CUE
// compare equal (numbers become float64, slices []any, maps map[string]any).
func normalize(v any) any {
	GinkgoHelper()
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	var out any
	Expect(json.Unmarshal(b, &out)).To(Succeed())
	return out
}

// lookup walks nested maps by key.
func lookup(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func str(m map[string]any, path ...string) string {
	s, _ := lookup(m, path...).(string)
	return s
}

func list(m map[string]any, path ...string) []any {
	l, _ := lookup(m, path...).([]any)
	return l
}

func dict(m map[string]any, path ...string) map[string]any {
	d, _ := lookup(m, path...).(map[string]any)
	return d
}
