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
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/opm-operator/test/utils"
)

// This spec validates the podinfo example module end-to-end: it deploys the
// controller, generates and builds the cluster Platform's module, applies the podinfo
// ModuleInstance, and asserts the rendered Deployment's pods reach Ready — which
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

	// redisPVCName is resolved from the redis instance's inventory by the
	// lifecycle Context below; Describe scope so AfterAll can clean it up.
	var redisPVCName string

	BeforeAll(func() {
		// This spec resolves the podinfo example module from a registry the
		// controller can reach. That requires either the in-cluster registry
		// override (LOCAL_REGISTRY, set by `task dev:e2e:local`) or GHCR pull
		// credentials (OPERATOR_DOCKER_CONFIG, set by the PR e2e workflow after
		// publishing the modules under a pre-release tag). Without one of these
		// the module cannot be pulled, so skip rather than time out.
		if os.Getenv("LOCAL_REGISTRY") == "" && os.Getenv("OPERATOR_DOCKER_CONFIG") == "" {
			Skip("example modules unresolvable: set LOCAL_REGISTRY (local) or OPERATOR_DOCKER_CONFIG (CI GHCR creds)")
		}

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

		// CI override: the example modules are published to private GHCR under a
		// pre-release tag before this suite runs, so the in-cluster controller
		// needs credentials to resolve them from the ghcr default registry. The
		// workflow writes a Docker config.json (ghcr.io auth) and points
		// OPERATOR_DOCKER_CONFIG at it; we mount it and set DOCKER_CONFIG so the
		// CUE module loader (ociauth) authenticates. No-op for local runs, which
		// resolve from the unauthenticated in-cluster registry above.
		if dockerCfg := os.Getenv("OPERATOR_DOCKER_CONFIG"); dockerCfg != "" {
			By("provisioning GHCR pull credentials for the controller")
			_, err = utils.Run(exec.Command("kubectl", "-n", namespace, "create", "secret", "generic",
				"ghcr-auth", "--from-file=config.json="+dockerCfg))
			Expect(err).NotTo(HaveOccurred(), "Failed to create ghcr-auth secret")

			// Strategic merge so the volume/mount/env are added by name without
			// clobbering the manager's existing tmp volume or args.
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
			_, err = utils.Run(exec.Command("kubectl", "-n", namespace, "patch", "deployment",
				"opm-operator-controller-manager", "--type=strategic", "-p", patch))
			Expect(err).NotTo(HaveOccurred(), "Failed to mount GHCR credentials")
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
		// Teardown order matters. The ModuleInstance carries the
		// `opmodel.dev/cleanup` finalizer and, with spec.prune=true,
		// prunes its managed resources by IMPERSONATING the podinfo-applier
		// ServiceAccount. The fixture bundles that SA/RBAC and the CR in one
		// file, so deleting the file wholesale removes the SA out from under the
		// prune — the controller then stalls with DeletionSAMissing, the
		// finalizer never clears, and the CRD deletion in `make undeploy`
		// (config/default includes ../crd) blocks until the test binary times
		// out. So delete the CR first, let it drain while the SA still exists,
		// and only then remove the RBAC.
		// Redis lifecycle cleanup first: the same SA-before-CR ordering applies,
		// and the prune=false spec deliberately orphans workloads that no CR
		// deletion will ever clean up — remove them by name.
		By("removing the redis ModuleInstance")
		_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "delete", "moduleinstance", "redis",
			"--ignore-not-found", "--wait=false"))
		if _, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "wait", "--for=delete",
			"moduleinstance/redis", "--timeout=2m")); err != nil {
			_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "patch", "moduleinstance", "redis",
				"--type=merge", "-p", `{"metadata":{"finalizers":null}}`))
		}

		By("removing orphaned redis workloads and the applier RBAC")
		_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "delete", "--ignore-not-found", "--wait=false",
			"statefulset/redis-redis", "service/redis-redis"))
		if redisPVCName != "" {
			_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "delete", "--ignore-not-found", "--wait=false",
				"pvc/"+redisPVCName))
		}
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "test/fixtures/modules/redis/moduleinstance.yaml")))

		By("removing the podinfo ModuleInstance")
		_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "delete", "moduleinstance", "podinfo",
			"--ignore-not-found", "--wait=false"))

		By("waiting for the ModuleInstance finalizer to clear")
		// Bounded wait for the controller to finish pruning and clear the
		// finalizer; if it stalls anyway, strip the finalizer so teardown cannot
		// wedge the CRD deletion below.
		if _, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "wait", "--for=delete",
			"moduleinstance/podinfo", "--timeout=2m")); err != nil {
			_, _ = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "patch", "moduleinstance", "podinfo",
				"--type=merge", "-p", `{"metadata":{"finalizers":null}}`))
		}

		By("removing the podinfo applier RBAC")
		// The CR is gone; this clears the ServiceAccount/Role/RoleBinding (and is
		// a no-op for the already-deleted ModuleInstance).
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "test/fixtures/modules/podinfo/moduleinstance.yaml")))

		By("removing the cluster Platform")
		// The Platform has no finalizer, so a non-blocking delete is sufficient.
		_, _ = utils.Run(exec.Command("kubectl", "delete", "--ignore-not-found", "--wait=false", "-f",
			filepath.Join(projectDir, "config/samples/opmodel.dev_v1alpha1_platform.yaml")))

		By("undeploying the controller-manager")
		_, _ = utils.Run(exec.Command("make", "undeploy"))

		By("uninstalling CRDs")
		_, _ = utils.Run(exec.Command("make", "uninstall"))
	})

	AfterEach(func() {
		// On failure (e.g. the render wait times out), dump the ModuleInstance
		// status and controller logs while they still exist — AfterEach runs
		// before the AfterAll teardown. Module resolution/apply errors surface
		// in the CR conditions and controller log, which are otherwise invisible
		// in CI and make a flaky pull undiagnosable.
		if !CurrentSpecReport().Failed() {
			return
		}
		By("dumping diagnostics for the failed spec")
		if out, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "podinfo",
			"-o", "yaml")); err == nil {
			fmt.Fprintf(GinkgoWriter, "--- ModuleInstance default/podinfo ---\n%s\n", out)
		}
		if out, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
			"-o", "yaml")); err == nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "--- ModuleInstance default/redis ---\n%s\n", out)
		}
		if out, err := utils.Run(exec.Command("kubectl", "-n", namespace, "logs",
			"-l", "control-plane=controller-manager", "--tail=300")); err == nil {
			fmt.Fprintf(GinkgoWriter, "--- controller-manager logs ---\n%s\n", out)
		}
	})

	It("deploys podinfo and its pods become Ready (proving liveness + readiness pass)", func() {
		By("applying the podinfo ModuleInstance")
		_, err := utils.Run(exec.Command("kubectl", "apply", "-f",
			filepath.Join(projectDir, "test/fixtures/modules/podinfo/moduleinstance.yaml")))
		Expect(err).NotTo(HaveOccurred(), "Failed to apply the podinfo ModuleInstance")

		By("waiting for the controller to render the podinfo Deployment")
		// Render normally completes in seconds; the generous window lets the
		// controller's reconcile backoff absorb a transient registry pull error
		// (cold GHCR fetch in CI) rather than flaking the suite.
		Eventually(func(g Gomega) {
			_, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "deploy", deploymentName))
			g.Expect(err).NotTo(HaveOccurred(), "podinfo Deployment not created yet")
		}, 5*time.Minute, 3*time.Second).Should(Succeed())

		By("waiting for the podinfo Deployment's pods to become Ready")
		// moduleinstance.yaml requests replicas: 2; both must pass their probes.
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

	It("registers the cleanup finalizer on the ModuleInstance", func() {
		finalizers, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "podinfo",
			"-o", "jsonpath={.metadata.finalizers}"))
		Expect(err).NotTo(HaveOccurred())
		Expect(finalizers).To(ContainSubstring("opmodel.dev/cleanup"),
			"deployed controller should register the cleanup finalizer")
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

	// Deployed-controller lifecycle: live prune on a values update and
	// prune=false orphan on delete. Uses the redis example fixture because its
	// persistence.enabled knob is the suite's only value-gated resource — it
	// swaps the /data volume between a rendered PVC (enabled, the default) and
	// an emptyDir (disabled), so flipping it drops the PVC from the render and
	// the deployed controller must prune the live object. These are the halves
	// the envtest tier structurally cannot provide: real SSA through the
	// running manager against a real API server, with impersonated prune.
	Context("ModuleInstance lifecycle (redis)", Ordered, func() {
		var initialInventoryCount int

		It("deploys redis and reaches Ready with a PVC in the inventory", func() {
			By("applying the redis ModuleInstance")
			_, err := utils.Run(exec.Command("kubectl", "apply", "-f",
				filepath.Join(projectDir, "test/fixtures/modules/redis/moduleinstance.yaml")))
			Expect(err).NotTo(HaveOccurred(), "Failed to apply the redis ModuleInstance")

			By("waiting for the ModuleInstance to become Ready")
			Eventually(func(g Gomega) {
				out, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}"))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"), "redis ModuleInstance not Ready yet")
			}, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("confirming the cleanup finalizer is registered")
			finalizers, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
				"-o", "jsonpath={.metadata.finalizers}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(finalizers).To(ContainSubstring("opmodel.dev/cleanup"))

			By("resolving the rendered PVC from the inventory")
			redisPVCName, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
				"-o", "jsonpath={.status.inventory.entries[?(@.kind=='PersistentVolumeClaim')].name}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(redisPVCName).NotTo(BeEmpty(), "inventory should record the rendered PVC")

			count, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
				"-o", "jsonpath={.status.inventory.count}"))
			Expect(err).NotTo(HaveOccurred())
			initialInventoryCount, err = strconv.Atoi(count)
			Expect(err).NotTo(HaveOccurred())

			By("confirming the PVC exists in the cluster")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "pvc", redisPVCName))
			Expect(err).NotTo(HaveOccurred(), "rendered PVC should exist")
		})

		It("prunes the live PVC when a values update drops it from the render", func() {
			By("disabling persistence via a values update")
			_, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "patch", "moduleinstance", "redis",
				"--type=merge", "-p", `{"spec":{"values":{"persistence":{"enabled":false}}}}`))
			Expect(err).NotTo(HaveOccurred(), "Failed to update the redis values")

			// Deterministic status first (design: inventory/status before live
			// objects): the inventory must drop the PVC entry and shrink.
			By("waiting for the inventory to drop the PVC entry")
			Eventually(func(g Gomega) {
				out, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
					"-o", "jsonpath={.status.inventory.entries[?(@.kind=='PersistentVolumeClaim')].name}"))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(BeEmpty(), "inventory still lists a PVC")

				count, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "moduleinstance", "redis",
					"-o", "jsonpath={.status.inventory.count}"))
				g.Expect(err).NotTo(HaveOccurred())
				n, err := strconv.Atoi(count)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(n).To(BeNumerically("<", initialInventoryCount), "inventory did not shrink")
			}, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for the live PVC to be pruned by the deployed controller")
			// PVC deletion completes once the StatefulSet rollout replaces the
			// pod that mounted it (pvc-protection finalizer).
			Eventually(func(g Gomega) {
				out, _ := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "pvc", redisPVCName))
				g.Expect(out).To(ContainSubstring("NotFound"), "pruned PVC still present")
			}, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("confirming the StatefulSet survived the update")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "statefulset", "redis-redis"))
			Expect(err).NotTo(HaveOccurred(), "redis StatefulSet should still exist")
		})

		It("orphans live workloads when a prune=false instance is deleted", func() {
			By("disabling prune on the instance")
			_, err := utils.Run(exec.Command("kubectl", "-n", mrNamespace, "patch", "moduleinstance", "redis",
				"--type=merge", "-p", `{"spec":{"prune":false}}`))
			Expect(err).NotTo(HaveOccurred(), "Failed to set spec.prune=false")

			By("deleting the ModuleInstance")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "delete", "moduleinstance", "redis",
				"--wait=false"))
			Expect(err).NotTo(HaveOccurred(), "Failed to delete the redis ModuleInstance")

			By("waiting for the CR to be removed (finalizer released without pruning)")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "wait", "--for=delete",
				"moduleinstance/redis", "--timeout=2m"))
			Expect(err).NotTo(HaveOccurred(), "redis ModuleInstance was not removed")

			By("confirming the rendered workloads remain live (orphaned)")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "statefulset", "redis-redis"))
			Expect(err).NotTo(HaveOccurred(), "orphaned StatefulSet should remain")
			_, err = utils.Run(exec.Command("kubectl", "-n", mrNamespace, "get", "service", "redis-redis"))
			Expect(err).NotTo(HaveOccurred(), "orphaned Service should remain")

			By("confirming the namespace is intact")
			phase, err := utils.Run(exec.Command("kubectl", "get", "ns", mrNamespace,
				"-o", "jsonpath={.status.phase}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(phase).To(Equal("Active"))
		})
	})
})
