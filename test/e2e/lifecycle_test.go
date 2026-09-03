//go:build e2e
// +build e2e

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/opm-operator/test/utils"
)

// Live-tier lifecycle coverage.
//
// The former prune_test.go and finalizer_test.go stub files were deleted:
// their logic is covered at the envtest tier (test/integration/apply/prune_test.go,
// test/integration/reconcile/stale_pruning_test.go,
// test/integration/reconcile/finalizer_test.go, internal/reconcile/deletion_test.go)
// and their live-tier value is subsumed by the deployed-controller lifecycle
// assertions in podinfo_test.go (finalizer registration, live prune on a values
// update, prune=false delete orphaning live workloads).
//
// This file holds the live ARTIFACT pipeline spec: a real source-controller at
// the pinned flux2 distribution version serving a real `flux push artifact`
// tarball to the deployed controller through an OCIRepository + ModulePackage.

const (
	// fluxNamespace is where `task flux:install` places source-controller.
	fluxNamespace = "flux-system"
	// fluxCIMarker is the env var CI sets (OPM_E2E_FLUX=1) to make a missing
	// Flux prerequisite a hard failure instead of a local skip-with-notice.
	fluxCIMarker = "OPM_E2E_FLUX"
)

// fluxInstalledBySuite tracks whether source-controller was installed by this
// suite, so teardown only removes what it installed (CertManager precedent).
var fluxInstalledBySuite = false

// ensureFluxPrereqs makes the flux CLI and the pinned source-controller
// available, following the suite's gating idiom: under the CI marker
// (OPM_E2E_FLUX=1) a missing prerequisite fails the spec with an explicit
// message; locally it skips with a notice.
func ensureFluxPrereqs() {
	bail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if os.Getenv(fluxCIMarker) != "" {
			Fail(fmt.Sprintf("%s is set (CI hard-fail gating): %s", fluxCIMarker, msg))
		}
		Skip(msg)
	}

	if _, err := exec.LookPath("flux"); err != nil {
		bail("flux CLI not found on PATH; it is required to install source-controller " +
			"and push the fixture artifact — install it from https://fluxcd.io/flux/installation/")
	}

	// Install (idempotent apply) via the Taskfile so the distribution version
	// pin stays single-sourced in .tasks/flux.yaml.
	alreadyInstalled := false
	if _, err := utils.Run(exec.Command("kubectl", "-n", fluxNamespace,
		"get", "deployment", "source-controller")); err == nil {
		alreadyInstalled = true
	}

	By("installing the pinned Flux source-controller")
	if out, err := utils.Run(exec.Command("task", "flux:install")); err != nil {
		bail("failed to install the pinned Flux source-controller: %v\n%s", err, out)
	}
	if !alreadyInstalled {
		fluxInstalledBySuite = true
	}

	By("waiting for source-controller to become Available")
	if out, err := utils.Run(exec.Command("kubectl", "-n", fluxNamespace, "rollout", "status",
		"deployment/source-controller", "--timeout=120s")); err != nil {
		bail("source-controller did not become Available after install: %v\n%s", err, out)
	}
}

// This spec proves the operator consumes a real source-controller Artifact:
// the podinfo modulepackage fixture is pushed with `flux push artifact` to the
// kind-connected local registry, the fixture's OCIRepository is reconciled by
// the real source-controller, and the deployed controller fetches, extracts,
// and renders the real tarball to a Ready workload. It is self-contained (own
// controller deploy/teardown) so it does not depend on the ordering of other
// top-level specs.
var _ = Describe("ModulePackage live artifact pipeline", Ordered, func() {
	const (
		pkgNamespace   = "default"
		pkgName        = "podinfo"
		ociRepoName    = "podinfo-release"
		deploymentName = "podinfo-podinfo"
	)

	var (
		projectDir       string
		artifactRevision string
	)

	BeforeAll(func() {
		ensureFluxPrereqs()

		var err error
		projectDir, err = utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred())

		By("ensuring the local OCI registry is running and reachable from Kind")
		_, err = utils.Run(exec.Command("task", "registry:start"))
		Expect(err).NotTo(HaveOccurred(), "Failed to start the local registry")
		_, err = utils.Run(exec.Command("task", "registry:connect"))
		Expect(err).NotTo(HaveOccurred(), "Failed to connect the registry to the kind network")

		By("pushing the podinfo modulepackage fixture as a Flux OCI artifact")
		// Deterministic tag v0.0.1 (release:publish default RELEASE_TAG),
		// matching the fixture ocirepository.yaml ref.
		_, err = utils.Run(exec.Command("task", "release:publish", "PKG=podinfo"))
		Expect(err).NotTo(HaveOccurred(), "Failed to push the fixture artifact")

		By("creating the manager namespace")
		// Ignore an already-exists error from a prior spec in the same suite.
		_, _ = utils.Run(exec.Command("kubectl", "create", "ns", namespace))

		By("installing CRDs")
		_, err = utils.Run(exec.Command("make", "install"))
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		_, err = utils.Run(exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage)))
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		// Local-dev override: resolve catalog/module deps from the in-cluster
		// registry (opm-registry:5000) instead of the ghcr default.
		if localRegistry := os.Getenv("LOCAL_REGISTRY"); localRegistry != "" {
			By("overriding controller registry for local dev")
			patch := fmt.Sprintf(
				`-p=[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--registry=%s"}]`,
				localRegistry)
			_, err = utils.Run(exec.Command("kubectl", "-n", namespace, "patch", "deployment",
				"opm-operator-controller-manager", "--type=json", patch))
			Expect(err).NotTo(HaveOccurred(), "Failed to override controller registry")
		}

		By("waiting for the controller-manager to be Available")
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", namespace, "get", "deploy",
				"opm-operator-controller-manager", "-o", "jsonpath={.status.availableReplicas}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("1"), "controller-manager not Available yet")
		}, 3*time.Minute, 3*time.Second).Should(Succeed())

		By("applying the cluster Platform")
		// ModulePackage reconciliation is Platform-gated (PlatformNotReady
		// blocks apply until the platform store holds a built platform).
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "config/samples/opmodel.dev_v1alpha1_platform.yaml")))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the Platform")

		By("waiting for the Platform to become Ready")
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "get", "platform", "cluster",
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("True"), "Platform not Ready yet")
		}, 4*time.Minute, 5*time.Second).Should(Succeed())
	})

	AfterAll(func() {
		// Teardown order matters (see podinfo_test.go): the ModulePackage
		// prunes by impersonating the podinfo-deploy ServiceAccount bundled in
		// the same fixture file, so delete the CR first and let it drain while
		// the SA still exists, then remove the RBAC and the OCIRepository.
		By("removing the podinfo ModulePackage")
		_, _ = utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "delete", "modulepackage", pkgName,
			"--ignore-not-found", "--wait=false"))

		By("waiting for the ModulePackage finalizer to clear")
		if _, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "wait", "--for=delete",
			"modulepackage/"+pkgName, "--timeout=2m")); err != nil {
			_, _ = utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "patch", "modulepackage", pkgName,
				"--type=merge", "-p", `{"metadata":{"finalizers":null}}`))
		}

		By("removing the fixture RBAC and OCIRepository")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "test/fixtures/modulepackages/podinfo/modulepackage.yaml")))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "test/fixtures/modulepackages/podinfo/ocirepository.yaml")))

		By("removing the cluster Platform")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "config/samples/opmodel.dev_v1alpha1_platform.yaml")))

		By("undeploying the controller-manager")
		_, _ = utils.Run(exec.Command("make", "undeploy"))

		By("uninstalling CRDs")
		_, _ = utils.Run(exec.Command("make", "uninstall"))

		if fluxInstalledBySuite {
			By("uninstalling Flux (installed by this suite)")
			_, _ = utils.Run(exec.Command("task", "flux:uninstall"))
		}
	})

	AfterEach(func() {
		// On failure, dump the moving parts of the pipeline while they still
		// exist — AfterEach runs before the AfterAll teardown.
		if !CurrentSpecReport().Failed() {
			return
		}
		By("dumping diagnostics for the failed spec")
		if out, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "ocirepository", ociRepoName,
			"-o", "yaml")); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "--- OCIRepository %s/%s ---\n%s\n", pkgNamespace, ociRepoName, out)
		}
		if out, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "modulepackage", pkgName,
			"-o", "yaml")); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "--- ModulePackage %s/%s ---\n%s\n", pkgNamespace, pkgName, out)
		}
		if out, err := utils.Run(exec.Command("kubectl", "-n", fluxNamespace, "logs",
			"deployment/source-controller", "--tail=100")); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "--- source-controller logs ---\n%s\n", out)
		}
		if out, err := utils.Run(exec.Command("kubectl", "-n", namespace, "logs",
			"-l", "control-plane=controller-manager", "--tail=300")); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "--- controller-manager logs ---\n%s\n", out)
		}
	})

	It("reports a real Artifact with revision and digest on the OCIRepository", func() {
		By("applying the fixture OCIRepository and ModulePackage")
		_, err := utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "test/fixtures/modulepackages/podinfo/ocirepository.yaml")))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the OCIRepository")
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "test/fixtures/modulepackages/podinfo/modulepackage.yaml")))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the ModulePackage fixture")

		By("waiting for source-controller to resolve the pushed artifact")
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "ocirepository", ociRepoName,
				"-o", "jsonpath={.status.artifact.revision}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).NotTo(BeEmpty(), "OCIRepository has no artifact revision yet")
			artifactRevision = out
		}, 2*time.Minute, 3*time.Second).Should(Succeed())

		// OCIRepository revisions are "<tag>@<digest>" for a real registry pull.
		Expect(artifactRevision).To(ContainSubstring("sha256:"),
			"artifact revision should carry a content digest")

		digest, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "ocirepository", ociRepoName,
			"-o", "jsonpath={.status.artifact.digest}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(digest).To(ContainSubstring("sha256:"), "artifact should report a digest")
	})

	It("fetches, extracts, and renders the artifact to a Ready workload", func() {
		By("waiting for the ModulePackage to become Ready")
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "modulepackage", pkgName,
				"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("True"), "ModulePackage not Ready yet")
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting for the rendered Deployment's pods to become Ready")
		// The fixture package sets values replicas: 2; both must pass probes.
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "deploy", deploymentName,
				"-o", "jsonpath={.status.readyReplicas}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("2"), "rendered podinfo pods not all Ready yet")
		}, 5*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("propagates the source-controller artifact revision into the ModulePackage status", func() {
		Expect(artifactRevision).NotTo(BeEmpty(), "artifact revision not captured by the pipeline spec")

		statusRevision, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "modulepackage", pkgName,
			"-o", "jsonpath={.status.source.artifactRevision}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(statusRevision).To(Equal(artifactRevision),
			"ModulePackage status should carry the source-controller-reported artifact revision")

		statusDigest, err := utils.Run(exec.Command("kubectl", "-n", pkgNamespace, "get", "modulepackage", pkgName,
			"-o", "jsonpath={.status.source.artifactDigest}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(statusDigest).To(ContainSubstring("sha256:"),
			"ModulePackage status should carry the artifact digest")
	})
})
