package platform

import (
	"slices"
	"testing"
)

func coords(pairs ...string) []ClaimCoordinate {
	out := make([]ClaimCoordinate, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, ClaimCoordinate{Catalog: pairs[i], Version: pairs[i+1]})
	}
	return out
}

func TestPackageIdentity_SameInputsSameIdentity(t *testing.T) {
	claims := coords(
		"opmodel.dev/catalogs/k8up@v1", "1.2.0",
		"opmodel.dev/catalogs/cnpg@v1", "0.4.1",
	)
	a := NewPackageIdentity(7, claims)
	b := NewPackageIdentity(7, claims)
	if a != b {
		t.Fatalf("the same inputs must yield the same identity: %v != %v", a, b)
	}
	if a.String() != b.String() {
		t.Fatalf("the same inputs must yield the same string form: %q != %q", a, b)
	}
}

func TestPackageIdentity_ClaimOrderDoesNotMatter(t *testing.T) {
	forward := NewPackageIdentity(3, coords(
		"opmodel.dev/catalogs/a@v1", "1.0.0",
		"opmodel.dev/catalogs/b@v1", "2.0.0",
		"opmodel.dev/catalogs/c@v2", "3.0.0",
	))
	reversed := NewPackageIdentity(3, coords(
		"opmodel.dev/catalogs/c@v2", "3.0.0",
		"opmodel.dev/catalogs/b@v1", "2.0.0",
		"opmodel.dev/catalogs/a@v1", "1.0.0",
	))
	if forward != reversed {
		t.Fatalf("claim order must not change the identity: %v != %v", forward, reversed)
	}
	want := []string{
		"opmodel.dev/catalogs/a@v1@1.0.0",
		"opmodel.dev/catalogs/b@v1@2.0.0",
		"opmodel.dev/catalogs/c@v2@3.0.0",
	}
	if got := reversed.Claims(); !slices.Equal(got, want) {
		t.Fatalf("Claims should report canonical order, got %v want %v", got, want)
	}
}

func TestPackageIdentity_EveryInputChangeYieldsANewIdentity(t *testing.T) {
	base := NewPackageIdentity(4, coords("opmodel.dev/catalogs/k8up@v1", "1.2.0"))

	cases := map[string]PackageIdentity{
		"a later CR generation":    NewPackageIdentity(5, coords("opmodel.dev/catalogs/k8up@v1", "1.2.0")),
		"a claim at a new version": NewPackageIdentity(4, coords("opmodel.dev/catalogs/k8up@v1", "1.3.0")),
		"a different catalog":      NewPackageIdentity(4, coords("opmodel.dev/catalogs/cnpg@v1", "1.2.0")),
		"an added claim": NewPackageIdentity(4, coords(
			"opmodel.dev/catalogs/k8up@v1", "1.2.0",
			"opmodel.dev/catalogs/cnpg@v1", "0.4.1",
		)),
		"the claim withdrawn": NewPackageIdentity(4, nil),
	}
	for name, other := range cases {
		if base == other {
			t.Errorf("%s must yield a different identity, both are %v", name, base)
		}
		if base.String() == other.String() {
			t.Errorf("%s must yield a different string form, both are %q", name, base)
		}
	}
}

func TestPackageIdentity_RepeatedCoordinateCollapses(t *testing.T) {
	once := NewPackageIdentity(2, coords("opmodel.dev/catalogs/k8up@v1", "1.2.0"))
	twice := NewPackageIdentity(2, coords(
		"opmodel.dev/catalogs/k8up@v1", "1.2.0",
		"opmodel.dev/catalogs/k8up@v1", "1.2.0",
	))
	if once != twice {
		t.Fatalf("a repeated coordinate must not change the identity: %v != %v", once, twice)
	}
}

func TestPackageIdentity_StringForm(t *testing.T) {
	bare := NewPackageIdentity(9, nil)
	if got := bare.String(); got != "gen-9" {
		t.Fatalf("with no active claims the identity keeps the plain generation form, got %q", got)
	}
	if bare.Claims() != nil {
		t.Fatalf("with no active claims Claims should be empty, got %v", bare.Claims())
	}
	if bare.Generation() != 9 {
		t.Fatalf("Generation = %d, want 9", bare.Generation())
	}

	withClaim := NewPackageIdentity(9, coords("opmodel.dev/catalogs/k8up@v1", "1.2.0"))
	got := withClaim.String()
	if len(got) != len("gen-9-")+16 {
		t.Fatalf("an identity carrying claims should be the generation plus a bounded digest, got %q", got)
	}
	if got[:len("gen-9-")] != "gen-9-" {
		t.Fatalf("the string form should stay prefixed by its generation, got %q", got)
	}
}

func TestPackageIdentity_Zero(t *testing.T) {
	var zero PackageIdentity
	if !zero.IsZero() {
		t.Fatal("the zero identity should report IsZero")
	}
	if NewPackageIdentity(1, nil).IsZero() {
		t.Fatal("a real generation is not the zero identity")
	}
}
