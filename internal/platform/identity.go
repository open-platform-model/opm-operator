package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// ClaimCoordinate is one active claim's catalog coordinate: the provider
// catalog's major-suffixed CUE module path and the released version the claim
// pins. It is the unit a [PackageIdentity] is built from, and the same pair
// the generated module's #registry entry carries.
type ClaimCoordinate struct {
	Catalog string
	Version string
}

// String returns the coordinate's canonical "<catalog>@<version>" form.
func (c ClaimCoordinate) String() string { return c.Catalog + "@" + c.Version }

// PackageIdentity identifies one generated platform package by the two inputs
// the package is a function of (0015:D13): the Platform CR's
// .metadata.generation and the sorted set of active claims' catalog
// coordinates. Enhancement 0015:D17 makes it the unit the operator holds,
// because a claim activating changes the package while leaving the generation
// untouched: keying on the generation alone would leave a render consuming a
// platform that does not contain the provider just accepted.
//
// The identity is computed from the inputs at generation time and stamped on
// the result. It is never read back as an input and nothing reconstructs the
// active set from it, which keeps generation a pure function: the same tuple
// yields the same identity, and an identity mismatch is the signal that the
// tuple moved.
//
// The value is comparable, so it keys the store's maps directly, and its
// fields are unexported so an identity can only be built through
// [NewPackageIdentity] and therefore always carries canonically ordered
// claims.
type PackageIdentity struct {
	generation int64

	// claims is the canonical form of the active set: every coordinate's
	// "<catalog>@<version>" string, sorted and deduplicated, comma-joined.
	// Sorting is what makes the identity independent of the order the claims
	// were listed in.
	claims string
}

// NewPackageIdentity returns the identity of a package generated for the
// given Platform CR generation from the given active claims. The claims are
// canonically ordered, so callers may pass them in any order; repeated
// coordinates collapse, since one coordinate contributes one #registry entry
// however many times it is listed.
func NewPackageIdentity(generation int64, claims []ClaimCoordinate) PackageIdentity {
	coords := make([]string, 0, len(claims))
	for _, c := range claims {
		coords = append(coords, c.String())
	}
	slices.Sort(coords)
	coords = slices.Compact(coords)
	return PackageIdentity{generation: generation, claims: strings.Join(coords, ",")}
}

// Generation returns the Platform CR generation the identity was built for.
func (id PackageIdentity) Generation() int64 { return id.generation }

// Claims returns the active claims' coordinates in canonical order, as
// "<catalog>@<version>" strings. The slice is freshly built, so a caller may
// keep or sort it without disturbing the identity.
func (id PackageIdentity) Claims() []string {
	if id.claims == "" {
		return nil
	}
	return strings.Split(id.claims, ",")
}

// IsZero reports whether id is the zero identity, which no generated package
// carries: a stored Platform's .metadata.generation is at least 1.
func (id PackageIdentity) IsZero() bool { return id == PackageIdentity{} }

// String returns the identity's stable, bounded, filesystem-safe form:
// "gen-<generation>" when no claim is active, and "gen-<generation>-<digest>"
// otherwise, where digest is the first 8 bytes of the SHA-256 of the
// canonical claim list. Bounded rather than the claim list verbatim because
// the string is both a directory name and a status field; the enumerable form
// of the active set is Platform status's resolved registry union, not this.
//
// The same inputs always produce the same string and any change to either
// input produces a different one. A platform with no active claims keeps the
// plain "gen-<generation>" form the operator used before claims existed.
func (id PackageIdentity) String() string {
	name := "gen-" + strconv.FormatInt(id.generation, 10)
	if id.claims == "" {
		return name
	}
	sum := sha256.Sum256([]byte(id.claims))
	return name + "-" + hex.EncodeToString(sum[:8])
}
