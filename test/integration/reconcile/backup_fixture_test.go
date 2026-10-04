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
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/kernel"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/open-platform-model/library/opm/helper/platformmodule"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
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
// authored) on purpose; the fixture README says why.
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
			RuntimeName: core.LabelManagedByControllerValue,
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
			RuntimeName: core.LabelManagedByControllerValue,
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
			RuntimeName: core.LabelManagedByControllerValue,
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
})
