// Command apicoverage builds the API coverage matrix: every client-facing RPC
// method of the current TL layer (from github.com/teamgram/proto) against the
// handlers implemented under app/.
//
// Usage (from the repo root):
//
//	go run ./tools/apicoverage            # writes docs/project/api-coverage.md
//	go run ./tools/apicoverage -summary   # prints the summary only
//
// Statuses:
//
//	ok       handler exists and is not a stub
//	partial  real handler with some branches returning ErrEnterpriseIsBlocked/ErrMethodNotImpl
//	blocked  stub returning ErrEnterpriseIsBlocked (enterprise-only upstream)
//	notimpl  stub returning ErrMethodNotImpl or marked "TODO: not impl"
//	missing  no handler at all
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const protoModule = "github.com/teamgram/proto"

var statuses = []string{"ok", "partial", "blocked", "notimpl", "missing"}

type method struct {
	TLName string // messages.getHistory
	GoName string // MessagesGetHistory
	NS     string // messages
	Status string
	File   string // repo-relative file:line
}

func main() {
	out := flag.String("out", "docs/project/api-coverage.md", "output markdown file")
	summaryOnly := flag.Bool("summary", false, "print summary to stdout, write nothing")
	flag.Parse()

	dir, version, err := protoModuleInfo()
	check(err)
	methods, layer, err := loadMethods(dir, version)
	check(err)
	handlers, err := loadHandlers("app")
	check(err)

	for _, m := range methods {
		if h, ok := handlers[m.GoName]; ok {
			m.Status, m.File = h.status, h.file
		} else {
			m.Status = "missing"
		}
	}

	if *summaryOnly {
		fmt.Print(renderSummary(methods, layer))
		return
	}
	check(os.MkdirAll(filepath.Dir(*out), 0o755))
	check(os.WriteFile(*out, []byte(render(methods, layer)), 0o644))
	fmt.Print(renderSummary(methods, layer))
	fmt.Printf("written %s\n", *out)
}

// protoModuleInfo returns the source directory of the proto module (the
// third_party copy when replaced) and its required version, which encodes the
// layer (v0.228.0 -> 228).
func protoModuleInfo() (dir, version string, err error) {
	b, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}} {{.Version}}", protoModule).Output()
	if err != nil {
		return "", "", fmt.Errorf("go list %s: %w (run `go mod download`)", protoModule, err)
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 {
		return "", "", fmt.Errorf("go list %s: unexpected output %q", protoModule, b)
	}
	return fields[0], fields[1], nil
}

var (
	rpcRe     = regexp.MustCompile(`^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)
	serviceRe = regexp.MustCompile(`^service\s+(\w+)`)
	layerRe   = regexp.MustCompile(`^v0\.(\d+)\.`)
)

// loadMethods reads the client-facing RPC list from schema.tl.sync_service.proto.
func loadMethods(dir, version string) ([]*method, string, error) {
	f, err := os.Open(filepath.Join(dir, "mtproto", "schema.tl.sync_service.proto"))
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	layer := "?"
	if m := layerRe.FindStringSubmatch(version); m != nil {
		layer = m[1]
	}

	seen := map[string]bool{}
	var methods []*method
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if serviceRe.MatchString(line) {
			continue
		}
		m := rpcRe.FindStringSubmatch(line)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		raw := m[1]
		ns, name := "", raw
		if i := strings.Index(raw, "_"); i > 0 {
			ns, name = raw[:i], raw[i+1:]
		}
		tl := name
		if ns != "" {
			tl = ns + "." + name
		}
		methods = append(methods, &method{TLName: tl, GoName: goName(raw), NS: nsOrRoot(ns)})
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].TLName < methods[j].TLName })
	return methods, layer, sc.Err()
}

func nsOrRoot(ns string) string {
	if ns == "" {
		return "(root)"
	}
	return ns
}

// goName converts messages_getHistory to MessagesGetHistory, matching the
// handler method names generated for the bff cores.
func goName(raw string) string {
	var b strings.Builder
	for _, part := range strings.Split(raw, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

type handler struct {
	status string
	file   string
}

var funcRe = regexp.MustCompile(`^func \(\w+ \*\w+\) (\w+)\(`)

// loadHandlers scans app/ for core handler methods and classifies their bodies.
func loadHandlers(root string) (map[string]handler, error) {
	res := map[string]handler{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if !strings.Contains(path, string(filepath.Separator)+"core"+string(filepath.Separator)) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		for i := 0; i < len(lines); i++ {
			m := funcRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			start := i
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "func ") {
					end = j
					break
				}
			}
			body := strings.Join(lines[start:end], "\n")
			h := handler{status: classify(body), file: fmt.Sprintf("%s:%d", path, start+1)}
			if prev, ok := res[m[1]]; !ok || rank(h.status) < rank(prev.status) {
				res[m[1]] = h
			}
		}
		return nil
	})
	return res, err
}

// stubMaxLines is the max number of code lines (signature, logging, return,
// closing brace) a handler body may have to still count as a pure stub.
const stubMaxLines = 6

func classify(body string) string {
	code := stripComments(body)
	n := 0
	for _, l := range strings.Split(code, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	blocked := strings.Contains(code, "ErrEnterpriseIsBlocked")
	notimpl := strings.Contains(code, "ErrMethodNotImpl") || strings.Contains(body, "TODO: not impl")
	switch {
	case n <= stubMaxLines && blocked:
		return "blocked"
	case n <= stubMaxLines && notimpl:
		return "notimpl"
	case blocked || strings.Contains(code, "ErrMethodNotImpl"):
		return "partial"
	default:
		return "ok"
	}
}

func stripComments(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if i := strings.Index(l, "//"); i >= 0 {
			l = l[:i]
		}
		b.WriteString(l + "\n")
	}
	return b.String()
}

func rank(s string) int {
	for i, x := range statuses {
		if x == s {
			return i
		}
	}
	return len(statuses)
}

func counts(ms []*method) map[string]int {
	c := map[string]int{}
	for _, m := range ms {
		c[m.Status]++
	}
	return c
}

func renderSummary(ms []*method, layer string) string {
	c := counts(ms)
	var b strings.Builder
	fmt.Fprintf(&b, "layer %s: %d methods — ok %d, partial %d, blocked %d, notimpl %d, missing %d (ok = %.1f%%)\n",
		layer, len(ms), c["ok"], c["partial"], c["blocked"], c["notimpl"], c["missing"], 100*float64(c["ok"])/float64(len(ms)))
	return b.String()
}

func render(ms []*method, layer string) string {
	var b strings.Builder
	b.WriteString("# API coverage matrix\n\n")
	b.WriteString("> Generated by `go run ./tools/apicoverage`. Do not edit by hand.\n")
	b.WriteString("> Statuses: `ok` implemented, `partial` some branches blocked/not implemented, `blocked` enterprise stub, `notimpl` not-implemented stub, `missing` no handler.\n")
	b.WriteString("> `ok` means a handler exists, not that it is conformant: conformance is tracked by tests.\n\n")
	b.WriteString("## Summary\n\n")
	b.WriteString("    " + renderSummary(ms, layer) + "\n")

	byNS := map[string][]*method{}
	var nss []string
	for _, m := range ms {
		if _, ok := byNS[m.NS]; !ok {
			nss = append(nss, m.NS)
		}
		byNS[m.NS] = append(byNS[m.NS], m)
	}
	sort.Strings(nss)

	b.WriteString("| namespace | total | ok | partial | blocked | notimpl | missing |\n|---|---|---|---|---|---|---|\n")
	for _, ns := range nss {
		c := counts(byNS[ns])
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d |\n", ns, len(byNS[ns]), c["ok"], c["partial"], c["blocked"], c["notimpl"], c["missing"])
	}

	b.WriteString("\n## Methods\n")
	for _, ns := range nss {
		fmt.Fprintf(&b, "\n### %s\n\n| method | status | handler |\n|---|---|---|\n", ns)
		for _, m := range byNS[ns] {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", m.TLName, m.Status, m.File)
		}
	}
	return b.String()
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "apicoverage:", err)
		os.Exit(1)
	}
}
