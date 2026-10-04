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
// equal: the default render's object set, Deployment, pod spec and metrics
// Service against a kustomize build of config/default. Excluded: the image
// (the module names a release), labels and the selector (the catalog's,
// pinned in module_test.go), the fields the catalog sets to Kubernetes API
// defaults, and binding names (a binding is matched by its role and subjects).
var _ = Describe("The operator module against the kustomize install", func() {
	It("renders the object set of config/default, bindings matched by role and subjects", func() {
		Expect(objectSetDiffs(mustRender(moduleDir), kustomizeBuild(repoRoot))).To(BeEmpty())
	})

	It("fails naming the binding when config/rbac gains one the module does not render", func() {
		tree := GinkgoT().TempDir()
		copyTree(filepath.Join(repoRoot, "config"), filepath.Join(tree, "config"))
		rbac := filepath.Join(tree, "config", "rbac")
		binding := `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: metrics-reader-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: metrics-reader
subjects:
- kind: ServiceAccount
  name: controller-manager
  namespace: system
`
		Expect(os.WriteFile(filepath.Join(rbac, "metrics_reader_role_binding.yaml"), []byte(binding), 0o644)).To(Succeed())
		kust := filepath.Join(rbac, "kustomization.yaml")
		b, err := os.ReadFile(kust)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(kust, append(b, []byte("- metrics_reader_role_binding.yaml\n")...), 0o644)).To(Succeed())

		diffs := objectSetDiffs(mustRender(moduleDir), kustomizeBuild(tree))
		Expect(diffs).To(ConsistOf(
			"only in config/: ClusterRoleBinding roleRef=ClusterRole/opm-operator-metrics-reader " +
				"subjects=[ServiceAccount opm-operator-system/opm-operator-controller-manager]"))
	})

	It("fails naming the field when the Deployment's replicas change in config/manager only", func() {
		tree := GinkgoT().TempDir()
		copyTree(filepath.Join(repoRoot, "config"), filepath.Join(tree, "config"))
		path := filepath.Join(tree, "config", "manager", "manager.yaml")
		b, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		changed := strings.Replace(string(b), "replicas: 1", "replicas: 2", 1)
		Expect(changed).NotTo(Equal(string(b)))
		Expect(os.WriteFile(path, []byte(changed), 0o644)).To(Succeed())

		Expect(parityDiffs(mustRender(moduleDir), kustomizeBuild(tree))).To(
			ContainElement(ContainSubstring("deployment.replicas")))
	})

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
	diff("deployment", comparableDeployment(dict(rDeploy, "spec")), comparableDeployment(dict(mDeploy, "spec")), &diffs)
	diff("pod", comparablePodSpec(dict(rDeploy, "spec", "template", "spec")),
		comparablePodSpec(dict(mDeploy, "spec", "template", "spec")), &diffs)
	diff("service", comparableService(dict(rService, "spec")), comparableService(dict(mService, "spec")), &diffs)
	return diffs
}

// objectSetDiffs lists every object that only one of the render and the
// kustomize build has, by kind, namespace and name; a binding by kind,
// namespace, role and subjects instead, since the catalog names a binding
// after its role. A changed object is named once on each side.
func objectSetDiffs(objs []obj, manifest []map[string]any) []string {
	GinkgoHelper()
	rendered := map[string]bool{}
	for _, o := range objs {
		rendered[objectKey(o.Object)] = true
	}
	built := map[string]bool{}
	for _, m := range manifest {
		built[objectKey(m)] = true
	}
	var diffs []string
	for k := range built {
		if !rendered[k] {
			diffs = append(diffs, "only in config/: "+k)
		}
	}
	for k := range rendered {
		if !built[k] {
			diffs = append(diffs, "only in the module: "+k)
		}
	}
	sort.Strings(diffs)
	return diffs
}

// objectKey identifies an object for objectSetDiffs.
func objectKey(m map[string]any) string {
	kind := str(m, "kind")
	ns := str(m, "metadata", "namespace")
	switch kind {
	case "RoleBinding", "ClusterRoleBinding":
		subjects := make([]string, 0, len(list(m, "subjects")))
		for _, s := range list(m, "subjects") {
			sm, _ := s.(map[string]any)
			subjects = append(subjects, fmt.Sprintf("%s %s/%s", str(sm, "kind"), str(sm, "namespace"), str(sm, "name")))
		}
		sort.Strings(subjects)
		key := fmt.Sprintf("%s roleRef=%s/%s subjects=[%s]", kind,
			str(m, "roleRef", "kind"), str(m, "roleRef", "name"), strings.Join(subjects, ", "))
		if ns != "" {
			key += " namespace=" + ns
		}
		return key
	default:
		return fmt.Sprintf("%s %s/%s", kind, ns, str(m, "metadata", "name"))
	}
}

// comparableDeployment keeps the Deployment spec outside the pod spec, the
// selector and the pod labels: the replicas, the rollout fields and the pod
// annotations, with the Kubernetes API defaults dropped.
func comparableDeployment(spec map[string]any) map[string]any {
	GinkgoHelper()
	s := normalize(spec).(map[string]any)
	delete(s, "selector")
	if t, ok := s["template"].(map[string]any); ok {
		delete(t, "spec")
		if md, ok := t["metadata"].(map[string]any); ok {
			delete(md, "labels")
		}
	}
	dropIf(s, "replicas", float64(1))
	dropIf(s, "revisionHistoryLimit", float64(10))
	dropIf(s, "progressDeadlineSeconds", float64(600))
	dropIf(s, "minReadySeconds", float64(0))
	dropIf(s, "strategy", map[string]any{"type": "RollingUpdate"})
	return dropEmptyMaps(s).(map[string]any)
}

// dropEmptyMaps removes empty maps and lists from v, recursively.
func dropEmptyMaps(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for k, e := range m {
		e = dropEmptyMaps(e)
		switch t := e.(type) {
		case map[string]any:
			if len(t) == 0 {
				delete(m, k)
				continue
			}
		case []any:
			if len(t) == 0 {
				delete(m, k)
				continue
			}
		}
		m[k] = e
	}
	return m
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
