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

// Package collector plays the garbage collector in an envtest suite.
//
// envtest runs an API server and etcd and no controller manager. A DELETE
// with Foreground propagation makes the API server set a deletionTimestamp
// and the foregroundDeletion finalizer on the object, of any kind, with or
// without dependents; removing that finalizer is the garbage collector's
// work. Without one the object stays, terminating, for ever.
//
// A Collector removes the finalizer from every terminating object it finds,
// so a Foreground delete ends as it does in a cluster. It deletes no
// dependent: envtest runs no workload controller either, so a Deployment has
// no ReplicaSet and no Pod to collect.
//
// A spec that needs an object to stay terminating pauses the collector for
// the object's namespace.
package collector

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

// sweepInterval is the pause between two sweeps of the API server.
const sweepInterval = 50 * time.Millisecond

// discoveryInterval is how often the served resources are listed again, so
// the kinds of a CustomResourceDefinition a spec installs are collected too.
const discoveryInterval = time.Second

// Collector removes the foregroundDeletion finalizer from terminating
// objects. Build one with Start.
type Collector struct {
	meta      metadata.Interface
	discovery discovery.DiscoveryInterface

	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	paused map[string]int
}

// Start runs a collector against the API server of cfg until Stop is called.
// Call it in BeforeSuite, after the environment started.
func Start(cfg *rest.Config) (*Collector, error) {
	// A copy without a client-side rate limit: the sweep lists every served
	// resource and must not slow the suite's own client down.
	own := rest.CopyConfig(cfg)
	own.QPS = -1
	meta, err := metadata.NewForConfig(own)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(own)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Collector{
		meta:      meta,
		discovery: disc,
		cancel:    cancel,
		done:      make(chan struct{}),
		paused:    map[string]int{},
	}
	go c.run(ctx)
	return c, nil
}

// Stop ends the collector and waits for its loop to return. Call it in
// AfterSuite, before the environment stops. It is safe on a nil Collector.
func (c *Collector) Stop() {
	if c == nil {
		return
	}
	c.cancel()
	<-c.done
}

// Pause makes the collector leave the objects of namespace alone until the
// returned function is called, so a spec can hold an object terminating. A
// sweep that already listed the namespace may still finish; pause before the
// delete. Cluster-scoped objects are never paused.
func (c *Collector) Pause(namespace string) (resume func()) {
	c.mu.Lock()
	c.paused[namespace]++
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.paused[namespace]--
			c.mu.Unlock()
		})
	}
}

func (c *Collector) isPaused(namespace string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return namespace != "" && c.paused[namespace] > 0
}

func (c *Collector) run(ctx context.Context) {
	defer close(c.done)
	var (
		resources []schema.GroupVersionResource
		listed    time.Time
	)
	for {
		if time.Since(listed) >= discoveryInterval {
			if found := c.discover(); len(found) > 0 {
				resources = found
			}
			listed = time.Now()
		}
		for _, gvr := range resources {
			if ctx.Err() != nil {
				return
			}
			c.sweep(ctx, gvr)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sweepInterval):
		}
	}
}

// discover returns every served resource that can be listed and patched.
// Events are left out: nothing deletes them with Foreground and there are
// many. A partial discovery failure (a CustomResourceDefinition being
// established) still returns the groups that answered.
func (c *Collector) discover() []schema.GroupVersionResource {
	lists, _ := c.discovery.ServerPreferredResources()
	var out []schema.GroupVersionResource
	for _, list := range lists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") || r.Name == "events" {
				continue
			}
			if !slices.Contains(r.Verbs, "list") || !slices.Contains(r.Verbs, "patch") {
				continue
			}
			out = append(out, gv.WithResource(r.Name))
		}
	}
	return out
}

// sweep removes the foregroundDeletion finalizer from the terminating objects
// of one resource. Errors are dropped: the next sweep tries again, and an
// object that went in between is what the sweep wants.
func (c *Collector) sweep(ctx context.Context, gvr schema.GroupVersionResource) {
	list, err := c.meta.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for i := range list.Items {
		item := &list.Items[i]
		if item.DeletionTimestamp == nil || c.isPaused(item.Namespace) {
			continue
		}
		if !slices.Contains(item.Finalizers, metav1.FinalizerDeleteDependents) {
			continue
		}
		kept := slices.DeleteFunc(slices.Clone(item.Finalizers), func(f string) bool {
			return f == metav1.FinalizerDeleteDependents
		})
		// The resourceVersion makes the patch fail when the object changed
		// since the list, so a finalizer added in between is never dropped.
		patch, err := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"resourceVersion": item.ResourceVersion,
				"finalizers":      kept,
			},
		})
		if err != nil {
			continue
		}
		_, err = c.meta.Resource(gvr).Namespace(item.Namespace).Patch(ctx, item.Name,
			types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			continue
		}
	}
}
