// Package boxaudit checks the code that runs inside box transactions
// (pkg/userbox, ADR 0011, p. 14). Its test runs over the repository, so
// `go test ./...` fails when such code breaks the rules of a transaction.
//
// Box code is every function literal passed to userbox.Run or to a RunBox
// method, and every function with a *userbox.Box parameter. In it:
//
//   - discarded: no error is dropped: no call that returns an error as a
//     statement of its own, and no error assigned to _. A dropped failure of
//     a statement would let the transaction go on (the recorder of userbox
//     stops it anyway, but the code must not rely on that);
//   - pool: no method of a *DAO type without the Tx suffix: it runs on the
//     pool, outside the transaction, does not see its writes and takes a
//     second connection;
//   - network: no call to an RPC client (a type named *Client), the cache
//     (CachedConn), Redis, Kafka (Producer, Publisher) or idgen
//     (IdGenerator): side effects go to Box.Emit and Box.InvalidateCache and
//     happen after commit;
//   - nested: no userbox.Run, userbox.Read, RunBox or ReadState: one box
//     transaction at a time, and its state is Box.State.
//
// Writers adds a rule for the whole repository, box code or not:
//
//   - writer: no SQL that writes user_state, user_pts_updates or
//     user_seq_updates outside pkg/userbox, or mq_outbox outside pkg/userbox
//     and pkg/outbox, in Go strings or in DAO table definitions.
//
// A finding that is not a violation is marked on its line, or on the line
// above it, with a comment
//
//	// boxaudit:ok <reason>
//
// The reason is required.
//
// The checker needs types: it type-checks the packages that import
// pkg/userbox against the export data `go list -export` builds.
package boxaudit

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
	"regexp"
	"sort"
	"strings"
)

// UserboxPath is the import path of the box transaction package.
const UserboxPath = "github.com/teamgram/teamgram-server/pkg/userbox"

// Finding is a call in box code that breaks a rule.
type Finding struct {
	Pos  token.Position
	Rule string
	Msg  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.Pos.Filename, f.Pos.Line, f.Rule, f.Msg)
}

var okComment = regexp.MustCompile(`boxaudit:ok\s+\S`)

// listedPackage is what `go list -json` says about a package.
type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	CgoFiles   []string
	Export     string
	Imports    []string
	ImportMap  map[string]string
	DepOnly    bool
	Error      *struct{ Err string }
}

// Check type-checks the packages matching patterns (run in dir) that import
// pkg/userbox, other than pkg/userbox itself, and checks their box code.
func Check(dir string, patterns ...string) ([]Finding, error) {
	args := append([]string{"list", "-e", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,CgoFiles,Export,Imports,ImportMap,DepOnly,Error"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, stderr.Bytes())
	}

	exports := map[string]string{}
	var targets []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err = dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if p.Error != nil {
			return nil, fmt.Errorf("go list %s: %s", p.ImportPath, p.Error.Err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly && p.ImportPath != UserboxPath && contains(p.Imports, UserboxPath) {
			targets = append(targets, p)
		}
	}

	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var findings []Finding
	for _, p := range targets {
		found, err := checkPackage(fset, p, exports, root)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i].Pos, findings[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		return a.Line < b.Line
	})
	return findings, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func checkPackage(fset *token.FileSet, p listedPackage, exports map[string]string, root string) ([]Finding, error) {
	var files []*ast.File
	for _, name := range append(p.GoFiles, p.CgoFiles...) {
		f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	lookup := func(path string) (io.ReadCloser, error) {
		if mapped, ok := p.ImportMap[path]; ok {
			path = mapped
		}
		export := exports[path]
		if export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(export)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	if _, err := conf.Check(p.ImportPath, fset, files, info); err != nil {
		return nil, fmt.Errorf("type-check %s: %w", p.ImportPath, err)
	}

	c := &checker{fset: fset, info: info, ok: map[string]bool{}, root: root}
	for _, f := range files {
		for _, cg := range f.Comments {
			for _, cm := range cg.List {
				if okComment.MatchString(cm.Text) {
					pos := fset.Position(cm.Pos())
					c.ok[fmt.Sprintf("%s:%d", pos.Filename, pos.Line)] = true
				}
			}
		}
		c.file(f)
	}
	return c.findings, nil
}

type checker struct {
	fset     *token.FileSet
	info     *types.Info
	ok       map[string]bool // file:line with a boxaudit:ok comment
	root     string
	findings []Finding
}

// file finds the box code of a file and checks it.
func (c *checker) file(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Body != nil && c.hasBoxParam(n.Type) {
				c.box(n.Body)
				return false
			}
		case *ast.FuncLit:
			if c.hasBoxParam(n.Type) {
				c.box(n.Body)
				return false
			}
		case *ast.CallExpr:
			if c.isRun(n) {
				for _, arg := range n.Args {
					if lit, ok := arg.(*ast.FuncLit); ok {
						c.box(lit.Body)
					}
				}
				return false
			}
		}
		return true
	})
}

// hasBoxParam tells whether a function has a *userbox.Box parameter.
func (c *checker) hasBoxParam(ft *ast.FuncType) bool {
	for _, field := range ft.Params.List {
		if isBox(c.info.Types[field.Type].Type) {
			return true
		}
	}
	return false
}

func isBox(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "Box" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == UserboxPath
}

// isRun tells whether a call starts a box transaction: userbox.Run or a
// method named RunBox.
func (c *checker) isRun(call *ast.CallExpr) bool {
	fn := c.callee(call)
	if fn == nil {
		return false
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == UserboxPath && fn.Name() == "Run" {
		return true
	}
	return fn.Name() == "RunBox" && isMethod(fn)
}

func isMethod(fn *types.Func) bool {
	return fn.Type().(*types.Signature).Recv() != nil
}

// callee returns the function or method a call calls, if static.
func (c *checker) callee(call *ast.CallExpr) *types.Func {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		fn, _ := c.info.Uses[fun].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if sel, ok := c.info.Selections[fun]; ok {
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		fn, _ := c.info.Uses[fun.Sel].(*types.Func)
		return fn
	}
	return nil
}

// box checks box code.
func (c *checker) box(body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ExprStmt:
			if call, ok := ast.Unparen(n.X).(*ast.CallExpr); ok && c.returnsError(call) >= 0 {
				c.report(n, "discarded", "the error of "+c.name(call)+" is dropped")
			}
		case *ast.AssignStmt:
			c.assign(n)
		case *ast.CallExpr:
			c.call(n)
		}
		return true
	})
}

// assign reports errors assigned to _.
func (c *checker) assign(n *ast.AssignStmt) {
	if len(n.Rhs) == 1 && len(n.Lhs) > 1 {
		call, ok := ast.Unparen(n.Rhs[0]).(*ast.CallExpr)
		if !ok {
			return
		}
		if i := c.returnsError(call); i >= 0 && i < len(n.Lhs) && isBlank(n.Lhs[i]) {
			c.report(n, "discarded", "the error of "+c.name(call)+" is assigned to _")
		}
		return
	}
	for i, rhs := range n.Rhs {
		call, ok := ast.Unparen(rhs).(*ast.CallExpr)
		if ok && i < len(n.Lhs) && isBlank(n.Lhs[i]) && c.returnsError(call) >= 0 {
			c.report(n, "discarded", "the error of "+c.name(call)+" is assigned to _")
		}
	}
}

func isBlank(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

var errorType = types.Universe.Lookup("error").Type()

// returnsError returns the index of the error result of a call, or -1.
func (c *checker) returnsError(call *ast.CallExpr) int {
	t := c.info.Types[call].Type
	switch t := t.(type) {
	case *types.Tuple:
		for i := range t.Len() {
			if types.Identical(t.At(i).Type(), errorType) {
				return i
			}
		}
	case nil:
	default:
		if types.Identical(t, errorType) {
			return 0
		}
	}
	return -1
}

// network types: the services, stores and queues box code must not call.
var networkTypes = regexp.MustCompile(`(Client2?|CachedConn|Redis|Producer|Publisher|IdGenerator)$`)

// call reports calls to the pool, the network and nested transactions.
func (c *checker) call(call *ast.CallExpr) {
	fn := c.callee(call)
	if fn == nil {
		return
	}
	if fn.Pkg() != nil && fn.Pkg().Path() == UserboxPath && !isMethod(fn) {
		switch fn.Name() {
		case "Run", "Read", "Check", "CheckJournal", "Publish":
			c.report(call, "nested", "userbox."+fn.Name()+" inside a box transaction")
		}
		return
	}
	if !isMethod(fn) {
		return
	}
	switch fn.Name() {
	case "RunBox", "ReadState":
		c.report(call, "nested", fn.Name()+" inside a box transaction")
		return
	}
	recv := receiverName(fn)
	switch {
	case strings.HasSuffix(recv, "DAO") && !strings.HasSuffix(fn.Name(), "Tx"):
		c.report(call, "pool", recv+"."+fn.Name()+" runs on the pool; use its Tx variant or b.Tx()")
	case networkTypes.MatchString(recv):
		c.report(call, "network", recv+"."+fn.Name()+" inside a box transaction; emit an event instead")
	}
}

// receiverName is the name of the type that declares a method.
func receiverName(fn *types.Func) string {
	t := fn.Type().(*types.Signature).Recv().Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

func (c *checker) name(call *ast.CallExpr) string {
	if fn := c.callee(call); fn != nil {
		if isMethod(fn) {
			return receiverName(fn) + "." + fn.Name()
		}
		return fn.Name()
	}
	return "the call"
}

func (c *checker) report(n ast.Node, rule, msg string) {
	pos := c.fset.Position(n.Pos())
	if c.ok[fmt.Sprintf("%s:%d", pos.Filename, pos.Line)] || c.ok[fmt.Sprintf("%s:%d", pos.Filename, pos.Line-1)] {
		return
	}
	if rel, err := filepath.Rel(c.root, pos.Filename); err == nil {
		pos.Filename = filepath.ToSlash(rel)
	}
	c.findings = append(c.findings, Finding{Pos: pos, Rule: rule, Msg: msg})
}
