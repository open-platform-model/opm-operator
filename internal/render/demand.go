package render

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/library/opm/module"
	"github.com/open-platform-model/library/opm/schema"
)

// componentResources and componentTraits are #resources and #traits on a
// component: the contract FQNs the component declares. They are definitions,
// so they are built with cue.Def rather than parsed.
//
// They are not in the library's opm/schema path inventory, which carries
// requiredResources and requiredTraits — the TRANSFORMER side of the same
// relation, read relative to a transformer's demand entry. These are the
// component side, and the render's own matching is what pairs them:
// render.cue.tmpl's predicate rung asks whether `comp.#resources[fqn]` exists
// for each `tf.requiredResources[fqn]`. Reading the component side is
// therefore reading exactly the keys matching consults, in the same keyspace
// TransformerRegistration.spec.provides carries.
var (
	componentResources = cue.MakePath(cue.Def("resources"))
	componentTraits    = cue.MakePath(cue.Def("traits"))
)

// declaredContracts returns every contract FQN the instance's components
// declare, sorted and deduplicated.
//
// This is the instance's DEMAND, and it is a property of the instance alone:
// no platform is consulted, so the result does not move when the platform
// does. It deliberately lists every declared contract rather than only the
// provider-fulfilled ones — narrowing to those would need the platform's
// contract inventory and reintroduce that coupling, and the caller's
// intersection with a claim's provides is exact without it, since a contract
// a claim provides is provider-fulfilled by construction.
//
// An instance with no components yields an empty slice, never nil, so a
// status write does not oscillate between absent and empty.
func declaredContracts(inst *module.Instance) ([]string, error) {
	contracts := make([]string, 0)
	if inst == nil {
		return contracts, nil
	}

	components := inst.Package.LookupPath(schema.Components)
	if !components.Exists() {
		return contracts, nil
	}
	if err := components.Err(); err != nil {
		return nil, fmt.Errorf("instance %s did not evaluate: %w", schema.Components, err)
	}

	iter, err := components.Fields()
	if err != nil {
		return nil, fmt.Errorf("reading instance %s: %w", schema.Components, err)
	}
	for iter.Next() {
		name := iter.Selector().Unquoted()
		for _, declared := range []cue.Path{componentResources, componentTraits} {
			fqns, err := contractKeys(iter.Value(), declared)
			if err != nil {
				return nil, fmt.Errorf("reading %s of component %q: %w", declared, name, err)
			}
			contracts = append(contracts, fqns...)
		}
	}

	slices.Sort(contracts)
	return slices.Compact(contracts), nil
}

// contractKeys returns the field names of the component's demand map at path.
// An absent map contributes nothing: #traits is optional on a component, and
// a component declaring neither is a component nothing can match rather than
// an error this function should raise.
func contractKeys(component cue.Value, path cue.Path) ([]string, error) {
	demands := component.LookupPath(path)
	if !demands.Exists() {
		return nil, nil
	}
	iter, err := demands.Fields()
	if err != nil {
		return nil, err
	}
	var fqns []string
	for iter.Next() {
		fqns = append(fqns, iter.Selector().Unquoted())
	}
	return fqns, nil
}
