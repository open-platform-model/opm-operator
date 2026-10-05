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

package version

import (
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

// semverRe matches the constant's expected shape: MAJOR.MINOR.PATCH with an
// optional pre-release, no leading "v", no build metadata.
var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func TestVersionIsSemver(t *testing.T) {
	if !semverRe.MatchString(Version) {
		t.Fatalf("Version %q is not a bare semver (expected e.g. 1.0.0-alpha.2)", Version)
	}
}

// TestReleasePleaseAnnotationOnConstLine guards the release automation
// contract: the generic updater only rewrites lines annotated with
// x-release-please-version, so the annotation must sit on the same line as
// the Version constant. A reformat that detaches it would silently freeze
// the released version.
func TestReleasePleaseAnnotationOnConstLine(t *testing.T) {
	src, err := os.ReadFile("version.go")
	if err != nil {
		t.Fatalf("reading version.go: %v", err)
	}
	for line := range strings.SplitSeq(string(src), "\n") {
		if strings.Contains(line, "x-release-please-version") {
			if strings.Contains(line, "const Version = ") && strings.Contains(line, Version) {
				return
			}
			t.Fatalf("x-release-please-version annotation found on a line without the Version constant: %q", line)
		}
	}
	t.Fatal("no line in version.go carries the x-release-please-version annotation")
}

func TestFullPrefixAndMetadata(t *testing.T) {
	full := Full()
	want := "v" + Version
	if full != want && !strings.HasPrefix(full, want+"+g") {
		t.Fatalf("Full() = %q; want %q or %q with a +g<rev>[.dirty] suffix", full, want, want)
	}
}

// installPage is the docs page whose operator versions release-please
// rewrites through extra-files (keep-install-page-current).
const installPage = "../../docs/site/start/install-the-operator.md"

// pageVersionRe matches a version as release-please's generic updater does.
var pageVersionRe = regexp.MustCompile(`v?\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?`)

// installPageBlockVersions returns the versions inside the page's
// x-release-please-start-version ... x-release-please-end blocks, each with
// its line number, and an error when the page has no block or a marker is
// unbalanced.
func installPageBlockVersions(src string) ([][2]string, error) {
	var found [][2]string
	blocks, open := 0, 0
	for i, line := range strings.Split(src, "\n") {
		n := i + 1
		switch {
		case strings.Contains(line, "x-release-please-start-version"):
			if open != 0 {
				return nil, fmt.Errorf("line %d: a start marker inside the block opened at line %d", n, open)
			}
			open = n
		case strings.Contains(line, "x-release-please-end"):
			if open == 0 {
				return nil, fmt.Errorf("line %d: an end marker without a start marker", n)
			}
			open = 0
			blocks++
		case open != 0:
			for _, v := range pageVersionRe.FindAllString(line, -1) {
				found = append(found, [2]string{fmt.Sprint(n), strings.TrimRight(v, ".")})
			}
		}
	}
	if open != 0 {
		return nil, fmt.Errorf("line %d: a start marker without an end marker", open)
	}
	if blocks == 0 {
		return nil, fmt.Errorf("no x-release-please-start-version block")
	}
	return found, nil
}

// TestInstallPageNamesThisRelease guards the install page's release-please
// blocks: release-please rewrites every version inside them in the Release
// PR, so they must exist, be balanced, and name only the operator's Version.
// A core or catalog version inside a block would be rewritten to the
// operator's version.
func TestInstallPageNamesThisRelease(t *testing.T) {
	src, err := os.ReadFile(installPage)
	if err != nil {
		t.Fatalf("reading %s: %v", installPage, err)
	}
	found, err := installPageBlockVersions(string(src))
	if err != nil {
		t.Fatalf("%s: %v", installPage, err)
	}
	for _, f := range found {
		if strings.TrimPrefix(f[1], "v") != Version {
			t.Errorf("%s:%s: version %s inside a release-please block, want %s (the operator's Version)", installPage, f[0], f[1], Version)
		}
	}
}

func TestLibraryVersion(t *testing.T) {
	lib := func(m *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{Deps: []*debug.Module{
			{Path: "github.com/fluxcd/pkg/ssa", Version: "v0.50.0"},
			m,
		}}
	}
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"plain dependency", lib(&debug.Module{Path: libraryModule, Version: "v1.0.0-beta.4"}), true, "v1.0.0-beta.4"},
		{"replaced by a version", lib(&debug.Module{Path: libraryModule, Version: "v1.0.0-beta.4",
			Replace: &debug.Module{Path: "github.com/fork/library", Version: "v1.0.0-beta.5"}}), true, "v1.0.0-beta.5"},
		{"replaced by a path", lib(&debug.Module{Path: libraryModule, Version: "v1.0.0-beta.4",
			Replace: &debug.Module{Path: "../library"}}), true, "(devel) ../library"},
		{"absent", lib(&debug.Module{Path: "github.com/other/module", Version: "v1.0.0"}), true, ""},
		{"no build info", nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := libraryVersion(tc.info, tc.ok); got != tc.want {
				t.Fatalf("libraryVersion = %q, want %q", got, tc.want)
			}
		})
	}
}
