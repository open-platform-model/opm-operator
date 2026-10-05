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

package apply_test

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
	fluxssa "github.com/fluxcd/pkg/ssa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

// callRecorder records the Get and Patch calls a ResourceManager's apply
// client issues: every apply Patch in call order, and per-kind totals.
type callRecorder struct {
	mu sync.Mutex

	// gets counts Gets of a key in keys; nil keys counts every Get.
	keys map[client.ObjectKey]bool
	gets int

	dryRunPatches int
	applyPatches  int

	// applied lists "Kind/name" of each non-dry-run Patch, in call order.
	applied []string
}

// recordingResourceManager returns a ResourceManager whose apply client goes
// through rec, and whose StatusPoller uses the plain client so the
// timing-dependent polling of WaitForSet is not counted.
func recordingResourceManager(rec *callRecorder) *fluxssa.ResourceManager {
	base, err := client.NewWithWatch(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	c := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object,
			opts ...client.GetOption) error {
			rec.mu.Lock()
			if rec.keys == nil || rec.keys[key] {
				rec.gets++
			}
			rec.mu.Unlock()
			return c.Get(ctx, key, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch,
			opts ...client.PatchOption) error {
			po := &client.PatchOptions{}
			po.ApplyOptions(opts)
			rec.mu.Lock()
			if len(po.DryRun) > 0 {
				rec.dryRunPatches++
			} else {
				rec.applyPatches++
				kind := obj.GetObjectKind().GroupVersionKind().Kind
				rec.applied = append(rec.applied, kind+"/"+obj.GetName())
			}
			rec.mu.Unlock()
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	poller := polling.NewStatusPoller(k8sClient, k8sClient.RESTMapper(), polling.Options{})
	return fluxssa.NewResourceManager(c, poller, fluxssa.Owner{Field: apply.FieldManager, Group: "test-owner"})
}

// reset clears the counts and the apply order.
func (r *callRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gets, r.dryRunPatches, r.applyPatches, r.applied = 0, 0, 0, nil
}

// counts returns the Get, dry-run Patch and apply Patch totals.
func (r *callRecorder) counts() [3]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return [3]int{r.gets, r.dryRunPatches, r.applyPatches}
}

// order returns the non-dry-run Patches in call order.
func (r *callRecorder) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.applied...)
}

func newObject(apiVersion, kind, namespace, name string, fields map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name},
	}
	if namespace != "" {
		obj["metadata"].(map[string]any)["namespace"] = namespace
	}
	maps.Copy(obj, fields)
	return &unstructured.Unstructured{Object: obj}
}

func podTemplate(app string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app": app}},
		"spec": map[string]any{
			"restartPolicy": "Always",
			"containers":    []any{map[string]any{"name": "app", "image": "busybox:1.36"}},
		},
	}
}

func newDeployment(namespace, name string) *unstructured.Unstructured {
	return newObject("apps/v1", "Deployment", namespace, name, map[string]any{"spec": map[string]any{
		"selector": map[string]any{"matchLabels": map[string]any{"app": name}},
		"template": podTemplate(name),
	}})
}

func newStatefulSet(namespace, name string) *unstructured.Unstructured {
	return newObject("apps/v1", "StatefulSet", namespace, name, map[string]any{"spec": map[string]any{
		"serviceName": name,
		"selector":    map[string]any{"matchLabels": map[string]any{"app": name}},
		"template":    podTemplate(name),
	}})
}

func newPVC(namespace, name string) *unstructured.Unstructured {
	return newObject("v1", "PersistentVolumeClaim", namespace, name, map[string]any{"spec": map[string]any{
		"accessModes": []any{"ReadWriteOnce"},
		"resources":   map[string]any{"requests": map[string]any{"storage": "1Gi"}},
	}})
}

// callCountSet returns forty built-in objects across the cluster-definition
// stage and eight library weights, all in namespace ns, with no CRD or custom
// resource (a CRD can trigger the discovery retry, which re-runs every stage).
func callCountSet(ns string) []*unstructured.Unstructured {
	set := make([]*unstructured.Unstructured, 0, 40)
	set = append(set, newObject("v1", "Namespace", "", ns, nil))
	for i := range 5 {
		set = append(set, newObject("v1", "ServiceAccount", ns, fmt.Sprintf("sa-%d", i), nil))
	}
	for i := range 2 {
		set = append(set,
			newObject("rbac.authorization.k8s.io/v1", "Role", ns, fmt.Sprintf("role-%d", i), map[string]any{
				"rules": []any{map[string]any{"apiGroups": []any{""}, "resources": []any{"configmaps"}, "verbs": []any{"get"}}},
			}),
			newObject("rbac.authorization.k8s.io/v1", "RoleBinding", ns, fmt.Sprintf("rb-%d", i), map[string]any{
				"roleRef": map[string]any{
					"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": fmt.Sprintf("role-%d", i),
				},
				"subjects": []any{map[string]any{"kind": "ServiceAccount", "name": fmt.Sprintf("sa-%d", i), "namespace": ns}},
			}))
	}
	for i := range 6 {
		set = append(set, newObject("v1", "Secret", ns, fmt.Sprintf("secret-%d", i), map[string]any{
			"stringData": map[string]any{"k": "v"},
		}))
	}
	for i := range 8 {
		set = append(set, newObject("v1", "ConfigMap", ns, fmt.Sprintf("cm-%d", i), map[string]any{
			"data": map[string]any{"k": fmt.Sprintf("v%d", i)},
		}))
	}
	for i := range 2 {
		set = append(set, newPVC(ns, fmt.Sprintf("pvc-%d", i)))
	}
	for i := range 5 {
		set = append(set, newObject("v1", "Service", ns, fmt.Sprintf("svc-%d", i), map[string]any{"spec": map[string]any{
			"selector": map[string]any{"app": fmt.Sprintf("deploy-%d", i)},
			"ports":    []any{map[string]any{"port": int64(80)}},
		}}))
	}
	for i := range 5 {
		set = append(set, newDeployment(ns, fmt.Sprintf("deploy-%d", i)))
	}
	for i := range 2 {
		tmpl := podTemplate(fmt.Sprintf("job-%d", i))
		tmpl["spec"].(map[string]any)["restartPolicy"] = "Never"
		set = append(set, newObject("batch/v1", "Job", ns, fmt.Sprintf("job-%d", i), map[string]any{
			"spec": map[string]any{"template": tmpl},
		}))
	}
	set = append(set,
		newObject("networking.k8s.io/v1", "Ingress", ns, "ingress", map[string]any{"spec": map[string]any{
			"defaultBackend": map[string]any{"service": map[string]any{
				"name": "svc-0", "port": map[string]any{"number": int64(80)},
			}},
		}}),
		newObject("policy/v1", "PodDisruptionBudget", ns, "pdb", map[string]any{"spec": map[string]any{
			"minAvailable": int64(1),
			"selector":     map[string]any{"matchLabels": map[string]any{"app": "deploy-0"}},
		}}),
	)
	return set
}

// Spec reference: design.md D6 of the adopt-kubernetes-object-packages change.
// One ApplyAll per library stage issues the same per-object calls as one
// ApplyAllStaged: one Get and one dry-run Patch per object, and one apply
// Patch per changed object. The same numbers were measured on Flux's
// ApplyAllStaged before Apply moved to one ApplyAll per stage, and are pinned
// here.
var _ = Describe("Apply call counts", func() {
	It("issues one Get and one dry-run Patch per object, and one apply Patch per changed object", func() {
		const ns = "apply-call-count"
		set := callCountSet(ns)
		Expect(set).To(HaveLen(40))

		rec := &callRecorder{keys: map[client.ObjectKey]bool{}}
		for _, obj := range set {
			rec.keys[client.ObjectKeyFromObject(obj)] = true
		}
		rm := recordingResourceManager(rec)

		By("applying the set for the first time")
		_, err := apply.Apply(ctx, rm, set, false)
		Expect(err).NotTo(HaveOccurred())
		first := rec.counts()
		AddReportEntry("first apply [gets dry-run-patches apply-patches]", first)

		By("applying the same set again, unchanged")
		rec.reset()
		_, err = apply.Apply(ctx, rm, callCountSet(ns), false)
		Expect(err).NotTo(HaveOccurred())
		second := rec.counts()
		AddReportEntry("second apply [gets dry-run-patches apply-patches]", second)

		Expect(first).To(Equal([3]int{40, 40, 40}), "first apply [gets, dry-run patches, apply patches]")
		Expect(second).To(Equal([3]int{40, 40, 0}), "unchanged apply [gets, dry-run patches, apply patches]")
	})
})
