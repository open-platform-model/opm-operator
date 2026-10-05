package inventory

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPackage_ExportsExactlyItsSurface pins this package to the conversion
// boundary: the Current alias and the four conversions between the API entry
// and the library's inventory.Entry. An identity relation, a stale set or a
// digest belongs to the library's opm/k8s/inventory (0012:D7), so any other
// export, under any name, fails here (0012:D3:R6). Methods count as
// Type.Method.
func TestPackage_ExportsExactlyItsSurface(t *testing.T) {
	files, err := os.ReadDir(".")
	require.NoError(t, err)
	fset := token.NewFileSet()
	var got []string
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		got = append(got, exportedNames(file)...)
	}
	slices.Sort(got)
	assert.Equal(t, []string{"Current", "FromEntries", "FromEntry", "ToEntries", "ToEntry"}, got)
}

func exportedNames(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv == nil {
				out = append(out, d.Name.Name)
				continue
			}
			out = append(out, receiverName(d.Recv.List[0].Type)+"."+d.Name.Name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiverName(e.X)
	default:
		return "?"
	}
}
