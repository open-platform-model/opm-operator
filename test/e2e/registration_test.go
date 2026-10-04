//go:build e2e

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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/opm-operator/test/fixtures"
	"github.com/open-platform-model/opm-operator/test/utils"
)

// This spec drives a claim rendered by a real provider module through the
// deployed controller: the claim reconciler accepts and activates it
// (0015:D3, 0015:D11), and the platform reconciler builds its catalog into the
// generated platform package. It covers both spellings a claim's
// spec.version can carry. opm renders a bare SemVer; a v-prefixed one reaches
// a cluster only through a hand edit of a live claim, which the CRD admits.
//
// The provider is the backup_provider fixture, whose claim names the backup
// catalog fixture. Both are published to GHCR and readable without
// credentials, so a run needs neither LOCAL_REGISTRY nor
// OPERATOR_DOCKER_CONFIG; it honours both when set, as the podinfo spec does.
// A LOCAL_REGISTRY run needs the backup fixtures seeded there.
//
// It is self-contained (own controller deploy and teardown), so it does not
// depend on the order of the other top-level specs.
var _ = Describe("TransformerRegistration claim", Ordered, func() {
	const (
		providerNamespace = "default"
		providerName      = "backup-provider"
		// The cluster-scoped name the backup-provider instance gives the
		// claim its module renders.
		claimName = "default.backup-provider"
	)

	var (
		projectDir      string
		providerFixture string
		backup          fixtures.Coordinate
		// identityBefore is the Platform's package identity before any claim
		// contributes; identityBare the one the bare-version claim produced.
		identityBefore string
		identityBare   string
	)

	kubectl := func(args ...string) (string, error) {
		return utils.Run(exec.Command("kubectl", args...))
	}

	// claimField and platformField read one jsonpath expression from the live
	// object. A read error fails the enclosing Eventually attempt.
	claimField := func(g Gomega, jsonpath string) string {
		out, err := kubectl("get", "transformerregistration", claimName, "-o", "jsonpath="+jsonpath)
		g.Expect(err).NotTo(HaveOccurred())
		return out
	}
	platformField := func(g Gomega, jsonpath string) string {
		out, err := kubectl("get", "platform", "cluster", "-o", "jsonpath="+jsonpath)
		g.Expect(err).NotTo(HaveOccurred())
		return out
	}
	backupEntry := func(field string) string {
		return fmt.Sprintf(`{.status.registry[?(@.catalog=="%s")].%s}`, backup.ModulePath, field)
	}

	BeforeAll(func() {
		var err error
		projectDir, err = utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred())
		providerFixture = filepath.Join(projectDir, "test/fixtures/modules/backup_provider/moduleinstance.yaml")

		// The catalog coordinate comes from the fixture's identity package,
		// the same source backup_provider's claim is pinned against.
		backup = fixtures.MustCatalog(GinkgoT(), "backup")
		Expect(backup.Version).NotTo(HavePrefix("v"), "fixture identity versions are bare SemVer")

		By("creating the manager namespace")
		// Ignore an already-exists error from a prior spec in the same suite.
		_, _ = kubectl("create", "ns", namespace)

		By("installing CRDs")
		_, err = utils.Run(exec.Command("make", "install"))
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		_, err = utils.Run(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage)))
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		// Local-dev override: resolve the catalogs and the fixtures from the
		// in-cluster registry instead of the GHCR default.
		if localRegistry := os.Getenv("LOCAL_REGISTRY"); localRegistry != "" {
			By("overriding controller registry for local dev")
			arg := fmt.Sprintf(`{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--registry=%s"}`,
				localRegistry)
			_, err = kubectl("-n", namespace, "patch", "deployment", "opm-operator-controller-manager",
				"--type=json", "-p=["+arg+"]")
			Expect(err).NotTo(HaveOccurred(), "Failed to override controller registry")
		}

		// CI override: the PR e2e workflow re-pins the provider instance to a
		// per-commit pre-release published to GHCR, which needs credentials.
		if dockerCfg := os.Getenv("OPERATOR_DOCKER_CONFIG"); dockerCfg != "" {
			By("provisioning GHCR pull credentials for the controller")
			// Tolerate only AlreadyExists; any other failure (an unreadable
			// OPERATOR_DOCKER_CONFIG) would otherwise surface as a rollout
			// timeout on a pod stuck mounting a missing secret.
			_, err = kubectl("-n", namespace, "create", "secret", "generic",
				"ghcr-auth", "--from-file=config.json="+dockerCfg)
			if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
				Fail(fmt.Sprintf("Failed to create the ghcr-auth secret: %v", err))
			}

			patch := `spec:
  template:
    spec:
      volumes:
        - name: ghcr-auth
          secret:
            secretName: ghcr-auth
            items:
              - {key: config.json, path: config.json}
      containers:
        - name: manager
          env:
            - {name: DOCKER_CONFIG, value: /ghcr}
          volumeMounts:
            - {name: ghcr-auth, mountPath: /ghcr, readOnly: true}`
			_, err = kubectl("-n", namespace, "patch", "deployment",
				"opm-operator-controller-manager", "--type=strategic", "-p", patch)
			Expect(err).NotTo(HaveOccurred(), "Failed to mount GHCR credentials")
		}

		By("waiting for the controller-manager rollout to settle")
		// A patched deployment rolls a new pod; settle on one leader before
		// the Platform is applied, so its first build is not raced by a
		// leader handoff.
		_, err = kubectl("-n", namespace, "rollout", "status",
			"deployment/opm-operator-controller-manager", "--timeout=180s")
		Expect(err).NotTo(HaveOccurred(), "controller-manager rollout did not settle")

		By("applying the cluster Platform")
		_, err = kubectl("apply", "-f", filepath.Join(projectDir, "config/samples/opmodel.dev_v1alpha1_platform.yaml"))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the Platform")

		By("waiting for the Platform to become Ready and recording its package identity")
		Eventually(func(g Gomega) {
			g.Expect(platformField(g, `{.status.conditions[?(@.type=="Ready")].status}`)).
				To(Equal("True"), "Platform not Ready yet")
			identityBefore = platformField(g, "{.status.packageIdentity}")
			g.Expect(identityBefore).NotTo(BeEmpty())
		}, 4*time.Minute, 5*time.Second).Should(Succeed())
	})

	AfterEach(func() {
		// On failure, dump the objects and the controller log while they
		// still exist: AfterEach runs before the AfterAll teardown. A refused
		// or deferred verdict and a failed platform build surface only in
		// conditions and the log.
		if !CurrentSpecReport().Failed() {
			return
		}
		By("dumping diagnostics for the failed spec")
		for _, args := range [][]string{
			{"get", "transformerregistration", claimName, "-o", "yaml"},
			{"get", "platform", "cluster", "-o", "yaml"},
			{"-n", providerNamespace, "get", "moduleinstance", providerName, "-o", "yaml"},
			{"-n", namespace, "logs", "-l", "control-plane=controller-manager", "--tail=300"},
		} {
			if out, err := kubectl(args...); err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "--- kubectl %s ---\n%s\n", strings.Join(args, " "), out)
			}
		}
	})

	AfterAll(func() {
		// Teardown order matters. The provider instance prunes its claim by
		// impersonating the applier ServiceAccount, so the instance goes while
		// the RBAC still exists. A claim that outlives it (prune stalled, or
		// an earlier spec failed) still holds its removal-guard finalizer,
		// which would block the CRD deletion in `make uninstall` until the
		// test binary times out; so any leftover claim goes next, with the
		// same bounded wait and finalizer strip, before the CRDs do.
		By("removing the backup-provider ModuleInstance")
		_, _ = kubectl("-n", providerNamespace, "delete", "moduleinstance", providerName,
			"--ignore-not-found", "--wait=false")
		if _, err := kubectl("-n", providerNamespace, "wait", "--for=delete",
			"moduleinstance/"+providerName, "--timeout=2m"); err != nil {
			_, _ = kubectl("-n", providerNamespace, "patch", "moduleinstance", providerName,
				"--type=merge", "-p", `{"metadata":{"finalizers":null}}`)
		}

		By("removing any leftover claim")
		_, _ = kubectl("delete", "transformerregistration", claimName, "--ignore-not-found", "--wait=false")
		if _, err := kubectl("wait", "--for=delete", "transformerregistration/"+claimName,
			"--timeout=1m"); err != nil {
			_, _ = kubectl("patch", "transformerregistration", claimName,
				"--type=merge", "-p", `{"metadata":{"finalizers":null}}`)
		}

		By("removing the backup-provider applier RBAC")
		_, _ = kubectl("delete", "--ignore-not-found", "--wait=false", "-f", providerFixture)

		By("removing the cluster Platform")
		_, _ = kubectl("delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "config/samples/opmodel.dev_v1alpha1_platform.yaml"))

		By("undeploying the controller-manager")
		_, _ = utils.Run(exec.Command("make", "undeploy"))

		By("uninstalling CRDs")
		_, _ = utils.Run(exec.Command("make", "uninstall"))
	})

	It("accepts a bare-version claim and builds it into the platform", func() {
		By("applying the backup-provider ModuleInstance")
		_, err := kubectl("apply", "-f", providerFixture)
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the backup-provider ModuleInstance")

		By("checking that the rendered claim carries the catalog's bare version")
		Eventually(func(g Gomega) {
			g.Expect(claimField(g, "{.spec.catalog}")).To(Equal(backup.ModulePath))
			g.Expect(claimField(g, "{.spec.version}")).To(Equal(backup.Version),
				"the claim backup_provider renders must name the backup catalog fixture's own bare version")
		}, 5*time.Minute, 3*time.Second).Should(Succeed())

		By("waiting for the claim to be accepted and active")
		Eventually(func(g Gomega) {
			g.Expect(claimField(g, "{.status.accepted},{.status.active}")).To(Equal("true,true"))
			g.Expect(claimField(g, `{.status.conditions[?(@.type=="Ready")].status}`)).To(Equal("True"))
			g.Expect(claimField(g, `{.status.conditions[?(@.type=="Ready")].reason}`)).To(Equal("Accepted"))
		}, 5*time.Minute, 3*time.Second).Should(Succeed())

		By("waiting for the Platform to build with the claimed catalog")
		Eventually(func(g Gomega) {
			g.Expect(platformField(g, backupEntry("source"))).To(Equal("Registration"))
			g.Expect(platformField(g, backupEntry("version"))).To(Equal(backup.Version))
			identityBare = platformField(g, "{.status.packageIdentity}")
			g.Expect(identityBare).NotTo(Equal(identityBefore), "package identity did not move")
			g.Expect(platformField(g, `{.status.conditions[?(@.type=="Ready")].status}`)).To(Equal("True"))
			g.Expect(platformField(g, `{.status.conditions[?(@.type=="Ready")].reason}`)).To(Equal("Generated"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("accepts a v-prefixed claim and builds it into the platform", func() {
		vVersion := "v" + backup.Version

		By("suspending backup-provider so its controller does not re-apply the rendered claim")
		_, err := kubectl("-n", providerNamespace, "patch", "moduleinstance", providerName,
			"--type=merge", "-p", `{"spec":{"suspend":true}}`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			out, err := kubectl("-n", providerNamespace, "get", "moduleinstance", providerName,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}/{.status.conditions[?(@.type=="Ready")].reason}`)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("False/Suspended"))
		}, 2*time.Minute, 2*time.Second).Should(Succeed())

		By("setting the live claim's spec.version to " + vVersion)
		_, err = kubectl("patch", "transformerregistration", claimName,
			"--type=merge", "-p", fmt.Sprintf(`{"spec":{"version":%q}}`, vVersion))
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the claim to be re-judged and accepted at the new version")
		// observedGeneration, accepted and active alone are not a verdict: a
		// deferred reconcile advances observedGeneration and leaves accepted
		// and active as the bare-version verdict set them. Only acceptance
		// writes Ready=True/Accepted, and its message names the version it
		// judged.
		Eventually(func(g Gomega) {
			g.Expect(claimField(g, "{.spec.version}")).To(Equal(vVersion), "the edit was reverted")
			g.Expect(claimField(g, "{.status.observedGeneration}")).To(Equal(claimField(g, "{.metadata.generation}")))
			g.Expect(claimField(g, "{.status.accepted},{.status.active}")).To(Equal("true,true"))
			g.Expect(claimField(g, `{.status.conditions[?(@.type=="Ready")].status}`)).To(Equal("True"))
			g.Expect(claimField(g, `{.status.conditions[?(@.type=="Ready")].reason}`)).To(Equal("Accepted"))
			g.Expect(claimField(g, `{.status.conditions[?(@.type=="Ready")].message}`)).
				To(ContainSubstring("at " + vVersion))
		}, 5*time.Minute, 3*time.Second).Should(Succeed())

		By("waiting for the Platform to build with the v-prefixed coordinate")
		// Asserted only after the verdict above: the platform reconciler folds
		// in a claim's coordinate as soon as its spec moves, carried by the
		// previous verdict, so a Platform build seen before the claim is
		// re-judged proves nothing about acceptance.
		var identityV string
		Eventually(func(g Gomega) {
			g.Expect(platformField(g, backupEntry("source"))).To(Equal("Registration"))
			// The entry names the same build in either spelling; the package
			// identity, a function of the claims' coordinates, is what shows
			// the platform was regenerated for the edited claim.
			g.Expect(strings.TrimPrefix(platformField(g, backupEntry("version")), "v")).To(Equal(backup.Version))
			identityV = platformField(g, "{.status.packageIdentity}")
			g.Expect(identityV).NotTo(BeEmpty())
			// The identity moves on a spelling-only edit only because the
			// claim coordinates keep the claim's own spelling
			// (opm-operator#234). A change that normalises the version to
			// bare SemVer must turn this into identityV == identityBare: the
			// same build, no regeneration.
			g.Expect(identityV).NotTo(Equal(identityBare), "package identity did not move")
			g.Expect(identityV).NotTo(Equal(identityBefore))
			g.Expect(platformField(g, `{.status.conditions[?(@.type=="Ready")].status}`)).To(Equal("True"))
			g.Expect(platformField(g, `{.status.conditions[?(@.type=="Ready")].reason}`)).To(Equal("Generated"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("checking that the package identity is stable")
		Consistently(func(g Gomega) {
			g.Expect(platformField(g, "{.status.packageIdentity}")).To(Equal(identityV))
		}, 15*time.Second, 3*time.Second).Should(Succeed())
	})

	It("deletes the provider and the claim is pruned with its guard released", func() {
		// Deletion runs ahead of the suspend check in the instance reconcile,
		// so the suspended provider still prunes what it rendered.
		By("deleting the backup-provider ModuleInstance")
		_, err := kubectl("-n", providerNamespace, "delete", "moduleinstance", providerName, "--wait=false")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the provider instance to be removed")
		_, err = kubectl("-n", providerNamespace, "wait", "--for=delete",
			"moduleinstance/"+providerName, "--timeout=3m")
		Expect(err).NotTo(HaveOccurred(), "backup-provider was not removed")

		By("waiting for the claim to be gone")
		// Nothing renders against the backup catalog, so the removal guard
		// (0015:D3) has nothing to protect and releases the claim.
		_, err = kubectl("wait", "--for=delete", "transformerregistration/"+claimName, "--timeout=2m")
		Expect(err).NotTo(HaveOccurred(), "the claim was not pruned, or its removal guard was not released")
	})
})
