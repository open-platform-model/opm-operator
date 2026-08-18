// Package identity is the single source of this module's path and version
// (core #IdentityPackage, enhancements 0010 D38 / 0011 D12). It sits at the
// bottom of the module's import graph — no intra-module imports, no core
// import; validation is external (a publishing tool unifies this package
// against core's #IdentityPackage).
package identity

// #VersionType mirrors core.#VersionType (SemVer 2.0), duplicated so this
// package stays import-free.
#VersionType: string & =~"^\\d+\\.\\d+\\.\\d+(-[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"

// ModulePath is the module's complete CUE module path, major suffix included
// — byte-identical to cue.mod's `module:` field.
ModulePath: "testing.opmodel.dev/modules/operator/hello_web@v0"

// Version is the module's bare SemVer; its major must agree with ModulePath's.
// Hand-managed: these fixtures are not on release-please's version train, so a
// bump is an explicit edit here (or `opm module version set`) followed by a
// re-pin in moduleinstance.yaml and the modulepackage fixture.
Version: #VersionType | *"0.1.3"
