package render

import (
	"fmt"
	"strings"

	oerrors "github.com/open-platform-model/poc-controller/pkg/errors"
)

// UnmatchedComponentsError is returned when one or more components have no
// matching transformer. It includes per-component diagnostics listing which
// transformers were evaluated and what was missing (labels, resources, traits).
//
// Each unmatched component is surfaced as a *oerrors.TransformError via Unwrap(),
// enabling callers to use errors.As for typed handling of individual failures.
type UnmatchedComponentsError struct {
	// Components is the list of component names with no matching transformer.
	Components []string

	// Matches is the full match result matrix, used to build per-component diagnostics.
	Matches map[string]map[string]MatchResult
}

func (e *UnmatchedComponentsError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d component(s) have no matching transformer: %v\n",
		len(e.Components), e.Components)

	for _, compName := range e.Components {
		tfResults, ok := e.Matches[compName]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "  component %q:\n", compName)
		for tfFQN, result := range tfResults {
			if result.Matched {
				continue
			}
			fmt.Fprintf(&sb, "    transformer %q did not match:\n", tfFQN)
			if len(result.MissingLabels) > 0 {
				fmt.Fprintf(&sb, "      missing labels:    %v\n", result.MissingLabels)
			}
			if len(result.MissingResources) > 0 {
				fmt.Fprintf(&sb, "      missing resources: %v\n", result.MissingResources)
			}
			if len(result.MissingTraits) > 0 {
				fmt.Fprintf(&sb, "      missing traits:    %v\n", result.MissingTraits)
			}
		}
	}

	return sb.String()
}

// Unwrap returns a slice of *oerrors.TransformError — one per unmatched component —
// so that callers can use errors.As to extract per-component failure details.
//
// Each TransformError carries the component name and the first non-matching
// transformer FQN. Its Cause is a plain terminal error describing the failure,
// not a nested UnmatchedComponentsError, to prevent infinite recursion when
// errors.As traverses the chain.
func (e *UnmatchedComponentsError) Unwrap() []error {
	errs := make([]error, 0, len(e.Components))
	for _, compName := range e.Components {
		compMatches := e.Matches[compName]
		// Find the first non-matching transformer FQN for context.
		// If the component had no transformers evaluated, leave TransformerFQN empty.
		tfFQN := ""
		for fqn, result := range compMatches {
			if !result.Matched {
				tfFQN = fqn
				break
			}
		}
		errs = append(errs, &oerrors.TransformError{
			ComponentName:  compName,
			TransformerFQN: tfFQN,
			Cause:          fmt.Errorf("component %q has no matching transformer", compName),
		})
	}
	return errs
}
