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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Until the module renders the install manifest, config/manager and
// config/default still produce dist/install.yaml, while the module writes the
// controller's pod by hand through the catalog. These specs keep the two
// equal: the default render's pod spec and metrics Service against a
// kustomize build of config/default. Excluded: the image (the module names a
// release), labels and the selector (the catalog's, pinned in module_test.go),
// the fields the catalog sets to Kubernetes API defaults, and binding names.
var _ = Describe("The operator module against the kustomize install", func() {
	It("renders the controller's pod spec and metrics Service of config/default", func() {
		manifest := kustomizeBuild(repoRoot)
		objs := mustRender(moduleDir)
		Expect(parityDiffs(objs, manifest)).To(BeEmpty())
	})

	It("fails naming the field when a manager argument changes in config/manager only", func() {
		tree := GinkgoT().TempDir()
		copyTree(filepath.Join(repoRoot, "config"), filepath.Join(tree, "config"))
		path := filepath.Join(tree, "config", "manager", "manager.yaml")
		b, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		changed := strings.Replace(string(b), "--health-probe-bind-address=:8081", "--health-probe-bind-address=:9091", 1)
		Expect(changed).NotTo(Equal(string(b)))
		Expect(os.WriteFile(path, []byte(changed), 0o644)).To(Succeed())

		diffs := parityDiffs(mustRender(moduleDir), kustomizeBuild(tree))
		Expect(diffs).To(ContainElement(ContainSubstring("containers[manager].args")))
	})
})

// kustomizeBuild renders config/default of the tree at root with
// hack/render-config.sh and the kustomize binary in $KUSTOMIZE (task dev:test
// passes it). Without it the spec skips, and fails under
// OPM_TEST_REGISTRY_FORCE=1.
func kustomizeBuild(root string) []map[string]any {
	GinkgoHelper()
	kustomize := os.Getenv("KUSTOMIZE")
	if kustomize == "" {
		registrySkip("KUSTOMIZE is unset; task dev:test passes the kustomize binary")
	}
	if _, err := os.Stat(kustomize); err != nil {
		registrySkip("KUSTOMIZE names no binary: " + err.Error())
	}
	out := runScript(root, nil, "hack/render-config.sh", mustAbs(kustomize), "controller:latest", "config/default")
	return yamlDocs(out)
}

// parityDiffs lists every compared field that differs between the module's
// render and the kustomize build, by path.
func parityDiffs(objs []obj, manifest []map[string]any) []string {
	GinkgoHelper()
	var mDeploy, mService map[string]any
	for _, m := range manifest {
		switch {
		case str(m, "kind") == "Deployment" && str(m, "metadata", "name") == "opm-operator-controller-manager":
			mDeploy = m
		case str(m, "kind") == "Service" && str(m, "metadata", "name") == "opm-operator-controller-manager-metrics-service":
			mService = m
		}
	}
	Expect(mDeploy).NotTo(BeNil(), "the kustomize build has the controller Deployment")
	Expect(mService).NotTo(BeNil(), "the kustomize build has the metrics Service")

	rDeploy := find(objs, "Deployment", "opm-operator-controller-manager").Object
	rService := find(objs, "Service", "opm-operator-controller-manager-metrics-service").Object

	var diffs []string
	diff("pod", comparablePodSpec(dict(rDeploy, "spec", "template", "spec")),
		comparablePodSpec(dict(mDeploy, "spec", "template", "spec")), &diffs)
	diff("service", comparableService(dict(rService, "spec")), comparableService(dict(mService, "spec")), &diffs)
	return diffs
}

// comparablePodSpec drops what the comparison excludes, keys containers and
// volumes by name, and treats an empty list as absent.
func comparablePodSpec(spec map[string]any) map[string]any {
	GinkgoHelper()
	s := normalize(spec).(map[string]any)
	// API defaults the catalog writes out and the manifest leaves implicit.
	dropIf(s, "restartPolicy", "Always")
	dropIf(s, "automountServiceAccountToken", true)
	dropIf(s, "dnsPolicy", "ClusterFirst")
	containers := map[string]any{}
	for _, c := range list(s, "containers") {
		cm := c.(map[string]any)
		delete(cm, "image")
		dropIf(cm, "terminationMessagePath", "/dev/termination-log")
		dropIf(cm, "terminationMessagePolicy", "File")
		for _, p := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
			if probe, ok := cm[p].(map[string]any); ok {
				dropIf(probe, "timeoutSeconds", float64(1))
				dropIf(probe, "successThreshold", float64(1))
				dropIf(probe, "failureThreshold", float64(3))
				dropIf(probe, "initialDelaySeconds", float64(0))
				dropIf(probe, "periodSeconds", float64(10))
				if get, ok := probe["httpGet"].(map[string]any); ok {
					dropIf(get, "scheme", "HTTP")
				}
			}
		}
		if res, ok := cm["resources"].(map[string]any); ok {
			for _, k := range []string{"limits", "requests"} {
				if q, ok := res[k].(map[string]any); ok {
					for name, v := range q {
						q[name] = fmt.Sprint(v) // cpu 2 and "2" are one quantity
					}
				}
			}
		}
		containers[fmt.Sprint(cm["name"])] = dropEmpty(cm)
	}
	s["containers"] = containers
	volumes := map[string]any{}
	for _, v := range list(s, "volumes") {
		vm := v.(map[string]any)
		volumes[fmt.Sprint(vm["name"])] = vm
	}
	s["volumes"] = volumes
	return dropEmpty(s).(map[string]any)
}

// comparableService keeps the metrics Service's ports and type.
func comparableService(spec map[string]any) map[string]any {
	GinkgoHelper()
	s := normalize(spec).(map[string]any)
	typ, _ := s["type"].(string)
	if typ == "" {
		typ = "ClusterIP"
	}
	ports := map[string]any{}
	for _, p := range list(s, "ports") {
		pm := p.(map[string]any)
		if _, ok := pm["protocol"]; !ok {
			pm["protocol"] = "TCP"
		}
		ports[fmt.Sprint(pm["name"])] = pm
	}
	return map[string]any{"type": typ, "ports": ports}
}

func dropIf(m map[string]any, key string, def any) {
	if v, ok := m[key]; ok && reflect.DeepEqual(v, def) {
		delete(m, key)
	}
}

// dropEmpty removes empty lists from v, recursively.
func dropEmpty(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			if l, ok := e.([]any); ok && len(l) == 0 {
				delete(t, k)
				continue
			}
			t[k] = dropEmpty(e)
		}
		return t
	case []any:
		for i := range t {
			t[i] = dropEmpty(t[i])
		}
		return t
	default:
		return v
	}
}

// diff appends one line per differing leaf, keyed by path.
func diff(path string, module, manifest any, out *[]string) {
	mm, ok1 := module.(map[string]any)
	km, ok2 := manifest.(map[string]any)
	if ok1 && ok2 {
		keys := map[string]bool{}
		for k := range mm {
			keys[k] = true
		}
		for k := range km {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			child := path + "." + k
			if strings.HasSuffix(path, ".containers") || strings.HasSuffix(path, ".volumes") {
				child = path + "[" + k + "]"
			}
			diff(child, mm[k], km[k], out)
		}
		return
	}
	if !reflect.DeepEqual(module, manifest) {
		*out = append(*out, fmt.Sprintf("%s: module %v, config/ %v", path, module, manifest))
	}
}
