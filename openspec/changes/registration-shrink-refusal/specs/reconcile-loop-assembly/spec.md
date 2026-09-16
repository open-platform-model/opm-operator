## ADDED Requirements

### Requirement: A rendered resource may be withheld from apply

The reconciler SHALL evaluate the rendered resources before applying them and MAY withhold an individual resource whose application would break a guarantee the operator holds. A withheld resource SHALL NOT be applied, and every other rendered resource SHALL be applied as usual, so one refused object does not strand the rest of the module.

Withholding SHALL NOT change the inventory. The inventory records which resources the instance owns, and the operator still owns a resource it declined to update; removing it would mark it stale and prune the very object the refusal exists to protect.

A reconcile that withheld a resource SHALL NOT report ready and SHALL NOT be treated as a no-op, because the cluster does not hold what the render produced.

#### Scenario: The rest of the module still applies

- **WHEN** one rendered resource is withheld
- **THEN** every other rendered resource is applied, and the apply is not reported as failed

#### Scenario: The withheld resource stays owned

- **WHEN** a resource is withheld
- **THEN** the inventory still lists it, and the prune does not delete it

#### Scenario: A withheld resource keeps the instance unconverged

- **WHEN** a reconcile withholds a resource
- **THEN** the instance reports not ready, and the outcome is not a no-op

#### Scenario: Nothing withheld behaves as before

- **WHEN** no rendered resource is withheld
- **THEN** apply, inventory, prune and the reported outcome are exactly as they were
