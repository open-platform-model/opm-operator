## MODIFIED Requirements

### Requirement: Staged apply ordering
Resources MUST be applied in the library's kind-class order, with Flux's `ApplyAll` as the apply engine. The `internal/apply` package MUST cut the resource set into the stages the library's `object.Stages` returns: the cluster definitions (every CustomResourceDefinition and core Namespace) first, then one stage per library weight in ascending weight. It MUST submit each stage in its own `ApplyAll` call, in stage order, so Flux's own sort inside a call orders only objects of one stage and never inverts the library order. After the cluster-definition stage it MUST wait for that stage's objects to become ready before it applies the next stage; a CRD is ready once its `Established` condition is True. Discovery of its kind can lag that condition; the requirement "Custom resources wait for discovery of a CRD in the same set" covers that lag. `Apply` MUST NOT reorder the slice its caller passed. Source: 0012:D4:R3, 0012:D5:R1.

#### Scenario: CRD applied before custom resource
- **WHEN** the resource set contains both a CRD and an instance of that CRD
- **THEN** the CRD is applied in the cluster-definition stage, which is waited for, before the instance is applied in a later stage

#### Scenario: Namespace applied before namespaced resource
- **WHEN** the resource set contains a Namespace and resources in that namespace
- **THEN** the Namespace is applied in the cluster-definition stage before the namespaced resources

#### Scenario: Library order holds where Flux's order differs
- **WHEN** the resource set contains a Deployment, a StatefulSet and a PersistentVolumeClaim, submitted with the workloads first
- **THEN** the PersistentVolumeClaim is applied before the Deployment and the StatefulSet

#### Scenario: Each stage is one ApplyAll call
- **WHEN** the resource set spans the cluster definitions and several library weights
- **THEN** each `ApplyAll` call receives objects of one stage only, and the stages are submitted in ascending library order

#### Scenario: The caller's slice keeps its order
- **WHEN** `Apply` returns, successfully or not
- **THEN** the slice the caller passed holds its objects in the order the caller gave
