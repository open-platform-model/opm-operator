package validate

import (
	"strings"

	"cuelang.org/go/cue"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"

	oerrors "github.com/open-platform-model/opm-operator/pkg/errors"
)

const fieldNotAllowed = "field not allowed"

func Config(schema cue.Value, values []cue.Value, context, name string) (cue.Value, *oerrors.ConfigError) {
	if !schema.Exists() || len(values) == 0 {
		return cue.Value{}, nil
	}

	var combined cueerrors.Error
	hasSchemaErrors := false
	hasMergeConflicts := false

	for _, value := range values {
		var changed bool
		combined, changed = appendSchemaErrors(schema, value, combined, false)
		if changed {
			hasSchemaErrors = true
		}
	}

	merged := values[0]
	for _, v := range values[1:] {
		merged = merged.Unify(v)
		if err := merged.Err(); err != nil {
			for _, ce := range cueerrors.Errors(err) {
				combined = cueerrors.Append(combined, ce)
			}
			hasMergeConflicts = true
		}
	}

	if !hasSchemaErrors && !hasMergeConflicts {
		combined, _ = appendSchemaErrors(schema, merged, combined, true)
	}

	if combined != nil {
		return cue.Value{}, &oerrors.ConfigError{Context: context, Name: name, RawError: combined}
	}

	return merged, nil
}

func appendSchemaErrors(schema, value cue.Value, acc cueerrors.Error, requireConcrete bool) (cueerrors.Error, bool) {
	beforeCount := len(cueerrors.Errors(acc))
	changed := false
	acc = walkDisallowed(schema, value, nil, acc)

	unified := schema.Unify(value)
	validateOpts := []cue.Option{}
	if requireConcrete {
		validateOpts = append(validateOpts, cue.Concrete(true))
	}
	if err := unified.Validate(validateOpts...); err != nil {
		for _, ce := range cueerrors.Errors(err) {
			f, _ := ce.Msg()
			if f == fieldNotAllowed {
				continue
			}
			acc = cueerrors.Append(acc, ce)
			changed = true
		}
	}

	if len(cueerrors.Errors(acc)) > beforeCount {
		changed = true
	}

	return acc, changed
}

func walkDisallowed(schema, val cue.Value, pathPrefix []string, acc cueerrors.Error) cueerrors.Error {
	iter, err := val.Fields(cue.Optional(true))
	if err != nil {
		return acc
	}
	for iter.Next() {
		sel := iter.Selector()
		child := iter.Value()
		fieldPath := append(append([]string{}, pathPrefix...), sel.String())

		if !schema.Allows(sel) {
			acc = cueerrors.Append(acc, &fieldNotAllowedError{pos: child.Pos(), path: fieldPath})
			continue
		}

		if child.IncompleteKind() == cue.StructKind {
			childSchema := schema.LookupPath(cue.MakePath(sel))
			if !childSchema.Exists() {
				continue
			}
			acc = walkDisallowed(childSchema, child, fieldPath, acc)
		}
	}
	return acc
}

type fieldNotAllowedError struct {
	pos  token.Pos
	path []string
}

func (e *fieldNotAllowedError) Position() token.Pos         { return e.pos }
func (e *fieldNotAllowedError) InputPositions() []token.Pos { return nil }
func (e *fieldNotAllowedError) Error() string               { return fieldNotAllowed }
func (e *fieldNotAllowedError) Path() []string {
	return append([]string{"values"}, normalizeFieldPath(e.path)...)
}
func (e *fieldNotAllowedError) Msg() (msg string, args []any) {
	return fieldNotAllowed, nil
}

func normalizeFieldPath(path []string) []string {
	if len(path) == 0 {
		return nil
	}
	joined := strings.Join(path, ".")
	joined = strings.TrimPrefix(joined, "#module.#config.")
	joined = strings.TrimPrefix(joined, "#module.#config")
	joined = strings.TrimPrefix(joined, "#config.")
	joined = strings.TrimPrefix(joined, "#config")
	joined = strings.TrimPrefix(joined, ".")
	if joined == "" {
		return nil
	}
	return strings.Split(joined, ".")
}
