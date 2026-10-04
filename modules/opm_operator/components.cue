package opm_operator

import (
	bp "opmodel.dev/catalogs/opm/blueprints/v1beta1"
	res "opmodel.dev/catalogs/opm/resources/v1beta1"
	exp "opmodel.dev/catalogs/opm/resources/v1alpha1"
	tr "opmodel.dev/catalogs/opm/traits/v1beta1"

	op "opmodel.dev/modules/opm_operator/operator"
)

// The names an operator installed from an earlier release's manifest has
// (kustomize's namePrefix "opm-operator-"). Constants: the cli locates an
// operator by them, whatever installed it.
let P = "opm-operator"
let SA = "\(P)-controller-manager"

// The administrator ClusterRoles the operator ships unbound, for cluster
// administrators to bind (keys of #rbacSource).
let adminRoles = [
	"metrics-reader",
	"moduleinstance-admin-role",
	"moduleinstance-editor-role",
	"moduleinstance-viewer-role",
	"platform-viewer-role",
	"modulepackage-viewer-role",
	"transformerregistration-viewer-role",
	"transformerregistration-admin-role",
]

#components: {
	// The operator's Namespace, owned by the module so an install records it.
	namespace: {
		exp.#Namespaces
		spec: namespaces: (_namespace): {}
	}

	// The CRDs, each imported spec embedded whole: #CRDSchema is closed, so a
	// field the catalog cannot carry (spec.conversion) refuses the render
	// instead of being dropped. Of the CRD's metadata the catalog carries the
	// annotations (the name is the key); any other metadata field, such as
	// labels, refuses the render too.
	crds: {
		res.#CRDs
		spec: crds: {
			for crdName, raw in #crdSource {
				(crdName): {
					raw.spec
					if raw.metadata.annotations != _|_ {
						annotations: raw.metadata.annotations
					}
					for k, _ in raw.metadata if k != "name" && k != "annotations" {
						(k): error("crds: \(crdName) carries metadata.\(k), which the catalog's CRD resource cannot render; refused instead of dropped")
					}
				}
			}
		}
	}

	// The controller Deployment, its ServiceAccount and the metrics Service.
	"controller-manager": {
		bp.#StatelessWorkload
		res.#ServiceAccount
		res.#Volumes
		tr.#SecurityContext
		tr.#WorkloadIdentity
		tr.#GracefulShutdown
		tr.#PodMetadata
		tr.#Expose

		metadata: resourceName: SA

		// Lands in the selector and the pod labels; the e2e suite selects the
		// controller's pods by it.
		metadata: labels: "control-plane": "controller-manager"

		spec: {
			statelessWorkload: {
				scaling: count: #config.replicas
				// Required by the blueprint; both are the Kubernetes defaults
				// the earlier manifest left implicit.
				restartPolicy: "Always"
				updateStrategy: type: "RollingUpdate"
				container: {
					name: "manager"
					image: {
						repository: #config.image.repository
						tag:        op.Image.tag
						digest:     op.Image.digest
						pullPolicy: "IfNotPresent"
					}
					command: ["/manager"]
					args: [
						"--metrics-bind-address=:8443",
						"--leader-elect",
						"--health-probe-bind-address=:8081",
						if #config.registry != _|_ {"--registry=\(#config.registry)"},
						if #config.defaultServiceAccount != _|_ {"--default-service-account=\(#config.defaultServiceAccount)"},
						for a in #config.extraArgs {a},
					]
					env: GOMEMLIMIT: value: _goMemLimit
					resources: #config.resources
					livenessProbe: {
						httpGet: {path: "/healthz", port: 8081}
						initialDelaySeconds: 15
						periodSeconds:       20
					}
					readinessProbe: {
						httpGet: {path: "/readyz", port: 8081}
						initialDelaySeconds: 5
						periodSeconds:       10
					}
					securityContext: {
						readOnlyRootFilesystem:   true
						allowPrivilegeEscalation: false
						capabilities: drop: ["ALL"]
					}
					volumeMounts: tmp: spec.volumes.tmp & {mountPath: "/tmp"}
				}
			}

			volumes: tmp: {emptyDir: {}, readOnly: false}

			// Pod level, covering the container: Pod Security restricted.
			securityContext: {
				runAsNonRoot: true
				seccompProfile: type: "RuntimeDefault"
			}

			gracefulShutdown: terminationGracePeriodSeconds: 10

			podMetadata: annotations: "kubectl.kubernetes.io/default-container": "manager"

			// automountToken is optional in the schema but read unguarded by
			// the catalog, so it is set (true is the API default).
			workloadIdentity: {name: SA, automountToken: true}
			serviceAccount: {name: SA, automountToken: true}

			expose: {
				name: "\(P)-controller-manager-metrics-service"
				type: "ClusterIP"
				ports: https: {targetPort: 8443, exposedPort: 8443}
			}
		}
	}

	// The controller's own roles, each bound to its ServiceAccount. The
	// catalog names each binding after its role.
	"manager-rbac": {
		res.#Role
		spec: role: {
			name:  "\(P)-manager-role"
			scope: "cluster"
			rules: #rbacSource["manager-role"].rules
			subjects: [{name: SA}]
		}
	}

	"metrics-auth-rbac": {
		res.#Role
		spec: role: {
			name:  "\(P)-metrics-auth-role"
			scope: "cluster"
			rules: #rbacSource["metrics-auth-role"].rules
			subjects: [{name: SA}]
		}
	}

	"leader-election": {
		res.#Role
		spec: role: {
			name:  "\(P)-leader-election-role"
			scope: "namespace"
			rules: #rbacSource["leader-election-role"].rules
			subjects: [{name: SA}]
		}
	}

	// The administrator ClusterRoles, unbound. The released catalog's
	// #Role requires a subject and always renders a binding, so these go
	// through the raw-objects resource until a catalog release renders a role
	// with no subjects (catalog_opm add-subjectless-roles); their rules still
	// come from the generated RBAC data. Nothing else uses raw objects.
	"admin-roles": {
		exp.#Objects
		spec: objects: {
			for k in adminRoles {
				"\(P)-\(k)": {
					apiVersion: #rbacSource[k].apiVersion
					kind:       #rbacSource[k].kind
					rules:      #rbacSource[k].rules
				}
			}
		}
	}
}
