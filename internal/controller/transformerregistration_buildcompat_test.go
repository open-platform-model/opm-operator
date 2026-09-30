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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/catalog"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// modFile renders a cue.mod/module.cue for the module at path with the given
// major-qualified dependencies.
func modFile(path string, deps map[string]string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "module: %q\nlanguage: version: \"v0.17.0\"\ndeps: {\n", path)
	for depPath, version := range deps {
		fmt.Fprintf(&out, "\t%q: v: %q\n", depPath, version)
	}
	out.WriteString("}\n")
	return out.String()
}

// requiringCatalog builds a catalog whose committed module file declares the
// given dependencies, which is what Requires reads.
func requiringCatalog(deps map[string]string, contracts ...string) *catalog.Catalog {
	cat := providerCatalog(contracts...)
	cat.Source = catalogSourceRequiring(deps)
	return cat
}

// platformDirWith writes a generated-platform directory whose module file
// declares the given resolved dependencies, and returns its path.
func platformDirWith(deps map[string]string) string {
	dir := GinkgoT().TempDir()
	Expect(os.MkdirAll(filepath.Join(dir, "cue.mod"), 0o750)).To(Succeed())
	Expect(os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(modFile("opmodel.dev/platforms/cluster@v0", deps)),
		0o600,
	)).To(Succeed())
	return dir
}

// buildCompatReconciler returns a reconciler whose store holds a platform
// generated with the given resolved dependencies.
func buildCompatReconciler(cat *catalog.Catalog, platformDeps map[string]string) *TransformerRegistrationReconciler {
	r := acceptanceReconciler(&stubCatalogs{cat: cat})
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{
		Identity: platformIdentity(1),
		Dir:      platformDirWith(platformDeps),
		Platform: platformProviding(nil),
	})
	r.Store = store
	return r
}

// judgeRepeats is how often a determinism spec repeats one judgement. Map
// iteration order varies per call, so one call proves nothing: on the
// two-major platform the old index picked the second major about 87% of the
// time, and 200 calls make a lucky pass negligible.
const judgeRepeats = 200

// twoMajorPlatform is a generated platform's resolution carrying two majors
// of one catalog, as an enabled v4 entry beside a disabled v5 entry yields.
func twoMajorPlatform() map[string]string {
	return map[string]string{
		"opmodel.dev/core@v2":         "v2.0.0",
		"opmodel.dev/catalogs/opm@v4": "v4.2.0",
		"opmodel.dev/catalogs/opm@v5": "v5.0.0",
	}
}

var _ = Describe("TransformerRegistration acceptance: D8 — build compatibility", func() {
	It("refuses a provider requiring a newer build within the same major", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"opmodel.dev/core@v2": "v2.1.0"}, backupTrait),
			map[string]string{"opmodel.dev/core@v2": "v2.0.0"},
		)
		judged := judge(ctx, r, claim.Name)

		Expect(judged.Status.Accepted).To(BeFalse())
		ready := readyOf(judged)
		Expect(ready.Reason).To(Equal(status.BuildIncompatibleReason))
	})

	It("refuses a provider on a different major without comparing versions", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		// The catalog's requirement is numerically LOWER than the platform's.
		// Only the major mismatch can refuse it, so a version comparison
		// reaching this case would accept.
		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"opmodel.dev/core@v1": "v1.0.0"}, backupTrait),
			map[string]string{"opmodel.dev/core@v2": "v2.0.0"},
		)
		judged := judge(ctx, r, claim.Name)

		Expect(judged.Status.Accepted).To(BeFalse())
		Expect(readyOf(judged).Reason).To(Equal(status.BuildIncompatibleReason))
		Expect(readyOf(judged).Message).To(ContainSubstring(`resolved "opmodel.dev/core@v2 at v2.0.0"`),
			"the mismatch names the platform's major with the version it resolved to")
	})

	It("does not refuse a provider requiring a build at or below the platform's", func() {
		ctx := context.Background()

		for _, catalogVersion := range []string{"v2.0.0", "v2.0.0-alpha.1"} {
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := buildCompatReconciler(
				requiringCatalog(map[string]string{"opmodel.dev/core@v2": catalogVersion}, backupTrait),
				map[string]string{"opmodel.dev/core@v2": "v2.0.0"},
			)
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeTrue(),
				"requirement %s is at or below the platform's v2.0.0", catalogVersion)
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
		}
	})

	It("does not compare an OPM path the platform does not carry", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		// The catalog requires an OPM module the platform never resolved.
		// Unshared, so there is nothing to compare and nothing to refuse.
		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"opmodel.dev/catalogs/unrelated@v1": "v1.9.0"}, backupTrait),
			map[string]string{"opmodel.dev/core@v2": "v2.0.0"},
		)
		judged := judge(ctx, r, claim.Name)

		Expect(judged.Status.Accepted).To(BeTrue())
	})

	It("does not compare a dependency outside the OPM namespace", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		// A third-party dependency is not the platform's business, even when
		// the platform happens to carry the same path.
		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"cue.dev/x/k8s.io@v0": "v0.12.0"}, backupTrait),
			map[string]string{
				"opmodel.dev/core@v2": "v2.0.0",
				"cue.dev/x/k8s.io@v0": "v0.11.0",
			},
		)
		judged := judge(ctx, r, claim.Name)

		Expect(judged.Status.Accepted).To(BeTrue(),
			"a newer third-party requirement is not a platform incompatibility")
	})

	It("words the refusal with the path, both versions, the caveat and the fix", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"opmodel.dev/core@v2": "v2.1.0"}, backupTrait),
			map[string]string{"opmodel.dev/core@v2": "v2.0.0"},
		)
		msg := readyOf(judge(ctx, r, claim.Name)).Message

		// 0015:D8 requires the wording, not just the refusal.
		Expect(msg).To(ContainSubstring("opmodel.dev/core"), "the path")
		Expect(msg).To(ContainSubstring("v2.1.0"), "what the catalog requires")
		Expect(msg).To(ContainSubstring("v2.0.0"), "what the platform resolved")
		Expect(msg).To(ContainSubstring("conservative"), "the comparison is conservative")
		Expect(msg).To(ContainSubstring("what the provider was tidied against, not what it uses"),
			"why it is conservative")
		Expect(msg).To(ContainSubstring("lowering the requirement"), "the author's fix")
	})

	It("compares a provider against the resolved entry of its own major", func() {
		cat := requiringCatalog(map[string]string{"opmodel.dev/catalogs/opm@v4": "v4.1.0"}, backupTrait)

		// The platform carries two majors of one catalog. A requirement at or
		// below its own major's resolution must pass on every call, whichever
		// order the platform's resolution is read in.
		refused := 0
		for range judgeRepeats {
			msg, err := buildIncompatibility(cat, twoMajorPlatform())
			Expect(err).NotTo(HaveOccurred())
			if msg != "" {
				refused++
			}
		}
		Expect(refused).To(BeZero(), "calls refusing an opm@v4 provider out of %d", judgeRepeats)
	})

	It("refuses a newer build against its own major, never as a major mismatch", func() {
		cat := requiringCatalog(map[string]string{"opmodel.dev/catalogs/opm@v5": "v5.1.0"}, backupTrait)

		deviating := 0
		for range judgeRepeats {
			msg, err := buildIncompatibility(cat, twoMajorPlatform())
			Expect(err).NotTo(HaveOccurred())
			if !strings.Contains(msg, `"v5.0.0"`) ||
				!strings.Contains(msg, "cannot require a newer build") ||
				strings.Contains(msg, "majors are not comparable") {
				deviating++
			}
		}
		Expect(deviating).To(BeZero(),
			"calls not refusing opm@v5 v5.1.0 as newer than v5.0.0, out of %d", judgeRepeats)
	})

	It("refuses a third major with one stable message naming every resolved major", func() {
		cat := requiringCatalog(map[string]string{"opmodel.dev/catalogs/opm@v6": "v6.0.0"}, backupTrait)

		messages := map[string]int{}
		deviating := 0
		for range judgeRepeats {
			msg, err := buildIncompatibility(cat, twoMajorPlatform())
			Expect(err).NotTo(HaveOccurred())
			messages[msg]++
			if !strings.Contains(msg, "opmodel.dev/catalogs/opm@v4 at v4.2.0") ||
				!strings.Contains(msg, "opmodel.dev/catalogs/opm@v5 at v5.0.0") ||
				!strings.Contains(msg, "majors are not comparable") {
				deviating++
			}
		}
		Expect(deviating).To(BeZero(),
			"calls whose message misses a resolved major, out of %d (%d distinct messages)",
			judgeRepeats, len(messages))
		Expect(messages).To(HaveLen(1), "distinct messages over %d calls", judgeRepeats)
	})

	It("names an unversioned platform major by its path alone", func() {
		cat := requiringCatalog(map[string]string{"opmodel.dev/catalogs/opm@v6": "v6.0.0"}, backupTrait)

		// A local replacement serves opm@v5 with no version beside a versioned
		// opm@v4.
		msg, err := buildIncompatibility(cat, map[string]string{
			"opmodel.dev/core@v2":         "v2.0.0",
			"opmodel.dev/catalogs/opm@v4": "v4.2.0",
			"opmodel.dev/catalogs/opm@v5": "",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(msg).To(ContainSubstring(
			`resolved "opmodel.dev/catalogs/opm@v4 at v4.2.0, opmodel.dev/catalogs/opm@v5"`))
		Expect(msg).To(ContainSubstring("majors are not comparable"))
	})

	It("accepts an own-major provider on a platform carrying a second major in its closure", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		claim := createClaim(ctx, ns)
		ownProvidedInventory(ctx, ns, claim.Name)

		// The platform's closure carries two majors of one catalog, the
		// provider's own opm@v4 among them, as an enabled opm@v4 entry beside a
		// disabled opm@v5 entry yields: the generated closure roots every
		// registry entry, disabled ones included.
		r := buildCompatReconciler(
			requiringCatalog(map[string]string{"opmodel.dev/catalogs/opm@v4": "v4.1.0"}, backupTrait),
			twoMajorPlatform(),
		)
		judged := judge(ctx, r, claim.Name)

		Expect(judged.Status.Accepted).To(BeTrue())
		Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
	})
})
