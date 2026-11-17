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

package cdn

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const cdnAlertRulesFile = "../../teamgramd/deploy/prometheus/server/rules/cdn.yml"

// cdnMetricFiles declare the metrics of the CDN and name the values of
// their labels: the results of the redirects and the states of the queue
// (metrics.go), the results of the preparations (preparer.go).
var cdnMetricFiles = []string{"metrics.go", "preparer.go"}

// cdnMetrics reads the pluma_cdn_* metrics of cdnMetricFiles as Prometheus
// names them, with their labels, and the string literals of the files,
// among which are the values of the labels.
func cdnMetrics(t *testing.T) (map[string]map[string]bool, map[string]bool) {
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
	for _, path := range cdnMetricFiles {
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
				metrics[strings.Join(parts, "_")] = labels
			}
			return true
		})
	}
	if _, ok := metrics["pluma_cdn_redirects_total"]; !ok || len(metrics) < 10 {
		t.Fatalf("the CDN metrics found: %v", metrics)
	}
	return metrics, values
}

var (
	cdnSelectorRe = regexp.MustCompile(`\b(pluma_cdn_[a-z_]+)\b(\{[^}]*\})?`)
	cdnMatcherRe  = regexp.MustCompile(`([a-z_]+)\s*(=~|!~|!=|=)\s*"([^"]*)"`)
	httpStatusRe  = regexp.MustCompile(`^[1-5][0-9][0-9]$`)
)

// TestAlertRules keeps the alerts of the CDN (docs/ops/cdn.md, task 107
// "Метрики и алерты") on the metrics bff publishes: a query of a metric, a
// label or a value the code does not have is silently empty, and its alert
// never fires.
func TestAlertRules(t *testing.T) {
	metrics, values := cdnMetrics(t)
	data, err := os.ReadFile(cdnAlertRulesFile)
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
	var alerts []string
	for _, g := range rules.Groups {
		for _, r := range g.Rules {
			selectors := cdnSelectorRe.FindAllStringSubmatch(r.Expr, -1)
			if len(selectors) == 0 {
				t.Errorf("%s: %q queries no metric of the CDN", r.Alert, r.Expr)
			}
			for _, s := range selectors {
				labels, ok := metrics[s[1]]
				if !ok {
					t.Errorf("%s: metric %s is not published", r.Alert, s[1])
					continue
				}
				for _, m := range cdnMatcherRe.FindAllStringSubmatch(s[2], -1) {
					if !labels[m[1]] {
						t.Errorf("%s: %s has no label %s", r.Alert, s[1], m[1])
					}
					vals := []string{m[3]}
					if m[2] == "=~" || m[2] == "!~" {
						vals = strings.Split(m[3], "|")
					}
					for _, v := range vals {
						// The origin labels its answers by their HTTP status
						// (origin.go, strconv.Itoa of the status).
						if m[1] == "code" && httpStatusRe.MatchString(v) {
							continue
						}
						if !values[v] {
							t.Errorf("%s: %s{%s} matches %q, which the code never sets", r.Alert, s[1], m[1], v)
						}
					}
				}
			}
			alerts = append(alerts, r.Alert)
		}
	}
	// The alerts task 107 requires.
	for _, want := range []string{"CdnDeliveryUnhealthy", "CdnPrepareFailing", "CdnFallbackHigh", "CdnBudget", "CdnOriginBusy", "CdnDevHttp"} {
		if !slices.Contains(alerts, want) {
			t.Errorf("alert %s is missing: %v", want, alerts)
		}
	}
}
