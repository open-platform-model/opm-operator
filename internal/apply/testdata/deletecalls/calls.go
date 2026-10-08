// Package deletecalls is a fixture for the delete call-site test: one call of
// each form that deletes a cluster object, and calls of the same method names
// that delete none. It is never built into the operator.
package deletecalls

import (
	"context"

	"github.com/fluxcd/pkg/runtime/conditions"
	fluxssa "github.com/fluxcd/pkg/ssa"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// wrapper embeds a controller-runtime client, as the resource manager's
// delete guard does.
type wrapper struct {
	client.Client
}

// registry has methods of the same names and is no Kubernetes client.
type registry struct{}

func (registry) Delete(string)      {}
func (registry) DeleteAllOf(string) {}

// Deletes holds the calls the matcher must count: nine.
func Deletes(
	ctx context.Context,
	c client.Client,
	w client.Writer,
	typed kubernetes.Interface,
	dyn dynamic.Interface,
	rm *fluxssa.ResourceManager,
	obj *unstructured.Unstructured,
) {
	_ = c.Delete(ctx, obj)                                                          // controller-runtime client
	_ = c.DeleteAllOf(ctx, obj)                                                     // a collection
	_ = w.Delete(ctx, obj)                                                          // the writer half alone
	_ = wrapper{c}.Delete(ctx, obj)                                                 // a type that embeds a client
	_ = wrapper{c}.Client.DeleteAllOf(ctx, obj)                                     // through the embedded field
	_ = typed.CoreV1().ConfigMaps("ns").Delete(ctx, "name", metav1.DeleteOptions{}) // client-go typed
	_ = typed.CoreV1().ConfigMaps("ns").DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	_ = dyn.Resource(schema.GroupVersionResource{}).Delete(ctx, "name", metav1.DeleteOptions{}) // client-go dynamic
	_, _ = rm.Delete(ctx, obj, fluxssa.DeleteOptions{})                                         // the resource manager
}

// NotDeletes holds calls of the same names the matcher must not count.
func NotDeletes(obj *corev1.ConfigMap, setter conditions.Setter) {
	conditions.Delete(setter, "Ready") // removes a status condition
	registry{}.Delete("key")
	registry{}.DeleteAllOf("prefix")
	m := map[string]int{}
	delete(m, obj.Name)
}
