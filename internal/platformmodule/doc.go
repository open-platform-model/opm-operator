// Package platformmodule generates the operator's platform CUE module from the
// cluster-singleton Platform CR (enhancement 0019 D6). The CR keeps naming
// catalog coordinates in typed fields; this package turns them into a real
// CUE module on the operator's own disk, which is what the render build
// consumes now that a platform carries its catalogs by import (0019 D5).
//
// Three seams, each independently testable:
//
//   - [Generate] is pure: CR-derived input plus the resolved dependency
//     closure in, deterministic file bytes out. The same input always yields
//     byte-identical files.
//   - [Closure] derives the module's full dependency list from the pinned
//     modules' published module files: the roots (core and every subscribed
//     catalog) plus everything they transitively require, at the maximum
//     version any requirement names. It is the tidied list a `cue mod tidy`
//     would write, computed without a tidy on the reconcile path (0019 D13:
//     tidying happens once, at platform-package generation).
//   - [Layout] owns the on-disk lifecycle: per-generation directories,
//     staging plus rename so a module is either absent or complete, retention
//     of the current and previous generation, and the boot-time reset.
//
// The generated module lives under the reserved-unpublished path
// [ModulePath] and is never published, written to the cluster or served to
// anything but this process (0019 D6).
package platformmodule
