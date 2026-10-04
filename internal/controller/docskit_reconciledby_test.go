/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestDocsKitReconciledByNamesTheControllers keeps the resource reference's
// "Served by" line true: docs-kit.cue's reconciledBy states, per kind, the
// Named(...) of the controller whose builder calls For(&<pkg>.<Kind>{}). The
// docs bundle cannot read Go, so a renamed controller fails here until
// docs-kit.cue follows it.
func TestDocsKitReconciledByNamesTheControllers(t *testing.T) {
	stated := readReconciledBy(t, filepath.Join("..", "..", "docs-kit.cue"))
	scanned, unnamed, err := scanControllerNames(".")
	if err != nil {
		t.Fatalf("scan internal/controller: %v", err)
	}
	if len(stated) == 0 {
		t.Fatal("docs-kit.cue: the crd source has no reconciledBy entries")
	}

	kinds := map[string]bool{}
	for k := range stated {
		kinds[k] = true
	}
	for k := range scanned {
		kinds[k] = true
	}
	sorted := make([]string, 0, len(kinds))
	for k := range kinds {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, kind := range sorted {
		want, inCode := scanned[kind]
		got, inCue := stated[kind]
		switch {
		case !inCue:
			t.Errorf("%s: the %q controller reconciles it, but docs-kit.cue's reconciledBy has no entry", kind, want)
		case !inCode && unnamed[kind]:
			t.Errorf("%s: docs-kit.cue's reconciledBy names %q, but the controller for %s has no literal Named(...)", kind, got, kind)
		case !inCode:
			t.Errorf("%s: docs-kit.cue's reconciledBy names %q, but no controller in internal/controller calls For(&%s{})", kind, got, kind)
		case got != want:
			t.Errorf("%s: docs-kit.cue's reconciledBy names %q, the controller is Named(%q)", kind, got, want)
		}
	}
}

// readReconciledBy returns the reconciledBy map of the crd source of the
// opm-operator bundle in docs-kit.cue.
func readReconciledBy(t *testing.T, path string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	v := cuecontext.New().CompileBytes(src, cue.Filename(path))
	if err := v.Err(); err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	iter, err := v.LookupPath(cue.ParsePath(`bundles."opm-operator".sources`)).List()
	if err != nil {
		t.Fatalf("%s: bundles.\"opm-operator\".sources: %v", path, err)
	}
	for iter.Next() {
		src := iter.Value()
		if kind, _ := src.LookupPath(cue.ParsePath("kind")).String(); kind != "crd" {
			continue
		}
		out := map[string]string{}
		if err := src.LookupPath(cue.ParsePath("reconciledBy")).Decode(&out); err != nil {
			t.Fatalf("%s: the crd source's reconciledBy: %v", path, err)
		}
		return out
	}
	t.Fatalf("%s: the opm-operator bundle has no crd source", path)
	return nil
}

// scanControllerNames maps each kind to the name of the controller that
// reconciles it: a function in dir whose builder chain holds exactly one
// For(&<pkg>.<Kind>{...}) and one Named("<name>"). unnamed holds the kinds
// whose builder calls For once but Named only with a non-literal argument, so
// the test can say why the kind has no scanned name.
func scanControllerNames(dir string) (out map[string]string, unnamed map[string]bool, err error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, nil, err
	}
	out = map[string]string{}
	unnamed = map[string]bool{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			kinds, names, nonLiteral := builderCalls(fn.Body)
			switch {
			case len(kinds) == 1 && len(names) == 1 && nonLiteral == 0:
				out[kinds[0]] = names[0]
			case len(kinds) == 1 && len(names) == 0 && nonLiteral > 0:
				unnamed[kinds[0]] = true
			}
		}
	}
	return out, unnamed, nil
}

// builderCalls collects the For kinds and the literal Named names in body, and
// counts the Named calls whose argument is not a string literal.
func builderCalls(body *ast.BlockStmt) (kinds, names []string, nonLiteral int) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "For":
			if u, ok := call.Args[0].(*ast.UnaryExpr); ok && u.Op == token.AND {
				if lit, ok := u.X.(*ast.CompositeLit); ok {
					if typ, ok := lit.Type.(*ast.SelectorExpr); ok {
						kinds = append(kinds, typ.Sel.Name)
					}
				}
			}
		case "Named":
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				nonLiteral++
				break
			}
			if s, err := strconv.Unquote(lit.Value); err == nil {
				names = append(names, s)
			}
		}
		return true
	})
	return kinds, names, nonLiteral
}
