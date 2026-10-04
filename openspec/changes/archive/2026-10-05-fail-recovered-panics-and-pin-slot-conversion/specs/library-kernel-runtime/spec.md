## MODIFIED Requirements

### Requirement: Render concurrency is a manager flag bounded by memory

The manager SHALL accept `--max-concurrent-renders` (integer, default 1) and SHALL construct from it one pool of render slots shared by the ModuleInstance and ModulePackage reconcilers, so the flag is the maximum number of renders in flight across the whole process, both kinds together. A reconcile SHALL take a slot before it calls its renderer (platform lease, acquisition, synthesis and render), SHALL hold it while the result is exported for apply (render digest and conversion to unstructured objects), and SHALL release it only after the rendered CUE values are dropped, on success, on error and on a panic that the controller runtime recovers. So no reconcile holds a rendered CUE value without a slot, and the flag bounds the builds resident in the process. Each of the two controllers SHALL also use the flag as its maximum concurrent reconciles, so phases outside the render are not serialised behind the other kind's renders. A reconcile whose wait for a slot ends because its context is cancelled SHALL return the context error without calling the renderer and without patching the object's status, emitting an event or recording reconcile metrics. The Platform controller SHALL stay serial and SHALL take no slot. The flag's help SHALL state the memory sizing rule (per-render cost grows with component count) and that the bound is shared by both kinds, so the value is chosen against the pod's memory limit.

#### Scenario: Default keeps reconciles serial

- **WHEN** the manager starts without the flag
- **THEN** the ModuleInstance and ModulePackage controllers each reconcile one object at a time, and at most one render is in flight across both

#### Scenario: Raising the bound allows overlap

- **WHEN** the manager starts with `--max-concurrent-renders=4`
- **THEN** up to four ModuleInstances (and four ModulePackages) reconcile at once, and at most four renders are in flight across both kinds together

#### Scenario: Both kinds share the slots

- **WHEN** the manager runs with `--max-concurrent-renders=1` and a ModuleInstance and a ModulePackage are enqueued at the same time
- **THEN** one renders while the other waits for the slot, and both reach Ready

#### Scenario: Shutdown while waiting for a slot

- **WHEN** the manager is stopping and a reconcile is still waiting for a render slot
- **THEN** the reconcile returns the context error without calling the renderer, and the object's status is not patched

#### Scenario: A panicking render frees its slot

- **WHEN** a renderer panics while a reconcile holds a render slot and the controller runtime recovers the panic
- **THEN** the slot is free again, and the next render of either kind takes it without waiting

#### Scenario: Conversion runs while the slot is held

- **WHEN** a ModuleInstance or ModulePackage reconcile exports its render result for apply, on a pool of one slot
- **THEN** that slot is taken for the whole export, and it is free again once the reconcile has dropped the rendered resources
