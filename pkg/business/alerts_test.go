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

package business

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const businessAlertRulesFile = "../../teamgramd/deploy/prometheus/server/rules/business.yml"

// businessMetricFiles declare the metrics of Telegram Business: the worker
// of the automatic replies (msg), the updates queued for connected bots
// (inbox) and the chat links resolved (bff). autoreply.go names the kinds
// (AutoReplyKind.String).
var businessMetricFiles = []string{
	"../../app/messenger/msg/msg/internal/core/business_autoreply_worker.go",
	"../../app/messenger/msg/inbox/internal/core/inbox.botBusinessUpdateToInbox_handler.go",
	"../../app/bff/business/internal/core/account.resolveBusinessChatLink_handler.go",
	"autoreply.go",
}

// businessMetrics reads the business_* metrics of businessMetricFiles as
// Prometheus names them, with their labels, and the string literals of the
// files, among which are the values of the labels.
func businessMetrics(t *testing.T) (map[string]map[string]bool, map[string]bool) {
	t.Helper()
	metrics := map[string]map[string]bool{}
	values := map[string]bool{}
	unquote := func(e ast.Expr) string {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, path := range businessMetricFiles {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if s := unquote(n); s != "" {
					values[s] = true
				}
			case *ast.CompositeLit:
				sel, ok := n.Type.(*ast.SelectorExpr)
				if !ok || !strings.HasSuffix(sel.Sel.Name, "VecOpts") {
					return true
				}
				var parts []string
				labels := map[string]bool{}
				for _, el := range n.Elts {
					kv := el.(*ast.KeyValueExpr)
					switch kv.Key.(*ast.Ident).Name {
					case "Namespace", "Subsystem", "Name":
						if s := unquote(kv.Value); s != "" {
							parts = append(parts, s)
						}
					case "Labels":
						for _, l := range kv.Value.(*ast.CompositeLit).Elts {
							labels[unquote(l)] = true
						}
					}
				}
				if name := strings.Join(parts, "_"); strings.HasPrefix(name, "business_") {
					metrics[name] = labels
				}
			}
			return true
		})
	}
	if len(metrics) != 3 {
		t.Fatalf("the business metrics found: %v, want 3", metrics)
	}
	return metrics, values
}

var (
	businessSelectorRe = regexp.MustCompile(`\b(business_[a-z_]+)\b(\{[^}]*\})?`)
	businessMatcherRe  = regexp.MustCompile(`([a-z_]+)\s*(=~|!~|!=|=)\s*"([^"]*)"`)
)

// TestAlertRules keeps the alerts of Telegram Business (docs/ops/
// business.md) on the metrics the services publish: a query of a metric,
// a label or a value the code does not have is silently empty, and its
// alert never fires.
func TestAlertRules(t *testing.T) {
	metrics, values := businessMetrics(t)
	data, err := os.ReadFile(businessAlertRulesFile)
	if err != nil {
		t.Fatal(err)
	}
	var rules struct {
		Groups []struct {
			Rules []struct {
				Alert string `yaml:"alert"`
				Expr  string `yaml:"expr"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(data, &rules); err != nil {
		t.Fatal(err)
	}
	alerts := map[string]string{}
	for _, g := range rules.Groups {
		for _, r := range g.Rules {
			selectors := businessSelectorRe.FindAllStringSubmatch(r.Expr, -1)
			if len(selectors) == 0 {
				t.Errorf("%s: %q queries no metric of Telegram Business", r.Alert, r.Expr)
			}
			for _, s := range selectors {
				labels, ok := metrics[s[1]]
				if !ok {
					t.Errorf("%s: metric %s is not published", r.Alert, s[1])
					continue
				}
				for _, m := range businessMatcherRe.FindAllStringSubmatch(s[2], -1) {
					if !labels[m[1]] {
						t.Errorf("%s: %s has no label %s", r.Alert, s[1], m[1])
					}
					vals := []string{m[3]}
					if m[2] == "=~" || m[2] == "!~" {
						vals = strings.Split(m[3], "|")
					}
					for _, v := range vals {
						if !values[v] {
							t.Errorf("%s: %s{%s} matches %q, which the code never sets", r.Alert, s[1], m[1], v)
						}
					}
				}
			}
			alerts[r.Alert] = r.Expr
		}
	}
	// The alert task 105 requires: the share of failed automatic replies.
	if !strings.Contains(alerts["BusinessAutoReplyFailing"], `business_autoreply_total{result="failed"}`) {
		t.Errorf("BusinessAutoReplyFailing on the failed automatic replies is missing: %v", alerts)
	}
}
