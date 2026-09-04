# Rendering: the platform module, concurrency and catalog skew

How a ModuleInstance or ModulePackage becomes Kubernetes objects since the
operator moved onto the library's single-build render (enhancement 0019,
change `operator-render-switch`), and the two knobs an administrator has.

## One build per render

The Platform reconciler turns the `cluster` Platform CR into a CUE module on
the operator's own disk, one directory per CR generation under
`--platform-dir`, builds it, and records the result (generation, directory,
built platform, skew policy) in a process-local store. A render then:

1. takes a lease on that record, so the platform reconciler never prunes the
   directory while the render reads it;
2. acquires the instance: for a ModuleInstance, fetches the module from the
   registry and synthesizes the instance; for a ModulePackage, loads the
   extracted package as a source-carrying instance;
3. calls the kernel's `Render`, which stages one generated render module that
   imports both the instance and the platform, builds it once in a fresh CUE
   context, and reads the matching verdicts and rendered objects off the
   built value.

Steps 1 and 2 evaluate in the process's single shared kernel and are
serialised behind the store's kernel gate. Step 3 shares nothing: the
context is created for the render and dropped with it, so renders of
different objects overlap freely. No platform value is held between renders
and nothing grows with render count.

## `--max-concurrent-renders`

The flag (default `1`) sets the maximum concurrent reconciles of the
ModuleInstance and ModulePackage controllers, and so the number of renders in
flight per kind. The Platform controller is always serial. The default keeps
the serial behaviour of earlier releases; raise it when reconcile latency
across many workloads matters.

The bound is memory, not cores. A render is single-threaded and its working
set grows with the module's component count. Measured in enhancement 0019
(experiment 08, fitted with R^2 = 0.9997):

| Quantity | Value |
| --- | --- |
| Working set per concurrent render | 61 MB + 7.75 MB per component |
| Process base (binary, module cache) | about 0.3 GB |
| CPU per render in flight | about 1.6 cores including the collector |
| Throughput saturation | physical cores / 1.6 renders in flight |

Size against the largest module the operator will see, not the average: the
pool has no admission control, so several large renders can coincide. A pod
rendering ordinary modules (10 to 25 components) at four concurrent renders
wants about 1 GB and is comfortable at 2 GB. A 129-component fleet at eight
concurrent renders wants 12 GB. Where memory is the tighter budget, fewer
workers is the right trade; throughput falls close to linearly down to two.

## `Platform.spec.skewPolicy`

Catalog version skew is a module whose `cue.mod` requires a newer build of an
OPM-namespace path (core or a catalog) than the platform pins. The render
always evaluates the platform's build; the policy decides what happens to the
mismatch:

- `Warn` (the default when unset): render, and report the skew as a Warning
  event with reason `RenderWarning` on the ModuleInstance or ModulePackage.
  The event is emitted when the object's set of warnings changes, not on
  every reconcile; unhandled optional traits are reported the same way.
- `Refuse`: refuse the render before evaluation. The workload reports
  `Ready=False` with reason `SkewRefused` and a message naming the path,
  the module's required build and the platform's build; nothing is applied.
  The fix is a platform pin bump or a module downgrade.

The policy is not part of the generated platform module: changing it alone
bumps the Platform generation, regenerates a byte-identical module in a new
directory and re-enqueues every workload through the Platform watch, so a
switch to `Refuse` takes effect on the next reconcile of each object.

Every render also logs the resolved-versions rows (each OPM path the module
requires, the build it asked for and the build the platform carries) at
verbosity 1.

## Error reasons

| Ready reason | Cause | Recovery |
| --- | --- | --- |
| `PlatformNotReady` | no platform module is recorded yet | automatic once the Platform is `Generated` |
| `ResolutionFailed` | a module identity mismatch, an unresolved platform demand, or a component no transformer matched | change the module or the platform's catalogs |
| `SkewRefused` | catalog skew under `Refuse` | bump the platform pin or downgrade the module |
| `RenderFailed` | a transformer failed, two enabled catalogs provide the same provider-fulfilled contract, or any other evaluation error | fix the module or the platform |
