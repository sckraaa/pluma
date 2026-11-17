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

package main

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/cdn"
)

const testCdnBase = "https://cdn-test.example.com"

// randomHex returns n random bytes in hex: a throwaway secret of a test.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// canaryURLOf returns the URL cdn canary-url printed, and checks that the
// curl line fetches it with the range of the health check.
func canaryURLOf(t *testing.T, out string) *url.URL {
	t.Helper()
	var signed string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, testCdnBase+"/") {
			signed = line
		}
	}
	if signed == "" {
		t.Fatalf("no URL in the output:\n%s", out)
	}
	curl := "curl -sS -r 131072-135167 -o /dev/null -w '%{http_code} %{size_download}\\n' '" + signed + "'"
	expect := `Expect "206 4096"`
	if strings.Contains(out, "expired at") {
		expect = `Expect "403 or 410"`
	}
	if !strings.Contains(out, curl) || !strings.Contains(out, expect) {
		t.Fatalf("no curl line of the canary range in the output:\n%s", out)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// cdn canary-url signs the canary as Gcore checks a Secure Token
// (https://docs.gcore.com/cdn/cdn-resource-options/security/use-a-secure-token/configure-and-use-secure-token.md:
// md5 = base64url(MD5(expires + path + " " + key)) without "="), for an
// hour, from the CDN variables of both bff.yaml files.
func TestCdnCanaryURLGcore(t *testing.T) {
	setBffEnv(t)
	key := randomHex(t, 16)
	t.Setenv("CDN_GCORE_BASE_URL", testCdnBase)
	t.Setenv("CDN_GCORE_TOKEN_KEY", key)
	t.Setenv("CDN_ORIGIN_AUTH", randomHex(t, 32))
	now := time.Unix(1_800_000_000, 0)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var out bytes.Buffer
		if err := cdnCommand([]string{"cdn", "canary-url", "201"}, f, &out, now); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		u := canaryURLOf(t, out.String())
		exp := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
		sum := md5.Sum([]byte(exp + "/c/v1 " + key))
		want := base64.RawURLEncoding.EncodeToString(sum[:])
		if u.Path != cdn.CanaryPath || u.Query().Get("expires") != exp || u.Query().Get("md5") != want {
			t.Errorf("%s: URL %s, want /c/v1 with md5 %s and expires %s", f, u, want, exp)
		}
		if !strings.Contains(out.String(), "Delivery 201 (gcore, gcore-md5)") {
			t.Errorf("%s: the delivery is not named:\n%s", f, out.String())
		}

		// -expired: a valid token whose expires has passed.
		out.Reset()
		if err := cdnCommand([]string{"cdn", "canary-url", "-expired", "201"}, f, &out, now); err != nil {
			t.Fatalf("%s -expired: %v", f, err)
		}
		u = canaryURLOf(t, out.String())
		e, err := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
		sum = md5.Sum([]byte(u.Query().Get("expires") + "/c/v1 " + key))
		if err != nil || e >= now.Unix() || u.Query().Get("md5") != base64.RawURLEncoding.EncodeToString(sum[:]) {
			t.Errorf("%s -expired: URL %s, want a valid token that expired before %d", f, u, now.Unix())
		}
	}
}

// writeCdnConfig writes a bff.yaml with one delivery, 201.
func writeCdnConfig(t *testing.T, signer, base, key, ttl string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bff.yaml")
	yaml := "Cdn:\n" +
		"  Deliveries:\n" +
		"    - Id: 201\n" +
		"      Name: gcore\n" +
		"      Signer: \"" + signer + "\"\n" +
		"      BaseURL: \"" + base + "\"\n" +
		"      Key: \"" + key + "\"\n" +
		"  Origin:\n" +
		"    AuthValue: \"" + randomHex(t, 32) + "\"\n" +
		"  UrlTTL: \"" + ttl + "\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A path-hmac URL is accepted by the origin's check with the TTL of the
// config: for an hour when CDN_URL_TTL is longer, within 2·CDN_URL_TTL
// when it is shorter (the e2e stand's 20s).
func TestCdnCanaryURLPathHMAC(t *testing.T) {
	keyBytes := make([]byte, cdn.PathHMACKeySize)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	now := time.Unix(1_800_000_123, 0)
	for _, tc := range []struct {
		ttl    time.Duration
		within time.Duration
	}{{6 * time.Hour, 2 * time.Hour}, {20 * time.Second, 40 * time.Second}} {
		cfg := writeCdnConfig(t, cdn.SignerPathHMAC, testCdnBase, key, tc.ttl.String())
		var out bytes.Buffer
		if err := cdnCommand([]string{"cdn", "canary-url", "201"}, cfg, &out, now); err != nil {
			t.Fatalf("ttl %v: %v", tc.ttl, err)
		}
		u := canaryURLOf(t, out.String())
		origin, err := cdn.NewPathHMACSigner(testCdnBase, keyBytes, tc.ttl)
		if err != nil {
			t.Fatal(err)
		}
		object, bucket, err := origin.Verify(u.Path, now)
		if err != nil || object != cdn.CanaryPath {
			t.Fatalf("ttl %v: the origin refuses %s: %q %v", tc.ttl, u.Path, object, err)
		}
		if left := time.Duration(bucket-now.Unix()) * time.Second; left <= 0 || left > tc.within {
			t.Errorf("ttl %v: the URL works for %v, want at most %v", tc.ttl, left, tc.within)
		}
		if _, _, err := origin.Verify(u.Path, now.Add(tc.within+time.Second)); !errors.Is(err, cdn.ErrURLExpired) {
			t.Errorf("ttl %v: after %v the origin answers %v, want expired", tc.ttl, tc.within, err)
		}

		// -expired: the origin answers 410 (expired), not 403.
		out.Reset()
		if err := cdnCommand([]string{"cdn", "canary-url", "-expired", "201"}, cfg, &out, now); err != nil {
			t.Fatalf("ttl %v -expired: %v", tc.ttl, err)
		}
		u = canaryURLOf(t, out.String())
		if _, _, err := origin.Verify(u.Path, now); !errors.Is(err, cdn.ErrURLExpired) {
			t.Errorf("ttl %v -expired: the origin answers %v, want expired", tc.ttl, err)
		}
	}
}

// What cdn canary-url refuses: bad usage (2); a CDN that is off, a delivery
// that is not on, a config bff would not start with (1).
func TestCdnCanaryURLRefused(t *testing.T) {
	for _, args := range [][]string{{"cdn"}, {"cdn", "canary-url"}, {"cdn", "canary-url", "x"}, {"cdn", "url", "201"}, {"cdn", "canary-url", "201", "202"}, {"cdn", "canary-url", "-expired"}, {"cdn", "canary-url", "-old", "201"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut, nil); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	key := randomHex(t, 16)
	for _, tc := range []struct {
		name, cfg, args, want string
	}{
		{"off", writeCdnConfig(t, "", "", "", ""), "201", "the CDN is off"},
		{"unknown delivery", writeCdnConfig(t, "", testCdnBase, key, ""), "202", "delivery 202 is unknown or off; the deliveries on: 201"},
		{"http without the stand", writeCdnConfig(t, "", "http://cdn-test.example.com", key, ""), "201", "CDN_DEV_ALLOW_HTTP"},
		{"short key", writeCdnConfig(t, "", testCdnBase, "abc", ""), "201", "gcore-md5 key of 3 characters"},
	} {
		var out, errOut bytes.Buffer
		if code := run([]string{"-f", tc.cfg, "cdn", "canary-url", tc.args}, &out, &errOut, nil); code != 1 || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%s: exit %d, %q; want 1 and %q", tc.name, code, errOut.String(), tc.want)
		}
		if strings.Contains(errOut.String(), key) {
			t.Errorf("%s: the key is printed: %q", tc.name, errOut.String())
		}
	}
}
