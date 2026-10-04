## MODIFIED Requirements

### Requirement: Reason constants

The status package SHALL define the reason constants used on the Ready condition, including `SkewRefused` (Ready=False: the platform's skew policy is `Refuse` and the module requires a newer catalog build than the platform pins), `DuplicateIdentities` (Ready=False: two or more rendered objects share one Kubernetes apply identity, so the render is refused before apply naming every producing component; enhancement 0015 D15) and `ReconcilePanic` (Ready=False, Reconciling=True: a ModuleInstance or ModulePackage reconcile panicked and the attempt was recorded as a failure before the panic propagated) beside the existing `ResolutionFailed`, `RenderFailed`, `PlatformNotReady` and the Platform reasons `Generated`, `BuildFailed`, `GenerateFailed`.

#### Scenario: Reason constants available
- **WHEN** code imports `internal/status`
- **THEN** all reason constants are available as exported string constants

#### Scenario: Skew refusal reason is available

- **WHEN** a render is refused under the `Refuse` policy
- **THEN** the reconciler marks `Ready=False` with reason `SkewRefused`

#### Scenario: Duplicate-identity reason is available

- **WHEN** a render is refused because two compiled objects share one apply identity
- **THEN** the reconciler marks `Ready=False` with reason `DuplicateIdentities`

#### Scenario: Reconcile-panic reason is available

- **WHEN** a ModuleInstance or ModulePackage reconcile panics
- **THEN** the reconciler marks `Ready=False` and `Reconciling=True` with reason `ReconcilePanic` and removes `Stalled`
