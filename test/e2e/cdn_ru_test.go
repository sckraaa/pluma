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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Delivery 202 of the Russian node (task 109, ADR 0072 §8): a client whose
// address is in Russia is redirected to it, any other to 201 (CDN_ROUTES
// "RU:202,201;*:201" of the stand). The node is test/e2e/rufiles: the
// node's nginx of Ubuntu 26.04 with the files deploy.sh renders from
// teamgramd/deploy/ru, behind a stand-in of the owner's stream, pulling
// from bff's origin through the stand's Caddy with its own
// X-Pluma-Origin-Auth. The clients from Russia come through the Edge entry
// of the gateway with an address of the stand's GeoIP database (below
// 128.0.0.0 Russia), as through the node's edge.

const (
	// ruFilesDC is delivery 202.
	ruFilesDC = 202
)

var (
	// ruFilesAddr is the loopback address of rufiles' 443 when the run
	// started (it says the stand has rufiles; servicePort has the current
	// one), ruFilesDomain
	// the name of delivery 202, ruFilesCA the run's CA of its certificate
	// (run.sh, gen_rufiles).
	ruFilesAddr   = os.Getenv("PLUMA_E2E_RU_FILES")
	ruFilesDomain = os.Getenv("PLUMA_E2E_RU_FILES_DOMAIN")
	ruFilesCA     = os.Getenv("PLUMA_E2E_RU_FILES_CA")
)

func needRuFiles(t *testing.T) {
	t.Helper()
	if ruFilesAddr == "" || ruFilesDomain == "" || ruFilesCA == "" {
		t.Skip("PLUMA_E2E_RU_FILES* are not set (run.sh test sets them)")
	}
}

// ruFilesClient is the HTTP side of a patched client in Russia: the name
// of delivery 202 is dialled at rufiles' loopback port, and its
// certificate is checked against the run's CA.
func ruFilesClient(t *testing.T) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(ruFilesCA)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		t.Fatal("no certificate in the CA of delivery 202")
	}
	d := &net.Dialer{}
	// Not ruFilesAddr, the port at the start of the run: rufiles is
	// recreated with web, on another port.
	ruAddr := servicePort(t, "rufiles", "443")
	c := mtclient.NewCdnHTTPClient(func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == ruFilesDomain+":443" {
			addr = ruAddr
		}
		return d.DialContext(ctx, network, addr)
	})
	c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return c
}

// loginRussia logs in from a patched client at clientAddr in Russia,
// through the Edge entry.
func loginRussia(ctx context.Context, t *testing.T, phone, firstName, clientAddr string) *user {
	t.Helper()
	cfg := edgeConfig(t, "e2e "+firstName+" in Russia", edgeURL(t), edgeHeader(clientAddr))
	cfg.Params = mtclient.PlumaCdnParams()
	return loginWith(ctx, t, phone, firstName, cfg)
}

// ruRedirect waits for the redirect of loc at offset to delivery 202 and
// checks its URL: the name of the delivery, the path of the file's id and
// the Secure Token of Gcore, which the node's nginx checks.
func ruRedirect(ctx context.Context, t *testing.T, s cdnStand, u *user, loc *mtproto.InputFileLocation, offset int64) *mtclient.HttpsRedirect {
	t.Helper()
	f := waitCdnAnswer(ctx, t, u, loc, offset, true, max(cdnRedirectWithin, s.healthWithin()))
	r, err := mtclient.ParseHttpsRedirect(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.DC != ruFilesDC {
		t.Fatalf("a client from Russia redirected to %d, want %d", r.DC, ruFilesDC)
	}
	ru, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := ru.Query()
	if ru.Scheme != "https" || ru.Host != ruFilesDomain || ru.Path != "/o/"+fmt.Sprintf("%x", r.ID) || q.Get("md5") == "" ||
		q.Get("expires") != strconv.FormatInt(r.Expires.Unix(), 10) {
		t.Fatalf("the URL of delivery 202 is not https://%s/o/<id> with the Secure Token", ruFilesDomain)
	}
	return r
}

// ruGet asks for the range from-to of the object at rawURL, as the client
// does, and returns the status, the headers and the body.
func ruGet(ctx context.Context, t *testing.T, c *http.Client, rawURL string, from, to int64) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if from >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to))
	}
	req.Header.Set("User-Agent", "Pluma")
	res, err := c.Do(req)
	if err != nil {
		t.Fatalf("delivery 202: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("delivery 202: the body: %v", err)
	}
	return res.StatusCode, res.Header, body
}

// TestHttpsCdnRussia: Alice sends a file of 3 MB; Bob, in Russia, is
// redirected to delivery 202 and downloads it from the node's nginx by
// ranges (206 with the exact Content-Range, the hashes of the home agree),
// while Alice, of no known country, is redirected to 201. Carol, also in
// Russia, gets another token of the same object and is served from the
// node's cache; a tampered token is 403, none 403, an expired one 410. The
// origin counts the bytes the node pulled for 202, and no secret of 202 is
// in the logs.
func TestHttpsCdnRussia(t *testing.T) {
	needEdges(t)
	needRuFiles(t)
	s := httpsCdnStand(t)
	isolateFakeCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := loginPatched(ctx, t, phones[0], "Alice")
	bob := loginRussia(ctx, t, phones[1], "Bob", "5.8.16.40")
	carol := loginRussia(ctx, t, phones[2], "Carol", "5.8.16.41")
	getState(ctx, t, bob)
	getState(ctx, t, carol)
	usageBefore := ruOriginBytes(t)
	// bff checks 202 at rufiles:443 with the SNI of its name: the node, its
	// certificate, its Secure Token and its way to the origin. Until then a
	// client from Russia would be redirected to 201, and held there.
	waitRuHealthy(t, s.healthWithin()+30*time.Second)

	data := randomBlob(t, 3<<20+5)
	sent := sendBigDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	loc := documentFileLocation(receiveDocument(ctx, t, bob, sent.GetId()))
	r := ruRedirect(ctx, t, s, bob, loc, 0)

	// The whole file by 128 KB, as tdesktop and Android ask, through the
	// fetcher of the patched clients: 206, the exact Content-Range and
	// Content-Length, and the hashes of the home for every piece.
	client := ruFilesClient(t)
	got, err := mtclient.NewHttpsDownload(bob.c, &mtclient.HttpsFetcher{HTTP: client}, r, int64(len(data))).All(ctx, 128<<10)
	if err != nil {
		t.Fatalf("through delivery 202: %v (%v)", err, errors.Unwrap(err))
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("through delivery 202: %d bytes, not the file", len(got))
	}

	// A range across two slices of the node's cache, and the end of the
	// file, as the node's nginx answers them.
	size := int64(len(data))
	for _, rg := range [][2]int64{{1048000, 1049999}, {size - 50, size - 1}} {
		code, h, body := ruGet(ctx, t, client, r.URL, rg[0], rg[1])
		if code != http.StatusPartialContent || h.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", rg[0], rg[1], size) ||
			int64(len(body)) != rg[1]-rg[0]+1 {
			t.Errorf("range %d-%d: %d, Content-Range %q, %d bytes", rg[0], rg[1], code, h.Get("Content-Range"), len(body))
		}
	}

	// Alice, of no known country, is redirected to 201 for the same file.
	if ra := httpsRedirect(ctx, t, s, alice, documentFileLocation(sent), 0); !bytes.Equal(ra.ID, r.ID) {
		t.Error("the sender was redirected to another copy of the file")
	}

	// Carol's token is another one of the same object: the node serves it
	// from its cache.
	var rc *mtclient.HttpsRedirect
	for deadline := time.Now().Add(30 * time.Second); ; {
		rc = ruRedirect(ctx, t, s, carol, loc, 0)
		if rc.URL != r.URL {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Carol keeps getting Bob's URL")
		}
		sleepCtx(ctx, t, time.Second)
	}
	code, h, body := ruGet(ctx, t, client, rc.URL, 0, 131071)
	if code != http.StatusPartialContent || h.Get("X-Cache-Status") != "HIT" || len(body) != 128<<10 {
		t.Errorf("another token of the object: %d, X-Cache-Status %q, %d bytes; want 206 from the cache", code, h.Get("X-Cache-Status"), len(body))
	}

	// Refusals of the node: a tampered token and none are 403, an expired
	// one 410.
	u, err := url.Parse(rc.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	md5 := []byte(q.Get("md5"))
	md5[0] ^= 1
	tampered, noToken := *u, *u
	tampered.RawQuery = "md5=" + string(md5) + "&expires=" + q.Get("expires")
	noToken.RawQuery = "expires=" + q.Get("expires")
	for name, c := range map[string]struct {
		url  string
		want int
	}{
		"tampered token": {tampered.String(), http.StatusForbidden},
		"no token":       {noToken.String(), http.StatusForbidden},
	} {
		if code, _, _ := ruGet(ctx, t, client, c.url, 0, 99); code != c.want {
			t.Errorf("%s: %d, want %d", name, code, c.want)
		}
	}
	sleepCtx(ctx, t, time.Until(rc.Expires)+2*time.Second)
	if code, _, _ := ruGet(ctx, t, client, rc.URL, 0, 99); code != http.StatusGone {
		t.Errorf("an expired token: %d, want 410", code)
	}

	// The origin counted what the node pulled for 202: at least the file,
	// written within a minute (usageFlushEvery of pkg/cdn).
	deadline := time.Now().Add(90 * time.Second)
	for ruOriginBytes(t)-usageBefore < size {
		if time.Now().After(deadline) {
			t.Fatalf("the origin counted %d bytes for 202, want at least %d", ruOriginBytes(t)-usageBefore, size)
		}
		sleepCtx(ctx, t, 5*time.Second)
	}

	ruAuth := strings.TrimSpace(stackOut(t, "exec", "-T", "bff", "printenv", "CDN_RU_ORIGIN_AUTH"))
	ruKey := strings.TrimSpace(stackOut(t, "exec", "-T", "bff", "printenv", "CDN_RU_TOKEN_KEY"))
	if len(ruAuth) != 64 || ruKey == "" {
		t.Fatal("bff has no CDN_RU_ORIGIN_AUTH or CDN_RU_TOKEN_KEY of the run")
	}
	notInLogs(t, map[string]string{
		"signed URL of 202": r.URL, "URL signature of 202": q.Get("md5"),
		"origin header of 202": ruAuth, "Secure Token key of 202": ruKey,
	}, map[string][]byte{"file_token": r.FileToken, "key": r.Key})
	if logs := stackOut(t, "logs", "--no-color", "rufiles"); strings.Contains(logs, ruAuth) || strings.Contains(logs, ruKey) {
		t.Error("the logs of the node hold a secret of 202")
	}
}

// waitRuHealthy waits for bff's check of delivery 202 to pass, as
// cdn_deliveries records it, for within.
func waitRuHealthy(t *testing.T, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		out := stackSQL(t, fmt.Sprintf("SELECT CONCAT(healthy_until, ' ', unhealthy_reason) FROM cdn_deliveries WHERE dc_id = %d", ruFilesDC))
		until, reason, _ := strings.Cut(out, " ")
		if ms, err := strconv.ParseInt(until, 10, 64); err == nil && ms > time.Now().UnixMilli() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery 202 is not healthy after %v: %q", within, reason)
		}
		time.Sleep(time.Second)
	}
}

// ruOriginBytes is origin_bytes of delivery 202 in the month, 0 before
// the first flush.
func ruOriginBytes(t *testing.T) int64 {
	t.Helper()
	out := stackSQL(t, fmt.Sprintf("SELECT COALESCE(SUM(origin_bytes), 0) FROM cdn_delivery_usage WHERE dc_id = %d AND month = '%s'",
		ruFilesDC, time.Now().UTC().Format("2006-01")))
	n, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		t.Fatalf("origin_bytes of 202: %q", out)
	}
	return n
}
