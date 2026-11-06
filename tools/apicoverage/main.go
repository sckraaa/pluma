// Command apicoverage builds the API coverage matrix: every client-facing RPC
// method of the current TL layer (from github.com/teamgram/proto) against the
// handlers implemented under app/.
//
// Usage (from the repo root):
//
//	go run ./tools/apicoverage            # writes docs/project/api-coverage.md
//	go run ./tools/apicoverage -summary   # prints the summary only
//
// It also writes docs/project/api-coverage.json, the public progress the site
// shows (pluma.chat/progress): counts by area, nothing else.
//
// Statuses:
//
//	ok       handler exists and is not a stub
//	partial  real handler with some branches returning ErrEnterpriseIsBlocked/ErrMethodNotImpl
//	blocked  stub returning ErrEnterpriseIsBlocked (enterprise-only upstream)
//	notimpl  stub returning ErrMethodNotImpl or marked "TODO: not impl"
//	fake     no handler; the bff proxy answers a canned success
//	         (app/bff/bff/client/fake_rpc_result.go)
//	missing  no handler at all
//
// A canned answer for a type name that is no method of the layer never
// fires: the tool fails on one.
package main

import (
	"bufio"
	"encoding/json"
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

var statuses = []string{"ok", "partial", "blocked", "notimpl", "fake", "missing"}

// fakeFile holds the canned answers of the bff proxy, by Go type name.
const fakeFile = "app/bff/bff/client/fake_rpc_result.go"

type method struct {
	TLName string // messages.getHistory
	GoName string // MessagesGetHistory
	NS     string // messages
	Status string
	File   string // repo-relative file:line
	// Scenario: a conformance scenario calls it (test/conformance/testdata).
	Scenario bool
}

func main() {
	out := flag.String("out", "docs/project/api-coverage.md", "output markdown file")
	summaryOnly := flag.Bool("summary", false, "print summary to stdout, write nothing")
	jsonOut := flag.String("json", "docs/project/api-coverage.json", "output JSON file of the public progress")
	flag.Parse()

	dir, version, err := protoModuleInfo()
	check(err)
	methods, layer, err := loadMethods(dir, version)
	check(err)
	handlers, err := loadHandlers("app")
	check(err)

	fakes, err := loadFakes(fakeFile)
	check(err)
	check(unknownFakes(fakes, methods))

	scenarios, err := loadScenarioCalls(filepath.Join("test", "conformance", "testdata"))
	check(err)
	for _, m := range methods {
		if h, ok := handlers[m.GoName]; ok {
			m.Status, m.File = h.status, h.file
		} else if line, ok := fakes["TL"+m.GoName]; ok {
			m.Status, m.File = "fake", fmt.Sprintf("%s:%d", fakeFile, line)
		} else {
			m.Status = "missing"
		}
		m.Scenario = scenarios[m.TLName]
	}

	if *summaryOnly {
		fmt.Print(renderSummary(methods, layer))
		return
	}
	// A document checked into the repository, not a secret: the usual
	// permissions of checked-in files.
	check(os.MkdirAll(filepath.Dir(*out), 0o755))                    //nolint:gosec // G301: a repository directory
	check(os.WriteFile(*out, []byte(render(methods, layer)), 0o644)) //nolint:gosec // G306: a checked-in document
	progress, err := renderProgress(methods, layer)
	check(err)
	check(os.WriteFile(*jsonOut, progress, 0o644)) //nolint:gosec // G306: a checked-in document, served publicly
	fmt.Print(renderSummary(methods, layer))
	fmt.Printf("written %s, %s\n", *out, *jsonOut)
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
		b, err := os.ReadFile(path) //nolint:gosec // G122: reads the developer's own source tree
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
	// Upstream enterprise stubs either return ErrEnterpriseIsBlocked or log the
	// "License key ... required" message and return an empty but valid result.
	// The marker is matched in body: stripComments cuts the URL at "//".
	licensed := strings.Contains(body, licenseStubMarker)
	blocked := strings.Contains(code, "ErrEnterpriseIsBlocked") || licensed
	notimpl := strings.Contains(code, "ErrMethodNotImpl") || strings.Contains(body, "TODO: not impl")
	switch {
	case n <= stubMaxLines && blocked:
		return "blocked"
	case n <= stubMaxLines && notimpl:
		return "notimpl"
	case n <= emptyResultStubMaxLines && licensed:
		return "blocked"
	case n <= emptyResultStubMaxLines && strings.Contains(body, "TODO: not impl"):
		return "notimpl"
	case blocked || notimpl:
		return "partial"
	default:
		return "ok"
	}
}

// licenseStubMarker is logged by upstream handlers that are enterprise-only.
const licenseStubMarker = "License key from https://teamgram.net required"

// emptyResultStubMaxLines bounds stubs that build an empty result object
// (a constructor call with a few empty fields) instead of returning an error.
const emptyResultStubMaxLines = 14

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

// loadScenarioCalls returns the methods the conformance fixtures call
// (task 007): the "call" of every step.
func loadScenarioCalls(dir string) (map[string]bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	calls := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var fx struct {
			Steps []struct {
				Call string `json:"call"`
			} `json:"steps"`
		}
		if err = json.Unmarshal(b, &fx); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, st := range fx.Steps {
			calls[st.Call] = true
		}
	}
	return calls, nil
}

var fakeRe = regexp.MustCompile(`^\s*case "(TL\w+)":`)

// loadFakes returns the Go type names the bff proxy has a canned answer for,
// with their lines.
func loadFakes(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	res := map[string]int{}
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if m := fakeRe.FindStringSubmatch(sc.Text()); m != nil {
			res[m[1]] = line
		}
	}
	return res, sc.Err()
}

// unknownFakes reports the canned answers whose type name is no method of
// the layer: the proxy matches the request's type name, so they never fire.
func unknownFakes(fakes map[string]int, ms []*method) error {
	known := map[string]bool{}
	for _, m := range ms {
		known["TL"+m.GoName] = true
	}
	var bad []string
	for name, line := range fakes {
		if !known[name] {
			bad = append(bad, fmt.Sprintf("%s:%d %s", fakeFile, line, name))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("canned answers for no method of the layer: %s", strings.Join(bad, ", "))
}

func counts(ms []*method) map[string]int {
	c := map[string]int{}
	for _, m := range ms {
		c[m.Status]++
		if m.Scenario {
			c["scenario"]++
		}
	}
	return c
}

func renderSummary(ms []*method, layer string) string {
	c := counts(ms)
	var b strings.Builder
	fmt.Fprintf(&b, "layer %s: %d methods — ok %d, partial %d, blocked %d, notimpl %d, fake %d, missing %d (ok = %.1f%%); with a conformance scenario %d\n",
		layer, len(ms), c["ok"], c["partial"], c["blocked"], c["notimpl"], c["fake"], c["missing"], 100*float64(c["ok"])/float64(len(ms)), c["scenario"])
	return b.String()
}

func render(ms []*method, layer string) string {
	var b strings.Builder
	b.WriteString("# API coverage matrix\n\n")
	b.WriteString("> Generated by `go run ./tools/apicoverage`. Do not edit by hand.\n")
	b.WriteString("> Statuses: `ok` implemented, `partial` some branches blocked/not implemented, `blocked` enterprise stub, `notimpl` not-implemented stub, `fake` no handler, a canned success of the bff proxy, `missing` no handler.\n")
	b.WriteString("> `ok` means a handler exists, not that it is conformant: conformance is tracked by tests.\n")
	b.WriteString("> `scenario`: a conformance scenario (test/conformance, ADR 0018) compares the method with Telegram.\n\n")
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

	b.WriteString("| namespace | total | ok | partial | blocked | notimpl | fake | missing | scenario |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, ns := range nss {
		c := counts(byNS[ns])
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d |\n", ns, len(byNS[ns]), c["ok"], c["partial"], c["blocked"], c["notimpl"], c["fake"], c["missing"], c["scenario"])
	}

	b.WriteString("\n## Methods\n")
	for _, ns := range nss {
		fmt.Fprintf(&b, "\n### %s\n\n| method | status | scenario | handler |\n|---|---|---|---|\n", ns)
		for _, m := range byNS[ns] {
			sc := ""
			if m.Scenario {
				sc = "yes"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", m.TLName, m.Status, sc, m.File)
		}
	}
	return b.String()
}

// areas group the methods of the API into the parts of the messenger the
// public progress names: by namespace, and the methods of messages.* that
// serve another part go to it by name (the stickers and emoji a user sees
// are messages.getStickerSet and the like, stickers.* only makes sets; the
// folders are messages.*DialogFilter*). A method of no area counts under
// "other".
var areas = []struct {
	key     string
	ns      []string
	methods *regexp.Regexp // of TL names, before the namespaces
}{
	{"messages", []string{"messages"}, nil},
	{"channels", []string{"channels"}, nil},
	{"contacts", []string{"contacts", "users"}, nil},
	{"account", []string{"account", "auth"}, nil},
	{"media", []string{"photos", "upload"}, nil},
	{"stickers", []string{"stickers"}, regexp.MustCompile(`^messages\.\w*(Sticker|CustomEmoji|EmojiGroups|EmojiKeywords|EmojiURL)`)},
	{"calls", []string{"phone"}, nil},
	{"stories", []string{"stories"}, nil},
	{"folders", []string{"folders", "chatlists", "communities"}, regexp.MustCompile(`^messages\.\w*DialogFilter`)},
	{"bots", []string{"bots"}, regexp.MustCompile(`^messages\.\w*(Bot|InlineQuery|WebView|WebApp|AttachMenu)`)},
	{"payments", []string{"payments", "premium", "fragment"}, nil},
}

// constructorSuffix ends the name of a method the schema has in more than
// one version (messages.getDialogFiltersEFD48C89 and …F19ED96D): the
// public progress counts it once.
var constructorSuffix = regexp.MustCompile(`[0-9A-F]{8}$`)

// statusRank orders the statuses from the best: a method of several
// versions takes the best of them.
var statusRank = map[string]int{"ok": 0, "partial": 1, "fake": 2, "notimpl": 3, "blocked": 4, "missing": 5}

// areaOf is the area of a method by its TL name, "other" for none.
func areaOf(tl, ns string) string {
	for _, a := range areas {
		if a.methods != nil && a.methods.MatchString(tl) {
			return a.key
		}
	}
	for _, a := range areas {
		for _, n := range a.ns {
			if n == ns {
				return a.key
			}
		}
	}
	return "other"
}

type progressCounts struct {
	Total   int `json:"total"`
	Ok      int `json:"ok"`
	Partial int `json:"partial"`
}

type progressArea struct {
	Key string `json:"key"`
	progressCounts
}

// renderProgress is the public progress: the methods of the layer, how many
// are implemented and partly implemented, overall and by area.
func renderProgress(ms []*method, layer string) ([]byte, error) {
	// One entry per method name, of the best status of its versions.
	byName := map[string]*method{}
	var names []string
	for _, m := range ms {
		name := m.TLName
		if strings.ContainsAny(name[max(0, len(name)-8):], "0123456789") {
			name = constructorSuffix.ReplaceAllString(name, "")
		}
		if old, ok := byName[name]; !ok {
			byName[name] = &method{TLName: name, NS: m.NS, Status: m.Status}
			names = append(names, name)
		} else if statusRank[m.Status] < statusRank[old.Status] {
			old.Status = m.Status
		}
	}
	add := func(c *progressCounts, m *method) {
		c.Total++
		switch m.Status {
		case "ok":
			c.Ok++
		case "partial":
			c.Partial++
		}
	}
	var all progressCounts
	byArea := map[string]*progressCounts{}
	for _, name := range names {
		m := byName[name]
		key := areaOf(m.TLName, m.NS)
		if byArea[key] == nil {
			byArea[key] = &progressCounts{}
		}
		add(byArea[key], m)
		add(&all, m)
	}
	out := struct {
		Layer string `json:"layer"`
		progressCounts
		Areas []progressArea `json:"areas"`
	}{Layer: layer, progressCounts: all}
	for _, a := range areas {
		if c := byArea[a.key]; c != nil {
			out.Areas = append(out.Areas, progressArea{Key: a.key, progressCounts: *c})
		}
	}
	if c := byArea["other"]; c != nil {
		out.Areas = append(out.Areas, progressArea{Key: "other", progressCounts: *c})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "apicoverage:", err)
		os.Exit(1)
	}
}
