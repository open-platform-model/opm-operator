// Package core provides shared primitives for the OPM rendering pipeline.
// Resource is the atomic output unit. Label constants and GVK weights support
// downstream K8s inventory and apply ordering.
//
// This package depends on the CUE SDK and k8s.io/apimachinery; nothing below
// internal/cmdutil/ should import it directly (use the Unstructured adapter there).
package core

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Resource is a single rendered platform resource produced by the pipeline.
//
// Value holds the raw CUE output from a transformer, avoiding premature
// conversion to Go-native formats. Callers convert to their required format
// (YAML, JSON, *unstructured.Unstructured, etc.) only when needed via the
// conversion methods below.
//
// Release, Component, and Transformer record provenance for inventory tracking
// and display.
type Resource struct {
	// Value is the CUE value of the rendered resource (e.g. a Kubernetes manifest).
	// Concrete and fully evaluated — safe to encode directly to YAML or JSON.
	Value cue.Value

	// Release is the name of the ModuleRelease that produced this resource.
	Release string

	// Component is the source component name within the release.
	Component string

	// Transformer is the FQN of the transformer that produced this resource.
	Transformer string
}

// Kind returns the resource kind (e.g. "Deployment").
func (r *Resource) Kind() string {
	s, _ := r.Value.LookupPath(cue.ParsePath("kind")).String() //nolint:errcheck // best-effort; empty on non-concrete
	return s
}

// Name returns the resource name from metadata.name.
func (r *Resource) Name() string {
	s, _ := r.Value.LookupPath(cue.ParsePath("metadata.name")).String() //nolint:errcheck // best-effort
	return s
}

// Namespace returns the resource namespace. Empty for cluster-scoped resources.
func (r *Resource) Namespace() string {
	s, _ := r.Value.LookupPath(cue.ParsePath("metadata.namespace")).String() //nolint:errcheck // best-effort
	return s
}

// APIVersion returns the resource apiVersion (e.g. "apps/v1").
func (r *Resource) APIVersion() string {
	s, _ := r.Value.LookupPath(cue.ParsePath("apiVersion")).String() //nolint:errcheck // best-effort
	return s
}

// GVK returns the GroupVersionKind parsed from apiVersion and kind.
func (r *Resource) GVK() schema.GroupVersionKind {
	apiVersion := r.APIVersion()
	kind := r.Kind()
	group, version := parseAPIVersion(apiVersion)
	return schema.GroupVersionKind{Group: group, Version: version, Kind: kind}
}

// Labels returns the resource labels from metadata.labels.
func (r *Resource) Labels() map[string]string {
	return r.decodeStringMap("metadata.labels")
}

// Annotations returns the resource annotations from metadata.annotations.
func (r *Resource) Annotations() map[string]string {
	return r.decodeStringMap("metadata.annotations")
}

// decodeStringMap decodes a CUE path into a map[string]string.
func (r *Resource) decodeStringMap(path string) map[string]string {
	v := r.Value.LookupPath(cue.ParsePath(path))
	if !v.Exists() {
		return nil
	}
	var m map[string]string
	if err := v.Decode(&m); err != nil {
		return nil
	}
	return m
}

// parseAPIVersion splits "group/version" or "version" into group and version.
func parseAPIVersion(apiVersion string) (group, version string) {
	if idx := strings.LastIndex(apiVersion, "/"); idx >= 0 {
		return apiVersion[:idx], apiVersion[idx+1:]
	}
	return "", apiVersion
}

// String returns a human-readable summary: "Kind/namespace/name".
func (r *Resource) String() string {
	ns := r.Namespace()
	if ns != "" {
		return fmt.Sprintf("%s/%s/%s", r.Kind(), ns, r.Name())
	}
	return fmt.Sprintf("%s/%s", r.Kind(), r.Name())
}
