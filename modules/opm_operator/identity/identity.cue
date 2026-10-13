// Package identity is the single source of this module's path and version
// (core #IdentityPackage). Release tooling writes these two fields only.
package identity

// ModulePath is byte-identical to cue.mod's `module:` field.
ModulePath: "opmodel.dev/modules/opm_operator@v0"

// Version is the module's bare SemVer, on its own train: it is not the
// operator's version (that is operator.Version).
Version: "0.2.0"
