package render

import (
	"fmt"
	"sort"

	"cuelang.org/go/cue"

	"github.com/open-platform-model/poc-controller/pkg/provider"
)

type MatchResult struct {
	Matched          bool     `json:"matched"`
	MissingLabels    []string `json:"missingLabels"`
	MissingResources []string `json:"missingResources"`
	MissingTraits    []string `json:"missingTraits"`
}

type MatchPlan struct {
	Matches         map[string]map[string]MatchResult
	Unmatched       []string
	UnhandledTraits map[string][]string
}

type MatchedPair struct {
	ComponentName  string
	TransformerFQN string
}

type NonMatchedPair struct {
	ComponentName    string
	TransformerFQN   string
	MissingLabels    []string
	MissingResources []string
	MissingTraits    []string
}

// Match compares each component against all transformers in the provider, returning a MatchPlan
// that details which transformers matched which components and what was missing for non-matches.
// It also identifies any traits present in components that are not handled by any matched transformer,
// which will be ignored in rendering and should be surfaced as warnings to the user.
//
//nolint:gocyclo // matching is naturally branchy but kept in one place for parity with matcher.cue
func Match(components cue.Value, p *provider.Provider) (*MatchPlan, error) {
	if p == nil {
		return nil, fmt.Errorf("provider is required")
	}
	plan := &MatchPlan{Matches: map[string]map[string]MatchResult{}, UnhandledTraits: map[string][]string{}}

	compIter, err := components.Fields()
	if err != nil {
		return nil, fmt.Errorf("iterating components: %w", err)
	}

	transformers := p.Data.LookupPath(cue.ParsePath("#transformers"))
	if !transformers.Exists() {
		return plan, nil
	}

	for compIter.Next() {
		compName := compIter.Selector().Unquoted()
		compVal := compIter.Value()
		labels := labelPairs(compVal.LookupPath(cue.ParsePath("metadata.labels")))
		resources := fieldKeys(compVal.LookupPath(cue.MakePath(cue.Def("resources"))))
		traits := fieldKeys(compVal.LookupPath(cue.MakePath(cue.Def("traits"))))

		plan.Matches[compName] = map[string]MatchResult{}
		tfIter, err := transformers.Fields()
		if err != nil {
			return nil, fmt.Errorf("iterating transformers: %w", err)
		}
		matchedTFs := []string{}
		for tfIter.Next() {
			tfFQN := tfIter.Selector().Unquoted()
			tfVal := tfIter.Value()
			missingLabels := missingMapLabels(tfVal.LookupPath(cue.ParsePath("requiredLabels")), labels)
			missingResources := missingKeys(tfVal.LookupPath(cue.ParsePath("requiredResources")), resources)
			missingTraits := missingKeys(tfVal.LookupPath(cue.ParsePath("requiredTraits")), traits)

			result := MatchResult{
				Matched:          len(missingLabels) == 0 && len(missingResources) == 0 && len(missingTraits) == 0,
				MissingLabels:    missingLabels,
				MissingResources: missingResources,
				MissingTraits:    missingTraits,
			}
			plan.Matches[compName][tfFQN] = result
			if result.Matched {
				matchedTFs = append(matchedTFs, tfFQN)
			}
		}

		if len(matchedTFs) == 0 {
			plan.Unmatched = append(plan.Unmatched, compName)
		}

		handled := map[string]struct{}{}
		for _, tfFQN := range matchedTFs {
			tfVal := transformers.LookupPath(cue.MakePath(cue.Str(tfFQN)))
			for _, fqn := range fieldKeys(tfVal.LookupPath(cue.ParsePath("requiredTraits"))) {
				handled[fqn] = struct{}{}
			}
			for _, fqn := range fieldKeys(tfVal.LookupPath(cue.ParsePath("optionalTraits"))) {
				handled[fqn] = struct{}{}
			}
		}
		for _, fqn := range traits {
			if _, ok := handled[fqn]; !ok {
				plan.UnhandledTraits[compName] = append(plan.UnhandledTraits[compName], fqn)
			}
		}
		sort.Strings(plan.UnhandledTraits[compName])
	}

	sort.Strings(plan.Unmatched)
	return plan, nil
}

// MatchedPairs returns all matched component-transformer pairs,
// sorted by component name and then transformer FQN.
func (p *MatchPlan) MatchedPairs() []MatchedPair {
	pairs := make([]MatchedPair, 0)
	for compName, tfResults := range p.Matches {
		for tfFQN, result := range tfResults {
			if result.Matched {
				pairs = append(pairs, MatchedPair{ComponentName: compName, TransformerFQN: tfFQN})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].ComponentName != pairs[j].ComponentName {
			return pairs[i].ComponentName < pairs[j].ComponentName
		}
		return pairs[i].TransformerFQN < pairs[j].TransformerFQN
	})
	return pairs
}

// NonMatchedPairs returns all non-matched component-transformer pairs
// with missing labels, resources, and traits. Sorted by component
// name then transformer FQN.
func (p *MatchPlan) NonMatchedPairs() []NonMatchedPair {
	pairs := make([]NonMatchedPair, 0)
	for compName, tfResults := range p.Matches {
		for tfFQN, result := range tfResults {
			if !result.Matched {
				pairs = append(pairs, NonMatchedPair{
					ComponentName:    compName,
					TransformerFQN:   tfFQN,
					MissingLabels:    result.MissingLabels,
					MissingResources: result.MissingResources,
					MissingTraits:    result.MissingTraits,
				})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].ComponentName != pairs[j].ComponentName {
			return pairs[i].ComponentName < pairs[j].ComponentName
		}
		return pairs[i].TransformerFQN < pairs[j].TransformerFQN
	})
	return pairs
}

// Warnings returns warnings for traits not handled by any matched
// transformer. Those trait values will be ignored in rendering.
func (p *MatchPlan) Warnings() []string {
	if len(p.UnhandledTraits) == 0 {
		return nil
	}
	compNames := make([]string, 0, len(p.UnhandledTraits))
	for compName := range p.UnhandledTraits {
		compNames = append(compNames, compName)
	}
	sort.Strings(compNames)
	var warnings []string
	for _, compName := range compNames {
		traits := append([]string(nil), p.UnhandledTraits[compName]...)
		sort.Strings(traits)
		for _, fqn := range traits {
			warnings = append(warnings, fmt.Sprintf(
				"component %q: trait %q is not handled by any matched transformer (values will be ignored)",
				compName, fqn,
			))
		}
	}
	return warnings
}

// labelPairs converts a cue struct of string fields into a set of
// "key=value" pairs for matching against required labels.
func labelPairs(v cue.Value) map[string]struct{} {
	pairs := map[string]struct{}{}
	iter, err := v.Fields(cue.Optional(true))
	if err != nil {
		return pairs
	}
	for iter.Next() {
		str, err := iter.Value().String()
		if err != nil {
			continue
		}
		pairs[fmt.Sprintf("%s=%s", iter.Selector().Unquoted(), str)] = struct{}{}
	}
	return pairs
}

// fieldKeys returns the sorted list of field keys in the given cue struct value.
// No options are passed so that definition fields (#resources, #traits) are returned correctly.
func fieldKeys(v cue.Value) []string {
	iter, err := v.Fields()
	if err != nil {
		return nil
	}
	var out []string
	for iter.Next() {
		out = append(out, iter.Selector().Unquoted())
	}
	sort.Strings(out)
	return out
}

// missingMapLabels compares required labels in a transformer against
// the "key=value" pairs present in a component's metadata.labels.
func missingMapLabels(required cue.Value, have map[string]struct{}) []string {
	iter, err := required.Fields(cue.Optional(true))
	if err != nil {
		return nil
	}
	var missing []string
	for iter.Next() {
		str, err := iter.Value().String()
		if err != nil {
			continue
		}
		pair := fmt.Sprintf("%s=%s", iter.Selector().Unquoted(), str)
		if _, ok := have[pair]; !ok {
			missing = append(missing, pair)
		}
	}
	sort.Strings(missing)
	return missing
}

// missingKeys compares required keys in a transformer against the
// keys present in a component, returning any missing keys.
func missingKeys(required cue.Value, have []string) []string {
	haveSet := map[string]struct{}{}
	for _, k := range have {
		haveSet[k] = struct{}{}
	}
	iter, err := required.Fields(cue.Optional(true))
	if err != nil {
		return nil
	}
	var missing []string
	for iter.Next() {
		key := iter.Selector().Unquoted()
		if _, ok := haveSet[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
