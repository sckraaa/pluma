// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Fixture is what a scenario got from a server, normalized.
type Fixture struct {
	Scenario string `json:"scenario"`
	// Source says where the fixture was recorded: server, layer, date.
	Source string `json:"source,omitempty"`
	Steps  []Step `json:"steps"`
}

// Step is one call of a scenario.
type Step struct {
	Call   string `json:"call"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error is an RPC error, its number of a FLOOD_WAIT_X and the like
// replaced by X.
type Error struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

var trailingNumber = regexp.MustCompile(`_\d+$`)

// NewError returns the normalized error of code and message.
func NewError(code int32, message string) *Error {
	return &Error{Code: code, Message: trailingNumber.ReplaceAllString(message, "_X")}
}

// FixturePath is where the fixture of a scenario lives.
func FixturePath(scenario string) string {
	return filepath.Join("testdata", scenario+".json")
}

// ReadFixture reads the fixture of a scenario.
func ReadFixture(scenario string) (*Fixture, error) {
	b, err := os.ReadFile(FixturePath(scenario))
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err = json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", FixturePath(scenario), err)
	}
	return &f, nil
}

// WriteFixture writes f to path, indented and with sorted keys, so that a
// new recording shows as a readable diff. The path is testdata or the
// directory the tester gives in PLUMA_CONFORMANCE_OUT.
func WriteFixture(path string, f *Fixture) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // G703: the tester's own directory
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600) //nolint:gosec // G703: the tester's own directory
}

// Rules say how a scenario compares.
type Rules struct {
	// Loose are paths whose values may differ between servers (a config, a
	// list of countries): they must exist and have the same kind. A path
	// is keys joined by dots, [] for any list item, * for any key, matched
	// against "steps[N].result..." without the step prefix.
	Loose []string
	// Deviations are the known differences of our server, each with the
	// decision that made it: paths that are not compared at all.
	Deviations []Deviation
}

// Deviation is a deliberate difference from Telegram.
type Deviation struct {
	Step   int    // index of the step
	Path   string // as in Loose; "" for the whole step
	Reason string // the ADR or task
}

// Compare returns the differences of got from want, one line each.
func Compare(want, got *Fixture, r Rules) []string {
	var diffs []string
	if len(want.Steps) != len(got.Steps) {
		diffs = append(diffs, fmt.Sprintf("%d steps, want %d", len(got.Steps), len(want.Steps)))
	}
	for i := 0; i < len(want.Steps) && i < len(got.Steps); i++ {
		w, g := want.Steps[i], got.Steps[i]
		prefix := fmt.Sprintf("step %d (%s)", i, w.Call)
		if r.deviates(i, "") {
			continue
		}
		if w.Call != g.Call {
			diffs = append(diffs, fmt.Sprintf("%s: call %s", prefix, g.Call))
			continue
		}
		switch {
		case w.Error != nil || g.Error != nil:
			if w.Error == nil || g.Error == nil || *w.Error != *g.Error {
				diffs = append(diffs, fmt.Sprintf("%s: error %s, want %s", prefix, errString(g.Error), errString(w.Error)))
			}
		default:
			compareTree(&diffs, r, i, "", w.Result, g.Result)
		}
	}
	return diffs
}

func errString(e *Error) string {
	if e == nil {
		return "none"
	}
	return fmt.Sprintf("%d %s", e.Code, e.Message)
}

func (r Rules) deviates(step int, path string) bool {
	for _, d := range r.Deviations {
		if d.Step == step && (d.Path == "" || matchPath(d.Path, path)) {
			return true
		}
	}
	return false
}

func (r Rules) loose(path string) bool {
	for _, p := range r.Loose {
		if matchPath(p, path) {
			return true
		}
	}
	return false
}

// matchPath matches path against pattern; a pattern also matches every
// path under it.
func matchPath(pattern, path string) bool {
	ps, xs := strings.Split(pattern, "."), strings.Split(path, ".")
	if len(xs) < len(ps) {
		return false
	}
	for i, p := range ps {
		if p != "*" && p != xs[i] {
			return false
		}
	}
	return true
}

func compareTree(diffs *[]string, r Rules, step int, path string, want, got any) {
	at := func() string {
		if path == "" {
			return fmt.Sprintf("step %d", step)
		}
		return fmt.Sprintf("step %d: %s", step, path)
	}
	if r.deviates(step, path) {
		return
	}
	if r.loose(path) {
		if kind(want) != kind(got) {
			*diffs = append(*diffs, fmt.Sprintf("%s: %s, want %s", at(), kind(got), kind(want)))
		}
		return
	}
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: %s, want an object", at(), kind(got)))
			return
		}
		if w["_"] != g["_"] {
			*diffs = append(*diffs, fmt.Sprintf("%s: %v, want %v", at(), g["_"], w["_"]))
			return
		}
		for _, k := range keys(w, g) {
			sub := k
			if path != "" {
				sub = path + "." + k
			}
			wv, inW := w[k]
			gv, inG := g[k]
			switch {
			case k == "_":
			case !inG:
				if !r.deviates(step, sub) && !r.loose(sub) {
					*diffs = append(*diffs, fmt.Sprintf("step %d: %s is missing", step, sub))
				}
			case !inW:
				if !r.deviates(step, sub) && !r.loose(sub) {
					*diffs = append(*diffs, fmt.Sprintf("step %d: %s is extra (%v)", step, sub, short(gv)))
				}
			default:
				compareTree(diffs, r, step, sub, wv, gv)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: %s, want a list", at(), kind(got)))
			return
		}
		if len(w) != len(g) {
			*diffs = append(*diffs, fmt.Sprintf("%s: %d items, want %d", at(), len(g), len(w)))
			return
		}
		sub := "[]"
		if path != "" {
			sub = path + ".[]"
		}
		for i := range w {
			compareTree(diffs, r, step, sub, w[i], g[i])
		}
	default:
		if fmt.Sprint(want) != fmt.Sprint(got) {
			*diffs = append(*diffs, fmt.Sprintf("%s: %v, want %v", at(), short(got), short(want)))
		}
	}
}

func keys(a, b map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]any{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func kind(v any) string {
	switch t := v.(type) {
	case map[string]any:
		// Constructors of one type (boolTrue, boolFalse) are one kind.
		return "object"
	case []any:
		return "list"
	case string:
		if strings.HasPrefix(t, "$") {
			return "label"
		}
		return "string"
	case bool:
		return "bool"
	case nil:
		return "nothing"
	}
	return "number"
}

func short(v any) string {
	s := fmt.Sprint(v)
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}

// Foreign returns where f holds something that is not the account's own:
// a user other than self, a chat, a message text without prefix. A fixture
// recorded from a real account must have none of it, because other
// activity of the account may get into the recording. The values are not
// in the result.
func Foreign(f *Fixture, prefix string) []string {
	var out []string
	for i, s := range f.Steps {
		foreign(&out, fmt.Sprintf("step %d", i), "", s.Result, prefix)
	}
	return out
}

func foreign(out *[]string, at, key string, t any, prefix string) {
	switch v := t.(type) {
	case map[string]any:
		for _, k := range keys(v, nil) {
			foreign(out, at+"."+k, k, v[k], prefix)
		}
	case []any:
		for i, x := range v {
			sub := fmt.Sprintf("%s[%d]", at, i)
			m, _ := x.(map[string]any)
			if p, ok := m["_"].(string); ok {
				sub += "(" + p + ")" // the type only, not a value
			}
			switch {
			case key == "users" && m["self"] != true:
				*out = append(*out, sub+": a user other than self")
			case key == "chats":
				*out = append(*out, sub+": a chat")
			default:
				foreign(out, sub, key, x, prefix)
			}
		}
	case string:
		if key == "message" && !strings.HasPrefix(v, prefix) {
			*out = append(*out, at+": a text not of the scenario")
		}
	}
}
