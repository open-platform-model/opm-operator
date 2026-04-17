package status

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/pkg/core"
)

// DigestSet holds the four reconcile digests tracked in ModuleRelease.status.
// Uses named fields rather than a map for type safety (design decision 3).
//
// Maps to status fields:
//
//	Source    → lastAttemptedSourceDigest / lastAppliedSourceDigest
//	Config    → lastAttemptedConfigDigest / lastAppliedConfigDigest
//	Render    → lastAttemptedRenderDigest / lastAppliedRenderDigest
//	Inventory → status.inventory.digest
type DigestSet struct {
	// Source is the artifact content digest from Flux OCIRepository.status.artifact.
	Source string

	// Config is the SHA-256 of normalized user values.
	Config string

	// Render is the SHA-256 of the sorted, serialized rendered resource set.
	Render string

	// Inventory is the SHA-256 of the owned resource inventory
	// (computed via internal/inventory.ComputeDigest).
	Inventory string
}

// ModuleSourceDigest computes a deterministic source digest from a CUE module
// path and version. Replaces SourceDigest for the CUE-native module resolution
// path where there is no Flux artifact digest.
func ModuleSourceDigest(modulePath, moduleVersion string) string {
	sum := sha256.Sum256([]byte(modulePath + "@" + moduleVersion))
	return fmt.Sprintf("sha256:%x", sum)
}

// ConfigDigest computes a deterministic SHA-256 digest of the release values.
// Serializes RawValues to canonical JSON (sorted keys), then hashes.
// Returns the SHA-256 of empty input if values is nil (nil = no config),
// consistent with inventory.ComputeDigest(nil).
// Format: "sha256:<hex>"
func ConfigDigest(values *releasesv1alpha1.RawValues) string {
	if values == nil || len(values.Raw) == 0 {
		sum := sha256.Sum256(nil)
		return fmt.Sprintf("sha256:%x", sum)
	}
	// RawValues embeds apiextensionsv1.JSON which stores raw bytes.
	// Unmarshal then re-marshal with sorted keys for canonical form.
	var obj any
	if err := json.Unmarshal(values.Raw, &obj); err != nil {
		// If the raw bytes are not valid JSON, hash them directly.
		sum := sha256.Sum256(values.Raw)
		return fmt.Sprintf("sha256:%x", sum)
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		sum := sha256.Sum256(values.Raw)
		return fmt.Sprintf("sha256:%x", sum)
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", sum)
}

// RenderDigest computes a deterministic SHA-256 digest of the rendered resource set.
// Sorts resources by GVK + namespace + name (same order as inventory.ComputeDigest
// for consistency), serializes each via core.Resource.MarshalJSON(),
// and hashes the concatenation.
// Format: "sha256:<hex>"
func RenderDigest(resources []*core.Resource) (string, error) {
	sorted := make([]*core.Resource, len(resources))
	copy(sorted, resources)
	sort.SliceStable(sorted, func(i, j int) bool {
		gi, gj := sorted[i].GVK(), sorted[j].GVK()
		if gi.Group != gj.Group {
			return gi.Group < gj.Group
		}
		if gi.Kind != gj.Kind {
			return gi.Kind < gj.Kind
		}
		if sorted[i].Namespace() != sorted[j].Namespace() {
			return sorted[i].Namespace() < sorted[j].Namespace()
		}
		return sorted[i].Name() < sorted[j].Name()
	})

	h := sha256.New()
	for _, r := range sorted {
		b, err := r.MarshalJSON()
		if err != nil {
			return "", fmt.Errorf("render digest: %w", err)
		}
		h.Write(b)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// IsNoOp returns true if all four digests in current match lastApplied.
// Returns false if any lastApplied field is empty (handles first reconcile).
func IsNoOp(current, lastApplied DigestSet) bool {
	if lastApplied.Source == "" || lastApplied.Config == "" ||
		lastApplied.Render == "" || lastApplied.Inventory == "" {
		return false
	}
	return current.Source == lastApplied.Source &&
		current.Config == lastApplied.Config &&
		current.Render == lastApplied.Render &&
		current.Inventory == lastApplied.Inventory
}
