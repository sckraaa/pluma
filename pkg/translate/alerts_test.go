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

package translate

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	alertRulesFile = "../../teamgramd/deploy/prometheus/server/rules/translate.yml"
	dashboardFile  = "../../teamgramd/deploy/grafana/dashboards/pluma-overview.json"
)

// declaredMetric is a metric of metrics.go as Prometheus names it.
type declaredMetric struct {
	labels    map[string]bool
	histogram bool
}

// declaredMetrics reads the metrics and the string constants (the values
// of their labels) of metrics.go, so that what the alerts and the
// dashboard query is checked against the code itself.
func declaredMetrics(t *testing.T) (map[string]declaredMetric, map[string]bool) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "metrics.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
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
	metrics := map[string]declaredMetric{}
	values := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			sel, ok := n.Type.(*ast.SelectorExpr)
			if !ok || !strings.HasSuffix(sel.Sel.Name, "VecOpts") {
				return true
			}
			var ns, name string
			m := declaredMetric{labels: map[string]bool{}, histogram: sel.Sel.Name == "HistogramVecOpts"}
			for _, el := range n.Elts {
				kv := el.(*ast.KeyValueExpr)
				switch kv.Key.(*ast.Ident).Name {
				case "Namespace":
					ns = unquote(kv.Value)
				case "Name":
					name = unquote(kv.Value)
				case "Labels":
					for _, l := range kv.Value.(*ast.CompositeLit).Elts {
						m.labels[unquote(l)] = true
					}
				}
			}
			metrics[ns+"_"+name] = m
		case *ast.ValueSpec:
			for _, v := range n.Values {
				if s := unquote(v); s != "" {
					values[s] = true
				}
			}
		}
		return true
	})
	if len(metrics) == 0 || len(values) == 0 {
		t.Fatal("no metrics or label values found in metrics.go")
	}
	return metrics, values
}

var (
	selectorRe = regexp.MustCompile(`\b(translate_[a-z_]+)\b(\{[^}]*\})?`)
	matcherRe  = regexp.MustCompile(`([a-z_]+)\s*(=~|!~|!=|=)\s*"([^"]*)"`)
	groupingRe = regexp.MustCompile(`\b(?:by|without)\s*\(([^)]*)\)`)
)

// checkExpr reports a query of expr that names a metric, a label or a
// value of a label metrics.go does not have: such a query is silently
// empty, and an alert on it never fires.
func checkExpr(t *testing.T, where, expr string, metrics map[string]declaredMetric, values map[string]bool) {
	t.Helper()
	labels := map[string]bool{}
	for _, s := range selectorRe.FindAllStringSubmatch(expr, -1) {
		name := s[1]
		m, ok := metrics[name]
		if !ok {
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				if base, cut := strings.CutSuffix(name, suffix); cut && metrics[base].histogram {
					m, ok = metrics[base], true
					if suffix == "_bucket" {
						labels["le"] = true
					}
				}
			}
		}
		if !ok {
			t.Errorf("%s: metric %s is not in metrics.go", where, name)
			continue
		}
		for l := range m.labels {
			labels[l] = true
		}
		for _, mm := range matcherRe.FindAllStringSubmatch(s[2], -1) {
			if !m.labels[mm[1]] {
				t.Errorf("%s: %s has no label %s", where, name, mm[1])
			}
			vals := []string{mm[3]}
			if mm[2] == "=~" || mm[2] == "!~" {
				vals = strings.Split(mm[3], "|")
			}
			for _, v := range vals {
				if !values[v] {
					t.Errorf("%s: %s{%s} matches %q, which is not a value of metrics.go", where, name, mm[1], v)
				}
			}
		}
	}
	for _, g := range groupingRe.FindAllStringSubmatch(expr, -1) {
		for _, l := range strings.Split(g[1], ",") {
			if l = strings.TrimSpace(l); l != "" && !labels[l] {
				t.Errorf("%s: grouping by %s, which no metric of the query has", where, l)
			}
		}
	}
}

// TestAlertRules keeps the alerts of the translation (ADR 0064, section
// 13; docs/ops/translation.md, "Тревоги") on the metrics bff publishes.
func TestAlertRules(t *testing.T) {
	metrics, values := declaredMetrics(t)
	data, err := os.ReadFile(alertRulesFile)
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
			if !strings.Contains(r.Expr, "translate_") {
				t.Errorf("%s: %q queries no metric of the translation", r.Alert, r.Expr)
			}
			checkExpr(t, filepath.Base(alertRulesFile)+": "+r.Alert, r.Expr, metrics, values)
			alerts[r.Alert] = r.Expr
		}
	}
	// The alert the ADR requires: the engine unusable for 10 minutes.
	if !strings.Contains(alerts["TranslateEngineDown"], "translate_engine_up") {
		t.Errorf("TranslateEngineDown on translate_engine_up is missing: %v", alerts)
	}
}

// TestDashboardQueries does the same for the Translation row of the
// dashboard.
func TestDashboardQueries(t *testing.T) {
	metrics, values := declaredMetrics(t)
	data, err := os.ReadFile(dashboardFile)
	if err != nil {
		t.Fatal(err)
	}
	type panel struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	}
	var dash struct {
		Panels []panel `json:"panels"`
	}
	if err := json.Unmarshal(data, &dash); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range dash.Panels {
		for _, tg := range p.Targets {
			if strings.Contains(tg.Expr, "translate_") {
				n++
				checkExpr(t, "dashboard: "+p.Title, tg.Expr, metrics, values)
			}
		}
	}
	if n == 0 {
		t.Error("the dashboard has no panel of the translation")
	}
}
