package render

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

// FinalizeValue converts a CUE value to finalized form by extracting syntax,
// converting to AST, and rebuilding without constraints.
func FinalizeValue(cueCtx *cue.Context, v cue.Value) (cue.Value, error) {
	syntaxNode := v.Syntax(cue.Final())

	expr, ok := syntaxNode.(ast.Expr)
	if !ok {
		return cue.Value{}, fmt.Errorf(
			"finalization produced %T instead of ast.Expr; "+
				"value likely contains unresolved imports or definition fields",
			syntaxNode,
		)
	}

	dataVal := cueCtx.BuildExpr(expr)
	if err := dataVal.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("building finalized value: %w", err)
	}
	return dataVal, nil
}
