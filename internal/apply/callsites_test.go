package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// moduleRoot is the repository root, seen from this package.
	moduleRoot = "../.."

	clientPkg = "sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteMethods are the method names by which a Kubernetes client deletes
// cluster objects: one object or a collection.
var deleteMethods = map[string]bool{
	"Delete": true, "DeleteAllOf": true, "DeleteCollection": true, "DeleteAll": true,
}

// deleteCall is one call that deletes a cluster object.
type deleteCall struct {
	File   string // path relative to the repository root
	Line   int
	Method string
}

// listedPackage is the part of `go list -json` this test reads.
type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
	DepOnly    bool
}

// findDeleteCalls type-checks the non-test files of the packages the
// patterns name and returns every use of a method that deletes a cluster
// object, called or taken as a value. A use counts by types, never by the
// method name alone: the receiver is a controller-runtime client (it
// implements client.Writer, as a type that embeds one does), or the method
// takes a client.Object (an interface narrowed to the delete), or it belongs
// to a client-go client or to the Flux resource manager.
func findDeleteCalls(t *testing.T, patterns ...string) []deleteCall {
	t.Helper()
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}

	args := append([]string{"list", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,Export,DepOnly"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}

	exports := map[string]string{}
	var targets []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		if pkg.Export != "" {
			exports[pkg.ImportPath] = pkg.Export
		}
		if !pkg.DepOnly {
			targets = append(targets, pkg)
		}
	}
	if len(targets) == 0 {
		t.Fatalf("go list %v named no package", patterns)
	}

	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
	clientLib, err := imp.Import(clientPkg)
	if err != nil {
		t.Fatalf("importing %s: %v", clientPkg, err)
	}
	writer, ok := clientLib.Scope().Lookup("Writer").Type().Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("%s.Writer is no interface", clientPkg)
	}

	clientObject := clientLib.Scope().Lookup("Object").Type()

	var calls []deleteCall
	for _, pkg := range targets {
		files := make([]*ast.File, 0, len(pkg.GoFiles))
		for _, name := range pkg.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
		if _, err := (&types.Config{Importer: imp}).Check(pkg.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-checking %s: %v", pkg.ImportPath, err)
		}
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				// Every selector of a delete method counts, called or not:
				// a method value (del := c.Delete) deletes as a call does.
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || !deleteMethods[sel.Sel.Name] {
					return true
				}
				// A package-level function, such as conditions.Delete, is
				// no selection: it has no receiver.
				selection := info.Selections[sel]
				if selection == nil || !deletesClusterObjects(selection, writer, clientObject) {
					return true
				}
				pos := fset.Position(sel.Sel.Pos())
				rel, err := filepath.Rel(root, pos.Filename)
				if err != nil {
					t.Fatal(err)
				}
				calls = append(calls, deleteCall{File: filepath.ToSlash(rel), Line: pos.Line, Method: sel.Sel.Name})
				return true
			})
		}
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].File != calls[j].File {
			return calls[i].File < calls[j].File
		}
		return calls[i].Line < calls[j].Line
	})
	return calls
}

// deletesClusterObjects reports whether a selected method is a delete of a
// Kubernetes client: its receiver is a controller-runtime client, or it takes
// a client.Object, as the same method does on an interface narrowed to it, or
// it belongs to a client-go client or the Flux resource manager.
func deletesClusterObjects(selection *types.Selection, writer *types.Interface, clientObject types.Type) bool {
	recv := selection.Recv()
	if types.Implements(recv, writer) || types.Implements(types.NewPointer(recv), writer) {
		return true
	}
	if sig, ok := selection.Obj().Type().(*types.Signature); ok {
		for param := range sig.Params().Variables() {
			if types.Identical(param.Type(), clientObject) {
				return true
			}
		}
	}
	pkg := selection.Obj().Pkg()
	if pkg == nil {
		return false
	}
	return strings.HasPrefix(pkg.Path(), "k8s.io/client-go/") || pkg.Path() == "github.com/fluxcd/pkg/ssa"
}

// The list of places that may delete a cluster object is closed: the prune,
// which asks the library's delete verdict first, and the resource manager's
// delete guard. A delete anywhere else would go out without a verdict or a
// precondition (0012:D4:R1).
func TestDeleteCallSitesAreClosed(t *testing.T) {
	allowed := map[string]bool{
		"internal/apply/prune.go":  true,
		"internal/apply/claims.go": true,
	}
	calls := findDeleteCalls(t, "./internal/...", "./cmd/...")

	seen := map[string]bool{}
	for _, call := range calls {
		seen[call.File] = true
		if !allowed[call.File] {
			t.Errorf("%s:%d calls %s on a Kubernetes client: only %v may delete a cluster object",
				call.File, call.Line, call.Method, []string{"internal/apply/prune.go", "internal/apply/claims.go"})
		}
	}
	for file := range allowed {
		if !seen[file] {
			t.Errorf("%s holds no delete call: the matcher no longer finds the calls it must find", file)
		}
	}
}

// The matcher finds a delete of each form a client offers, and counts no
// method of the same name on another type.
func TestDeleteCallMatcher(t *testing.T) {
	calls := findDeleteCalls(t, "./internal/apply/testdata/deletecalls")

	got := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.File != "internal/apply/testdata/deletecalls/calls.go" {
			t.Fatalf("unexpected file %s", call.File)
		}
		got = append(got, call.Method)
	}
	want := []string{
		"Delete", "DeleteAllOf", "Delete", "Delete", "DeleteAllOf", // controller-runtime, in source order
		"Delete", "DeleteCollection", "Delete", // client-go typed and dynamic
		"Delete", // the Flux resource manager
		"Delete", // an interface narrowed to the delete
		"Delete", // a method value
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matched %v, want %v", got, want)
	}
}
