package status

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// DigestSet holds the four reconcile digests tracked in ModuleInstance.status.
// This package computes the source and config digests; the render and
// inventory digests are the library's (opm/k8s/inventory).
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

	// Render is the render digest: RenderDigest from the library's
	// opm/k8s/inventory over the render's one object.Export.
	Render string

	// Inventory is the inventory digest: Digest from the library's
	// opm/k8s/inventory over the inventory's entries.
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
// Returns the SHA-256 of empty input if values is nil (nil = no config).
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

// renderInputsEncoding names the encoding RenderInputKey.Digest hashes. A
// change to the parts or their order changes this tag, which changes every
// digest and so costs one render per object, never a wrong skip.
const renderInputsEncoding = "opm-render-inputs/v1"

// RenderInputKey holds the inputs a render is a function of: what the
// operator renders, with what values, against which platform package, under
// which skew policy, by which operator and library. Its digest is recorded in
// status.lastAppliedInputs and compared before the next render.
type RenderInputKey struct {
	// Source is ModuleSourceDigest for a ModuleInstance, the Flux artifact
	// digest for a ModulePackage.
	Source string
	// Config is ConfigDigest(spec.values); ConfigDigest(nil) for a
	// ModulePackage.
	Config string
	// PackageIdentity is the platform package identity in the string form
	// Platform.status.packageIdentity carries.
	PackageIdentity string
	// SkewPolicy is the catalog skew policy as the API spells it ("Warn" or
	// "Refuse").
	SkewPolicy string
	// OperatorVersion is the running operator's version.Full().
	OperatorVersion string
	// LibraryVersion is the running operator's version.Library().
	LibraryVersion string
}

// parts returns the key's parts in their fixed encoding order.
func (k RenderInputKey) parts() []string {
	return []string{k.Source, k.Config, k.PackageIdentity, k.SkewPolicy, k.OperatorVersion, k.LibraryVersion}
}

// Complete reports whether every part is set. An incomplete key is never
// recorded and never matches, so a missing part always means "render".
func (k RenderInputKey) Complete() bool {
	return !slices.Contains(k.parts(), "")
}

// Digest returns "sha256:<hex>" over the encoding tag and each part as
// "<len>:<value>", in field order. The length prefixes make the encoding
// unambiguous without escaping: no two different keys encode alike.
func (k RenderInputKey) Digest() string {
	h := sha256.New()
	writePart := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s)) + ":" + s))
	}
	writePart(renderInputsEncoding)
	for _, p := range k.parts() {
		writePart(p)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}
