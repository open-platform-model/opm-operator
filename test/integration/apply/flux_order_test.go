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
	"strings"
	"sync"

	"github.com/fluxcd/cli-utils/pkg/object"
	ssautils "github.com/fluxcd/pkg/ssa/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"

	"sigs.k8s.io/controller-runtime/pkg/client"

	libobject "github.com/open-platform-model/library/opm/k8s/object"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

// recordingClient records every write that is not a dry run, in the order
// the staged apply makes them.
type recordingClient struct {
	client.Client

	mu     sync.Mutex
	writes []*unstructured.Unstructured
}

func (r *recordingClient) Patch(
	ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption,
) error {
	po := &client.PatchOptions{}
	po.ApplyOptions(opts)
	if u, ok := obj.(*unstructured.Unstructured); ok && len(po.DryRun) == 0 {
		r.mu.Lock()
		r.writes = append(r.writes, u.DeepCopy())
		r.mu.Unlock()
	}
	return r.Client.Patch(ctx, obj, patch, opts...)
}

// firstWrites returns the first write of each object, in order.
func (r *recordingClient) firstWrites() []*unstructured.Unstructured {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[object.ObjMetadata]bool{}
	var out []*unstructured.Unstructured
	for _, u := range r.writes {
		id := object.UnstructuredToObjMetadata(u)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, u)
	}
	return out
}

// orderSet returns one new object of every kind class the test API server
// serves, in an order that is neither Flux's nor the library's. The
// namespaced objects live in a Namespace of the set, and every name carries
// suffix, so every object is created and therefore written.
func orderSet(suffix string) []*unstructured.Unstructured {
	ns := "flux-order-" + suffix
	name := "flux-order-" + suffix
	group := "order-" + suffix + ".example.com"

	mk := func(apiVersion, kind, namespace string, fields map[string]any) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{}}
		maps.Copy(u.Object, fields)
		u.SetAPIVersion(apiVersion)
		u.SetKind(kind)
		u.SetNamespace(namespace)
		u.SetName(name)
		return u
	}
	labels := map[string]any{"app": name}
	podTemplate := func(restartPolicy string) map[string]any {
		return map[string]any{
			"metadata": map[string]any{"labels": labels},
			"spec": map[string]any{
				"restartPolicy": restartPolicy,
				"containers": []any{map[string]any{
					"name":  "main",
					"image": "registry.invalid/none:1",
				}},
			},
		}
	}
	selector := map[string]any{"matchLabels": labels}
	roleRef := func(kind string) map[string]any {
		return map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": kind, "name": name}
	}
	subjects := []any{map[string]any{"kind": "ServiceAccount", "name": name, "namespace": ns}}
	rules := []any{map[string]any{
		"apiGroups": []any{""}, "resources": []any{"configmaps"}, "verbs": []any{"get"},
	}}

	crd := newTestCRD(group, "Widget", "widgets")
	widget := newTestCustomResource(group, "Widget", name)
	widget.SetNamespace(ns)

	return []*unstructured.Unstructured{
		mk("admissionregistration.k8s.io/v1", "ValidatingWebhookConfiguration", "", nil),
		widget,
		mk("policy/v1", "PodDisruptionBudget", ns, map[string]any{
			"spec": map[string]any{"minAvailable": int64(1), "selector": selector},
		}),
		mk("apps/v1", "Deployment", ns, map[string]any{
			"spec": map[string]any{"selector": selector, "template": podTemplate("Always")},
		}),
		mk("batch/v1", "Job", ns, map[string]any{
			"spec": map[string]any{"template": map[string]any{"spec": podTemplate("Never")["spec"]}},
		}),
		mk("v1", "PersistentVolumeClaim", ns, map[string]any{
			"spec": map[string]any{
				"accessModes": []any{"ReadWriteOnce"},
				"resources":   map[string]any{"requests": map[string]any{"storage": "1Gi"}},
			},
		}),
		mk("v1", "Service", ns, map[string]any{
			"spec": map[string]any{
				"selector": labels,
				"ports":    []any{map[string]any{"port": int64(80)}},
			},
		}),
		mk("networking.k8s.io/v1", "NetworkPolicy", ns, map[string]any{
			"spec": map[string]any{"podSelector": selector},
		}),
		mk("v1", "Secret", ns, map[string]any{"stringData": map[string]any{"key": "value"}}),
		mk("batch/v1", "CronJob", ns, map[string]any{
			"spec": map[string]any{
				"schedule": "0 0 1 1 *",
				"jobTemplate": map[string]any{
					"spec": map[string]any{"template": map[string]any{"spec": podTemplate("Never")["spec"]}},
				},
			},
		}),
		mk("apps/v1", "DaemonSet", ns, map[string]any{
			"spec": map[string]any{"selector": selector, "template": podTemplate("Always")},
		}),
		mk("v1", "ConfigMap", ns, map[string]any{"data": map[string]any{"key": "value"}}),
		mk("rbac.authorization.k8s.io/v1", "RoleBinding", ns, map[string]any{
			"roleRef": roleRef("Role"), "subjects": subjects,
		}),
		mk("v1", "LimitRange", ns, map[string]any{
			"spec": map[string]any{"limits": []any{map[string]any{
				"type": "Container", "default": map[string]any{"cpu": "100m"},
			}}},
		}),
		mk("apps/v1", "StatefulSet", ns, map[string]any{
			"spec": map[string]any{"serviceName": name, "selector": selector, "template": podTemplate("Always")},
		}),
		mk("networking.k8s.io/v1", "Ingress", ns, map[string]any{
			"spec": map[string]any{"defaultBackend": map[string]any{
				"service": map[string]any{"name": name, "port": map[string]any{"number": int64(80)}},
			}},
		}),
		mk("rbac.authorization.k8s.io/v1", "Role", ns, map[string]any{"rules": rules}),
		mk("v1", "ServiceAccount", ns, nil),
		mk("v1", "ResourceQuota", ns, map[string]any{
			"spec": map[string]any{"hard": map[string]any{"pods": "10"}},
		}),
		mk("rbac.authorization.k8s.io/v1", "ClusterRoleBinding", "", map[string]any{
			"roleRef": roleRef("ClusterRole"), "subjects": subjects,
		}),
		mk("scheduling.k8s.io/v1", "PriorityClass", "", map[string]any{"value": int64(1000)}),
		mk("storage.k8s.io/v1", "StorageClass", "", map[string]any{"provisioner": "example.com/none"}),
		mk("rbac.authorization.k8s.io/v1", "ClusterRole", "", map[string]any{"rules": rules}),
		func() *unstructured.Unstructured {
			u := mk("v1", "Namespace", "", nil)
			u.SetName(ns)
			return u
		}(),
		crd,
	}
}

// fluxStage is the stage Flux's own rules put an object in: the cluster
// definitions, then the class definitions, then everything else. The
// operator sets no custom stage.
func fluxStage(u *unstructured.Unstructured) int {
	switch {
	case ssautils.IsClusterDefinition(u):
		return 0
	case ssautils.IsClassDefinition(u):
		return 1
	default:
		return 2
	}
}

func describeWrite(u *unstructured.Unstructured) string {
	gvk := u.GroupVersionKind()
	return fmt.Sprintf("%s/%s (stage %d, weight %d)", gvk.Group, gvk.Kind, fluxStage(u), libobject.Weight(gvk))
}

// orderGuidance is what a failure of this spec tells the maintainer.
const orderGuidance = `
The order of a real staged apply contradicts the library's kind weights or
Flux's stage rules (0012:D5:R1).

What to do:
  - Do not edit this spec or its set of objects to make it pass.
  - A weight contradiction: the library's weight table follows Flux, not the
    other way round. Change opm/k8s/object/weights.go in the library, and the
    Flux list in its flux_order_test.go, so that the table agrees with the
    pinned Flux version. Hold the pin bump until that library release exists.
  - A stage contradiction: Flux changed the precedence of its stages, or the
    operator changed what it passes to Flux. internal/apply/flux_order_test.go
    mirrors that precedence in stagedOrder; bring it in line with the pinned
    ApplyAllStaged, then read what that test reports.
`

var _ = Describe("Staged apply order", func() {
	It("writes a new set stage by stage and never against the library's kind weights", func() {
		resources := orderSet(rand.String(6))
		recorder := &recordingClient{Client: k8sClient}
		rm := apply.NewResourceManager(recorder, "test-owner")

		DeferCleanup(func() {
			// Best effort: the test API server runs no garbage collector, so
			// the Namespace stays Terminating. The CRD is gone before a later
			// spec can meet it.
			var crds []*unstructured.Unstructured
			for _, u := range resources {
				if u.GetNamespace() != "" {
					continue
				}
				if u.GetKind() == "CustomResourceDefinition" {
					crds = append(crds, u)
					continue
				}
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, u))).To(Succeed())
			}
			deleteCRDsAndWait(crds...)
		})

		result, err := apply.Apply(ctx, rm, resources, apply.ApplyOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Created).To(Equal(len(resources)))

		order := recorder.firstWrites()
		Expect(order).To(HaveLen(len(resources)), "every new object is written once")

		lines := make([]string, 0, len(order))
		for _, u := range order {
			lines = append(lines, describeWrite(u))
		}
		GinkgoWriter.Printf("order of the writes:\n  %s\n", strings.Join(lines, "\n  "))

		var problems []string
		for i, a := range order {
			for _, b := range order[i+1:] {
				if fluxStage(a) > fluxStage(b) {
					problems = append(problems, fmt.Sprintf("stage: %s was written before %s", describeWrite(a), describeWrite(b)))
				}
				if libobject.Weight(a.GroupVersionKind()) > libobject.Weight(b.GroupVersionKind()) {
					problems = append(problems, fmt.Sprintf("weight: %s was written before %s", describeWrite(a), describeWrite(b)))
				}
			}
		}
		Expect(problems).To(BeEmpty(), orderGuidance)
	})
})
