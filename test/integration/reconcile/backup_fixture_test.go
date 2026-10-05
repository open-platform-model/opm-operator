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

package reconcile_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/mod/modfile"
	"github.com/open-platform-model/library/opm/catalog"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/schema"
	"golang.org/x/mod/semver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/labels"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// backupTraitFQN is the provider-fulfilled contract the backup fixture set is
// built around: opm declares it and ships no transformer for it.
const backupTraitFQN = "opmodel.dev/catalogs/opm/traits/backup@v1alpha1"

// These specs hold the backup fixture set (test/fixtures/catalogs/backup,
// test/fixtures/modules/backup_provider, test/fixtures/modules/backup_consumer)
// to the facts a cluster relies on when it accepts and activates the
// provider's claim (0015:D3, 0015:D11): the claim backup_provider renders
// names the backup catalog fixture at its declared build and lists exactly
// the contracts that catalog implements, and backup_consumer renders through
// that catalog once it is in the registry and is refused naming the contract
// while it is not. The literal claim in backup_provider is what the first spec
// guards: a bump of the backup catalog that skips its re-pin fails here. That
// literal deviates from 0015:D11:R1 (no field of a rendered registration is
// authored) on purpose; the fixture README says why. The last two specs run
// acceptance on that claim against the catalog as published (core older than
// the library's ProvidesSince) and against a copy pinned to ProvidesSince, one
// for each way the library derives a catalog's provider set.
var _ = Describe("Backup fixture set (registry-backed)", func() {
	var (
		k        *kernel.Kernel
		registry string
		backup   fixtures.Coordinate
	)

	BeforeEach(func() {
		skipIfNoTestRegistry()
		registry = os.Getenv("CUE_REGISTRY")
		k = kernel.New(kernel.WithRegistry(registry))
		backup = fixtures.MustCatalog(GinkgoT(), "backup")
	})

	It("renders a claim that names the backup catalog and provides exactly what it implements", func() {
		store := generatedPlatformStore(k, registry)
		r := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: labels.ManagedByController,
		}

		provider := fixtures.Must(GinkgoT(), "backup_provider")
		res, err := r.RenderModule(ctx, "backup-provider", "default", provider.ModulePath, provider.Tag(), nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Resources).To(HaveLen(1), "backup_provider renders its claim and nothing else")

		claim, err := res.Resources[0].ToUnstructured()
		Expect(err).NotTo(HaveOccurred())
		Expect(claim.GetAPIVersion()).To(Equal("opmodel.dev/v1alpha1"))
		Expect(claim.GetKind()).To(Equal("TransformerRegistration"))
		Expect(claim.GetName()).To(Equal("default.backup-provider"),
			"the claim's name is instance-derived (0015:D12)")

		catalogPath, _, err := unstructured.NestedString(claim.Object, "spec", "catalog")
		Expect(err).NotTo(HaveOccurred())
		version, _, err := unstructured.NestedString(claim.Object, "spec", "version")
		Expect(err).NotTo(HaveOccurred())
		provides, _, err := unstructured.NestedStringSlice(claim.Object, "spec", "provides")
		Expect(err).NotTo(HaveOccurred())
		ref, _, err := unstructured.NestedStringMap(claim.Object, "spec", "providerRef")
		Expect(err).NotTo(HaveOccurred())

		Expect(catalogPath).To(Equal(backup.ModulePath), "the claim names the backup catalog fixture")
		Expect(version).To(Equal(backup.Version),
			"the claim names the backup catalog's declared build: a catalog bump re-pins backup_provider")
		Expect(ref).To(Equal(map[string]string{"name": "backup-provider", "namespace": "default"}),
			"providerRef is stamped from the rendering instance (0015:D11)")

		// Acceptance resolves the catalog with the claim's own fields, the bare
		// SemVer opm renders included, and re-derives provides from it
		// (0015:D11): this is the same call with the same inputs. A library
		// that resolves only a v-prefixed version fails here, as acceptance
		// would refuse the claim CatalogUnresolved (opm-operator#210).
		cat, err := k.AcquireCatalogFromRegistry(ctx, catalogPath, version)
		Expect(err).NotTo(HaveOccurred())
		derived, err := cat.Provides()
		Expect(err).NotTo(HaveOccurred())
		Expect(derived).To(ConsistOf(backupTraitFQN), "the backup catalog implements exactly opm's backup trait")
		Expect(provides).To(ConsistOf(derived), "the claim lists exactly what the catalog implements")
	})

	It("refuses the consumer while no provider of the backup trait is in the registry", func() {
		store := generatedPlatformStore(k, registry)
		r := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: labels.ManagedByController,
		}

		consumer := fixtures.Must(GinkgoT(), "backup_consumer")
		_, err := r.RenderModule(ctx, "backup-consumer", "default", consumer.ModulePath, consumer.Tag(), nil)
		Expect(err).To(HaveOccurred(), "an unprovided load-bearing trait refuses the render (0010:D28)")
		Expect(err.Error()).To(ContainSubstring(backupTraitFQN), "the refusal names the contract")
	})

	It("renders the consumer through the backup catalog once it is in the registry", func() {
		store := generatedPlatformStoreWith(k, registry, platformmodule.Entry{
			Path:    backup.ModulePath,
			Version: backup.Version,
			Enable:  true,
		})
		r := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: labels.ManagedByController,
		}

		consumer := fixtures.Must(GinkgoT(), "backup_consumer")
		res, err := r.RenderModule(ctx, "backup-consumer", "default", consumer.ModulePath, consumer.Tag(), nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequiredContracts).To(ContainElement(backupTraitFQN),
			"the consumer's demand names the contract a claim's removal guard counts (0015:D3)")
		Expect(res.Resources).To(HaveLen(1), "an emptyDir renders nothing; the backup policy is the one object")

		obj := res.Resources[0]
		Expect(obj.Kind()).To(Equal("ConfigMap"))
		Expect(obj.Name()).To(Equal("backup-consumer-data-backup"))
		registryPath := strings.SplitN(backup.ModulePath, "@", 2)[0]
		Expect(obj.Transformer).To(HavePrefix(registryPath+"/transformers/backup-transformer@"),
			"the backup catalog's transformer rendered it")

		u, err := obj.ToUnstructured()
		Expect(err).NotTo(HaveOccurred())
		schedule, _, err := unstructured.NestedString(u.Object, "data", "schedule")
		Expect(err).NotTo(HaveOccurred())
		Expect(schedule).To(Equal("0 3 * * *"), "the module's default schedule reaches the rendered policy")
	})

	// The library derives a catalog's provider set in one of two ways. A
	// catalog whose committed cue.mod pins a core at or after
	// schema.ProvidesSince carries the `provides` field core computes, and the
	// library decodes it. A catalog pinned to an older core carries no such
	// field, and the library falls back to a deprecated fold over
	// #transformers. The fold is removed before GA, after catalog_opm is
	// republished against a newer core. Acceptance compares that set with the
	// claim for exact equality (0015:D11), so each path must accept the same
	// valid claim. Each spec asserts the facts that pick its path, so a
	// fixture or library move that breaks a premise fails here naming it. On
	// a catalog pinned at or after ProvidesSince the fold and the field give
	// the same set, so these specs cannot tell which of the two ran there.

	It("accepts a claim on a catalog built against a core older than the provides field (fold fallback)", func() {
		store := generatedPlatformStore(k, registry)
		claim := renderBackupClaim(k, store, registry, "backup-provider-fold")

		// The catalog as acceptance acquires it: from the registry, at the
		// claim's own coordinate.
		cat, err := k.AcquireCatalogFromRegistry(ctx, backup.ModulePath, backup.Version)
		Expect(err).NotTo(HaveOccurred())

		pin := catalogCorePin(cat)
		Expect(semver.Compare(pin, "v"+schema.ProvidesSince)).To(BeNumerically("<", 0),
			"premise: the backup catalog fixture pins core %s, older than ProvidesSince %s; "+
				"while the library keeps its fold this fixture must stay there (fixture README)",
			pin, schema.ProvidesSince)
		Expect(cat.Package.LookupPath(schema.CatalogProvides).Exists()).To(BeFalse(),
			"premise: a catalog built against core %s carries no provides field", pin)

		expectProvidesExactlyBackup(cat, claim)
		expectAccepted(acceptBackupClaimWith(store, k, claim))
	})

	It("accepts a claim on a catalog built against a core with the provides field (decoded field)", func() {
		store := generatedPlatformStore(k, registry)
		claim := renderBackupClaim(k, store, registry, "backup-provider-field")

		dir := backupCatalogPinnedTo(backup, "v"+schema.ProvidesSince)
		cat, err := k.AcquireCatalogFromDir(ctx, dir)
		Expect(err).NotTo(HaveOccurred())

		platformCore := platformCorePin(store)
		Expect(semver.Compare(platformCore, "v"+schema.ProvidesSince)).To(BeNumerically(">=", 0),
			"premise: the generated platform's core %s is not older than ProvidesSince %s, "+
				"so build compatibility accepts the copy (0015:D8)", platformCore, schema.ProvidesSince)

		pin := catalogCorePin(cat)
		Expect(semver.Compare(pin, "v"+schema.ProvidesSince)).To(Equal(0),
			"premise: the copy pins core %s, exactly ProvidesSince %s", pin, schema.ProvidesSince)
		field := cat.Package.LookupPath(schema.CatalogProvides)
		Expect(field.Exists()).To(BeTrue(),
			"premise: a catalog built against core %s carries the provides field core derives; "+
				"if not, ProvidesSince names a core that does not derive it", pin)
		var decoded []string
		Expect(field.Decode(&decoded)).To(Succeed())

		derived := expectProvidesExactlyBackup(cat, claim)
		Expect(decoded).To(ConsistOf(derived), "the provides field is the set the catalog reports")

		dirCatalogs := &fixedCatalog{path: backup.ModulePath, version: backup.Version, cat: cat}
		expectAccepted(acceptBackupClaimWith(store, dirCatalogs, claim))
	})
})

// renderBackupClaim renders backup_provider as the named instance in the
// default namespace, creates its claim and the provider instance whose
// inventory owns it, and removes both when the spec ends. The instance name
// keeps each spec's claim distinct; the cleanup keeps either spec from being
// refused DuplicateClaim by the other's claim on the same catalog.
func renderBackupClaim(
	k *kernel.Kernel,
	store *platformstore.Store,
	registry, instance string,
) *unstructured.Unstructured {
	GinkgoHelper()
	const namespace = "default"

	r := &render.KernelModuleRenderer{
		Kernel:      k,
		Store:       store,
		Registry:    registry,
		RuntimeName: labels.ManagedByController,
	}
	provider := fixtures.Must(GinkgoT(), "backup_provider")
	res, err := r.RenderModule(ctx, instance, namespace, provider.ModulePath, provider.Tag(), nil)
	Expect(err).NotTo(HaveOccurred())
	Expect(res.Resources).To(HaveLen(1), "backup_provider renders its claim and nothing else")
	claim, err := res.Resources[0].ToUnstructured()
	Expect(err).NotTo(HaveOccurred())

	Expect(k8sClient.Create(ctx, claim)).To(Succeed())
	DeferCleanup(removeClaim, claim.GetName())

	// Status is a subresource: a Create carrying it drops it, so the
	// inventory goes on through a status update, as the controller writes it.
	mi := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: instance, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{Path: provider.ModulePath, Version: provider.Version},
		},
	}
	Expect(k8sClient.Create(ctx, mi)).To(Succeed())
	DeferCleanup(func(ctx context.Context) {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, mi))).To(Succeed())
	})
	mi.Status.Inventory = &releasesv1alpha1.Inventory{
		Count: 1,
		Entries: []releasesv1alpha1.InventoryEntry{{
			Group:   "opmodel.dev",
			Kind:    "TransformerRegistration",
			Name:    claim.GetName(),
			Version: "v1alpha1",
		}},
	}
	Expect(k8sClient.Status().Update(ctx, mi)).To(Succeed())
	return claim
}

// removeClaim deletes a claim and waits until it is gone. Acceptance puts a
// removal-guard finalizer on it, and no manager runs here to release it, so
// the finalizer is stripped first.
func removeClaim(ctx context.Context, name string) {
	var reg releasesv1alpha1.TransformerRegistration
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &reg)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	if len(reg.Finalizers) > 0 {
		patch := client.MergeFrom(reg.DeepCopy())
		reg.Finalizers = nil
		Expect(client.IgnoreNotFound(k8sClient.Patch(ctx, &reg, patch))).To(Succeed())
	}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &reg))).To(Succeed())
	Eventually(func() bool {
		gone := &releasesv1alpha1.TransformerRegistration{}
		return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: name}, gone))
	}, 10*time.Second, 100*time.Millisecond).Should(BeTrue(), "claim %s was not removed", name)
}

// expectProvidesExactlyBackup asserts that the catalog's derived provider set
// is exactly opm's backup trait and that the claim lists exactly that set,
// and returns it.
func expectProvidesExactlyBackup(cat *catalog.Catalog, claim *unstructured.Unstructured) []string {
	GinkgoHelper()
	derived, err := cat.Provides()
	Expect(err).NotTo(HaveOccurred())
	Expect(derived).To(ConsistOf(backupTraitFQN), "the backup catalog implements exactly opm's backup trait")
	provides, _, err := unstructured.NestedStringSlice(claim.Object, "spec", "provides")
	Expect(err).NotTo(HaveOccurred())
	Expect(provides).To(ConsistOf(derived), "the claim lists exactly what the catalog implements")
	return derived
}

// acceptBackupClaimWith runs the real acceptance reconciler once on the
// claim, with no manager, and returns the claim as stored.
func acceptBackupClaimWith(
	store *platformstore.Store,
	catalogs controller.CatalogAcquirer,
	claim *unstructured.Unstructured,
) releasesv1alpha1.TransformerRegistration {
	GinkgoHelper()
	r := &controller.TransformerRegistrationReconciler{
		Client:        k8sClient,
		Scheme:        k8sClient.Scheme(),
		EventRecorder: events.NewFakeRecorder(16),
		Catalogs:      catalogs,
		Store:         store,
	}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: claim.GetName()}})
	Expect(err).NotTo(HaveOccurred())

	var judged releasesv1alpha1.TransformerRegistration
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim.GetName()}, &judged)).To(Succeed())
	return judged
}

// expectAccepted asserts the Accepted verdict. ProvidesMismatch is checked
// before every later check, so an accepted claim passed the exact-equality
// comparison of its provides list.
func expectAccepted(judged releasesv1alpha1.TransformerRegistration) {
	GinkgoHelper()
	ready := apimeta.FindStatusCondition(judged.Status.Conditions, status.ReadyCondition)
	Expect(ready).NotTo(BeNil(), "acceptance recorded no verdict")
	Expect(judged.Status.Accepted).To(BeTrue(), "claim refused: %s: %s", ready.Reason, ready.Message)
	Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	Expect(ready.Reason).To(Equal(status.AcceptedReason))
}

// catalogCorePin returns the core version the catalog's committed cue.mod
// requires, the input the library's path choice reads.
func catalogCorePin(cat *catalog.Catalog) string {
	GinkgoHelper()
	reqs, err := cat.Requires()
	Expect(err).NotTo(HaveOccurred())
	pin := reqs[coreModulePath]
	Expect(semver.IsValid(pin)).To(BeTrue(), "the catalog requires core at a SemVer version, got %q", pin)
	return pin
}

// platformCorePin returns the core version the generated platform resolved.
func platformCorePin(store *platformstore.Store) string {
	GinkgoHelper()
	generated, ok := store.Generated()
	Expect(ok).To(BeTrue(), "the store holds a generated platform")
	mf := readModFile(filepath.Join(generated.Dir, "cue.mod", "module.cue"))
	dep := mf.Deps[coreModulePath]
	Expect(dep).NotTo(BeNil(), "the generated platform requires %s", coreModulePath)
	return dep.Version
}

// coreModulePath is core's major-qualified module path.
const coreModulePath = "opmodel.dev/core@v2"

// backupCatalogPinnedTo copies the backup catalog fixture into a temporary
// directory and moves its core requirement to version, editing the module
// file through CUE's own parser and formatter. The copy keeps every other
// pin, opm's included, so only the core decides the provider-set path.
func backupCatalogPinnedTo(backup fixtures.Coordinate, version string) string {
	GinkgoHelper()
	root, err := fixtures.CatalogDir()
	Expect(err).NotTo(HaveOccurred())
	dir := filepath.Join(GinkgoT().TempDir(), "backup")
	Expect(os.CopyFS(dir, os.DirFS(filepath.Join(root, "backup")))).To(Succeed())

	path := filepath.Join(dir, "cue.mod", "module.cue")
	mf := readModFile(path)
	Expect(mf.Module).To(Equal(backup.ModulePath), "the copy is the backup catalog fixture")
	dep := mf.Deps[coreModulePath]
	Expect(dep).NotTo(BeNil(), "the backup catalog fixture requires %s", coreModulePath)
	dep.Version = version
	data, err := modfile.Format(mf)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(path, data, 0o600)).To(Succeed())
	return dir
}

func readModFile(path string) *modfile.File {
	GinkgoHelper()
	data, err := os.ReadFile(path) //nolint:gosec // a test fixture or temporary path
	Expect(err).NotTo(HaveOccurred())
	mf, err := modfile.Parse(data, path)
	Expect(err).NotTo(HaveOccurred())
	return mf
}

// fixedCatalog is a CatalogAcquirer that answers one coordinate with one
// already-acquired catalog and refuses every other, so acceptance judges the
// claim against the pin-rewritten copy at the claim's own coordinate.
type fixedCatalog struct {
	path, version string
	cat           *catalog.Catalog
}

func (f *fixedCatalog) AcquireCatalogFromRegistry(
	_ context.Context,
	modPath, version string,
) (*catalog.Catalog, error) {
	if modPath != f.path || version != f.version {
		return nil, fmt.Errorf("test acquirer serves only %s at %s, asked for %s at %s",
			f.path, f.version, modPath, version)
	}
	return f.cat, nil
}
