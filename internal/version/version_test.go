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
	"os"
	"regexp"
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
