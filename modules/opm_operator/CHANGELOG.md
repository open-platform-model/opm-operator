# Changelog

## [0.2.0](https://github.com/open-platform-model/opm-operator/compare/opm_operator-v0.1.0...opm_operator-v0.2.0) (2026-10-09)


### ⚠ BREAKING CHANGES

* judge every prune and delete with the library ownership verdict ([#271](https://github.com/open-platform-model/opm-operator/issues/271))
* **apply:** keep PersistentVolumeClaims on a forced recreate unless spec.dataPolicy is Delete ([#268](https://github.com/open-platform-model/opm-operator/issues/268))
* **apply:** keep PersistentVolumeClaims on prune and deletion unless spec.dataPolicy is Delete ([#267](https://github.com/open-platform-model/opm-operator/issues/267))

### Features

* **api:** record the last applied module version in plain text on status ([#242](https://github.com/open-platform-model/opm-operator/issues/242)) ([dd0d798](https://github.com/open-platform-model/opm-operator/commit/dd0d798bb0b11387660e12b42394688f77ba24db))
* **apply:** keep PersistentVolumeClaims on prune and deletion unless spec.dataPolicy is Delete ([#267](https://github.com/open-platform-model/opm-operator/issues/267)) ([24b2360](https://github.com/open-platform-model/opm-operator/commit/24b236060ef2bf7353009d89f37fa96a0c93a0b1))
* **controller:** report whether a module instance has rolled out in a Healthy condition ([#257](https://github.com/open-platform-model/opm-operator/issues/257)) ([59537b7](https://github.com/open-platform-model/opm-operator/commit/59537b7fc265980a352065a3ff2881e1d9898878))
* **controller:** skip the render when its inputs are unchanged ([#246](https://github.com/open-platform-model/opm-operator/issues/246)) ([ac81eec](https://github.com/open-platform-model/opm-operator/commit/ac81eec2f2eab36da8325ae416e83214658c4cd7))
* judge every prune and delete with the library ownership verdict ([#271](https://github.com/open-platform-model/opm-operator/issues/271)) ([5bad00a](https://github.com/open-platform-model/opm-operator/commit/5bad00aba49ffbd29fa633fb04a2bdae3746fed8))


### Bug Fixes

* **apply:** impersonate the ServiceAccount only and drop the users and groups grant ([#260](https://github.com/open-platform-model/opm-operator/issues/260)) ([44da5e9](https://github.com/open-platform-model/opm-operator/commit/44da5e950f7bc0c7e6d71fc0891b43ae4b3ad865))
* **apply:** keep PersistentVolumeClaims on a forced recreate unless spec.dataPolicy is Delete ([#268](https://github.com/open-platform-model/opm-operator/issues/268)) ([553c59f](https://github.com/open-platform-model/opm-operator/commit/553c59f2eb16cb06667b0ed4767ae4f3e546f278))
* **controller:** unblock the stuck deletes of a module instance ([#263](https://github.com/open-platform-model/opm-operator/issues/263)) ([7474778](https://github.com/open-platform-model/opm-operator/commit/7474778526025e3a839542e56ac838b56b460780))

## 0.1.0 (2026-10-04)


### Features

* **rbac:** ship unbound viewer roles for platforms, packages and registrations ([#223](https://github.com/open-platform-model/opm-operator/issues/223)) ([6a14adb](https://github.com/open-platform-model/opm-operator/commit/6a14adb91a12e56fa7e4f5e2ac9deeea45567299))


### Bug Fixes

* **deps:** deploy operator v1.0.0-beta.8 from the operator module ([#229](https://github.com/open-platform-model/opm-operator/issues/229)) ([05d0396](https://github.com/open-platform-model/opm-operator/commit/05d03964bf7651fb06885e10ac6aab3c08417cf2))
