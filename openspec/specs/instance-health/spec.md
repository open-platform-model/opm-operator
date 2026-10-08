# instance-health Specification

## Purpose
Report, in a `Healthy` condition separate from `Ready`, whether the objects a ModuleInstance or ModulePackage applied have rolled out, judged by the library's `opm/k8s/health` from uncached reads taken after the apply, and requeue until they have.

## Requirements

### Requirement: A Healthy condition reports whether the applied objects have rolled out

The operator SHALL write a `Healthy` condition on a ModuleInstance and on a ModulePackage. It reports whether the objects in `status.inventory` have rolled out, as judged by the library's `opm/k8s/health`. It SHALL be one of:

- `True` with reason `RolledOut`: every inventory object was read and `health.IsHealthy` holds for its `health.Evaluate` status;
- `False` with reason `ProgressDeadlineExceeded`: at least one object is a Deployment for which `health.ProgressDeadlineExceeded` holds;
- `False` with reason `NotRolledOut`: no Deployment is stalled, and at least one object is not healthy or does not exist;
- `Unknown` with reason `HealthUnknown`: no object is known to be unhealthy, and an object could not be read, the reader could not be built, or the inventory is empty.

When more than one applies, `ProgressDeadlineExceeded` wins over `NotRolledOut`, which wins over `HealthUnknown`. `RolledOut` holds only when none of the others applies. The message SHALL begin with the ready count and the total `health.Aggregate` returns. For `ProgressDeadlineExceeded` and `NotRolledOut` it SHALL name up to five objects that are not healthy, each with its kind, namespace, name and status (`Missing` for an object that does not exist), followed by how many more there are. For `ProgressDeadlineExceeded` the stalled Deployments come first, in inventory order, then the other objects that are not healthy, in inventory order; for `NotRolledOut` every named object is in inventory order. For a read failure it SHALL name the error of the first unreadable object in inventory order. A read refused because the object's kind is no longer served (the API server has no mapping for it) counts as a missing object, not an unreadable one, because such an object cannot exist. Two judgements of the same cluster state SHALL produce the same message.

`Ready` SHALL keep its meaning: the render was applied. No reason, status or timing of `Ready` depends on `Healthy`. `spec.dependsOn` SHALL keep waiting on `Ready`, and a TransformerRegistration SHALL keep activating on its provider's `Ready`.

#### Scenario: Every object healthy

- **WHEN** an instance's inventory holds a ConfigMap and a Deployment whose observed generation, updated, available and total replicas match its spec
- **THEN** `Healthy` is `True` with reason `RolledOut` and a message beginning "2/2 objects ready"

#### Scenario: A rollout in progress

- **WHEN** the Deployment's controller has not yet observed its current generation
- **THEN** `Healthy` is `False` with reason `NotRolledOut`, and the message names `Deployment <namespace>/<name> (NotReady)`
- **AND** `Ready` is `True` with reason `ReconciliationSucceeded`

#### Scenario: A stalled Deployment

- **WHEN** the Deployment has observed its generation and reports `Progressing` with reason `ProgressDeadlineExceeded`, and another object is not ready
- **THEN** `Healthy` is `False` with reason `ProgressDeadlineExceeded`, and the message names the stalled Deployment first, whatever its place in the inventory

#### Scenario: A missing object

- **WHEN** an inventory object does not exist on the cluster
- **THEN** `Healthy` is `False` with reason `NotRolledOut`, and the message names the object with status `Missing`

#### Scenario: A kind that is no longer served

- **WHEN** an inventory object's kind has no mapping on the API server, because its CustomResourceDefinition was deleted
- **THEN** the object counts as `Missing`, and `Healthy` is `False` with reason `NotRolledOut`

#### Scenario: An unreadable object

- **WHEN** every readable object is healthy and one object's read fails with an error other than not found
- **THEN** `Healthy` is `Unknown` with reason `HealthUnknown`, and the message names the error

#### Scenario: An empty inventory

- **WHEN** the inventory holds no objects
- **THEN** `Healthy` is `Unknown` with reason `HealthUnknown`

#### Scenario: Ready and its readers are unchanged

- **WHEN** a ModulePackage is `Ready=True` and `Healthy=False`
- **THEN** a ModulePackage whose `spec.dependsOn` names it proceeds
- **AND** a TransformerRegistration provided by a `Ready=True`, `Healthy=False` ModuleInstance activates as before

### Requirement: Health is judged only after a successful outcome

The reconciler SHALL judge `Healthy` on every reconcile that leaves the object `Ready=True` with reason `ReconciliationSucceeded`. That is after a successful apply, and after the prune when it runs, on a `NoOp`, and on a reconcile that skips its render because its inputs are unchanged. It SHALL judge the entries the reconcile committed to `status.inventory`. A reconcile that fails, is refused, times out, panics, is suspended or is being deleted SHALL leave `Healthy` as it was.

#### Scenario: A failed render keeps the last judgement

- **WHEN** an instance with `Healthy=True` fails its next render
- **THEN** `Healthy` is unchanged

#### Scenario: A NoOp re-judges

- **WHEN** a reconcile renders, finds nothing to apply, and a Deployment in the inventory has since lost an available replica
- **THEN** `Healthy` is `False` with reason `NotRolledOut`

### Requirement: Objects are read uncached, after the apply, through the applying identity

The reconciler SHALL read each inventory object uncached. When an effective ServiceAccount applies the instance, it SHALL read through the client impersonating that ServiceAccount. Otherwise it SHALL read through the manager's uncached API reader, never through the manager's cached client. On a successful apply it SHALL read only after the apply and prune have returned. The reconciler SHALL NOT set `Healthy=True` from a read taken before the apply it reports on. Reads SHALL run with at most 8 in flight and SHALL share one deadline of 30 seconds. A read that does not finish within it counts as unreadable. When the impersonated reader cannot be built, `Healthy` SHALL be `Unknown` with reason `HealthUnknown`. Source: 0012:D3 (the frontend fetches every object with its own client).

#### Scenario: An impersonated instance reads as its ServiceAccount

- **WHEN** a ModuleInstance with `spec.serviceAccountName: deployer` is applied
- **THEN** every health read is made as `system:serviceaccount:<namespace>:deployer`

#### Scenario: An instance without a ServiceAccount reads uncached

- **WHEN** a ModuleInstance with no effective ServiceAccount is applied
- **THEN** the health reads go to the API server through the manager's uncached reader

#### Scenario: A just-updated Deployment is not reported rolled out

- **WHEN** an apply changes a Deployment's pod template and the health read returns it before its controller has observed the new generation
- **THEN** `Healthy` is `False` with reason `NotRolledOut`

#### Scenario: A missing ServiceAccount on a skip

- **WHEN** a skipped reconcile cannot build the impersonated reader because the ServiceAccount no longer exists
- **THEN** `Healthy` is `Unknown` with reason `HealthUnknown` and the message names the error

### Requirement: A reconcile requeues until the instance has rolled out

When `Healthy` is `False` with reason `NotRolledOut`, or `Unknown` because a read failed or the reader could not be built, the reconcile SHALL requeue after half the time since `status.lastAppliedAt`, at least 5 seconds and at most 2 minutes (5 seconds when `lastAppliedAt` is unset or in the future). When `Healthy` is `False` with reason `ProgressDeadlineExceeded`, the reconcile SHALL stop the fast requeue and requeue after the stalled recheck interval (30 minutes). When `Healthy` is `True`, or `Unknown` because the inventory is empty, health SHALL NOT cause a requeue: a ModuleInstance then requeues on the instance reconcile interval alone (`reconcile-loop-assembly`), and not at all when that interval is `0`. A ModulePackage SHALL requeue after the shorter of the health requeue and `spec.interval`. A health requeue SHALL NOT set `status.nextRetryAt` and SHALL NOT count as a failure.

#### Scenario: A fresh rollout is checked quickly

- **WHEN** an instance was applied 4 seconds ago and is `NotRolledOut`
- **THEN** the reconcile requeues after 5 seconds and `status.nextRetryAt` is unset

#### Scenario: A long rollout settles at the ceiling

- **WHEN** an instance applied 10 minutes ago is still `NotRolledOut`
- **THEN** the reconcile requeues after 2 minutes

#### Scenario: A stalled rollout stops the fast requeue

- **WHEN** `Healthy` becomes `False` with reason `ProgressDeadlineExceeded`
- **THEN** the reconcile requeues after 30 minutes

#### Scenario: A rolled-out instance is not requeued for health

- **WHEN** a ModuleInstance's reconcile ends `RolledOut`
- **THEN** health adds no requeue, and the reconcile requeues on the instance reconcile interval only
- **AND** it returns without a requeue when that interval is `0`

#### Scenario: A package keeps its interval as an upper bound

- **WHEN** a ModulePackage with `spec.interval: 1m` is `NotRolledOut` and was applied 10 minutes ago
- **THEN** it requeues after 1 minute

### Requirement: Both kinds print the Healthy status

ModuleInstance and ModulePackage SHALL each carry a printer column `Healthy` showing the `Healthy` condition's status, placed right after `Ready` at the default priority.

#### Scenario: kubectl get shows Healthy

- **WHEN** a user runs `kubectl get moduleinstances`
- **THEN** the output has a `HEALTHY` column showing `True`, `False`, `Unknown` or nothing
