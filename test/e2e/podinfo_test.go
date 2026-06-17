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

// This spec validates the podinfo example module end-to-end: it deploys the
// controller, materializes the cluster Platform, applies the podinfo
// ModuleRelease, and asserts the rendered Deployment's pods reach Ready — which
// is only possible if the modelled HTTP liveness (/healthz) and readiness
// (/readyz) probes pass against the running podinfo container. It then inspects
// the deployed container to confirm the probe contract matches the module.
//
// It is self-contained (own controller deploy/teardown) so it does not depend
// on the ordering of other top-level specs. The example modules and the catalog
// they pin must already be published to the Kind-reachable registry; run via
// `task dev:e2e:local` (which sets LOCAL_REGISTRY so the controller resolves
// from the in-cluster opm-registry).
var _ = Describe("Podinfo example module", Ordered, func() {
	const (
		mrNamespace    = "default"
		deploymentName = "podinfo-podinfo"
		serviceName    = "podinfo-podinfo"
	)

	var projectDir string

	BeforeAll(func() {
		var err error
		projectDir, err = utils.GetProjectDir()
		Expect(err).NotTo(HaveOccurred())

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
			_, err = utils.Run(exec.Command("kubectl", "-n", namespace, "patch", "deployment",
				"opm-operator-controller-manager",
				"--type=json",
				fmt.Sprintf(`-p=[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--registry=%s"}]`, localRegistry)))
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
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "config/samples/releases_v1alpha1_platform.yaml")))
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
		// Use --wait=false on the CR deletes: the controller prunes managed
		// resources and clears finalizers asynchronously, so a blocking delete
		// would stall teardown (and a controller restart could stall it
		// indefinitely). The CRD uninstall below removes any lingering CRs.
		By("removing the podinfo ModuleRelease")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "test/fixtures/modules/podinfo/modulerelease.yaml")))

		By("removing the cluster Platform")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "config/samples/releases_v1alpha1_platform.yaml")))

		By("undeploying the controller-manager")
		_, _ = utils.Run(exec.Command("make", "undeploy"))

		By("uninstalling CRDs")
		_, _ = utils.Run(exec.Command("make", "uninstall"))
	})

	It("deploys podinfo and its pods become Ready (proving liveness + readiness pass)", func() {
		By("applying the podinfo ModuleRelease")
		_, err := utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "test/fixtures/modules/podinfo/modulerelease.yaml")))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the podinfo ModuleRelease")

		By("waiting for the controller to render the podinfo Deployment")
		Eventually(func(g Gomega) {
			_, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName))
			g.Expect(err).NotTo(HaveOccurred(), "podinfo Deployment not created yet")
		}, 3*time.Minute, 3*time.Second).Should(Succeed())

		By("waiting for the podinfo Deployment's pods to become Ready")
		// modulerelease.yaml requests replicas: 2; both must pass their probes.
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName,
				"-o", "jsonpath={.status.readyReplicas}"))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).To(Equal("2"), "podinfo pods not all Ready yet")
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("confirming the governing Service was rendered")
		_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "service", serviceName))
		Expect(err).NotTo(HaveOccurred(), "podinfo Service should exist")
	})

	It("renders the modelled probe contract onto the running container", func() {
		container := "{.spec.template.spec.containers[0]."

		livenessPath, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName,
			"-o", "jsonpath="+container+"livenessProbe.httpGet.path}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(livenessPath).To(Equal("/healthz"))

		readinessPath, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName,
			"-o", "jsonpath="+container+"readinessProbe.httpGet.path}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(readinessPath).To(Equal("/readyz"))

		livenessPort, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName,
			"-o", "jsonpath="+container+"livenessProbe.httpGet.port}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(livenessPort).To(Equal("9898"))

		containerPort, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName,
			"-o", "jsonpath="+container+"ports[0].containerPort}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(containerPort).To(Equal("9898"))
	})
})
