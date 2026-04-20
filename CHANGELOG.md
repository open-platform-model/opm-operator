# Changelog

## [0.6.2](https://github.com/open-platform-model/opm-operator/compare/v0.6.1...v0.6.2) (2026-04-20)


### Bug Fixes

* **ci:** build image for linux/arm64 platform ([32eb334](https://github.com/open-platform-model/opm-operator/commit/32eb3347cb722d665a710239f9bc6c4e2b3db568))

## [0.6.1](https://github.com/open-platform-model/opm-operator/compare/v0.6.0...v0.6.1) (2026-04-20)


### Bug Fixes

* **apply:** wire StatusPoller into ResourceManager ([71fae5a](https://github.com/open-platform-model/opm-operator/commit/71fae5ab5162aab2d8492bfd358b00b2799ea9f8))

## [0.6.0](https://github.com/open-platform-model/opm-operator/compare/v0.5.0...v0.6.0) (2026-04-20)


### Features

* **controller:** add --default-service-account flag and tenancy guide ([9b705ac](https://github.com/open-platform-model/opm-operator/commit/9b705acd3a1e1002973c15747d93b967bb21c1c1))
* **reconcile:** stall on DeletionSAMissing with orphan-exit annotation ([69aa1ba](https://github.com/open-platform-model/opm-operator/commit/69aa1ba002fb1f60df7fc4c6f49e55ad7083b32d))


### Documentation

* **openspec:** archive rename-to-opm-operator change ([fbefd81](https://github.com/open-platform-model/opm-operator/commit/fbefd81b16080877b1c11f1bb092baca691c6851))
* **readme:** bump go prerequisite to v1.26.2 ([0fd9916](https://github.com/open-platform-model/opm-operator/commit/0fd9916c59252be9a842960e615447dd78a36bce))
* **skills:** tighten verify-change output structure for determinism ([d29dec9](https://github.com/open-platform-model/opm-operator/commit/d29dec9502d6b435dd4bb72b2f88a8b1d9db45d3))

## [0.5.0](https://github.com/open-platform-model/poc-controller/compare/v0.4.4...v0.5.0) (2026-04-20)


### ⚠ BREAKING CHANGES

* **k8s:** kustomize namespace changes from poc-controller-system to opm-operator-system; namePrefix changes from poc-controller- to opm-operator-; app.kubernetes.io/name label value changes across all manager manifests. Deployment selector is immutable, so upgrading from v0.4.x requires deleting the old namespace and RBAC before applying v0.5.0 install.yaml. See design.md for the documented upgrade procedure. User ModuleRelease/BundleRelease CRs are unaffected; they carry
* go module path changes from github.com/open-platform-model/poc-controller to github.com/open-platform-model/opm-operator. External consumers must update import paths. Pre-1.0, so no /v2 suffix required.

### Documentation

* **design:** add rbac-delegation research document ([d0ae6c5](https://github.com/open-platform-model/poc-controller/commit/d0ae6c5d6aa20f646885cc3868d36d851687bcb7))
* **openspec:** add default-sa-and-tenancy-guide change ([cb94910](https://github.com/open-platform-model/poc-controller/commit/cb94910ce308fb057b5d7c1bae0e84108973dd2c))
* **openspec:** add deletion-sa-missing-stall change ([1833a7a](https://github.com/open-platform-model/poc-controller/commit/1833a7a8b9e6a9c6955fd2af1da644ba5d3f7b44))
* **openspec:** add rename-to-opm-operator change and adr-015 ([97e2a81](https://github.com/open-platform-model/poc-controller/commit/97e2a8156f71e0894bdca6007b9a02ecc69a901c))
* rename project references from poc-controller to opm-operator ([fcb08d8](https://github.com/open-platform-model/poc-controller/commit/fcb08d8e6dd8bac59484a38831015fcd32339fd8))


### Code Refactoring

* **catalog:** rename cue catalog module path to opm-operator ([962bddf](https://github.com/open-platform-model/poc-controller/commit/962bddfe56f37d6a283acb604287184a79bcaabd))
* **k8s:** rename kustomize namespace and labels to opm-operator ([63345ea](https://github.com/open-platform-model/poc-controller/commit/63345ea3403265c990f6fd10b002c7e20133a9e5))
* rename go module path to opm-operator ([59d6e66](https://github.com/open-platform-model/poc-controller/commit/59d6e66d32c3088897e6bd320d4031d2a1c71bfd))


### Miscellaneous Chores

* **build:** rename kind cluster and buildx builder to opm-operator ([c13e102](https://github.com/open-platform-model/poc-controller/commit/c13e102bfc6be800240e7241cc3efb763eb6ed10))
* remove stale experiments 001 and 002 ([faa1213](https://github.com/open-platform-model/poc-controller/commit/faa1213d406dfdb0b0dfd23f999c7c958e4db91c))

## [0.4.4](https://github.com/open-platform-model/poc-controller/compare/v0.4.3...v0.4.4) (2026-04-19)


### Bug Fixes

* **rbac:** grant impersonate on users and groups for ssa apply ([c978639](https://github.com/open-platform-model/poc-controller/commit/c978639e15f3cc74e2153f87db8ea5b726743f07))


### Code Refactoring

* **render:** switch runtime identity from #runtimeLabels map to #runtimeName string ([f080bbe](https://github.com/open-platform-model/poc-controller/commit/f080bbe12e4af47fe53e3f2ec5011321173f279a))


### Miscellaneous Chores

* **openspec:** archive catalog-runtime-managed-by and sync spec ([ba1c19c](https://github.com/open-platform-model/poc-controller/commit/ba1c19c7098260ae0de45b2cb50d064de9bc7d76))

## [0.4.3](https://github.com/open-platform-model/poc-controller/compare/v0.4.2...v0.4.3) (2026-04-18)


### Bug Fixes

* **reconcile:** correctness trio (rename, prune guard, sa groups) ([30dc138](https://github.com/open-platform-model/poc-controller/commit/30dc138f98a13ea0c4fb221576000a9861547a04))


### Documentation

* **design:** add impersonation and privilege escalation analysis ([dfed6f4](https://github.com/open-platform-model/poc-controller/commit/dfed6f44cfd1a132c2c2ddc1f83204bb89b2a175))
* **openspec:** record §8 scratch-revert proof results ([af2deb6](https://github.com/open-platform-model/poc-controller/commit/af2deb62d5edd50d7249c6fd9a51bb2f599fc7c2))


### Miscellaneous Chores

* **manager:** raise controller resource limits to 1Gi/2 CPU ([3ca09ac](https://github.com/open-platform-model/poc-controller/commit/3ca09ac10f0789a59eded3e4f57d813b3265ce5e))
* **openspec:** archive add-image-build-cicd and sync spec ([5c7ea83](https://github.com/open-platform-model/poc-controller/commit/5c7ea83831907dd8d0684cd5ad68c26371d34c60))
* **openspec:** archive fix-reconcile-correctness-trio and sync spec ([b18a03a](https://github.com/open-platform-model/poc-controller/commit/b18a03adc1c24e2c95509acc687f2e04ea629d18))

## [0.4.2](https://github.com/open-platform-model/poc-controller/compare/v0.4.1...v0.4.2) (2026-04-18)


### Bug Fixes

* **ci:** write sbom to explicit path for cosign attest ([2a62336](https://github.com/open-platform-model/poc-controller/commit/2a62336eae89c8e18bb5c683b4eda6f805b123c0))
* **ci:** write sbom to explicit path for cosign attest ([069975a](https://github.com/open-platform-model/poc-controller/commit/069975a9be3caacf8f2fb3883cfc2b06b732243b))

## [0.4.1](https://github.com/open-platform-model/poc-controller/compare/v0.4.0...v0.4.1) (2026-04-17)


### Bug Fixes

* **ci:** drop s390x and ppc64le from release image matrix ([9f2ec87](https://github.com/open-platform-model/poc-controller/commit/9f2ec87f61a3e16515b699ae28f8cc2d8d4a0cbe))
* **ci:** drop s390x and ppc64le from release image matrix ([34329ec](https://github.com/open-platform-model/poc-controller/commit/34329ecc376574b9bf3fa51f0d284eb38518caf5))

## [0.4.0](https://github.com/open-platform-model/poc-controller/compare/v0.3.0...v0.4.0) (2026-04-17)


### Features

* **ci:** add signed image build and publish workflows ([799e844](https://github.com/open-platform-model/poc-controller/commit/799e844d3cbc2228a82415f85073d7d19bca0592))
* **ci:** add signed image build and publish workflows ([a62bc2e](https://github.com/open-platform-model/poc-controller/commit/a62bc2e58d2de819ba3f6063931fe2c7c20bbc93))


### Bug Fixes

* **ci:** disable buildkit default attestations on pr builds ([49326dd](https://github.com/open-platform-model/poc-controller/commit/49326dd3ae18258193e9387659a6e8acd2e95e7a))

## [0.3.0](https://github.com/open-platform-model/poc-controller/compare/v0.2.0...v0.3.0) (2026-04-17)


### Features

* **controller:** wire Release reconciler and source-controller scheme ([d445707](https://github.com/open-platform-model/poc-controller/commit/d4457078e72f15b6057a4299f76420c93a489162))
* **release:** add Release CRD and Flux artifact reconciler ([e17947a](https://github.com/open-platform-model/poc-controller/commit/e17947a350a71ee3a8f24fa3ee516afca14ac901))


### Bug Fixes

* **reconcile:** requeue after finalizer add ([54f158c](https://github.com/open-platform-model/poc-controller/commit/54f158ccc7d146e3d479d47fa1989445de38d9b7))


### Documentation

* migrate make command references to task ([109edb8](https://github.com/open-platform-model/poc-controller/commit/109edb8082d243f16b486c6abe49f4a88685292b))
* **openspec:** add release-artifact-loading spec ([ad38832](https://github.com/open-platform-model/poc-controller/commit/ad38832355221e440f07af160f80e6f427a521a4))
* **openspec:** add release-depends-on spec ([c1cfbf1](https://github.com/open-platform-model/poc-controller/commit/c1cfbf1957ac2b30a2e6d27cac1afb83029a450b))
* **openspec:** add release-kind-detection spec ([49fd03f](https://github.com/open-platform-model/poc-controller/commit/49fd03ff7af4fa6e90740f6bf5d4893d26b5030d))
* **openspec:** archive release-cr and sync release-reconcile-loop spec ([637500e](https://github.com/open-platform-model/poc-controller/commit/637500e9114006f8842d57553dff5389edb06b09))
* **openspec:** sync artifact-fetch and source-resolution specs ([98eab9e](https://github.com/open-platform-model/poc-controller/commit/98eab9e6b3aafe34e11fd41a39bc82cbee7507da))


### Code Refactoring

* **apply:** use APIReader for ServiceAccount existence check ([08314ea](https://github.com/open-platform-model/poc-controller/commit/08314ea3e7d73ceaa3e4c7fc32d9c6945ab823bb))
* **fixtures:** rename hello module path to testing.opmodel.dev/modules/hello ([31b1837](https://github.com/open-platform-model/poc-controller/commit/31b1837c152c0d301da2d19a0da92652e3ce0e91))
* **taskfile:** consolidate build+deploy into operator taskfile ([71f17b9](https://github.com/open-platform-model/poc-controller/commit/71f17b9e7d5bee328554d1b7749c5f8bc921badd))


### Miscellaneous Chores

* **openspec:** add changes ([a173f59](https://github.com/open-platform-model/poc-controller/commit/a173f592286e64e0ddcfe260b028fbd21dbadce5))

## [0.2.0](https://github.com/open-platform-model/poc-controller/compare/v0.1.0...v0.2.0) (2026-04-17)


### Features

* **apply:** implement prune for stale resources ([a72ece8](https://github.com/open-platform-model/poc-controller/commit/a72ece88e38a25152dd0403ffe86ea53526d9852))
* **apply:** implement SSA apply with flux resource manager ([60922ce](https://github.com/open-platform-model/poc-controller/commit/60922ce78b569bab5944232e65b825c88f0a5ab3))
* **build:** add flux source-controller install/uninstall targets ([9440e46](https://github.com/open-platform-model/poc-controller/commit/9440e46acc65fdeca76516db01a19f2bb43e136c))
* **build:** add kind-load target and set imagePullPolicy for local dev ([6d98d38](https://github.com/open-platform-model/poc-controller/commit/6d98d380fea6c9e167a71ca5494a508876284171))
* **build:** add local dev workflow with registry, kind-load, and samples ([25ebf8b](https://github.com/open-platform-model/poc-controller/commit/25ebf8bf7efbdfb84fe54762476ff0ee60470d52))
* **catalog:** add CUE composition module for registry-based provider resolution ([5627b7d](https://github.com/open-platform-model/poc-controller/commit/5627b7db2317193ddf40c83580f1fa8aa792ae9a))
* **catalog:** implement composition-based provider loading ([4a61ea6](https://github.com/open-platform-model/poc-controller/commit/4a61ea6b73359b2b22078eee55e8f47dbfa33a8f))
* **controller:** wire catalog provider loading with registry flags ([ff10dcf](https://github.com/open-platform-model/poc-controller/commit/ff10dcf7cab7d10aaf4918a7b240f6ce0a8a190c))
* **cue-native:** add registry precedence test, drop bundlerelease flux stub, archive change ([1b23475](https://github.com/open-platform-model/poc-controller/commit/1b23475e399f9f55d994459adf8555b757d5fda3))
* **inventory,pkg:** copy cli packages and implement inventory bridge ([524a566](https://github.com/open-platform-model/poc-controller/commit/524a56624a7cdcbd2e39037427849df73c08df1a))
* **metrics:** add prometheus metrics for reconcile lifecycle ([5f80d0f](https://github.com/open-platform-model/poc-controller/commit/5f80d0fd7643b4a70577d8e16d17efc0142e553c))
* **reconcile:** add exponential backoff and storm prevention ([7c2814b](https://github.com/open-platform-model/poc-controller/commit/7c2814b293878f7fb9a3eb6c98d19457e6b95729))
* **reconcile:** add failure counter increment/reset logic in phase 7 ([d8fa1b9](https://github.com/open-platform-model/poc-controller/commit/d8fa1b95abcfcfab035e534e02fc49d6a9f405e7))
* **reconcile:** add finalizer registration and deletion cleanup ([9576e4b](https://github.com/open-platform-model/poc-controller/commit/9576e4beb9e53f7128cc3d7684265a20026f5055))
* **reconcile:** add kubernetes event emission for reconcile lifecycle ([9f7860b](https://github.com/open-platform-model/poc-controller/commit/9f7860b5d6e1fa2e9ba18fae14dc7db843969e35))
* **reconcile:** add serviceaccount impersonation for apply and prune ([3df74c8](https://github.com/open-platform-model/poc-controller/commit/3df74c8fe9f15e1d15050f783f966adbdfe3e67a))
* **reconcile:** add SSA dry-run drift detection in phase 4 ([2e5d901](https://github.com/open-platform-model/poc-controller/commit/2e5d90187e57d7e49590123f1723c12e99262122))
* **reconcile:** assemble full modulerelease reconcile loop ([f383a1b](https://github.com/open-platform-model/poc-controller/commit/f383a1b14bc3bb76616b706e17fc74e6b735f7fa))
* **reconcile:** decouple renderer + unpend phase tests; archive fix-reconcile-test-coverage ([f5c6c41](https://github.com/open-platform-model/poc-controller/commit/f5c6c41a7c5f8e93e8061ac5b6b7c5726a3908df))
* **reconcile:** implement suspend/resume for ModuleRelease ([f0fb58f](https://github.com/open-platform-model/poc-controller/commit/f0fb58f225e874fa33be1caa003c4a35b3e6d064))
* **reconcile:** patch drift + counters on noop; archive fix-noop-drift-persistence ([a27e327](https://github.com/open-platform-model/poc-controller/commit/a27e32776443e32a56939a9b5e25f54453eece3a))
* **render:** implement CUE rendering bridge for controller reconciliation ([35ab5db](https://github.com/open-platform-model/poc-controller/commit/35ab5dbb50d6086a4df0646ef79548dc3917d5fd))
* replace flux source with cue-native module resolution ([e3b91af](https://github.com/open-platform-model/poc-controller/commit/e3b91af91073bea9edcace8901724dba2cd800d2))
* **samples:** add jellyfin modulerelease and update hello sample ([c8e24e7](https://github.com/open-platform-model/poc-controller/commit/c8e24e7ad1532d3f4b3a97682d6b3cfaa32bfe1d))
* **source:** implement artifact fetch with digest verification and CUE validation ([066e833](https://github.com/open-platform-model/poc-controller/commit/066e833683b4d41fb2538e5d3d79901a879b7e76))
* **source:** implement OCIRepository resolution and controller watch ([5ab59bd](https://github.com/open-platform-model/poc-controller/commit/5ab59bd48835bc29c45f5aceb54d4ae56a9f5242))
* **status:** implement bounded reconcile history tracking ([58a591d](https://github.com/open-platform-model/poc-controller/commit/58a591d647bcc4ce918b2896b4584de8bdf99040))
* **status:** implement condition helpers and flux interface compliance ([3fb586c](https://github.com/open-platform-model/poc-controller/commit/3fb586cd199ac1ac54c282b7014e9dd495d21043))
* **status:** implement digest computation for no-op detection ([fb06c4d](https://github.com/open-platform-model/poc-controller/commit/fb06c4d392acb338715e32ee733106b2d70e150a))
* **synthesis:** add cue release package synthesis ([bd022ae](https://github.com/open-platform-model/poc-controller/commit/bd022aed55c3a718c1c1817081ba8532453c9c80))


### Bug Fixes

* **build:** improve local-run with registry flag, flux network-policy, and sample updates ([0103ab8](https://github.com/open-platform-model/poc-controller/commit/0103ab88d18db09baf0b3f9348a29044937d1789))
* **deploy:** add emptyDir tmp volume to manager deployment ([1adfb83](https://github.com/open-platform-model/poc-controller/commit/1adfb83156955e777859aa9b591877b628cfdba6))
* **e2e:** default registry to ghcr so controller starts in CI ([8b1576a](https://github.com/open-platform-model/poc-controller/commit/8b1576a84d0de43e7e10b99b0f840fae603c152d))
* **test:** skip fixture-dependent specs when CUE_REGISTRY points to ghcr ([b988964](https://github.com/open-platform-model/poc-controller/commit/b988964849c7dcfe0c41d500a2f52bd5bcda5c05))


### Documentation

* add ADR template and workflow instructions ([d4d94e9](https://github.com/open-platform-model/poc-controller/commit/d4d94e9e88057de71fc8fa3eaea5d371c90e90a8))
* add testing guide and flux SSA staging reference ([865a085](https://github.com/open-platform-model/poc-controller/commit/865a08545ce1ee0aa09ed5f8d7e819464e1d4aa3))
* **adr:** add architecture decision records 001-013 ([688f005](https://github.com/open-platform-model/poc-controller/commit/688f005028bb3e3a7412536a25ca122bb5b8ed98))
* **claude:** update testing section with tier model ([fe5c4de](https://github.com/open-platform-model/poc-controller/commit/fe5c4de01d30bcb6df706119687d43f5fe4334ff))
* **constitution:** update principles for cue-native module resolution ([1062525](https://github.com/open-platform-model/poc-controller/commit/106252551a76f2f832971cef1e8ba947b1d3107b))
* **design:** add scope, naming, CUE OCI, and reconcile loop documents ([355a285](https://github.com/open-platform-model/poc-controller/commit/355a2851388d1de05f8d539092b1fecc6150830e))
* **design:** refine module-release-api for experimental scope ([d7c1834](https://github.com/open-platform-model/poc-controller/commit/d7c1834e1d4f867d4140a1571afcd28cb85683a8))
* **design:** resolve runtime-owned labels injection via #runtimeLabels in #TransformerContext ([2960e9f](https://github.com/open-platform-model/poc-controller/commit/2960e9fd9e1460face89a721c095d7a77bed83d1))
* **enhancements:** remove duplicate metadata tables from template sub-files ([9002249](https://github.com/open-platform-model/poc-controller/commit/90022492fc77b817c527a204ced807a7383325fe))
* **experiments:** add CUE OCI validation experiment ([9b915d0](https://github.com/open-platform-model/poc-controller/commit/9b915d0b208fa06b2b116f3d54dae0b7f77f4431))
* **experiments:** add module-and-release-package experiment ([c0936c5](https://github.com/open-platform-model/poc-controller/commit/c0936c53c8310dea5cb5dcdafff72c17dfa0ad8f))
* **openspec:** add changes 12-18 for remaining controller design ([ef0e6ff](https://github.com/open-platform-model/poc-controller/commit/ef0e6ff1333ed93ed13518f49af8b6aa8720f4eb))
* **openspec:** add cue-native-module-release change proposal ([d907144](https://github.com/open-platform-model/poc-controller/commit/d90714450f0a69b823cce847171b837d691a6517))
* **openspec:** add reconcile-backoff-and-storm-prevention change ([d6de542](https://github.com/open-platform-model/poc-controller/commit/d6de542f40335577c127358eab43a31506c66268))
* **openspec:** update cue-native-module-release task status and formatting ([d3177c5](https://github.com/open-platform-model/poc-controller/commit/d3177c54b83d7fe7fc20c94ca2c26e227e0e4511))
* **samples:** add working sample CRs for ModuleRelease and OCIRepository ([98ec6cc](https://github.com/open-platform-model/poc-controller/commit/98ec6cc0e9cccce22bdbe9706660809027f65a0a))
* **skills:** prefer no-body commits in commit skill ([9cd20a8](https://github.com/open-platform-model/poc-controller/commit/9cd20a82351452ba9709009332174c9985f97010))


### Code Refactoring

* **api:** remove DependsOn from ModuleRelease and BundleRelease ([2d01150](https://github.com/open-platform-model/poc-controller/commit/2d0115011ac3bf22ac1e7c9e40d503f901b07c3e))
* **controller:** migrate from legacy record.EventRecorder to events.k8s.io/v1 ([fbcc383](https://github.com/open-platform-model/poc-controller/commit/fbcc383cbabc612ecde16b451af4dc79c449509c))
* **loader:** extract LoadModulePackage into separate file ([1d60b98](https://github.com/open-platform-model/poc-controller/commit/1d60b98a63e51cf554cb93e67db1020840267546))
* **render:** use any instead of interface{} for stub types ([203bede](https://github.com/open-platform-model/poc-controller/commit/203bedec034ec99c0acce7dee3e1928f8f0afca1))
* **taskfile:** move e2e to dev namespace and add kind cue registry ([8d387f2](https://github.com/open-platform-model/poc-controller/commit/8d387f21495b5b471525da85b117f1ba1c20f099))


### Miscellaneous Chores

* **agent:** add claude agent file ([f184a1b](https://github.com/open-platform-model/poc-controller/commit/f184a1bbd4c3de3d608e44d0b29cb46f16dcc30a))
* **build:** add delete-samples target and update run command ([ada9e7e](https://github.com/open-platform-model/poc-controller/commit/ada9e7e1fd768cfc87ac1ea487c4c7f2b06c50a9))
* **openspec:** add change release-cr ([81579bc](https://github.com/open-platform-model/poc-controller/commit/81579bcdb7a2e93c883aec986d8106c4104769c2))
* **openspec:** add integration-test-coverage change artifacts ([8fb96dc](https://github.com/open-platform-model/poc-controller/commit/8fb96dc293462e986215df0c629657891d7cd0b8))
* **openspec:** add local kind deployment change proposals ([d5315a4](https://github.com/open-platform-model/poc-controller/commit/d5315a4e3f50d47d425e647e1646b471c40bf591))
* **openspec:** add project context and artifact rules ([5646084](https://github.com/open-platform-model/poc-controller/commit/5646084c124923915d77684b945b4e6a0458c90f))
* **openspec:** add release-automation change artifacts ([dede347](https://github.com/open-platform-model/poc-controller/commit/dede347a712bce7dcee9711596632f3e7e590c89))
* **openspec:** archive 01-cli-dependency-and-inventory-bridge ([c8fe131](https://github.com/open-platform-model/poc-controller/commit/c8fe1310cb02039139826b9a5e8d4f752603c0c9))
* **openspec:** archive 02-source-resolution ([13f3d58](https://github.com/open-platform-model/poc-controller/commit/13f3d584601e82f0918b3ff1ce435c7d0c627523))
* **openspec:** archive 03-artifact-fetch-and-cue-validation ([e3d5d88](https://github.com/open-platform-model/poc-controller/commit/e3d5d881105b1a98a9ab2e789cd5351d1ded9c68))
* **openspec:** archive catalog-provider-loading and catalog-registry-resolution ([d7d59bf](https://github.com/open-platform-model/poc-controller/commit/d7d59bf736263546dd871233c456ceb406f96fc0))
* **openspec:** archive cue-rendering-bridge and sync spec ([5f50077](https://github.com/open-platform-model/poc-controller/commit/5f5007766746be413b3d9dc9974432a7c1bbe133))
* **openspec:** archive digest-computation and sync spec ([108570d](https://github.com/open-platform-model/poc-controller/commit/108570d9c0308c11de49b11c2a65562cdce0e2b1))
* **openspec:** archive drift-detection and sync spec ([54e4736](https://github.com/open-platform-model/poc-controller/commit/54e47363eac2261a7b98e999d1a7252dd5a4ded1))
* **openspec:** archive events-emission and sync spec ([fb566e9](https://github.com/open-platform-model/poc-controller/commit/fb566e90cafaef09b0976fc15dd54b770c3a54d2))
* **openspec:** archive failure-counters and sync spec ([d5d7950](https://github.com/open-platform-model/poc-controller/commit/d5d7950befd73a87fe3fe5198b41d53cc7c79ce8))
* **openspec:** archive finalizer-and-deletion and sync spec ([400c4fd](https://github.com/open-platform-model/poc-controller/commit/400c4fdb26d6946e07a5899661cfba4a61742c38))
* **openspec:** archive history-tracking and sync spec ([68cac45](https://github.com/open-platform-model/poc-controller/commit/68cac45e21bcf31a8e9ec05006ddda98b67f6cb8))
* **openspec:** archive kind-image-loading change ([f73a92a](https://github.com/open-platform-model/poc-controller/commit/f73a92a725da2704972832bafffd63148dccfd0c))
* **openspec:** archive local-kind-deployment and test-oci-artifact changes ([0d6efc4](https://github.com/open-platform-model/poc-controller/commit/0d6efc4fd297a2598d8c426d1e8a92d7e5ac2a7c))
* **openspec:** archive metrics and sync spec ([e26fbb5](https://github.com/open-platform-model/poc-controller/commit/e26fbb5ffbd45600c58999e4d6d7b8c8ca2a13c9))
* **openspec:** archive migrate-events-api ([1dbc3f3](https://github.com/open-platform-model/poc-controller/commit/1dbc3f3f373139f0d3547f4fcf0dab7203487182))
* **openspec:** archive prune-stale-resources and sync spec ([b9ff97c](https://github.com/open-platform-model/poc-controller/commit/b9ff97caaa71248b7c544afc0d5a0b2cb0a50f4c))
* **openspec:** archive reconcile-loop-assembly and sync spec ([7d3b268](https://github.com/open-platform-model/poc-controller/commit/7d3b268adf95952a7ea65a7727a6ba8bfa139cb0))
* **openspec:** archive serviceaccount-impersonation and sync spec ([a01d40d](https://github.com/open-platform-model/poc-controller/commit/a01d40d45bb4aabba8eeeed4ffc7263f00fe8fdd))
* **openspec:** archive ssa-apply and sync spec ([9672b85](https://github.com/open-platform-model/poc-controller/commit/9672b851df8eae421015fd09f62ffc230dfa5f9f))
* **openspec:** archive status-conditions and sync spec ([15af893](https://github.com/open-platform-model/poc-controller/commit/15af893c53d57781c2e058ecd693b5f9f0ffc3bc))
* **openspec:** archive suspend-resume and sync spec ([06877b6](https://github.com/open-platform-model/poc-controller/commit/06877b65d3de7afb0672bb966645ef972fe9cfd5))
* **openspec:** fix formatting in release-automation artifacts ([b06a414](https://github.com/open-platform-model/poc-controller/commit/b06a4140e302164e06a723e35123b1790c476947))
* **openspec:** update ([24f3e3a](https://github.com/open-platform-model/poc-controller/commit/24f3e3a857d3beb48040b7baca3eca6c90e629a5))
* replace generic AGENTS.md with project-specific agent guide ([1a11334](https://github.com/open-platform-model/poc-controller/commit/1a1133494958804eae219afc9bd299b16e707b9d))
* **taskfile:** add silent mode and task descriptions ([d29b6fc](https://github.com/open-platform-model/poc-controller/commit/d29b6fc11298e579d847aadad0e934c73b8bbd2f))
* **testing:** fix CI/CD test frameworks ([4740833](https://github.com/open-platform-model/poc-controller/commit/4740833f293097335e51b34289d831fc5dcef5e5))
