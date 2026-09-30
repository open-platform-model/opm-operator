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

package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/open-platform-model/library/opm/catalog"
)

// platformModFileName is the generated platform module's module file, relative
// to the directory the Platform reconciler wrote it to.
const platformModFileName = "cue.mod/module.cue"

// opmNamespacePrefixes are the module-path prefixes 0015:D8's comparison covers. A
// catalog's third-party dependencies are not the platform's business: only a
// path both sides could share can make a provider build-incompatible with the
// platform it would run in.
var opmNamespacePrefixes = []string{"opmodel.dev/", "testing.opmodel.dev/"}

// platformRequirements reads the dependency versions the generated platform
// module committed, keyed by major-qualified path exactly as
// [catalog.Catalog.Requires] keys its own answer.
//
// This is the platform's RESOLVED side, not what its CR asked for:
// Platform.spec.registry records the subscriptions, while the generated
// module records the closure they resolved to, which is what a provider's
// transformers will actually build against (0019:D18's
// committed-resolution discipline).
func platformRequirements(dir string) (map[string]string, error) {
	path := filepath.Join(dir, filepath.FromSlash(platformModFileName))
	data, err := os.ReadFile(path) //nolint:gosec // dir is the operator's own --platform-dir
	if err != nil {
		return nil, fmt.Errorf("reading generated platform %s: %w", platformModFileName, err)
	}
	f, err := modfile.Parse(data, path)
	if err != nil {
		return nil, fmt.Errorf("parsing generated platform %s: %w", platformModFileName, err)
	}

	reqs := make(map[string]string, len(f.Deps))
	for depPath, dep := range f.Deps {
		if dep == nil {
			continue
		}
		reqs[depPath] = dep.Version
	}
	return reqs, nil
}

// buildIncompatibility compares the catalog's committed requirements against
// the platform's resolved versions and returns the refusal message for the
// first incompatible shared OPM-namespace path, or the empty string when
// every shared path is compatible (0015:D8, 0026:D7:R2).
//
// Two majors of one module are two paths. A requirement is compared against
// the platform's resolution of the provider's own major, which the platform
// can carry beside other majors of the same module (every registry entry,
// disabled ones included, is in the generated closure). Within that major, a
// requirement GREATER than the platform's is refused; at or below is
// accepted, because a provider tidied against an older build runs against a
// newer platform. A requirement is refused as a major mismatch, without
// comparing versions, only when the platform resolves the module in no path
// of that major, because majors do not compare.
//
// Refusing here rather than at render is the point: a render failure would
// name whichever unrelated module instance happened to trigger the build,
// while this names the provider that is actually incompatible.
//
// Paths are walked in sorted order so a catalog incompatible on several paths
// always reports the same one, rather than whichever the map iteration
// surfaced, and a mismatch message lists the platform's majors sorted, so the
// verdict and its wording never depend on the order the resolution is read in.
func buildIncompatibility(cat *catalog.Catalog, platformReqs map[string]string) (string, error) {
	catalogReqs, err := cat.Requires()
	if err != nil {
		return "", err
	}

	// platformReqs answers the same-major lookup directly, being keyed by
	// major-qualified path. This second index groups the platform's paths by
	// base path, sorted, to tell a major mismatch apart from a module the
	// platform does not carry at all.
	majorsByBase := make(map[string][]string, len(platformReqs))
	for qualified := range platformReqs {
		base, _, ok := ast.SplitPackageVersion(qualified)
		if !ok {
			continue
		}
		majorsByBase[base] = append(majorsByBase[base], qualified)
	}
	for _, qualified := range majorsByBase {
		sort.Strings(qualified)
	}

	paths := make([]string, 0, len(catalogReqs))
	for p := range catalogReqs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, qualified := range paths {
		if !inOPMNamespace(qualified) {
			continue
		}
		base, _, ok := ast.SplitPackageVersion(qualified)
		if !ok {
			continue
		}
		catalogVersion := catalogReqs[qualified]

		if platformVersion, same := platformReqs[qualified]; same {
			if catalogVersion == "" || platformVersion == "" {
				// A path a local replacement serves carries no version.
				// Nothing to compare, and nothing to refuse it on.
				continue
			}
			if semver.Compare(catalogVersion, platformVersion) > 0 {
				return conservativeRefusal(base, qualified, catalogVersion, platformVersion,
					"a provider cannot require a newer build than the platform it runs in"), nil
			}
			continue
		}

		resolved := majorsByBase[base]
		if len(resolved) == 0 {
			continue
		}
		return conservativeRefusal(base, qualified, catalogVersion, resolvedMajors(resolved, platformReqs),
			"majors are not comparable, so the requirement is refused without comparing versions"), nil
	}

	return "", nil
}

// resolvedMajors names each of the platform's sorted paths with the version it
// resolved to, or the path alone when a local replacement serves it with no
// version.
func resolvedMajors(qualified []string, platformReqs map[string]string) string {
	named := make([]string, 0, len(qualified))
	for _, path := range qualified {
		if version := platformReqs[path]; version != "" {
			named = append(named, path+" at "+version)
			continue
		}
		named = append(named, path)
	}
	return strings.Join(named, ", ")
}

// conservativeRefusal words a 0015:D8 refusal. 0015:D8 requires the wording, not just
// the refusal: the message names the path and both versions, says the
// comparison is conservative, and says what the author does about it.
func conservativeRefusal(base, qualified, catalogVersion, platformVersion, why string) string {
	return fmt.Sprintf(
		"Claimed catalog requires %s at %q but the platform resolved %q: %s. "+
			"The comparison is conservative: a cue.mod requirement records what the provider was tidied "+
			"against, not what it uses, so lowering the requirement on %s is the author's fix.",
		qualified, catalogVersion, platformVersion, why, base)
}

// inOPMNamespace reports whether a module path lives in a namespace the
// platform and a provider catalog can share.
func inOPMNamespace(path string) bool {
	for _, prefix := range opmNamespacePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
