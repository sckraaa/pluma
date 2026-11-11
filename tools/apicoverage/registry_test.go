package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const repoRoot = "../.."

var (
	coverageRow = regexp.MustCompile("(?m)^\\| `([a-z]+\\.[A-Za-z0-9_]+)` \\| (\\w+) \\|")
	versionTail = regexp.MustCompile(`[0-9A-F]{6,8}$`)
	methodName  = regexp.MustCompile(`[a-z]+\.[A-Za-z0-9_]+`)
	schemaField = regexp.MustCompile(`(?m)^func \(m \*(TL\w+)\) Set(\w+)\(`)
)

// TestEveryGapHasADecision keeps nothing from being forgotten: a method that
// is not done has an entry in docs/project/feature-registry.json saying
// whether it is to be done, and why.
func TestEveryGapHasADecision(t *testing.T) {
	coverage, err := os.ReadFile(filepath.Join(repoRoot, "docs/project/api-coverage.md"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs/project/feature-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		Name     string `json:"name"`
		Decision string `json:"decision"`
		Why      string `json:"why"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, it := range items {
		if it.Why == "" {
			t.Errorf("%s: a decision without a reason", it.Name)
		}
		switch it.Decision {
		case "implement", "not_needed", "unclear":
		default:
			t.Errorf("%s: unknown decision %q", it.Name, it.Decision)
		}
		for _, n := range methodName.FindAllString(it.Name, -1) {
			known[versionTail.ReplaceAllString(n, "")] = true
		}
	}
	for _, m := range coverageRow.FindAllStringSubmatch(string(coverage), -1) {
		if name := versionTail.ReplaceAllString(m[1], ""); m[2] != "ok" && !known[name] {
			t.Errorf("%s is %s and has no decision in docs/project/feature-registry.json", name, m[2])
		}
	}
}

// schemaEntries lists every field of every constructor and method of the
// vendored schema, by the setters of the generated code.
func schemaEntries(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "third_party/teamgram-proto/mtproto/codec_schema.tl.pb.go"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range schemaField.FindAllStringSubmatch(string(data), -1) {
		seen[m[1]+"."+m[2]] = true
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// TestSchemaHasNoUnreviewedEntries fails when a new layer brings a type or a
// field that nobody looked at: the method matrix cannot see fields. Review
// the new entries, add what they need to the registry, then rewrite
// docs/project/schema-baseline.txt (UPDATE_SCHEMA_BASELINE=1 go test).
func TestSchemaHasNoUnreviewedEntries(t *testing.T) {
	entries := schemaEntries(t)
	if len(entries) < 1000 {
		t.Fatalf("only %d schema entries found: the generated code changed its comments", len(entries))
	}
	path := filepath.Join(repoRoot, "docs/project/schema-baseline.txt")
	if os.Getenv("UPDATE_SCHEMA_BASELINE") != "" {
		if err := os.WriteFile(path, []byte(strings.Join(entries, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		known[l] = true
	}
	for _, e := range entries {
		if !known[e] {
			t.Errorf("not reviewed: %s", e)
		}
	}
}
