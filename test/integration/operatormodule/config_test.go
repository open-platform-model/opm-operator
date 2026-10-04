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

package operatormodule_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const mirrorRepository = "registry.internal/opm/opm-operator"

// everyValue sets every #config field to a value other than its default.
var everyValue = fmt.Sprintf(`{
	image: repository: %q
	registry: "opmodel.dev=registry.internal/opm,registry.cue.works"
	defaultServiceAccount: "opm-apply"
	resources: {
		requests: {cpu: "250m", memory: "512Mi"}
		limits: {cpu: 4, memory: "2Gi"}
	}
	replicas: 2
	extraArgs: ["--max-concurrent-renders=2", "--zap-log-level=debug"]
}`, mirrorRepository)

// moduleArgs are the arguments the module renders itself, first and in this
// order.
var moduleArgs = []any{"--metrics-bind-address=:8443", "--leader-elect", "--health-probe-bind-address=:8081"}

func envValue(c map[string]any, name string) any {
	for _, e := range c["env"].([]any) {
		if em := e.(map[string]any); em["name"] == name {
			return em["value"]
		}
	}
	return nil
}

var _ = Describe("The operator module's #config", func() {
	Context("with every value set", Ordered, func() {
		var objs []obj

		BeforeAll(func() {
			objs = mustRenderWith(moduleDir, everyValue)
		})

		It("renders every value into the Deployment, extra arguments after the typed ones", func() {
			d := find(objs, "Deployment", "opm-operator-controller-manager")
			Expect(lookup(d.Object, "spec", "replicas")).To(BeNumerically("==", 2))

			c := managerContainer(objs)
			Expect(c["args"]).To(Equal(append(append([]any{}, moduleArgs...),
				"--registry=opmodel.dev=registry.internal/opm,registry.cue.works",
				"--default-service-account=opm-apply",
				"--max-concurrent-renders=2",
				"--zap-log-level=debug",
			)))
			Expect(normalize(c["resources"])).To(Equal(normalize(map[string]any{
				"requests": map[string]any{"cpu": "250m", "memory": "512Mi"},
				"limits":   map[string]any{"cpu": "4", "memory": "2Gi"},
			})))
			Expect(envValue(c, "GOMEMLIMIT")).To(Equal("1638MiB"), "80 percent of 2Gi, in MiB")
		})

		It("keeps the module's tag and digest under a mirrored repository", func() {
			op, err := readModuleOperator(moduleDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(managerContainer(objs)["image"]).To(Equal(fmt.Sprintf("%s:v%s@%s", mirrorRepository, op.Version, op.Digest)))
		})

		It("leaves the Deployment selector equal to the fixed one", func() {
			d := find(objs, "Deployment", "opm-operator-controller-manager")
			Expect(dict(d.Object, "spec", "selector", "matchLabels")).To(Equal(fixedSelector))
		})
	})

	It("leaves out the optional arguments and derives GOMEMLIMIT from the default memory limit", func() {
		c := managerContainer(mustRender(moduleDir))
		Expect(c["args"]).To(Equal(moduleArgs))
		Expect(envValue(c, "GOMEMLIMIT")).To(Equal("3276MiB"), "80 percent of 4Gi, the earlier manifest's value")
		Expect(normalize(c["resources"])).To(Equal(normalize(map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "2", "memory": "4Gi"},
		})))
	})

	It("keeps the other resource defaults when only the memory limit is set", func() {
		c := managerContainer(mustRenderWith(moduleDir, `{resources: limits: memory: "8Gi"}`))
		Expect(normalize(c["resources"])).To(Equal(normalize(map[string]any{
			"requests": map[string]any{"cpu": "100m", "memory": "256Mi"},
			"limits":   map[string]any{"cpu": "2", "memory": "8Gi"},
		})))
		Expect(envValue(c, "GOMEMLIMIT")).To(Equal("6553MiB"))
	})

	It("renders CPU given as cores and as millicores", func() {
		c := managerContainer(mustRenderWith(moduleDir, `{resources: {requests: cpu: 0.5, limits: cpu: 4}}`))
		Expect(lookup(c, "resources", "requests", "cpu")).To(Equal("500m"))
		Expect(fmt.Sprint(lookup(c, "resources", "limits", "cpu"))).To(Equal("4"))
		c = managerContainer(mustRenderWith(moduleDir, `{resources: limits: cpu: "1500m"}`))
		Expect(lookup(c, "resources", "limits", "cpu")).To(Equal("1500m"))
	})

	DescribeTable("refuses",
		func(values string, wants ...string) {
			_, err := render(moduleDir, instanceName, instanceNamespace, values)
			Expect(err).To(HaveOccurred(), "values %s must be refused", values)
			for _, w := range wants {
				Expect(errText(err)).To(ContainSubstring(w))
			}
		},
		Entry("an image tag value", `{image: tag: "v9.9.9"}`, "image.tag"),
		Entry("an image digest value", `{image: digest: "sha256:0000"}`, "image.digest"),
		Entry("an image pull policy value", `{image: pullPolicy: "Always"}`, "image.pullPolicy"),
		Entry("the registry mapping in extraArgs", `{extraArgs: ["--registry=example.com"]}`,
			`"--registry=example.com"`, "#config.registry"),
		Entry("the single-dash registry mapping in extraArgs", `{extraArgs: ["-registry=example.com"]}`,
			`"-registry=example.com"`, "#config.registry"),
		Entry("the default service account in extraArgs", `{extraArgs: ["--default-service-account=x"]}`,
			`"--default-service-account=x"`, "#config.defaultServiceAccount"),
		Entry("the metrics address in extraArgs", `{extraArgs: ["--metrics-bind-address=:9443"]}`,
			`"--metrics-bind-address=:9443"`, "the module renders itself"),
		Entry("leader election in extraArgs", `{extraArgs: ["--leader-elect=false"]}`,
			`"--leader-elect=false"`, "the module renders itself"),
		Entry("the probe address in extraArgs", `{extraArgs: ["-health-probe-bind-address=:9091"]}`,
			`"-health-probe-bind-address=:9091"`, "the module renders itself"),
		Entry("a memory limit as a byte count", `{resources: limits: memory: 4294967296}`, "as <n>Mi or <n>Gi"),
		Entry("a memory limit in decimal units", `{resources: limits: memory: "4G"}`, "as <n>Mi or <n>Gi"),
		Entry("a fractional memory limit", `{resources: limits: memory: "1.5Gi"}`, "as <n>Mi or <n>Gi"),
		Entry("a CPU limit as a whole-number string", `{resources: limits: cpu: "4"}`,
			"resources.limits.cpu", `write 4, not "4"`),
		Entry("a CPU request as a fractional string", `{resources: requests: cpu: "0.5"}`,
			"resources.requests.cpu", `write 4, not "4"`),
	)

	It("refuses any instance but opm-operator in opm-operator-system, naming the expected coordinates", func() {
		for _, c := range []struct{ name, namespace string }{
			{"opm", "opm-system"},
			{instanceName, "opm-system"},
			{"opm", instanceNamespace},
		} {
			_, err := render(moduleDir, c.name, c.namespace, "{}")
			Expect(err).To(HaveOccurred(), "instance %s/%s", c.namespace, c.name)
			Expect(err.Error()).To(ContainSubstring(instanceNamespace + "/" + instanceName))
		}
	})
})
