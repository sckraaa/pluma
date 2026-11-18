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

// Package rutest tests the files of the Russian node (teamgramd/deploy/ru,
// task 109, docs/ops/ru-node.md): the rendering of deploy.sh, the check of
// the owner's nginx by owner-nginx.sh, the metrics of wgstat.sh and the
// alert rules here; nginx itself, with the real Ubuntu package, in
// nginx_integration_test.go.
package rutest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ruDir is teamgramd/deploy/ru, relative to this package.
const ruDir = ".."

// needBash skips a test without bash 4 (associative arrays): the scripts
// run on Ubuntu, and the tests on Linux.
func needBash(t *testing.T) {
	t.Helper()
	out, err := exec.Command("bash", "-c", "echo ${BASH_VERSINFO[0]}").Output()
	if err != nil {
		t.Skipf("no bash: %v", err)
	}
	if v, _ := strconv.Atoi(strings.TrimSpace(string(out))); v < 4 {
		t.Skipf("bash %s: the scripts need bash 4 or later", strings.TrimSpace(string(out)))
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// nodeEnv is a valid /etc/pluma-ru/env with throwaway secrets.
type nodeEnv struct {
	vars  map[string]string
	order []string
}

func newNodeEnv(t *testing.T) *nodeEnv {
	t.Helper()
	e := &nodeEnv{vars: map[string]string{}}
	e.set("PLUMA_RU_PUBLIC_IPV4", "192.0.2.10")
	e.set("PLUMA_RU_FILES_DOMAIN", "r1.pluma.test")
	e.set("PLUMA_RU_EDGE_DOMAIN", "ru.pluma.test")
	e.set("PLUMA_CDN_ORIGIN_DOMAIN", "origin.pluma.test")
	e.set("PLUMA_WEB_DOMAIN", "web.pluma.test")
	e.set("PLUMA_GCORE_EDGE_DOMAIN", "edge.gcore.test")
	e.set("CDN_RU_TOKEN_KEY", randomHex(t, 16))
	e.set("CDN_RU_ORIGIN_AUTH", randomHex(t, 32))
	e.set("CALLS_TURN_SECRETS", "ru="+randomHex(t, 32))
	return e
}

func (e *nodeEnv) set(name, value string) {
	if _, ok := e.vars[name]; !ok {
		e.order = append(e.order, name)
	}
	e.vars[name] = value
}

func (e *nodeEnv) del(name string) {
	delete(e.vars, name)
}

// secrets are the values that must never be printed.
func (e *nodeEnv) secrets() []string {
	var s []string
	for _, n := range []string{"CDN_RU_TOKEN_KEY", "CDN_RU_ORIGIN_AUTH", "CALLS_TURN_SECRETS"} {
		if v := e.vars[n]; v != "" {
			s = append(s, v)
			if _, after, ok := strings.Cut(v, "="); ok {
				s = append(s, after)
			}
		}
	}
	return s
}

func (e *nodeEnv) write(t *testing.T, path string, extra ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# throwaway values of a test\n\n")
	for _, n := range e.order {
		if v, ok := e.vars[n]; ok {
			fmt.Fprintf(&b, "%s=%s\n", n, v)
		}
	}
	for _, l := range extra {
		b.WriteString(l + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run runs a script of ruDir with args and the environment env; it returns
// stdout and stderr together, and the exit code.
func run(t *testing.T, env []string, script string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(ruDir, script)}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s: %v", script, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// render renders e into a new directory and returns it.
func render(t *testing.T, e *nodeEnv, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env")
	e.write(t, envFile)
	out := filepath.Join(dir, "etc")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	text, code := run(t, nil, "deploy.sh", append([]string{"render", envFile, out}, args...)...)
	for _, s := range e.secrets() {
		if strings.Contains(text, s) {
			t.Fatalf("deploy.sh render printed a secret:\n%s", text)
		}
	}
	return out, text, code
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

var placeholder = regexp.MustCompile(`@@[A-Z_]+@@`)

func TestRender(t *testing.T) {
	needBash(t)
	e := newNodeEnv(t)
	dir, text, code := render(t, e)
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	files := map[string]os.FileMode{
		"nginx.conf":                 0o644,
		"files.conf":                 0o644,
		"secrets.conf":               0o600,
		"turn.conf":                  0o640,
		"owner/pluma-upstreams.conf": 0o644,
		"owner/pluma-sni.map":        0o644,
		"owner/pluma-acme-http.conf": 0o644,
	}
	for f, want := range files {
		p := filepath.Join(dir, f)
		if got := mode(t, p); got != want {
			t.Errorf("%s: mode %v, want %v", f, got, want)
		}
		if m := placeholder.FindString(readFile(t, p)); m != "" {
			t.Errorf("%s: %s is left", f, m)
		}
	}

	conf := readFile(t, filepath.Join(dir, "files.conf"))
	for _, want := range []string{
		"server_name r1.pluma.test;",
		"ssl_certificate /etc/pluma-ru/letsencrypt/live/r1.pluma.test/fullchain.pem;",
		"server 10.78.0.2:443;", // the default address of the origin
		"proxy_set_header Host origin.pluma.test;",
		"proxy_ssl_name origin.pluma.test;",
		`"https://web.pluma.test" $http_origin;`,
		`"https://ru.pluma.test" $http_origin;`,
		`"https://edge.gcore.test" $http_origin;`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("files.conf has no %q", want)
		}
	}
	if strings.Contains(conf, "X-Cache-Status") {
		t.Error("files.conf of production has X-Cache-Status")
	}
	for _, s := range e.secrets() {
		if strings.Contains(conf, s) {
			t.Error("files.conf has a secret: secrets go to secrets.conf only")
		}
	}

	secrets := readFile(t, filepath.Join(dir, "secrets.conf"))
	for _, want := range []string{
		`map "" $pluma_token_key { default "` + e.vars["CDN_RU_TOKEN_KEY"] + `"; }`,
		`map "" $pluma_origin_auth { default "` + e.vars["CDN_RU_ORIGIN_AUTH"] + `"; }`,
	} {
		if !strings.Contains(secrets, want) {
			t.Errorf("secrets.conf has no %q", want)
		}
	}

	turn := readFile(t, filepath.Join(dir, "turn.conf"))
	shared := readFile(t, filepath.Join(ruDir, "..", "turn", "turnserver.conf"))
	if !strings.Contains(turn, shared) {
		t.Error("turn.conf does not carry teamgramd/deploy/turn/turnserver.conf as it is")
	}
	_, secret, _ := strings.Cut(e.vars["CALLS_TURN_SECRETS"], "=")
	for _, want := range []string{
		"\nlistening-ip=192.0.2.10\n", "\nrelay-ip=192.0.2.10\n", "\nexternal-ip=192.0.2.10\n",
		"\nstatic-auth-secret=" + secret + "\n",
	} {
		if !strings.Contains(turn, want) {
			t.Errorf("turn.conf has no %q", strings.TrimSpace(strings.SplitN(want, "=", 2)[0]))
		}
	}

	sni := readFile(t, filepath.Join(dir, "owner", "pluma-sni.map"))
	if !strings.Contains(sni, "\nru.pluma.test pluma_ru_edge;\n") || !strings.Contains(sni, "\nr1.pluma.test pluma_ru_files;\n") {
		t.Errorf("pluma-sni.map:\n%s", sni)
	}
	acme := readFile(t, filepath.Join(dir, "owner", "pluma-acme-http.conf"))
	for _, want := range []string{"server_name r1.pluma.test;", "server_name ru.pluma.test;", "root /var/lib/pluma-ru/acme;", "proxy_pass http://10.78.0.2:80;"} {
		if !strings.Contains(acme, want) {
			t.Errorf("pluma-acme-http.conf has no %q", want)
		}
	}
}

// TestRenderWithoutGcoreEdge: the Gcore edge's site is optional; without it
// the CORS map has the two sites of Pluma only.
func TestRenderWithoutGcoreEdge(t *testing.T) {
	needBash(t)
	e := newNodeEnv(t)
	e.del("PLUMA_GCORE_EDGE_DOMAIN")
	dir, text, code := render(t, e)
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	conf := readFile(t, filepath.Join(dir, "files.conf"))
	if strings.Contains(conf, "edge.gcore.test") || placeholder.MatchString(conf) {
		t.Errorf("files.conf without PLUMA_GCORE_EDGE_DOMAIN:\n%s", conf)
	}
	if !strings.Contains(conf, `"https://ru.pluma.test" $http_origin;`) {
		t.Error("files.conf lost the edge's site")
	}
}

func TestRenderTestBuild(t *testing.T) {
	needBash(t)
	e := newNodeEnv(t)
	e.set("PLUMA_RU_ORIGIN_ADDR", "127.0.0.1:10443")
	dir, text, code := render(t, e, "--cache-status-header")
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	conf := readFile(t, filepath.Join(dir, "files.conf"))
	for _, want := range []string{"add_header X-Cache-Status $upstream_cache_status always;", "server 127.0.0.1:10443;"} {
		if !strings.Contains(conf, want) {
			t.Errorf("files.conf has no %q", want)
		}
	}
}

func TestRenderRefusesBadValues(t *testing.T) {
	needBash(t)
	cases := []struct {
		name string
		edit func(e *nodeEnv)
		want string // in the message
	}{
		{"key with a dollar", func(e *nodeEnv) { e.set("CDN_RU_TOKEN_KEY", "abc$def12345") }, "CDN_RU_TOKEN_KEY"},
		{"key with a quote", func(e *nodeEnv) { e.set("CDN_RU_TOKEN_KEY", `abc"def12345`) }, "CDN_RU_TOKEN_KEY"},
		{"key too long", func(e *nodeEnv) { e.set("CDN_RU_TOKEN_KEY", strings.Repeat("a", 33)) }, "CDN_RU_TOKEN_KEY"},
		{"key too short", func(e *nodeEnv) { e.set("CDN_RU_TOKEN_KEY", "abcde") }, "CDN_RU_TOKEN_KEY"},
		{"origin auth short", func(e *nodeEnv) { e.set("CDN_RU_ORIGIN_AUTH", strings.Repeat("a", 63)) }, "CDN_RU_ORIGIN_AUTH"},
		{"origin auth upper case", func(e *nodeEnv) { e.set("CDN_RU_ORIGIN_AUTH", strings.Repeat("A", 64)) }, "CDN_RU_ORIGIN_AUTH"},
		{"another TURN label", func(e *nodeEnv) {
			e.set("CALLS_TURN_SECRETS", e.vars["CALLS_TURN_SECRETS"]+",hz="+strings.Repeat("b", 64))
		}, "only the secret labelled ru"},
		{"no ru label", func(e *nodeEnv) { e.set("CALLS_TURN_SECRETS", "hz="+strings.Repeat("b", 64)) }, "only the secret labelled ru"},
		{"TURN secret short", func(e *nodeEnv) { e.set("CALLS_TURN_SECRETS", "ru=abc") }, "the secret of ru"},
		{"TURN secret with a line break", func(e *nodeEnv) { e.set("CALLS_TURN_SECRETS", "ru="+strings.Repeat("c", 40)+"\nexternal-ip=1.2.3.4") }, "is not NAME=value"},
		{"domain upper case", func(e *nodeEnv) { e.set("PLUMA_RU_FILES_DOMAIN", "R1.pluma.test") }, "PLUMA_RU_FILES_DOMAIN"},
		{"domain with a semicolon", func(e *nodeEnv) { e.set("PLUMA_WEB_DOMAIN", "web.pluma.test;") }, "PLUMA_WEB_DOMAIN"},
		{"Gcore edge with a path", func(e *nodeEnv) { e.set("PLUMA_GCORE_EDGE_DOMAIN", "edge.gcore.test/x") }, "PLUMA_GCORE_EDGE_DOMAIN"},
		{"domain without a dot", func(e *nodeEnv) { e.set("PLUMA_CDN_ORIGIN_DOMAIN", "origin") }, "PLUMA_CDN_ORIGIN_DOMAIN"},
		{"same names", func(e *nodeEnv) { e.set("PLUMA_RU_EDGE_DOMAIN", "r1.pluma.test") }, "the same name"},
		{"address out of range", func(e *nodeEnv) { e.set("PLUMA_RU_PUBLIC_IPV4", "256.1.1.1") }, "PLUMA_RU_PUBLIC_IPV4"},
		{"origin address", func(e *nodeEnv) { e.set("PLUMA_RU_ORIGIN_ADDR", "home:443") }, "PLUMA_RU_ORIGIN_ADDR"},
		{"missing", func(e *nodeEnv) { e.del("PLUMA_WEB_DOMAIN") }, "PLUMA_WEB_DOMAIN is not set"},
		{"unknown", func(e *nodeEnv) { e.set("PLUMA_RU_TYPO", "x") }, "unknown variable PLUMA_RU_TYPO"},
		{"twice", func(e *nodeEnv) { e.order = append(e.order, "PLUMA_WEB_DOMAIN") }, "PLUMA_WEB_DOMAIN is set twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newNodeEnv(t)
			c.edit(e)
			dir, text, code := render(t, e)
			if code == 0 {
				t.Fatalf("render took it:\n%s", text)
			}
			if !strings.Contains(text, c.want) {
				t.Errorf("the message does not name %q:\n%s", c.want, text)
			}
			if _, err := os.Stat(filepath.Join(dir, "secrets.conf")); err == nil {
				t.Error("secrets.conf was written")
			}
		})
	}
}

// ownerTree is the stand-in of the owner's /etc/nginx on the node
// (testdata/owner/etc/nginx: the Ubuntu nginx.conf with stream {}, the SNI
// map of stream.d/sni.conf, a site behind it), as path under /etc/nginx →
// content.
func ownerTree(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("testdata", "owner", "etc", "nginx")
	tree := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		tree[rel] = readFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// dump prints tree as nginx -T does: every file read, after a
// "# configuration file <path>:" line. Order does not matter to
// owner-nginx.sh, which follows the includes.
func dump(tree map[string]string) string {
	names := make([]string, 0, len(tree))
	for n := range tree {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "# configuration file /etc/nginx/%s:\n%s\n", n, tree[n])
	}
	return b.String()
}

// installed adds to tree what owner-nginx.sh install adds.
func installed(t *testing.T, tree map[string]string, rendered string) {
	t.Helper()
	tree["stream.d/pluma-upstreams.conf"] = readFile(t, filepath.Join(rendered, "owner", "pluma-upstreams.conf"))
	tree["stream.d/pluma-sni.map"] = readFile(t, filepath.Join(rendered, "owner", "pluma-sni.map"))
	tree["conf.d/pluma-acme-http.conf"] = readFile(t, filepath.Join(rendered, "owner", "pluma-acme-http.conf"))
	tree["stream.d/sni.conf"] = strings.Replace(tree["stream.d/sni.conf"],
		"map $ssl_preread_server_name $sni_upstream {\n",
		"map $ssl_preread_server_name $sni_upstream {\n    include /etc/nginx/stream.d/pluma-sni.map;\n", 1)
}

// ownerCheck runs owner-nginx.sh check on the dump of tree and version.
func ownerCheck(t *testing.T, rendered string, tree map[string]string, version string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	d, v := filepath.Join(dir, "nginx-T.txt"), filepath.Join(dir, "nginx-V.txt")
	if err := os.WriteFile(d, []byte(dump(tree)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v, []byte(version), 0o644); err != nil {
		t.Fatal(err)
	}
	return run(t, []string{"PLUMA_RU_ETC=" + rendered}, "owner-nginx.sh", "check", "--dump", d, "--version", v)
}

func TestOwnerCheck(t *testing.T) {
	needBash(t)
	rendered, text, code := render(t, newNodeEnv(t))
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	version := readFile(t, filepath.Join("testdata", "nginx-V.txt"))

	text, code = ownerCheck(t, rendered, ownerTree(t), version)
	if code != 0 || !strings.Contains(text, "state not installed") || strings.Contains(text, "FAIL") {
		t.Fatalf("the layout of the node: exit %d\n%s", code, text)
	}
	for _, want := range []string{
		"ok    the SNI map of $sni_upstream is in /etc/nginx/stream.d/sni.conf",
		"ok    stream {} includes /etc/nginx/stream.d/pluma-upstreams.conf",
		"ok    http {} includes /etc/nginx/conf.d/pluma-acme-http.conf",
		"ok    the stream server listens on 443 of every IPv4 address",
		"ok    nginx has http_secure_link_module",
		"ok    nginx has http_slice_module",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}

	tree := ownerTree(t)
	installed(t, tree, rendered)
	text, code = ownerCheck(t, rendered, tree, version)
	if code != 0 || !strings.Contains(text, "state installed") {
		t.Fatalf("installed: exit %d\n%s", code, text)
	}

	cases := []struct {
		name    string
		edit    func(tree map[string]string) string // returns nginx -V
		message string
	}{
		{"listen on one address", func(tree map[string]string) string {
			tree["stream.d/sni.conf"] = strings.Replace(tree["stream.d/sni.conf"], "listen 443;", "listen 194.87.226.185:443;", 1)
			return version
		}, "does not listen on 443 of every IPv4 address"},
		{"no PROXY", func(tree map[string]string) string {
			tree["stream.d/sni.conf"] = strings.Replace(tree["stream.d/sni.conf"], "proxy_protocol on;", "", 1)
			return version
		}, "no proxy_protocol on"},
		{"stream.d outside stream", func(tree map[string]string) string {
			tree["nginx.conf"] = strings.Replace(tree["nginx.conf"], "stream {\n    include /etc/nginx/stream.d/*.conf;\n}",
				"stream {\n    include /etc/nginx/stream.d/sni.conf;\n}", 1)
			return version
		}, "no include in stream {} takes /etc/nginx/stream.d/pluma-upstreams.conf"},
		{"no conf.d in http", func(tree map[string]string) string {
			tree["nginx.conf"] = strings.Replace(tree["nginx.conf"], "include /etc/nginx/conf.d/*.conf;", "", 1)
			return version
		}, "no include in http {} takes /etc/nginx/conf.d/pluma-acme-http.conf"},
		{"no slice", func(tree map[string]string) string {
			return strings.Replace(version, "--with-http_slice_module", "", 1)
		}, "nginx is built without http_slice_module"},
		{"no secure_link", func(tree map[string]string) string {
			return strings.Replace(version, "--with-http_secure_link_module", "", 1)
		}, "nginx is built without http_secure_link_module"},
		{"name routed by the owner", func(tree map[string]string) string {
			tree["stream.d/sni.conf"] = strings.Replace(tree["stream.d/sni.conf"], "default owner_site;", "default owner_site;\n    r1.pluma.test owner_site;", 1)
			return version
		}, "the owner's map already routes r1.pluma.test"},
		{"name served by the owner", func(tree map[string]string) string {
			tree["sites-enabled/owner-site"] = strings.Replace(tree["sites-enabled/owner-site"], "server_name site.test;", "server_name site.test ru.pluma.test;", 1)
			return version
		}, "an http server of the owner already has server_name ru.pluma.test"},
		{"two maps", func(tree map[string]string) string {
			tree["stream.d/other.conf"] = "map $ssl_preread_server_name $other { default owner_site; }\n"
			return version
		}, "want one map $ssl_preread_server_name in stream {}, found 2"},
		{"no map", func(tree map[string]string) string {
			tree["stream.d/sni.conf"] = "server {\n    listen 443;\n    proxy_pass owner_site;\n}\nupstream owner_site { server 127.0.0.1:8443; }\n"
			return version
		}, "found 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := ownerTree(t)
			v := c.edit(tree)
			text, code := ownerCheck(t, rendered, tree, v)
			if code == 0 {
				t.Fatalf("check passed:\n%s", text)
			}
			if !strings.Contains(text, c.message) {
				t.Errorf("no %q in\n%s", c.message, text)
			}
		})
	}
}

// The comments and quoted strings of a configuration do not open blocks
// or end directives.
func TestOwnerCheckReadsCommentsAndQuotes(t *testing.T) {
	needBash(t)
	rendered, text, code := render(t, newNodeEnv(t))
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	tree := ownerTree(t)
	tree["stream.d/sni.conf"] = "# a comment with { and ; and }\n" + strings.Replace(tree["stream.d/sni.conf"],
		"proxy_connect_timeout 10s;", "proxy_connect_timeout 10s; # } ;\n    set $x \"a;b{c}\";", 1)
	text, code = ownerCheck(t, rendered, tree, readFile(t, filepath.Join("testdata", "nginx-V.txt")))
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, text)
	}
}

func TestWgstat(t *testing.T) {
	needBash(t)
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("no du")
	}
	rendered, text, code := render(t, newNodeEnv(t))
	if code != 0 {
		t.Fatalf("render: exit %d\n%s", code, text)
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(filepath.Join(cache, "a", "bc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "a", "bc", "slice"), bytes.Repeat([]byte{1}, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "pluma_ru.prom")
	text, code = run(t, []string{
		"PLUMA_RU_WG_IFACE=pluma-test-none",
		"PLUMA_RU_CACHE_DIR=" + cache,
		"PLUMA_RU_FILES_CONF=" + filepath.Join(rendered, "files.conf"),
	}, "wgstat.sh", out)
	if code != 0 {
		t.Fatalf("wgstat.sh: exit %d\n%s", code, text)
	}
	prom := readFile(t, out)
	for _, want := range []string{
		"\npluma_wg_up{interface=\"pluma-test-none\"} 0\n",
		"\npluma_wg_latest_handshake_seconds{interface=\"pluma-test-none\"} 0\n",
		"\npluma_ru_cache_max_bytes 16106127360\n", // max_size=15g
	} {
		if !strings.Contains(prom, want) {
			t.Errorf("no %q in\n%s", strings.TrimSpace(want), prom)
		}
	}
	m := regexp.MustCompile(`\npluma_ru_cache_bytes (\d+)\n`).FindStringSubmatch(prom)
	if m == nil {
		t.Fatalf("no pluma_ru_cache_bytes in\n%s", prom)
	}
	if n, _ := strconv.ParseInt(m[1], 10, 64); n < 1<<20 {
		t.Errorf("pluma_ru_cache_bytes %d, want at least the 1 MiB written", n)
	}
	if !regexp.MustCompile(`\npluma_ru_stats_timestamp_seconds \d{10}\n`).MatchString(prom) {
		t.Errorf("no timestamp in\n%s", prom)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("%s is left", e.Name())
		}
	}
}

const alertRulesFile = "../../prometheus/server/rules/ru-node.yml"

// The alert rules of the node are well formed and name only metrics that
// wgstat.sh writes or node-exporter has; promtool runs them in
// TestAlertRulesFire.
func TestAlertRules(t *testing.T) {
	var f struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert       string            `yaml:"alert"`
				Expr        string            `yaml:"expr"`
				For         string            `yaml:"for"`
				Labels      map[string]string `yaml:"labels"`
				Annotations map[string]string `yaml:"annotations"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, alertRulesFile)), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Groups) != 1 || len(f.Groups[0].Rules) == 0 {
		t.Fatalf("want one group with rules, got %+v", f.Groups)
	}
	script := readFile(t, filepath.Join(ruDir, "wgstat.sh"))
	metric := regexp.MustCompile(`\bpluma_(?:wg|ru)_[a-z_]+`)
	seen := map[string]bool{}
	for _, r := range f.Groups[0].Rules {
		if r.Alert == "" || seen[r.Alert] {
			t.Errorf("an alert without a name or twice: %q", r.Alert)
		}
		seen[r.Alert] = true
		if s := r.Labels["severity"]; s != "warning" && s != "critical" {
			t.Errorf("%s: severity %q", r.Alert, s)
		}
		if r.Annotations["summary"] == "" || !strings.Contains(r.Annotations["description"], "docs/ops/ru-node.md") {
			t.Errorf("%s: want a summary and a description that points to docs/ops/ru-node.md", r.Alert)
		}
		for _, m := range metric.FindAllString(r.Expr, -1) {
			if !strings.Contains(script, m+" ") && !strings.Contains(script, m+"{") {
				t.Errorf("%s: %s is not written by wgstat.sh", r.Alert, m)
			}
		}
	}
}
