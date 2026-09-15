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
		Generation: 1,
		Dir:        platformDirWith(platformDeps),
		Platform:   platformProviding(nil),
	})
	r.Store = store
	return r
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

		// D8 requires the wording, not just the refusal.
		Expect(msg).To(ContainSubstring("opmodel.dev/core"), "the path")
		Expect(msg).To(ContainSubstring("v2.1.0"), "what the catalog requires")
		Expect(msg).To(ContainSubstring("v2.0.0"), "what the platform resolved")
		Expect(msg).To(ContainSubstring("conservative"), "the comparison is conservative")
		Expect(msg).To(ContainSubstring("what the provider was tidied against, not what it uses"),
			"why it is conservative")
		Expect(msg).To(ContainSubstring("lowering the requirement"), "the author's fix")
	})
})
