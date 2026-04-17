# poc-controller
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Installing

Install the latest release directly from the GitHub release asset:

```sh
kubectl apply -f https://github.com/open-platform-model/poc-controller/releases/latest/download/install.yaml
```

The manifest pins the controller image by digest (`ghcr.io/open-platform-model/opm-operator:vX.Y.Z@sha256:...`), so the exact bytes from the release are pulled regardless of future tag movement.

Verify the release image signature with cosign:

```sh
cosign verify ghcr.io/open-platform-model/opm-operator:vX.Y.Z \
  --certificate-identity-regexp='^https://github.com/open-platform-model/poc-controller/\.github/workflows/release\.yml@refs/heads/main$' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

For a PR preview image, swap the workflow path to `image-pr.yml` and adjust the ref:

```sh
cosign verify ghcr.io/open-platform-model/opm-operator:pr-123 \
  --certificate-identity-regexp='^https://github.com/open-platform-model/poc-controller/\.github/workflows/image-pr\.yml@refs/pull/.*$' \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

#### Image tag semantics

| Tag | Stability | Purpose |
| --- | --- | --- |
| `:v<version>` | Immutable | Exact release version; points at a specific manifest-list digest. |
| `:<digest>` (`@sha256:...`) | Immutable | Cryptographic pin, always the same bytes. |
| `:latest` | Moves | Tracks the newest published release. |
| `:pr-<N>` | Mutable | Preview for PR `N`; overwrites on force-push. Not for production. |
| `:sha-<short7>` | Effectively immutable | Commit-pinned build; published on both PR and release runs. |

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
task docker:build docker:push IMG=<some-registry>/poc-controller:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
task operator:crds
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
task operator:controller:install IMG=<some-registry>/poc-controller:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
task operator:crds:remove
```

**UnDeploy the controller from the cluster:**

```sh
task operator:controller:uninstall
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
task operator:installer IMG=<some-registry>/poc-controller:tag
```

**NOTE:** The task above generates an 'install.yaml' file in the dist
directory. This file contains all the resources built with Kustomize,
which are necessary to install this project without its dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/poc-controller/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `task --list` for more information on all potential `task` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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

