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

// Package version carries the operator's build-time version identity.
//
// The version is burned into source rather than injected at build time:
// release-please rewrites the annotated Version constant on every Release PR
// (via extra-files in release-please-config.json), so any build of a tagged
// commit — container image, local go build, go install — reports the tag's
// version with no ldflags or Dockerfile cooperation. The PlatformReconciler
// publishes it to Platform.status.operatorVersion (enhancement 0006 D24),
// where the CLI's version-skew ceiling reads it.
package version

import (
	"runtime/debug"
)

// Version is the operator's semantic version, without the leading "v".
// The trailing annotation is load-bearing: release-please's generic updater
// rewrites this line on every Release PR. Do not detach the comment from the
// constant.
const Version = "1.0.0-alpha.8" // x-release-please-version

// Full returns the operator version as published to
// Platform.status.operatorVersion: "v" + Version, matching release tags
// (e.g. "v1.0.0-alpha.2").
//
// When the binary was built from a VCS checkout that exposes build info
// (local builds; release images build without .git and stay clean), a
// "+g<short-revision>" build-metadata suffix is appended, with ".dirty" for
// modified worktrees — provenance for dev builds. Consumers comparing
// versions must strip the "+" suffix (SemVer build metadata).
func Full() string {
	v := "v" + Version

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}

	var revision string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return v
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	v += "+g" + revision
	if dirty {
		v += ".dirty"
	}
	return v
}
