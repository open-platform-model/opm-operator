# Changelog

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
