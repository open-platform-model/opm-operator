// Package identity is the single source of this catalog's path and version
// (core #IdentityPackage). It sits at the bottom of the catalog's import graph,
// with no intra-module import and no core import, so the member packages can
// source the kind prefixes from it; a publishing tool unifies it against core's
// #IdentityPackage.
package identity

// ModulePath is the catalog's complete CUE module path, major suffix included,
// byte-identical to cue.mod's `module:` field.
ModulePath: "testing.opmodel.dev/catalogs/operator/provider@v0"

// Version is the catalog's bare SemVer. Hand-managed: this fixture is not on
// release-please's train, so a bump is `opm catalog version set <semver> .`
// (published versions are immutable; `hack/fixtures.sh check` refuses an edit
// without one). A plain literal, never a defaulted disjunction.
Version: "0.1.0"

// RegistryPath is the major-free OCI repository path.
RegistryPath: "testing.opmodel.dev/catalogs/operator/provider"

// kindPrefix mirrors core's #IdentityPackage.kindPrefix. Every member FQN
// hangs off this catalog's own path, so none can collide with a member of
// another catalog a platform subscribes beside it.
kindPrefix: {
	resources:    RegistryPath + "/resources"
	traits:       RegistryPath + "/traits"
	blueprints:   RegistryPath + "/blueprints"
	transformers: RegistryPath + "/transformers"
}
