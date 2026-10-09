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
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/open-platform-model/library/opm/k8s/labels"
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

// writeMethods are the method names by which a Kubernetes client or the Flux
// resource manager creates or changes cluster objects.
var writeMethods = map[string]bool{
	"Create": true, "Update": true, "Patch": true, "Apply": true, "ApplyAll": true, "ApplyAllStaged": true,
}

// writeHelpers are the package-level functions of controller-runtime that
// write an object through a client they are handed.
var writeHelpers = map[string]bool{"CreateOrUpdate": true, "CreateOrPatch": true}

const controllerutilPkg = "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

// deleteCall is one call that deletes a cluster object.
type deleteCall = clientCall

// clientCall is one use of a method that deletes or writes a cluster object.
type clientCall struct {
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
	return findClientCalls(t, deleteMethods, nil, patterns...)
}

// findWriteCalls is findDeleteCalls for the methods that create or change a
// cluster object (writeMethods), and for the controllerutil helpers that do
// so through a client (writeHelpers).
func findWriteCalls(t *testing.T, patterns ...string) []clientCall {
	t.Helper()
	return findClientCalls(t, writeMethods, writeHelpers, patterns...)
}

// findClientCalls returns every use, in the non-test files of the packages
// the patterns name, of a method in methods on a Kubernetes client (see
// findDeleteCalls for what counts as one), and every use of a controllerutil
// function in helpers.
func findClientCalls(t *testing.T, methods, helpers map[string]bool, patterns ...string) []clientCall {
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

	var calls []clientCall
	for _, pkg := range targets {
		files := make([]*ast.File, 0, len(pkg.GoFiles))
		for _, name := range pkg.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		info := &types.Info{
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Uses:       map[*ast.Ident]types.Object{},
		}
		if _, err := (&types.Config{Importer: imp}).Check(pkg.ImportPath, fset, files, info); err != nil {
			t.Fatalf("type-checking %s: %v", pkg.ImportPath, err)
		}
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				// Every selector of a delete method counts, called or not:
				// a method value (del := c.Delete) deletes as a call does.
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				// A package-level function, such as conditions.Delete, is
				// no selection: it has no receiver. Only the listed
				// controllerutil helpers count among those.
				selection := info.Selections[sel]
				switch {
				case selection != nil:
					if !methods[sel.Sel.Name] || !deletesClusterObjects(selection, writer, clientObject) {
						return true
					}
				case helpers[sel.Sel.Name]:
					fn, isFunc := info.Uses[sel.Sel].(*types.Func)
					if !isFunc || fn.Pkg() == nil || fn.Pkg().Path() != controllerutilPkg {
						return true
					}
				default:
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

// The list of places that may delete a cluster object is closed: the deletion
// plan runner, which deletes only what the library's plan names, the prune,
// which asks the library's delete verdict first, and the resource manager's
// delete guard. A delete anywhere else would go out without a verdict or a
// precondition (0012:D4:R1).
func TestDeleteCallSitesAreClosed(t *testing.T) {
	// The number of deletes each file may hold: the runner's one DELETE, the
	// prune's one, and the guard's two (with and without a UID
	// precondition). A further delete in a listed file needs its own verdict
	// or precondition, and this count changed with it.
	allowed := map[string]int{
		"internal/apply/deletion.go": 1,
		"internal/apply/prune.go":    1,
		"internal/apply/claims.go":   2,
	}
	calls := findDeleteCalls(t, "./internal/...", "./cmd/...")

	seen := map[string]int{}
	for _, call := range calls {
		seen[call.File]++
		if _, ok := allowed[call.File]; !ok {
			t.Errorf("%s:%d calls %s on a Kubernetes client: only %v may delete a cluster object",
				call.File, call.Line, call.Method, slices.Sorted(maps.Keys(allowed)))
		}
	}
	for file, want := range allowed {
		if seen[file] != want {
			t.Errorf("%s holds %d delete call(s), want %d", file, seen[file], want)
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

// The list of places that may create or change a cluster object is closed
// (0012:D4:R2). Objects of an instance are written by one call, the staged
// apply, which the reconcilers reach only after the library's apply verdict.
// Every other listed call writes no instance object: a dry run, or a status
// or finalizer patch of one of the operator's own kinds.
func TestWriteCallSitesAreClosed(t *testing.T) {
	// The number of writes each file may hold. A further write in a listed
	// file is either behind the apply verdict or no write of an instance
	// object, and this count changed with it.
	allowed := map[string]int{
		// The staged apply of an apply list.
		"internal/apply/apply.go": 1,
		// The dry run of the claim check: it changes nothing.
		"internal/apply/claims.go": 1,
		// The dry run of the taken-in check: it changes nothing.
		"internal/apply/takein.go": 1,
		// Status and finalizer patches of the operator's own kinds.
		"internal/reconcile/moduleinstance.go":                      10,
		"internal/reconcile/modulepackage.go":                       5,
		"internal/controller/platform_controller.go":                1,
		"internal/controller/transformerregistration_controller.go": 1,
		"internal/controller/transformerregistration_dependents.go": 1,
	}
	calls := findWriteCalls(t, "./internal/...", "./cmd/...", "./api/...")

	seen := map[string]int{}
	for _, call := range calls {
		seen[call.File]++
		if _, ok := allowed[call.File]; !ok {
			t.Errorf("%s:%d calls %s on a Kubernetes client: the file is not in the closed list of places that write a cluster object",
				call.File, call.Line, call.Method)
		}
	}
	for file, want := range allowed {
		if seen[file] != want {
			t.Errorf("%s holds %d write call(s), want %d", file, seen[file], want)
		}
	}
}

// The matcher finds a write of each form a client and the resource manager
// offer, and counts no method of the same name on another type.
func TestWriteCallMatcher(t *testing.T) {
	calls := findWriteCalls(t, "./internal/apply/testdata/writecalls")

	got := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.File != "internal/apply/testdata/writecalls/calls.go" {
			t.Fatalf("unexpected file %s", call.File)
		}
		got = append(got, call.Method)
	}
	want := []string{
		"Create", "Update", "Patch", "Apply", // controller-runtime client, in source order
		"Update", "Patch", "Create", // the status and subresource writers
		"Patch",                              // a type that embeds a client
		"Create", "Update", "Patch", "Apply", // client-go typed
		"Create", "Apply", // client-go dynamic
		"Apply", "ApplyAll", "ApplyAllStaged", // the Flux resource manager
		"Patch",                           // the Flux status patcher
		"Patch",                           // an interface narrowed to the patch
		"Update",                          // a method value
		"CreateOrUpdate", "CreateOrPatch", // the controllerutil helpers
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matched %v, want %v", got, want)
	}
}

// The operator never names the adopt annotation: it is read only inside the
// library's verdicts, so no code of the operator can set, change or remove
// it (0012:D8:R6).
func TestNoFileNamesTheAdoptAnnotation(t *testing.T) {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd", "api"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					if node.Sel.Name == "AnnotationAdopt" {
						t.Errorf("%s names the adopt annotation key", fset.Position(node.Pos()))
					}
				case *ast.BasicLit:
					if node.Kind == token.STRING && strings.Contains(node.Value, labels.AnnotationAdopt) {
						t.Errorf("%s holds the adopt annotation key as a literal", fset.Position(node.Pos()))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
