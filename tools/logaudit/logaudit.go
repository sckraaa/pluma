// Package logaudit finds logging that would put key material, login data or
// message content into the logs (audit findings #8, #9, #25; ADR 0003). Its
// test runs over the repository, so `go test ./...` fails when such logging
// comes back.
//
// It works on the syntax tree alone, without type information, so that it
// runs in a second. Each call of a logger (logx and friends) or of an error
// constructor (fmt.Errorf and friends: errors end up in logs) is checked:
//
//   - secret: an argument whose value is named like a secret: new_nonce, the
//     DH exponent a, g_b, auth_key bytes, AES keys and IVs, decrypted
//     handshake data, login codes and their hashes, passwords, tokens, or a
//     config section with credentials (Etcd, Mysql, Redis...);
//   - object: a TL object or data object printed whole: a parameter or local
//     of a type from the mtproto package or from app/ (requests, updates,
//     messages, rows), with any verb other than %T, or its String() or
//     DebugString();
//   - dump: hex or base64 of raw bytes (packets, keys);
//   - body: a format string that prints a request, reply, payload or value
//     with %v, %s or %q.
//
// A finding that is not a leak is marked on the call's line, or on the line
// above it, with a comment
//
//	// logaudit:ok <reason>
//
// The reason is required.
package logaudit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Finding is a call that may log something it must not.
type Finding struct {
	Pos  token.Position
	Rule string
	Msg  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", f.Pos.Filename, f.Pos.Line, f.Rule, f.Msg)
}

// logMethods are the methods of logx (package functions and logx.Logger) and
// of the standard log package.
var logMethods = set(
	"Debug", "Debugf", "Debugv", "Debugw",
	"Info", "Infof", "Infov", "Infow",
	"Error", "Errorf", "Errorv", "Errorw",
	"Slow", "Slowf", "Slowv", "Sloww",
	"Stat", "Statf", "Severe", "Severef", "Alert",
	"Print", "Printf", "Println",
	"Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln",
)

// errorFuncs build errors that callers log: package.function.
var errorFuncs = set("fmt.Errorf", "errors.Errorf", "errors.Wrapf", "errors.Wrap", "status.Errorf")

// notLoggers are package names whose Print*/Error* functions do not log.
var notLoggers = set("fmt", "errors", "status", "strings", "strconv", "bytes", "t", "b", "f", "tb")

// secretNames are value names (lower case, without '_') that hold secrets.
var secretNames = set(
	"newnonce", "getnewnonce",
	"gb", "getgb",
	"authkey", "getauthkey", "authkeydata", "keydata",
	"aeskey", "aesiv", "tmpaeskey", "tmpaesiv", "tmpaeskeyandiv", "keyandiv",
	"decrypted", "decrypteddata", "innerdata", "paddeddatawithhash", "answerwithhash",
	"phonecode", "getphonecode", "phonecodestring", "getphonecodestring",
	"phonecodeflagstring", "getphonecodeflagstring",
	"phonecodehash", "getphonecodehash", "codehash", "getcodehash",
	"logincode", "code", "hmackey",
	"password", "getpassword", "newpassword", "getnewpassword",
	"secret", "getsecret", "apnssecret",
	"token", "gettoken",
	"pass", "dsn",
	// config sections that carry credentials, printed whole
	"etcd", "mysql", "redis", "kv", "cache", "minio", "kafka", "rpcserverconf", "rpcclientconf",
)

// passThrough are functions whose result shows their arguments.
var passThrough = set(
	"string", "String", "Sprintf", "Sprint", "Sprintln",
	"EncodeToString", "Dump", "HexDump", "Bytes", "ToUpper", "ToLower", "TrimSpace",
)

// dumpFuncs encode raw bytes: package.function, or function for methods
// (base64.StdEncoding.EncodeToString).
var dumpFuncs = set("hex.EncodeToString", "hex.Dump", "hex2.HexDump", "EncodeToString", "spew.Sdump", "spew.Dump")

// objectPackages are import paths whose types are TL objects, requests or
// rows. Path prefixes end with '/'.
var objectPackages = []string{
	"github.com/teamgram/proto/mtproto",
	"github.com/teamgram/proto/mtproto/crypto",
	"github.com/teamgram/teamgram-server/app/",
}

// localObjectTypes are types, used unqualified in their own package, that
// hold handshake secrets or auth keys.
var localObjectTypes = set("HandshakeStateCtx", "reqDHParamsResult", "dhGenKey", "authKeyUtil", "handshakeEntry", "AuthKey", "AuthKeyInfo")

// safeTypes are types of objectPackages that hold no user data.
var safeTypes = set("RpcMetadata", "PeerUtil", "InputPeer", "Peer", "InputUser", "InputDialogPeer", "Bool", "Int32", "Int64", "Vector_Long", "Storage_FileType")

// bodyFormat matches a format string printing a whole request or payload.
var bodyFormat = regexp.MustCompile(`(?i)\b(request|reply|req|resp|response|payload|body|value|data|query|message|msg)s?\s*[:=(]\s*[{(\[]?\s*%[-+# 0-9.]*[vsq]`)

var okComment = regexp.MustCompile(`logaudit:ok\s+\S`)

// Check checks the Go files under dirs (relative to root), skipping tests,
// testdata and examples, and the paths in skip.
func Check(root string, dirs []string, skip []string) ([]Finding, error) {
	fset := token.NewFileSet()
	var findings []Finding
	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, s := range skip {
				if rel == s || strings.HasPrefix(rel, s+"/") {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() {
				switch d.Name() {
				case "testdata", "examples", "example":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path) //nolint:gosec // G122: reads the developer's own source tree
			if err != nil {
				return err
			}
			found, err := CheckFile(fset, rel, src)
			if err != nil {
				return err
			}
			findings = append(findings, found...)
			return nil
		})
		if err != nil {
			return nil, err
		}
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

// CheckFile checks one file.
func CheckFile(fset *token.FileSet, name string, src []byte) ([]Finding, error) {
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	c := &checker{fset: fset, imports: map[string]string{}, ok: map[int]bool{}}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		n := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			n = imp.Name.Name
		}
		c.imports[n] = p
	}
	for _, cg := range f.Comments {
		for _, cm := range cg.List {
			if okComment.MatchString(cm.Text) {
				c.ok[fset.Position(cm.Pos()).Line] = true
			}
		}
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			c.function(fd.Recv, fd.Type, fd.Body, nil)
		}
	}
	return c.findings, nil
}

type checker struct {
	fset     *token.FileSet
	imports  map[string]string // package name -> import path
	ok       map[int]bool      // lines with a logaudit:ok comment
	findings []Finding
}

// function checks the calls in a function body. objects are the names of
// parameters and locals holding TL objects, inherited by closures.
func (c *checker) function(recv *ast.FieldList, typ *ast.FuncType, body *ast.BlockStmt, outer map[string]bool) {
	objects := map[string]bool{}
	for k := range outer {
		objects[k] = true
	}
	for _, fl := range []*ast.FieldList{recv, typ.Params} {
		if fl == nil {
			continue
		}
		for _, field := range fl.List {
			if c.isObjectType(field.Type) {
				for _, n := range field.Names {
					objects[n.Name] = true
				}
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			c.function(nil, n.Type, n.Body, objects)
			return false
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, rhs := range n.Rhs {
					if id, ok := n.Lhs[i].(*ast.Ident); ok && c.isObjectExpr(rhs) {
						objects[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			if n.Type != nil && c.isObjectType(n.Type) {
				for _, id := range n.Names {
					objects[id.Name] = true
				}
			}
		case *ast.CallExpr:
			c.call(n, objects)
		}
		return true
	})
}

// isObjectType reports a (pointer to a) named type from objectPackages.
func (c *checker) isObjectType(t ast.Expr) bool {
	switch e := t.(type) {
	case *ast.StarExpr:
		return c.isObjectType(e.X)
	case *ast.ArrayType:
		return c.isObjectType(e.Elt)
	case *ast.MapType:
		return c.isObjectType(e.Value)
	case *ast.Ellipsis:
		return false // variadic ...interface{} of loggers
	}
	if id, ok := t.(*ast.Ident); ok {
		return localObjectTypes[id.Name]
	}
	sel, ok := t.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || safeTypes[sel.Sel.Name] {
		return false
	}
	path := c.imports[pkg.Name]
	for _, p := range objectPackages {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}

// isObjectExpr reports an expression building a TL object: &pkg.T{...},
// pkg.T{...}, pkg.MakeTLX(...) and its To_X() conversions.
func (c *checker) isObjectExpr(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.UnaryExpr:
		return e.Op == token.AND && c.isObjectExpr(e.X)
	case *ast.CompositeLit:
		return e.Type != nil && c.isObjectType(e.Type)
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if strings.HasPrefix(sel.Sel.Name, "MakeTL") {
				return true
			}
			if strings.HasPrefix(sel.Sel.Name, "To_") {
				return c.isObjectExpr(sel.X)
			}
		}
	}
	return false
}

// call checks one call if it logs or builds an error.
func (c *checker) call(call *ast.CallExpr, objects map[string]bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	name := sel.Sel.Name
	pkg := ""
	if id, ok := sel.X.(*ast.Ident); ok {
		pkg = id.Name
	}
	switch {
	case errorFuncs[pkg+"."+name]:
	case logMethods[name] && !notLoggers[pkg]:
	default:
		return
	}

	line := c.fset.Position(call.Pos()).Line
	if c.ok[line] || c.ok[line-1] {
		return
	}

	args := call.Args
	if (name == "Wrap" || name == "Wrapf") && len(args) > 0 {
		args = args[1:] // the wrapped error
	}
	format, verbs := "", []rune(nil)
	if len(args) > 0 && strings.HasSuffix(name, "f") {
		if lit, ok := args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			format, _ = strconv.Unquote(lit.Value)
			verbs = formatVerbs(format)
			args = args[1:]
		}
	}

	for _, loc := range bodyFormat.FindAllStringIndex(format, -1) {
		// the argument of the verb that ends the match
		if i := len(formatVerbs(format[:loc[1]])) - 1; i >= 0 && i < len(args) && opaqueValue(args[i]) {
			c.report(call, "body", fmt.Sprintf("format %q prints %s whole: log ids and sizes", format, exprString(args[i])))
		}
	}
	for i, arg := range args {
		verb := 'v'
		if format != "" {
			if i >= len(verbs) {
				continue
			}
			verb = verbs[i]
		}
		if verb == 'T' || verb == 'p' {
			continue
		}
		c.arg(call, arg, objects)
	}
}

// arg checks one argument of a logging call (any verb but %T and %p).
func (c *checker) arg(call *ast.CallExpr, arg ast.Expr, objects map[string]bool) {
	src := exprString(arg)
	ast.Inspect(arg, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ce, ok := n.(*ast.CallExpr); ok {
			if fn := funcName(ce.Fun); dumpFuncs[fn] || dumpFuncs[lastPart(fn)] {
				c.report(call, "dump", fmt.Sprintf("%s logs raw bytes: log their length", src))
				return false
			}
		}
		return true
	})
	for _, v := range valueNames(arg) {
		norm := strings.ToLower(strings.ReplaceAll(v, "_", ""))
		if v == "A" || (secretNames[norm] && v != "Code" && v != "GetCode") {
			c.report(call, "secret", fmt.Sprintf("%s is a secret", src))
			return
		}
	}
	if c.wholeObject(arg, objects) {
		c.report(call, "object", fmt.Sprintf("%s prints a whole TL object or row: log ids, sizes or %%T", src))
	}
}

// opaqueValue reports a plain variable or field, whose type the syntax tree
// does not tell; errors (err, ErrX) and calls (len, reflect.TypeOf, getters)
// are not.
func opaqueValue(e ast.Expr) bool {
	var name string
	switch e := e.(type) {
	case *ast.Ident:
		name = e.Name
	case *ast.SelectorExpr:
		name = e.Sel.Name
	default:
		return false
	}
	lower := strings.ToLower(name)
	return !strings.HasPrefix(lower, "err") && !strings.HasSuffix(lower, "err") && lower != "e"
}

// wholeObject reports an argument that is a TL object, its String() or its
// DebugString().
func (c *checker) wholeObject(arg ast.Expr, objects map[string]bool) bool {
	for {
		switch e := arg.(type) {
		case *ast.ParenExpr:
			arg = e.X
			continue
		case *ast.StarExpr:
			arg = e.X
			continue
		case *ast.UnaryExpr:
			arg = e.X
			continue
		case *ast.CallExpr:
			sel, ok := e.Fun.(*ast.SelectorExpr)
			if !ok || len(e.Args) != 0 {
				return false
			}
			if sel.Sel.Name == "DebugString" {
				return true
			}
			if sel.Sel.Name == "String" {
				arg = sel.X
				continue
			}
			return false
		case *ast.Ident:
			return objects[e.Name]
		case *ast.CompositeLit:
			return c.isObjectExpr(e)
		}
		return false
	}
}

func (c *checker) report(call *ast.CallExpr, rule, msg string) {
	c.findings = append(c.findings, Finding{Pos: c.fset.Position(call.Pos()), Rule: rule, Msg: msg})
}

// valueNames returns the names of the values an expression shows: x.y.Z
// shows Z, x.GetZ() shows GetZ, hex.EncodeToString(k[:8]) shows k.
func valueNames(e ast.Expr) []string {
	switch e := e.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		return []string{e.Sel.Name}
	case *ast.ParenExpr:
		return valueNames(e.X)
	case *ast.StarExpr:
		return valueNames(e.X)
	case *ast.UnaryExpr:
		return valueNames(e.X)
	case *ast.IndexExpr:
		return valueNames(e.X)
	case *ast.SliceExpr:
		return valueNames(e.X)
	case *ast.TypeAssertExpr:
		return valueNames(e.X)
	case *ast.BinaryExpr:
		return append(valueNames(e.X), valueNames(e.Y)...)
	case *ast.CallExpr:
		fn := lastPart(funcName(e.Fun))
		if fn == "len" || fn == "cap" {
			return nil
		}
		if passThrough[fn] || isConversion(e.Fun) {
			var names []string
			for _, a := range e.Args {
				names = append(names, valueNames(a)...)
			}
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && fn == "String" && len(e.Args) == 0 {
				names = append(names, valueNames(sel.X)...)
			}
			return names
		}
		return []string{fn}
	}
	return nil
}

func isConversion(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.ArrayType:
		return true
	case *ast.Ident:
		return f.Name == "string"
	}
	return false
}

// funcName returns pkg.F, x.M or F for a call's function.
func funcName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	}
	return ""
}

func lastPart(s string) string {
	return s[strings.LastIndex(s, ".")+1:]
}

// formatVerbs returns the verb of each argument of a printf format.
func formatVerbs(format string) []rune {
	var verbs []rune
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.[]*", format[i]) >= 0 {
			if format[i] == '*' {
				verbs = append(verbs, 'd')
			}
			i++
		}
		if i < len(format) && format[i] != '%' {
			verbs = append(verbs, rune(format[i]))
		}
	}
	return verbs
}

func exprString(e ast.Expr) string {
	return typeString(e)
}

// typeString renders short expressions for messages.
func typeString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return typeString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return typeString(e.Fun) + "(…)"
	case *ast.StarExpr:
		return "*" + typeString(e.X)
	case *ast.UnaryExpr:
		return e.Op.String() + typeString(e.X)
	case *ast.IndexExpr:
		return typeString(e.X) + "[…]"
	case *ast.SliceExpr:
		return typeString(e.X) + "[…:…]"
	case *ast.ParenExpr:
		return "(" + typeString(e.X) + ")"
	}
	return fmt.Sprintf("%T", e)
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}
