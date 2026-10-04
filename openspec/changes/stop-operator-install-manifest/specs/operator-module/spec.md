## MODIFIED Requirements

### Requirement: The controller's pod and Service stay those of the kustomize tree

While `config/manager` and `config/default` still install the operator for development (`task operator:installer`, `task operator:controller:install`) and in e2e, the controller Deployment's pod spec and the metrics Service the module renders SHALL equal those of a kustomize build of `config/default`, apart from the image, labels, the selector, the fields the catalog sets to Kubernetes API defaults, and the bindings' names. A test SHALL compare them on every pull request, so e2e never tests a controller pod the module does not ship. The operator's releases no longer publish a manifest built from `config/default`; the module's render is the only install manifest.

#### Scenario: A manifest-only edit fails the test

- **WHEN** a pull request changes the manager container's arguments, probes, environment, volumes or security context in `config/manager` and not in the module
- **THEN** the module's render test fails naming the differing field
