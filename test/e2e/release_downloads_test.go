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
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
)

// The releases of the client apps through the caches of /dl/ (task 115):
// the stand's releases service behind Caddy, as at home; the origin of the
// Gcore edge, which the tests reach as the resource does (its secret); and
// the Russian node's cache in rufiles, which pulls /dl/ through the stand's
// Caddy under the origin's name (cdn-origin.caddy), as amf pulls it through
// the tunnel.

// The release the test uploads: a desktop build for Linux, its installer
// and its update package, the same bytes on every run, so that an upload
// to a stand that already has it is CI's repeat (409), not another build.
const (
	dlVersion   = "7.0.9.00115"
	dlBuild     = "115"
	dlInstaller = "Pluma-" + dlVersion + ".tar.xz"
	dlUpdate    = "tlinuxupd115"
	// dlFilePath is the installer's path; the update package's is under
	// /dl/desktop, where the updater asks for it.
	dlFilePath   = "/dl/linux/" + dlVersion + "/" + dlInstaller
	dlUpdatePath = "/dl/desktop/linux/" + dlVersion + "/" + dlUpdate
)

// dlInstallerBytes is the installer: 3 MiB and a bit, from a fixed seed.
func dlInstallerBytes() []byte {
	r := rand.New(rand.NewChaCha8([32]byte{1, 1, 5}))
	b := make([]byte, 3<<20+1157)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// uploadRelease uploads the release to the site's /dl/upload as CI does.
func uploadRelease(t *testing.T, site *http.Client, installer, update []byte) {
	t.Helper()
	token, err := os.ReadFile(os.Getenv("PLUMA_E2E_RELEASES_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, f := range [][2]string{{"platform", "linux"}, {"version", dlVersion}, {"build", dlBuild}} {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []struct {
		field, name string
		data        []byte
	}{{"installer", dlInstaller, installer}, {"update", dlUpdate, update}} {
		w, err := mw.CreateFormFile(f.field, f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://"+siteHost+"/dl/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	resp, err := site.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("upload: %d %.200s", resp.StatusCode, answer)
	}
}

// dlAnswer is an answer with its body read.
type dlAnswer struct {
	code   int
	header http.Header
	body   []byte
}

func dlDo(t *testing.T, c *http.Client, method, url string, header http.Header) dlAnswer {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return dlAnswer{resp.StatusCode, resp.Header, body}
}

// TestReleaseDownloads: a release CI uploads to the site is served by the
// caches of /dl/. The site's latest link sends the browser to the CDN, the
// mirror's stays on the mirror. The origin of the Gcore edge serves the
// files with the service's Cache-Control (a file of a version immutable,
// the updater's list no-cache, a latest link no-store on the same name)
// and lets Gcore keep nothing else: /apiws, the 403 without the secret,
// writes and errors say no-store. The Russian node's cache keeps the files
// of a version and passes the lists and links every time.
func TestReleaseDownloads(t *testing.T) {
	needEdges(t)
	needRuFiles(t)
	if os.Getenv("PLUMA_E2E_RELEASES_TOKEN_FILE") == "" || stackReleasesCdnURL == "" {
		t.Skip("PLUMA_E2E_RELEASES_* are not set (run.sh test sets them)")
	}
	t.Parallel()

	webAddr, err := stackPort("web", "443")
	if err != nil {
		t.Fatal(err)
	}
	site := proxyClient(t, siteHost, webAddr, "")
	installer, update := dlInstallerBytes(), []byte("update package of build 115")
	uploadRelease(t, site, installer, update)

	t.Run("site", func(t *testing.T) {
		a := dlDo(t, site, http.MethodGet, "https://"+siteHost+"/dl/linux/latest", nil)
		if a.code != http.StatusFound || a.header.Get("Location") != stackReleasesCdnURL+dlFilePath || a.header.Get("Cache-Control") != "no-store" {
			t.Errorf("the site's latest link: %d %q %q", a.code, a.header.Get("Location"), a.header.Get("Cache-Control"))
		}
		mirror := ruMirrorClient(t, ruSiteHost, proxyV1("5.8.16.44"))
		a = dlDo(t, mirror, http.MethodGet, "https://"+ruSiteHost+"/dl/linux/latest", nil)
		if a.code != http.StatusFound || a.header.Get("Location") != dlFilePath {
			t.Errorf("the mirror's latest link: %d %q", a.code, a.header.Get("Location"))
		}
	})

	t.Run("Gcore edge origin", func(t *testing.T) {
		edge := proxyClient(t, gcoreEdgeHost, webAddr, "")
		base := "https://" + gcoreEdgeHost
		auth := http.Header{"X-Pluma-Edge-Auth": {edgeAuth}}

		a := dlDo(t, edge, http.MethodGet, base+dlFilePath, auth)
		if a.code != http.StatusOK || !bytes.Equal(a.body, installer) || a.header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("the installer: %d, %d bytes, Cache-Control %q", a.code, len(a.body), a.header.Get("Cache-Control"))
		}
		rangeHeader := http.Header{"X-Pluma-Edge-Auth": {edgeAuth}, "Range": {"bytes=1048570-1048589"}}
		if a = dlDo(t, edge, http.MethodGet, base+dlFilePath, rangeHeader); a.code != http.StatusPartialContent || !bytes.Equal(a.body, installer[1048570:1048590]) {
			t.Errorf("a range of the installer: %d %q", a.code, a.header.Get("Content-Range"))
		}
		if a = dlDo(t, edge, http.MethodGet, base+dlUpdatePath, auth); a.code != http.StatusOK || !bytes.Equal(a.body, update) {
			t.Errorf("the update package: %d %q", a.code, a.body)
		}
		a = dlDo(t, edge, http.MethodGet, base+"/dl/desktop/current4", auth)
		if a.code != http.StatusOK || !strings.Contains(string(a.body), `"/linux/`+dlVersion+`/`+dlUpdate+`"`) || a.header.Get("Cache-Control") != "no-cache" {
			t.Errorf("current4: %d %q %.200s", a.code, a.header.Get("Cache-Control"), a.body)
		}
		a = dlDo(t, edge, http.MethodGet, base+"/dl/linux/latest", auth)
		if a.code != http.StatusFound || a.header.Get("Location") != dlFilePath || a.header.Get("Cache-Control") != "no-store" {
			t.Errorf("the latest link: %d %q %q", a.code, a.header.Get("Location"), a.header.Get("Cache-Control"))
		}

		for name, c := range map[string]struct {
			method, path string
			header       http.Header
			code         int
		}{
			"an upload":         {http.MethodPost, "/dl/upload", auth, http.StatusMethodNotAllowed},
			"no secret":         {http.MethodGet, dlFilePath, nil, http.StatusForbidden},
			"a missing file":    {http.MethodGet, "/dl/linux/" + dlVersion + "/missing.tar.xz", auth, http.StatusNotFound},
			"a missing page":    {http.MethodGet, "/no-such-page", auth, http.StatusNotFound},
			"/apiws without ws": {http.MethodGet, "/apiws", auth, 0},
		} {
			a := dlDo(t, edge, c.method, base+c.path, c.header)
			if c.code != 0 && a.code != c.code {
				t.Errorf("%s: %d, want %d", name, a.code, c.code)
			}
			if a.header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s (%d): Cache-Control %q, want no-store", name, a.code, a.header.Get("Cache-Control"))
			}
		}
	})

	t.Run("Russian node", func(t *testing.T) {
		ru := ruFilesClient(t)
		ru.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		base := "https://" + ruFilesDomain

		// The first request may find the file cached by an earlier run on
		// the same stand; the second always does.
		a := dlDo(t, ru, http.MethodGet, base+dlFilePath, nil)
		if a.code != http.StatusOK || !bytes.Equal(a.body, installer) || !strings.Contains(a.header.Get("Cache-Control"), "immutable") {
			t.Fatalf("the installer: %d, %d bytes, Cache-Control %q", a.code, len(a.body), a.header.Get("Cache-Control"))
		}
		if a = dlDo(t, ru, http.MethodGet, base+dlFilePath, nil); a.header.Get("X-Cache-Status") != "HIT" || !bytes.Equal(a.body, installer) {
			t.Errorf("the installer again: X-Cache-Status %q", a.header.Get("X-Cache-Status"))
		}
		a = dlDo(t, ru, http.MethodGet, base+dlFilePath, http.Header{"Range": {"bytes=2097000-2097199"}})
		if a.code != http.StatusPartialContent || !bytes.Equal(a.body, installer[2097000:2097200]) || a.header.Get("X-Cache-Status") != "HIT" {
			t.Errorf("a range of the cached installer: %d %q %q", a.code, a.header.Get("Content-Range"), a.header.Get("X-Cache-Status"))
		}
		if a = dlDo(t, ru, http.MethodGet, base+dlUpdatePath, nil); a.code != http.StatusOK || !bytes.Equal(a.body, update) {
			t.Errorf("the update package: %d %q", a.code, a.body)
		}
		for range 2 {
			a = dlDo(t, ru, http.MethodGet, base+"/dl/desktop/current4", nil)
			if a.code != http.StatusOK || !strings.Contains(string(a.body), `"/linux/`+dlVersion+`/`+dlUpdate+`"`) || a.header.Get("X-Cache-Status") != "" {
				t.Errorf("current4: %d %q %.200s", a.code, a.header.Get("X-Cache-Status"), a.body)
			}
		}
		a = dlDo(t, ru, http.MethodGet, base+"/dl/linux/latest", nil)
		if a.code != http.StatusFound || a.header.Get("Location") != dlFilePath || a.header.Get("Cache-Control") != "no-store" {
			t.Errorf("the latest link: %d %q %q", a.code, a.header.Get("Location"), a.header.Get("Cache-Control"))
		}
		if a = dlDo(t, ru, http.MethodPost, base+"/dl/upload", nil); a.code != http.StatusForbidden {
			t.Errorf("an upload through the node: %d", a.code)
		}
	})
}
