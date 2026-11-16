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
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The deployment of the CDN (task 102, docs/ops/cdn.md): what the alerts
// query, how Prometheus finds the node and how the edge runs it, checked
// against the code and the node's configuration, so that a renamed metric
// or a moved port does not leave an alert that never fires or a node that
// does not start.
const (
	cdnAlertRules     = "../../teamgramd/deploy/prometheus/server/rules/cdn.yml"
	prometheusConfig  = "../../teamgramd/deploy/prometheus/server/prometheus.yml"
	edgeCompose       = "../../docker-compose.edge.yaml"
	nodeDeployConfig  = "../../teamgramd/deploy/cdn/cdn.yaml"
	nodeMetricsSource = "../../app/service/cdn/internal/metrics/metrics.go"
	cdnScrapeJob      = "ops.cdn"
)

// opsMetric is a metric as Prometheus names it.
type opsMetric struct {
	labels    map[string]bool
	histogram bool
}

// opsMetrics reads the metrics of the home (metrics.go, go-zero's options)
// and of the node (promauto), and the string constants of both packages:
// the values their labels take.
func opsMetrics(t *testing.T) (map[string]opsMetric, map[string]bool) {
	t.Helper()
	metrics := map[string]opsMetric{}
	values := map[string]bool{}
	unquote := func(e ast.Expr) string {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		return ""
	}
	labelsOf := func(e ast.Expr) map[string]bool {
		m := map[string]bool{}
		if cl, ok := e.(*ast.CompositeLit); ok {
			for _, el := range cl.Elts {
				m[unquote(el)] = true
			}
		}
		return m
	}
	fields := func(cl *ast.CompositeLit) map[string]ast.Expr {
		m := map[string]ast.Expr{}
		for _, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok {
					m[id.Name] = kv.Value
				}
			}
		}
		return m
	}

	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, nodeMetricsSource)
	for _, src := range sources {
		if strings.HasSuffix(src, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), src, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				// constants, and the strings of maps such as stateNames
				for _, v := range n.Values {
					if _, call := v.(*ast.CallExpr); call {
						continue
					}
					ast.Inspect(v, func(e ast.Node) bool {
						if lit, ok := e.(*ast.BasicLit); ok {
							if s := unquote(lit); s != "" {
								values[s] = true
							}
						}
						return true
					})
				}
			case *ast.CompositeLit:
				// metric.NewCounterVec(&metric.CounterVecOpts{...}) at home
				sel, ok := n.Type.(*ast.SelectorExpr)
				if !ok || !strings.HasSuffix(sel.Sel.Name, "VecOpts") {
					return true
				}
				fs := fields(n)
				name := strings.Join([]string{unquote(fs["Namespace"]), unquote(fs["Subsystem"]), unquote(fs["Name"])}, "_")
				metrics[name] = opsMetric{labels: labelsOf(fs["Labels"]), histogram: strings.HasPrefix(sel.Sel.Name, "Histogram")}
			case *ast.CallExpr:
				// promauto.NewCounterVec(prometheus.CounterOpts{...}, []string{...}) on the node
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) == 0 {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "promauto" {
					return true
				}
				opts, ok := n.Args[0].(*ast.CompositeLit)
				if !ok {
					return true
				}
				m := opsMetric{labels: map[string]bool{}, histogram: strings.Contains(sel.Sel.Name, "Histogram")}
				if len(n.Args) > 1 {
					m.labels = labelsOf(n.Args[1])
				}
				metrics[unquote(fields(opts)["Name"])] = m
			}
			return true
		})
	}
	for _, want := range []string{"pluma_cdn_node_healthy", "pluma_cdn_push_total", "pluma_cdn_served_bytes_total", "pluma_cdn_capacity_eviction_age_seconds"} {
		if _, ok := metrics[want]; !ok {
			t.Fatalf("metric %s not found in the sources: %v", want, slices.Sorted(func(yield func(string) bool) {
				for k := range metrics {
					if !yield(k) {
						return
					}
				}
			}))
		}
	}
	return metrics, values
}

var (
	opsSelectorRe = regexp.MustCompile(`\b(pluma_cdn_[a-z_]+|up)\b(\{[^}]*\})?`)
	opsMatcherRe  = regexp.MustCompile(`([a-z_]+)\s*(=~|!~|!=|=)\s*"([^"]*)"`)
	opsGroupingRe = regexp.MustCompile(`\b(?:by|without)\s*\(([^)]*)\)`)
)

// checkCdnExpr reports a query of expr that names a metric, a label or a
// value of a label the code does not have: such a query is silently empty,
// and the alert on it never fires.
func checkCdnExpr(t *testing.T, where, expr string, metrics map[string]opsMetric, values map[string]bool) {
	t.Helper()
	labels := map[string]bool{}
	for _, s := range opsSelectorRe.FindAllStringSubmatch(expr, -1) {
		name := s[1]
		if name == "up" {
			for _, mm := range opsMatcherRe.FindAllStringSubmatch(s[2], -1) {
				if mm[1] != "job" || mm[3] != cdnScrapeJob {
					t.Errorf("%s: up{%s%s%q} is not the node's scrape job %s", where, mm[1], mm[2], mm[3], cdnScrapeJob)
				}
			}
			continue
		}
		m, ok := metrics[name]
		bucket := false
		if !ok {
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				if base, cut := strings.CutSuffix(name, suffix); cut && metrics[base].histogram {
					m, ok, bucket = metrics[base], true, suffix == "_bucket"
				}
			}
		}
		if !ok {
			t.Errorf("%s: metric %s is in neither pkg/cdn nor the node", where, name)
			continue
		}
		for l := range m.labels {
			labels[l] = true
		}
		for _, mm := range opsMatcherRe.FindAllStringSubmatch(s[2], -1) {
			if bucket && mm[1] == "le" {
				for _, v := range strings.Split(mm[3], "|") {
					if _, err := strconv.ParseFloat(v, 64); err != nil {
						t.Errorf("%s: %s{le} matches %q, not a bound", where, name, v)
					}
				}
				continue
			}
			if !m.labels[mm[1]] {
				t.Errorf("%s: %s has no label %s", where, name, mm[1])
				continue
			}
			vals := []string{mm[3]}
			if mm[2] == "=~" || mm[2] == "!~" {
				vals = strings.Split(mm[3], "|")
			}
			for _, v := range vals {
				if !values[v] {
					t.Errorf("%s: %s{%s} matches %q, which is not a value the code sets", where, name, mm[1], v)
				}
			}
		}
	}
	for _, g := range opsGroupingRe.FindAllStringSubmatch(expr, -1) {
		for _, l := range strings.Split(g[1], ",") {
			if l = strings.TrimSpace(l); l != "" && !labels[l] {
				t.Errorf("%s: grouping by %s, which no metric of the query has", where, l)
			}
		}
	}
}

func readYAML(t *testing.T, file string, v any) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

// TestAlertRules keeps the alerts of the brief (CdnNodeUnhealthy,
// CdnPushFailing, CdnEgressBudget, CdnEvictionChurn, CdnNodeDown) on the
// metrics the home and the node publish.
func TestAlertRules(t *testing.T) {
	metrics, values := opsMetrics(t)
	var rules struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert  string            `yaml:"alert"`
				Expr   string            `yaml:"expr"`
				Labels map[string]string `yaml:"labels"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	readYAML(t, cdnAlertRules, &rules)
	var alerts []string
	for _, g := range rules.Groups {
		if g.Name != "cdn" {
			t.Errorf("group %q: the rules of the CDN are the group cdn", g.Name)
		}
		for _, r := range g.Rules {
			if !strings.Contains(r.Expr, "pluma_cdn_") {
				t.Errorf("%s: %q queries no metric of the CDN, so it would fire with the CDN off", r.Alert, r.Expr)
			}
			if s := r.Labels["severity"]; s != "warning" && s != "critical" {
				t.Errorf("%s: severity %q", r.Alert, s)
			}
			checkCdnExpr(t, filepath.Base(cdnAlertRules)+": "+r.Alert, r.Expr, metrics, values)
			alerts = append(alerts, r.Alert)
		}
	}
	slices.Sort(alerts)
	want := []string{"CdnEgressBudget", "CdnEvictionChurn", "CdnNodeDown", "CdnNodeUnhealthy", "CdnPushFailing"}
	if !slices.Equal(alerts, want) {
		t.Errorf("alerts %v, want %v", alerts, want)
	}
}

// TestPrometheusScrapesNode: the job the alerts name reads the node from a
// target file of the home, as the edge's SFU and node-exporter.
func TestPrometheusScrapesNode(t *testing.T) {
	var cfg struct {
		ScrapeConfigs []struct {
			JobName       string `yaml:"job_name"`
			FileSDConfigs []struct {
				Files []string `yaml:"files"`
			} `yaml:"file_sd_configs"`
		} `yaml:"scrape_configs"`
	}
	readYAML(t, prometheusConfig, &cfg)
	for _, sc := range cfg.ScrapeConfigs {
		if sc.JobName != cdnScrapeJob {
			continue
		}
		for _, fs := range sc.FileSDConfigs {
			for _, f := range fs.Files {
				if ok, _ := path.Match(f, "/etc/prometheus/targets/cdn.json"); ok {
					return
				}
			}
		}
		t.Fatalf("job %s does not read /etc/prometheus/targets/cdn.json", cdnScrapeJob)
	}
	t.Fatalf("%s has no job %s", prometheusConfig, cdnScrapeJob)
}

// TestEdgeRunsNode: the service cdn of the edge publishes the ports of the
// node's configuration (clients on Pluma's address, control and metrics on
// the tunnel's), sets every variable that configuration needs (pkg/conf
// refuses to start on an unset one) and mounts its data directory and its
// control certificates where the configuration looks for them.
func TestEdgeRunsNode(t *testing.T) {
	var node struct {
		MTProto struct {
			ListenOn string `yaml:"ListenOn"`
		} `yaml:"MTProto"`
		Store struct {
			DataDir string `yaml:"DataDir"`
		} `yaml:"Store"`
		Control struct {
			ListenOn     string `yaml:"ListenOn"`
			CertFile     string `yaml:"CertFile"`
			KeyFile      string `yaml:"KeyFile"`
			ClientCAFile string `yaml:"ClientCAFile"`
		} `yaml:"Control"`
		MetricsListenOn string `yaml:"MetricsListenOn"`
	}
	raw, err := os.ReadFile(nodeDeployConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Profiles    []string          `yaml:"profiles"`
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			Ports       []string          `yaml:"ports"`
			ReadOnly    bool              `yaml:"read_only"`
		} `yaml:"services"`
	}
	readYAML(t, edgeCompose, &compose)
	svc, ok := compose.Services["cdn"]
	if !ok {
		t.Fatalf("%s has no service cdn", edgeCompose)
	}
	if !slices.Contains(svc.Profiles, "cdn") {
		t.Errorf("service cdn without the profile cdn: an edge without the node would need its variables")
	}
	if !svc.ReadOnly {
		t.Errorf("service cdn is not read_only: the node writes only its volume")
	}

	// The whole file, comments too: pkg/conf expands a reference wherever it
	// is and refuses to start on one that is not set.
	refs := 0
	for _, ref := range regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`).FindAllStringSubmatch(string(raw), -1) {
		refs++
		if _, ok := svc.Environment[ref[1]]; !ok {
			t.Errorf("%s needs %s, which service cdn does not set", nodeDeployConfig, ref[1])
		}
	}
	if refs == 0 {
		t.Errorf("%s takes no variables: the test reads the wrong file", nodeDeployConfig)
	}

	port := func(hostPort string) string {
		_, p, err := net.SplitHostPort(hostPort)
		if err != nil {
			t.Fatalf("%q: %v", hostPort, err)
		}
		return p
	}
	// "host_ip:host_port:container_port/tcp", where host_ip and host_port
	// may be ${VAR} or ${VAR:-default}.
	published := map[string]string{}
	for _, p := range svc.Ports {
		spec, ok := strings.CutSuffix(p, "/tcp")
		i := strings.LastIndex(spec, ":")
		end := strings.Index(spec, ":")
		if strings.HasPrefix(spec, "${") {
			end = strings.Index(spec, "}") + 1
		}
		if !ok || i < 0 || end <= 0 || end >= i {
			t.Errorf("port %q: want host_ip:host_port:container_port/tcp", p)
			continue
		}
		published[spec[i+1:]] = spec[:end]
	}
	for what, c := range map[string]struct{ port, host string }{
		"clients": {port(node.MTProto.ListenOn), "${PLUMA_PUBLIC_IPV4}"},
		"control": {port(node.Control.ListenOn), "${EDGE_TUNNEL_IPV4:-10.77.0.1}"},
		"metrics": {port(node.MetricsListenOn), "${EDGE_TUNNEL_IPV4:-10.77.0.1}"},
	} {
		if host, ok := published[c.port]; !ok {
			t.Errorf("the %s port %s of the node is not published", what, c.port)
		} else if host != c.host {
			t.Errorf("the %s port %s is published on %s, want %s", what, c.port, host, c.host)
		}
	}

	mounts := map[string]bool{}
	for _, v := range svc.Volumes {
		parts := strings.Split(v, ":")
		if len(parts) < 2 {
			t.Errorf("volume %q", v)
			continue
		}
		// ${VAR:-default}:/target[:ro] splits the default off too.
		mounts[parts[len(parts)-1]] = true
		mounts[parts[len(parts)-2]] = true
	}
	for _, dir := range []string{node.Store.DataDir, path.Dir(node.Control.CertFile), path.Dir(node.Control.KeyFile), path.Dir(node.Control.ClientCAFile), "/etc/cdn/cdn.yaml"} {
		if !mounts[dir] {
			t.Errorf("service cdn does not mount %s", dir)
		}
	}
}
