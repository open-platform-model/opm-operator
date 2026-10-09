// Package writecalls is a fixture for the write call-site test: one call of
// each form that creates or changes a cluster object, and calls of the same
// method names that write none. It is never built into the operator.
package writecalls

import (
	"context"

	"github.com/fluxcd/pkg/runtime/patch"
	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// wrapper embeds a controller-runtime client, as the resource manager's
// delete guard does.
type wrapper struct {
	client.Client
}

// registry has methods of the same names and is no Kubernetes client.
type registry struct{}

func (registry) Create(string) {}
func (registry) Update(string) {}
func (registry) Patch(string)  {}
func (registry) Apply(string)  {}

// patcher is a controller-runtime client narrowed to its patch.
type patcher interface {
	Patch(ctx context.Context, obj client.Object, p client.Patch, opts ...client.PatchOption) error
}

// Writes holds the uses the matcher must count: twenty-two.
func Writes(
	ctx context.Context,
	c client.Client,
	w client.Writer,
	typed kubernetes.Interface,
	dyn dynamic.Interface,
	rm *fluxssa.ResourceManager,
	sp *patch.SerialPatcher,
	obj *unstructured.Unstructured,
	cm *corev1.ConfigMap,
) {
	_ = c.Create(ctx, obj) // controller-runtime client
	_ = c.Update(ctx, obj)
	_ = c.Patch(ctx, obj, client.Merge)
	_ = c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj))

	_ = c.Status().Update(ctx, obj) // the status writer
	_ = c.Status().Patch(ctx, obj, client.Merge)
	_ = c.SubResource("scale").Create(ctx, obj, obj) // a subresource writer

	_ = wrapper{c}.Patch(ctx, obj, client.Merge) // a type that embeds a client

	maps := typed.CoreV1().ConfigMaps("ns") // client-go typed
	_, _ = maps.Create(ctx, cm, metav1.CreateOptions{})
	_, _ = maps.Update(ctx, cm, metav1.UpdateOptions{})
	_, _ = maps.Patch(ctx, "name", types.MergePatchType, nil, metav1.PatchOptions{})
	_, _ = maps.Apply(ctx, corev1ac.ConfigMap("name", "ns"), metav1.ApplyOptions{})

	res := dyn.Resource(schema.GroupVersionResource{}) // client-go dynamic
	_, _ = res.Create(ctx, obj, metav1.CreateOptions{})
	_, _ = res.Apply(ctx, "name", obj, metav1.ApplyOptions{})

	_, _ = rm.Apply(ctx, obj, fluxssa.ApplyOptions{}) // the resource manager
	_, _ = rm.ApplyAll(ctx, []*unstructured.Unstructured{obj}, fluxssa.ApplyOptions{})
	_, _ = rm.ApplyAllStaged(ctx, []*unstructured.Unstructured{obj}, fluxssa.ApplyOptions{})

	_ = sp.Patch(ctx, cm) // the status patcher of the reconcilers

	var narrow patcher = c
	_ = narrow.Patch(ctx, obj, client.Merge) // an interface narrowed to the patch
	update := w.Update                       // a method value
	_ = update(ctx, obj)

	_, _ = controllerutil.CreateOrUpdate(ctx, c, cm, func() error { return nil })
	_, _ = controllerutil.CreateOrPatch(ctx, c, cm, func() error { return nil })
}

// NotWrites holds calls of the same names the matcher must not count.
func NotWrites(m map[string]int) {
	registry{}.Create("key")
	registry{}.Update("key")
	registry{}.Patch("key")
	registry{}.Apply("key")
	m["key"]++
}
