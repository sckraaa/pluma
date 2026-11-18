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

//go:build integration

package rutest

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ubuntuImage is the release of the Russian node (Ubuntu 26.04 LTS, amf),
// pinned by digest; its nginx, coturn and certbot come from the archive at
// test time, as on the node.
const ubuntuImage = "ubuntu:26.04@sha256:d66b366dac61b575bee2aef07e9c26e8070096fbe84a3d4d3eb0da069b5d76db"

// node is a container of ubuntuImage with the node's packages.
type node struct {
	t   *testing.T
	ctx context.Context
	c   *testcontainers.DockerContainer
}

func startNode(t *testing.T) *node {
	t.Helper()
	ctx := context.Background()
	c, err := testinfra.RunImage(ctx, ubuntuImage, testcontainers.WithCmd("sleep", "infinity"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	n := &node{t: t, ctx: ctx, c: c}
	n.sh("export DEBIAN_FRONTEND=noninteractive; apt-get update -qq && " +
		"apt-get install -y -qq --no-install-recommends nginx libnginx-mod-stream coturn curl openssl ca-certificates >/dev/null")
	t.Logf("node: %s", strings.TrimSpace(n.sh("nginx -v 2>&1; dpkg-query -W -f='coturn ${Version}\\n' coturn")))
	return n
}

// exec runs bash -c script with args in the container; it returns the
// output and the exit code.
func (n *node) exec(script string, args ...string) (string, int) {
	n.t.Helper()
	code, r, err := n.c.Exec(n.ctx, append([]string{"bash", "-c", script, "bash"}, args...), tcexec.Multiplexed())
	if err != nil {
		n.t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		n.t.Fatal(err)
	}
	return string(b), code
}

// sh is exec that must succeed.
func (n *node) sh(script string, args ...string) string {
	n.t.Helper()
	out, code := n.exec("set -euo pipefail\n"+script, args...)
	if code != 0 {
		n.t.Fatalf("exit %d: %s\n%s", code, script, out)
	}
	return out
}

func (n *node) put(path string, content []byte, mode int64) {
	n.t.Helper()
	n.sh("mkdir -p \"$(dirname \"$1\")\"", path)
	if err := n.c.CopyToContainer(n.ctx, content, path, mode); err != nil {
		n.t.Fatal(err)
	}
}

// putDir copies the files of the local directory dir (not its
// subdirectories) to the directory to.
func (n *node) putDir(dir, to string) {
	n.t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		n.t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			n.t.Fatal(err)
		}
		mode := int64(0o644)
		if strings.HasSuffix(e.Name(), ".sh") {
			mode = 0o755
		}
		n.put(to+"/"+e.Name(), b, mode)
	}
}

// pki is a throwaway CA and the certificates it signs.
type pki struct {
	t      *testing.T
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Pluma test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &pki{t: t, caCert: cert, caKey: key, caPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// leaf returns the certificate chain and the key of name, in PEM.
func (p *pki) leaf(name string, serial int64) (chain, key []byte) {
	p.t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		p.t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &k.PublicKey, p.caKey)
	if err != nil {
		p.t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		p.t.Fatal(err)
	}
	chain = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), p.caPEM...)
	return chain, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

// originConf is the stand-in of the origin at home (Caddy and bff's
// origin, pkg/cdn/origin.go): TLS under the origin's name, GET and HEAD of
// files with ranges, Cache-Control immutable, 403 without the access value
// of delivery 202. It refuses any header of the client the node must not
// pass home, and logs every request it serves.
const originConf = `
pid /run/origin-test.pid;
error_log stderr warn;
events {}
http {
	log_format o '$request_method $uri $http_range $status';
	server {
		listen 127.0.0.1:10443 ssl default_server;
		ssl_certificate /test/origin/cert.pem;
		ssl_certificate_key /test/origin/key.pem;
		return 421;
	}
	server {
		listen 127.0.0.1:10443 ssl;
		server_name origin.pluma.test;
		ssl_certificate /test/origin/cert.pem;
		ssl_certificate_key /test/origin/key.pem;
		access_log /test/origin/access.log o;
		root /test/origin/www;
		location / {
			if ($http_x_pluma_origin_auth != "@AUTH@") { return 403; }
			if ($http_user_agent != "") { return 400; }
			if ($http_cookie != "") { return 400; }
			if ($http_x_forwarded_for != "") { return 400; }
			add_header Cache-Control "public, max-age=2592000, immutable";
		}
	}
}
`

// answer is what curl got.
type answer struct {
	status  int
	headers map[string]string // lower-case names
	sha     string
	size    int
}

func (a answer) String() string {
	return fmt.Sprintf("%d %v (%d bytes)", a.status, a.headers, a.size)
}

// fetch runs curl in the container with args (the URL among them); every
// name of the test resolves to the container itself.
func (n *node) fetch(args ...string) answer {
	n.t.Helper()
	out := n.sh(`curl -sS -o /tmp/body -D /tmp/head --cacert /test/ca.pem \
		--resolve r1.pluma.test:443:127.0.0.1 --resolve site.test:443:127.0.0.1 \
		--resolve r1.pluma.test:80:127.0.0.1 --resolve ru.pluma.test:80:127.0.0.1 --resolve site.test:80:127.0.0.1 "$@"
cat /tmp/head
echo "SHA $(sha256sum /tmp/body | cut -d' ' -f1) $(stat -c %s /tmp/body)"`, args...)
	a := answer{headers: map[string]string{}}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "HTTP/"):
			f := strings.Fields(line)
			a.status, _ = strconv.Atoi(f[1])
			a.headers = map[string]string{}
		case strings.HasPrefix(line, "SHA "):
			f := strings.Fields(line)
			a.sha = f[1]
			a.size, _ = strconv.Atoi(f[2])
		default:
			if k, v, ok := strings.Cut(line, ":"); ok {
				a.headers[strings.ToLower(k)] = strings.TrimSpace(v)
			}
		}
	}
	return a
}

func (n *node) originRequests() int {
	n.t.Helper()
	out := n.sh("wc -l < /test/origin/access.log")
	c, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		n.t.Fatalf("access.log: %q", out)
	}
	return c
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// TestNodeNginx runs the node's nginx, rendered by deploy.sh from the
// templates, behind the owner's nginx installed by owner-nginx.sh, with
// the nginx of Ubuntu 26.04: the Secure Token of pkg/cdn's GcoreSigner,
// the ranges from the slices, the cache shared by tokens, CORS, port 80,
// and the owner's site and configuration untouched.
func TestNodeNginx(t *testing.T) {
	needBash(t)
	n := startNode(t)
	p := newPKI(t)
	e := newNodeEnv(t)
	e.set("PLUMA_RU_PUBLIC_IPV4", "127.0.0.1")
	e.set("PLUMA_RU_ORIGIN_ADDR", "127.0.0.1:10443")

	// The repository's files, as the checkout on the node.
	n.putDir(ruDir, "/src/teamgramd/deploy/ru")
	n.putDir(filepath.Join(ruDir, "..", "turn"), "/src/teamgramd/deploy/turn")

	// The owner's nginx as on the node; the package's default site goes.
	owner := ownerTree(t)
	n.sh("rm -f /etc/nginx/sites-enabled/default")
	for _, f := range []string{"nginx.conf", "stream.d/sni.conf", "sites-enabled/owner-site"} {
		n.put("/etc/nginx/"+f, []byte(owner[f]), 0o644)
	}
	chain, key := p.leaf("site.test", 2)
	n.put("/etc/ssl/owner-test/cert.pem", chain, 0o644)
	n.put("/etc/ssl/owner-test/key.pem", key, 0o600)
	n.put("/test/ca.pem", p.caPEM, 0o644)
	// The origin's certificate is trusted as Let's Encrypt's is on the node.
	n.sh("cat /test/ca.pem >> /etc/ssl/certs/ca-certificates.crt")

	// The origin.
	chain, key = p.leaf("origin.pluma.test", 3)
	n.put("/test/origin/cert.pem", chain, 0o644)
	n.put("/test/origin/key.pem", key, 0o600)
	n.put("/test/origin/nginx.conf", []byte(strings.ReplaceAll(originConf, "@AUTH@", e.vars["CDN_RU_ORIGIN_AUTH"])), 0o644)
	object := make([]byte, 2<<20+777) // three slices of 1 MiB
	if _, err := rand.Read(object); err != nil {
		t.Fatal(err)
	}
	objectPath := "/o/" + strings.Repeat("ab", 32)
	n.put("/test/origin/www"+objectPath, object, 0o644)
	canary := []byte("canary")
	n.put("/test/origin/www/c/v1", canary, 0o644)
	n.sh("nginx -c /test/origin/nginx.conf")

	// The node's files: deploy.sh render with the X-Cache-Status of the
	// test build, the layout of deploy.sh apply, the certificate as certbot
	// leaves it.
	var envText strings.Builder
	for _, name := range e.order {
		fmt.Fprintf(&envText, "%s=%s\n", name, e.vars[name])
	}
	n.put("/etc/pluma-ru/env", []byte(envText.String()), 0o600)
	n.sh(`useradd --system --user-group --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin pluma-ru
install -d -m 0755 /etc/pluma-ru /var/lib/pluma-ru /var/lib/pluma-ru/acme
install -d -m 0700 -o pluma-ru -g pluma-ru /var/cache/pluma-ru /var/cache/pluma-ru/files /var/lib/pluma-ru/nginx
bash /src/teamgramd/deploy/ru/deploy.sh render /etc/pluma-ru/env /etc/pluma-ru --cache-status-header`)
	chain, key = p.leaf("r1.pluma.test", 4)
	n.put("/etc/pluma-ru/letsencrypt/live/r1.pluma.test/fullchain.pem", chain, 0o644)
	n.put("/etc/pluma-ru/letsencrypt/live/r1.pluma.test/privkey.pem", key, 0o600)

	// The owner's nginx before Pluma, then owner-nginx.sh.
	n.sh("nginx")
	ownerSNI := n.sh("sha256sum /etc/nginx/stream.d/sni.conf")
	if out := n.sh("bash /src/teamgramd/deploy/ru/owner-nginx.sh check"); !strings.Contains(out, "state not installed") {
		t.Fatalf("check before install:\n%s", out)
	}
	t.Log(n.sh("bash /src/teamgramd/deploy/ru/owner-nginx.sh install"))
	if out := n.sh("bash /src/teamgramd/deploy/ru/owner-nginx.sh check"); !strings.Contains(out, "state installed") {
		t.Fatalf("check after install:\n%s", out)
	}
	// Installing again changes nothing.
	n.sh("bash /src/teamgramd/deploy/ru/owner-nginx.sh install")
	if c := strings.Count(n.sh("cat /etc/nginx/stream.d/sni.conf"), "pluma-sni.map"); c != 1 {
		t.Fatalf("the include line is in sni.conf %d times", c)
	}

	// The node's nginx, as pluma-ru-nginx.service starts it.
	n.sh("nginx -t -q -c /etc/pluma-ru/nginx.conf && nginx -c /etc/pluma-ru/nginx.conf")
	if got := strings.TrimSpace(n.sh("stat -c %U /var/lib/nginx/body")); got != "www-data" {
		t.Errorf("the owner's /var/lib/nginx/body belongs to %s now", got)
	}

	signer, err := cdn.NewGcoreSigner("https://r1.pluma.test", e.vars["CDN_RU_TOKEN_KEY"], time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(path string, now time.Time) string {
		u, _, err := signer.SignURL(path, now)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	now := time.Now()
	urlA := sign(objectPath, now)
	urlB := sign(objectPath, now.Add(time.Minute)) // another token of the same object

	t.Run("range, then the same range by another token from the cache", func(t *testing.T) {
		a := n.fetch("-H", "Range: bytes=100-199", urlA)
		if a.status != 206 || a.headers["content-range"] != fmt.Sprintf("bytes 100-199/%d", len(object)) || a.sha != sum(object[100:200]) {
			t.Fatalf("got %v", a)
		}
		if a.headers["x-cache-status"] != "MISS" {
			t.Errorf("first: X-Cache-Status %q", a.headers["x-cache-status"])
		}
		before := n.originRequests()
		a = n.fetch("-H", "Range: bytes=100-199", urlB)
		if a.status != 206 || a.sha != sum(object[100:200]) || a.headers["x-cache-status"] != "HIT" {
			t.Fatalf("second token: %v", a)
		}
		if after := n.originRequests(); after != before {
			t.Errorf("the second token went to the origin: %d requests, then %d", before, after)
		}
	})
	t.Run("range across slices", func(t *testing.T) {
		a := n.fetch("-H", "Range: bytes=1048000-1049999", urlA)
		if a.status != 206 || a.headers["content-range"] != fmt.Sprintf("bytes 1048000-1049999/%d", len(object)) || a.sha != sum(object[1048000:1050000]) {
			t.Fatalf("got %v", a)
		}
	})
	t.Run("suffix range", func(t *testing.T) {
		a := n.fetch("-H", "Range: bytes=-50", urlA)
		if a.status != 206 || a.headers["content-range"] != fmt.Sprintf("bytes %d-%d/%d", len(object)-50, len(object)-1, len(object)) || a.sha != sum(object[len(object)-50:]) {
			t.Fatalf("got %v", a)
		}
	})
	t.Run("whole object", func(t *testing.T) {
		a := n.fetch(urlA)
		if a.status != 200 || a.sha != sum(object) || a.size != len(object) {
			t.Fatalf("got %v", a)
		}
		if !strings.Contains(a.headers["cache-control"], "immutable") || a.headers["etag"] == "" {
			t.Errorf("the origin's Cache-Control and ETag are not passed: %v", a)
		}
		if a.headers["server"] != "nginx" {
			t.Errorf("Server %q, want nginx without a version", a.headers["server"])
		}
		if _, ok := a.headers["access-control-allow-origin"]; ok {
			t.Errorf("CORS without an Origin: %v", a)
		}
	})
	t.Run("HEAD", func(t *testing.T) {
		a := n.fetch("-I", urlA)
		if a.status != 200 || a.headers["content-length"] != strconv.Itoa(len(object)) {
			t.Fatalf("got %v", a)
		}
	})
	t.Run("canary", func(t *testing.T) {
		a := n.fetch(sign("/c/v1", now))
		if a.status != 200 || a.sha != sum(canary) {
			t.Fatalf("got %v", a)
		}
	})
	t.Run("refusals", func(t *testing.T) {
		u, err := url.Parse(urlA)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		md5 := []byte(q.Get("md5"))
		md5[0] ^= 1
		q.Set("md5", string(md5))
		tampered := *u
		tampered.RawQuery = q.Encode()
		noToken := *u
		noToken.RawQuery = "expires=" + q.Get("expires")
		before := n.originRequests()
		for _, c := range []struct {
			name   string
			args   []string
			status int
		}{
			{"tampered token", []string{tampered.String()}, 403},
			{"expired token", []string{sign(objectPath, now.Add(-2*time.Hour))}, 410},
			{"no token", []string{noToken.String()}, 403},
			{"token of another path", []string{strings.Replace(urlA, objectPath, "/o/"+strings.Repeat("cd", 32), 1)}, 403},
			{"another path", []string{"https://r1.pluma.test/x"}, 404},
			{"upper-case id", []string{"https://r1.pluma.test/o/" + strings.Repeat("AB", 32)}, 404},
			{"POST", []string{"-X", "POST", urlA}, 405},
		} {
			if a := n.fetch(c.args...); a.status != c.status {
				t.Errorf("%s: %v, want %d", c.name, a, c.status)
			} else if c.status == 405 && a.headers["allow"] != "GET, HEAD, OPTIONS" {
				t.Errorf("%s: Allow %q", c.name, a.headers["allow"])
			}
		}
		if after := n.originRequests(); after != before {
			t.Errorf("refused requests reached the origin: %d, then %d", before, after)
		}
	})
	t.Run("CORS", func(t *testing.T) {
		a := n.fetch("-X", "OPTIONS", "-H", "Origin: https://web.pluma.test", "-H", "Access-Control-Request-Method: GET",
			"-H", "Access-Control-Request-Headers: range", "https://r1.pluma.test"+objectPath)
		if a.status != 204 || a.headers["access-control-allow-origin"] != "https://web.pluma.test" ||
			a.headers["access-control-allow-methods"] != "GET, HEAD" || a.headers["access-control-allow-headers"] != "Range" {
			t.Errorf("preflight: %v", a)
		}
		a = n.fetch("-H", "Origin: https://ru.pluma.test", "-H", "Range: bytes=0-9", urlA)
		if a.status != 206 || a.headers["access-control-allow-origin"] != "https://ru.pluma.test" ||
			a.headers["access-control-expose-headers"] != "Content-Range, Content-Length" || a.headers["vary"] != "Origin" {
			t.Errorf("the edge's site: %v", a)
		}
		if _, ok := a.headers["access-control-allow-methods"]; ok {
			t.Errorf("preflight headers on a GET: %v", a)
		}
		a = n.fetch("-H", "Origin: https://edge.gcore.test", "-H", "Range: bytes=0-9", urlA)
		if a.status != 206 || a.headers["access-control-allow-origin"] != "https://edge.gcore.test" {
			t.Errorf("the Gcore edge's site: %v", a)
		}
		a = n.fetch("-H", "Origin: https://evil.test", "-H", "Range: bytes=0-9", urlA)
		if _, ok := a.headers["access-control-allow-origin"]; ok || a.status != 206 {
			t.Errorf("another site: %v", a)
		}
	})
	t.Run("port 80 and the owner's site", func(t *testing.T) {
		n.put("/var/lib/pluma-ru/acme/.well-known/acme-challenge/token1", []byte("token1.thumb"), 0o644)
		a := n.fetch("http://r1.pluma.test/.well-known/acme-challenge/token1")
		if a.status != 200 || a.sha != sum([]byte("token1.thumb")) {
			t.Errorf("the challenge of delivery 202: %v", a)
		}
		if a = n.fetch("http://ru.pluma.test/x?y=1"); a.status != 301 || a.headers["location"] != "https://ru.pluma.test/x?y=1" {
			t.Errorf("port 80 of the edge: %v", a)
		}
		if a = n.fetch("http://site.test/"); a.status != 301 || a.headers["location"] != "https://site.test/" {
			t.Errorf("port 80 of the owner's site: %v", a)
		}
		if a = n.fetch("https://site.test/"); a.status != 200 || a.sha != sum([]byte("owner site\n")) {
			t.Errorf("the owner's site through the SNI map: %v", a)
		}
	})

	t.Run("coturn takes turn.conf", func(t *testing.T) {
		out, code := n.exec("timeout 3 turnserver -c /etc/pluma-ru/turn.conf --pidfile=/tmp/turn.pid > /tmp/turn.log 2>&1; echo \"exit $?\"; cat /tmp/turn.log")
		if code != 0 || !strings.Contains(out, "exit 124") {
			t.Fatalf("turnserver did not keep running:\n%s", out)
		}
		for _, bad := range []string{"Bad configuration format", "CONFIG ERROR", "unknown option"} {
			if strings.Contains(out, bad) {
				t.Errorf("turnserver: %q in\n%s", bad, out)
			}
		}
		for _, s := range e.secrets() {
			if strings.Contains(out, s) {
				t.Error("turnserver printed the secret")
			}
		}
	})

	// owner-nginx.sh remove leaves the owner's configuration as it was.
	t.Log(n.sh("bash /src/teamgramd/deploy/ru/owner-nginx.sh remove"))
	if got := n.sh("sha256sum /etc/nginx/stream.d/sni.conf"); got != ownerSNI {
		t.Errorf("sni.conf after remove differs from the owner's")
	}
	if out, code := n.exec("ls /etc/nginx/stream.d/pluma-* /etc/nginx/conf.d/pluma-* 2>&1"); code == 0 {
		t.Errorf("files of Pluma are left:\n%s", out)
	}
	if out := n.sh("nginx -T 2>/dev/null"); strings.Contains(out, "pluma") {
		t.Error("the owner's nginx still reads files of Pluma")
	}
	if a := n.fetch("https://site.test/"); a.status != 200 {
		t.Errorf("the owner's site after remove: %v", a)
	}
}

// TestAlertRulesFire runs promtool of the Prometheus image at home on the
// alert rules of the node and testdata/ru-node.test.yml.
func TestAlertRulesFire(t *testing.T) {
	ctx := context.Background()
	image, err := testinfra.ComposeImage("prometheus")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := os.ReadFile(alertRulesFile)
	if err != nil {
		t.Fatal(err)
	}
	tests, err := os.ReadFile(filepath.Join("testdata", "ru-node.test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := testinfra.RunImage(ctx, image,
		testcontainers.WithEntrypoint("promtool"),
		testcontainers.WithCmd("test", "rules", "/rules/ru-node.test.yml"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{Reader: strings.NewReader(string(rules)), ContainerFilePath: "/rules/ru-node.yml", FileMode: 0o644},
			testcontainers.ContainerFile{Reader: strings.NewReader(string(tests)), ContainerFilePath: "/rules/ru-node.test.yml", FileMode: 0o644},
		),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(2*time.Minute)),
	)
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	}
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := c.Logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(logs)
	if st.ExitCode != 0 {
		t.Fatalf("promtool test rules: exit %d\n%s", st.ExitCode, out)
	}
	t.Logf("promtool: %s", strings.TrimSpace(string(out)))
}

// systemctlStandIn records the calls of deploy.sh apply in a container
// without systemd: every unit is inactive and not enabled.
const systemctlStandIn = `#!/bin/sh
echo "$*" >> /tmp/systemctl.log
case "$1" in
is-active) [ "$2" = --quiet ] || echo inactive; exit 3 ;;
is-enabled) echo disabled; exit 1 ;;
esac
exit 0
`

// TestDeployApply runs deploy.sh apply on Ubuntu 26.04 with systemctl
// recorded: the missing packages, the layout, the check of nginx before
// the first certificate, the files and units with their modes, a second
// run that changes nothing, and the start of nginx once the certificate
// is there.
func TestDeployApply(t *testing.T) {
	needBash(t)
	n := startNode(t)
	e := newNodeEnv(t)
	n.putDir(ruDir, "/src/teamgramd/deploy/ru")
	n.putDir(filepath.Join(ruDir, "..", "turn"), "/src/teamgramd/deploy/turn")
	n.put("/usr/local/bin/systemctl", []byte(systemctlStandIn), 0o755)
	var envText strings.Builder
	for _, name := range e.order {
		fmt.Fprintf(&envText, "%s=%s\n", name, e.vars[name])
	}
	n.put("/etc/pluma-ru/env", []byte(envText.String()), 0o600)

	noSecrets := func(out string) {
		t.Helper()
		for _, s := range e.secrets() {
			if strings.Contains(out, s) {
				t.Fatalf("deploy.sh printed a secret:\n%s", out)
			}
		}
	}
	apply := func() (string, string) {
		t.Helper()
		n.sh("rm -f /tmp/systemctl.log; touch /tmp/systemctl.log")
		out := n.sh("bash /src/teamgramd/deploy/ru/deploy.sh apply 2>&1")
		noSecrets(out)
		return out, n.sh("cat /tmp/systemctl.log")
	}

	// A world-readable env file is refused before anything happens.
	n.sh("chmod 0644 /etc/pluma-ru/env")
	if out, code := n.exec("bash /src/teamgramd/deploy/ru/deploy.sh apply 2>&1"); code == 0 || !strings.Contains(out, "mode 0600") {
		t.Fatalf("apply with env 0644: exit %d\n%s", code, out)
	}
	n.sh("chmod 0600 /etc/pluma-ru/env")

	out, calls := apply()
	t.Logf("first apply:\n%s", out)
	if !strings.Contains(out, "installing ") || !strings.Contains(out, "no certificate for r1.pluma.test yet") {
		t.Errorf("first apply:\n%s", out)
	}
	for _, want := range []string{
		"mask --quiet coturn.service", "mask --quiet prometheus-node-exporter.service",
		"daemon-reload", "restart pluma-ru-turn.service", "restart pluma-ru-node-exporter.service", "restart pluma-ru-wgstat.timer",
		"enable --quiet --now pluma-ru-wgstat.timer pluma-ru-certbot.timer",
	} {
		if !strings.Contains(calls, want+"\n") {
			t.Errorf("no systemctl %s in\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "pluma-ru-nginx.service") && !strings.Contains(calls, "is-active pluma-ru-nginx") {
		t.Errorf("nginx was started without a certificate:\n%s", calls)
	}
	modes := n.sh(`stat -c '%n %a %U:%G' /etc/pluma-ru/nginx.conf /etc/pluma-ru/files.conf /etc/pluma-ru/secrets.conf \
		/etc/pluma-ru/turn.conf /etc/pluma-ru/owner/pluma-sni.map /opt/pluma-ru/bin/wgstat.sh \
		/etc/systemd/system/pluma-ru-nginx.service /var/cache/pluma-ru/files /var/lib/pluma-ru/nginx /var/lib/pluma-ru/acme`)
	for _, want := range []string{
		"/etc/pluma-ru/nginx.conf 644 root:root", "/etc/pluma-ru/files.conf 644 root:root",
		"/etc/pluma-ru/secrets.conf 600 root:root", "/etc/pluma-ru/turn.conf 640 root:turnserver",
		"/etc/pluma-ru/owner/pluma-sni.map 644 root:root", "/opt/pluma-ru/bin/wgstat.sh 755 root:root",
		"/etc/systemd/system/pluma-ru-nginx.service 644 root:root", "/var/cache/pluma-ru/files 700 pluma-ru:pluma-ru",
		"/var/lib/pluma-ru/nginx 700 pluma-ru:pluma-ru", "/var/lib/pluma-ru/acme 755 root:root",
	} {
		if !strings.Contains(modes, want+"\n") {
			t.Errorf("no %q in\n%s", want, modes)
		}
	}
	if out := n.sh("ls -A /etc/pluma-ru"); strings.Contains(out, ".staging") {
		t.Errorf("a staging directory is left:\n%s", out)
	}

	// Nothing changed: nothing is restarted.
	out, calls = apply()
	if strings.Contains(out, "installing ") || strings.Contains(calls, "restart") || strings.Contains(calls, "daemon-reload") {
		t.Errorf("second apply changed something:\n%s\n%s", out, calls)
	}
	if !strings.Contains(calls, "start pluma-ru-turn.service\n") {
		t.Errorf("second apply did not make sure TURN runs:\n%s", calls)
	}

	// A template nginx refuses changes nothing in place.
	before := n.sh("sha256sum /etc/pluma-ru/files.conf")
	n.sh(`sed -i 's/max_ranges 1;/max_ranges one;/' /src/teamgramd/deploy/ru/files.conf.tmpl`)
	if out, code := n.exec("bash /src/teamgramd/deploy/ru/deploy.sh apply 2>&1"); code == 0 || !strings.Contains(out, "nginx -t failed on the new files") {
		t.Errorf("apply of a broken template: exit %d\n%s", code, out)
	}
	if after := n.sh("sha256sum /etc/pluma-ru/files.conf"); after != before {
		t.Error("a broken template replaced files.conf")
	}
	n.sh(`sed -i 's/max_ranges one;/max_ranges 1;/' /src/teamgramd/deploy/ru/files.conf.tmpl`)

	// With the certificate nginx starts.
	p := newPKI(t)
	chain, key := p.leaf("r1.pluma.test", 2)
	n.put("/etc/pluma-ru/letsencrypt/live/r1.pluma.test/fullchain.pem", chain, 0o644)
	n.put("/etc/pluma-ru/letsencrypt/live/r1.pluma.test/privkey.pem", key, 0o600)
	out, calls = apply()
	if strings.Contains(out, "no certificate") || !strings.Contains(calls, "restart pluma-ru-nginx.service\n") {
		t.Errorf("apply with the certificate:\n%s\n%s", out, calls)
	}

	// The units as systemd reads them, with the binaries and users the
	// packages brought.
	n.sh("rm /usr/local/bin/systemctl; DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends systemd >/dev/null")
	if out, code := n.exec("cd /etc/systemd/system && systemd-analyze verify --man=no pluma-ru-*.service pluma-ru-*.timer 2>&1"); code != 0 || strings.TrimSpace(out) != "" {
		t.Errorf("systemd-analyze verify: exit %d\n%s", code, out)
	}
	// It does find a broken unit here.
	n.put("/tmp/units/broken.service", []byte("[Service]\nExecStart=/nonexistent\nUser=nobody-here\n"), 0o644)
	if out, code := n.exec("systemd-analyze verify --man=no /tmp/units/broken.service 2>&1"); code == 0 {
		t.Errorf("systemd-analyze verify took a broken unit:\n%s", out)
	}
}
