//go:build integration

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

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appreleases"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

// documents stores the APK as dfs and media would, in memory.
type documents struct {
	got []*mtproto.Document
	n   []int
}

func (d *documents) Import(_ context.Context, doc *mtproto.Document, file []byte, _ map[string][]byte, _ int64) (*mtproto.Document, error) {
	stored := mtproto.MakeTLDocument(&mtproto.Document{Id: int64(1000 + len(d.got)), MimeType: doc.GetMimeType(), Size2: int64(len(file)), Attributes: doc.GetAttributes()}).To_Document()
	d.got = append(d.got, stored)
	d.n = append(d.n, len(file))
	return stored, nil
}

const token = "0123456789abcdef0123456789abcdef-token"

func newHandler(t *testing.T) (*Handler, *documents, *httptest.Server) {
	t.Helper()
	conf := itDB.Config()
	docs := &documents{}
	h := &Handler{
		PublicURL: "https://pluma.example",
		DataDir:   t.TempDir(),
		Token:     token,
		DB:        sqlx.NewMySQL(&conf),
		Documents: docs,
		Now:       time.Now,
	}
	for _, d := range []string{"files", "work"} {
		if err := os.MkdirAll(filepath.Join(h.DataDir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return h, docs, srv
}

type part struct{ name, file, body string }

func upload(t *testing.T, srv *httptest.Server, auth string, parts ...part) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.file != "" {
			w, err = mw.CreateFormFile(p.name, p.file)
		} else {
			w, err = mw.CreateFormField(p.name)
		}
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, p.body)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dl/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// got is an answer of the service, its body read.
type got struct {
	code   int
	header http.Header
	body   string
}

func get(t *testing.T, srv *httptest.Server, path string) got {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return got{resp.StatusCode, resp.Header, string(body)}
}

// TestRelease: CI uploads an Android build: the APK is stored as a
// document of the APK's type, the file is served and the latest link
// points at it, releases.json lists it; the same build again is 409, a
// wrong token 403; a newer build becomes current and its withdrawal brings
// the older back. iOS builds share a version: each is a release.
func TestRelease(t *testing.T) {
	h, docs, srv := newHandler(t)
	apk := "PK fake apk"
	code, body := upload(t, srv, token,
		part{name: "platform", body: "android"}, part{name: "version", body: "12.9.1.1"}, part{name: "build", body: "101"},
		part{name: "notes_en", body: "First"}, part{name: "notes_ru", body: "Первый"},
		part{name: "installer", file: "Pluma-12.9.1.1.apk", body: apk})
	if code != http.StatusOK {
		t.Fatalf("upload: %d %s", code, body)
	}
	if len(docs.got) != 1 || docs.got[0].GetMimeType() != androidMime || docs.n[0] != len(apk) ||
		docs.got[0].GetAttributes()[0].GetFileName() != "Pluma-12.9.1.1.apk" {
		t.Fatalf("the APK document: %v", docs.got)
	}
	rel, err := appreleases.Current(context.Background(), h.DB, appreleases.Android)
	if err != nil || rel == nil || rel.Build != 101 || rel.DocumentID != docs.got[0].GetId() || rel.Notes("ru") != "Первый" {
		t.Fatalf("current release %+v, %v", rel, err)
	}

	a := get(t, srv, "/dl/android/latest")
	if a.code != http.StatusFound || a.header.Get("Location") != "https://pluma.example/dl/android/12.9.1.1/Pluma-12.9.1.1.apk" {
		t.Errorf("latest: %d %s", a.code, a.header.Get("Location"))
	}
	a = get(t, srv, "/dl/android/12.9.1.1/Pluma-12.9.1.1.apk")
	if a.code != http.StatusOK || a.body != apk || a.header.Get("Content-Type") != androidMime {
		t.Errorf("the file: %d %q %s", a.code, a.body, a.header.Get("Content-Type"))
	}
	if a = get(t, srv, "/dl/android/12.9.1.1/..%2Fx"); a.code != http.StatusNotFound {
		t.Errorf("a path out of the release: %d", a.code)
	}

	// CI's repeat: the same build, version and file. Another file or
	// version under the same number is no repeat (a run number that started
	// again), and leaves no document in dfs.
	if code, _ = upload(t, srv, token, part{name: "platform", body: "android"}, part{name: "version", body: "12.9.1.1"},
		part{name: "build", body: "101"}, part{name: "notes_en", body: "Other notes"},
		part{name: "installer", file: "Pluma-12.9.1.1.apk", body: apk}); code != http.StatusConflict {
		t.Errorf("the same build again: %d", code)
	}
	for _, again := range [][]part{
		{{name: "version", body: "12.9.1.1"}, {name: "installer", file: "Pluma-12.9.1.1.apk", body: "other apk"}},
		{{name: "version", body: "12.9.1.9"}, {name: "installer", file: "Pluma-12.9.1.9.apk", body: apk}},
	} {
		parts := append([]part{{name: "platform", body: "android"}, {name: "build", body: "101"}}, again...)
		if code, body = upload(t, srv, token, parts...); code != http.StatusUnprocessableEntity {
			t.Errorf("build 101 with %v: %d %s", again, code, body)
		}
	}
	if len(docs.got) != 1 {
		t.Errorf("documents stored by refused uploads: %d", len(docs.got)-1)
	}
	if code, _ = upload(t, srv, "wrong"+token, part{name: "platform", body: "android"}); code != http.StatusForbidden {
		t.Errorf("a wrong token: %d", code)
	}
	if code, body = upload(t, srv, token, part{name: "platform", body: "android"}, part{name: "version", body: "12.9.1"},
		part{name: "build", body: "102"}, part{name: "installer", file: "x.apk", body: "x"}); code != http.StatusBadRequest {
		t.Errorf("a version of 3 parts: %d %s", code, body)
	}

	if code, body = upload(t, srv, token, part{name: "platform", body: "android"}, part{name: "version", body: "12.9.1.2"},
		part{name: "build", body: "102"}, part{name: "installer", file: "Pluma-12.9.1.2.apk", body: "PK 2"}); code != http.StatusOK {
		t.Fatalf("a newer build: %d %s", code, body)
	}
	if code, body = upload(t, srv, token, part{name: "platform", body: "ios"}, part{name: "version", body: "12.9.2"},
		part{name: "build", body: "55"}); code != http.StatusOK {
		t.Fatalf("an iOS release: %d %s", code, body)
	}
	// iOS has Telegram's version in every build: the next build is a new
	// release, its repeat CI's own.
	if code, body = upload(t, srv, token, part{name: "platform", body: "ios"}, part{name: "version", body: "12.9.2"},
		part{name: "build", body: "56"}); code != http.StatusOK {
		t.Fatalf("the next iOS build of the same version: %d %s", code, body)
	}
	if code, _ = upload(t, srv, token, part{name: "platform", body: "ios"}, part{name: "version", body: "12.9.2"},
		part{name: "build", body: "56"}); code != http.StatusConflict {
		t.Errorf("the same iOS build: %d", code)
	}
	list := get(t, srv, "/dl/releases.json").body
	var out struct {
		Platforms map[string]releaseView `json:"platforms"`
	}
	if err = json.Unmarshal([]byte(list), &out); err != nil {
		t.Fatal(err)
	}
	if out.Platforms["android"].Version != "12.9.1.2" || out.Platforms["ios"].Build != 56 || out.Platforms["ios"].URL != "https://pluma.example/iphone/install" {
		t.Errorf("releases.json %s", list)
	}

	if ok, err := appreleases.SetWithdrawn(context.Background(), h.DB, appreleases.Android, 102, true); err != nil || !ok {
		t.Fatalf("withdraw: %v %v", ok, err)
	}
	if rel, _ = appreleases.Current(context.Background(), h.DB, appreleases.Android); rel.Build != 101 {
		t.Errorf("after the withdrawal the current build is %d, want 101", rel.Build)
	}
}

// TestDesktopFiles: a desktop release takes an installer and an update
// package per processor; one without an installer, or with a package of
// another version, is refused. The updater finds the current packages at
// current4 and fetches them by its prefix and their links.
func TestDesktopFiles(t *testing.T) {
	_, _, srv := newHandler(t)
	code, body := upload(t, srv, token, part{name: "platform", body: "macos"}, part{name: "version", body: "7.0.9.00007"},
		part{name: "build", body: "7"},
		part{name: "installer", file: "Pluma-7.0.9.00007-macos-universal.zip", body: "zip"},
		part{name: "update", file: "tmacupd7", body: "pkg"})
	if code != http.StatusOK {
		t.Fatalf("upload: %d %s", code, body)
	}
	if code, _ = upload(t, srv, token, part{name: "platform", body: "linux"}, part{name: "version", body: "7.0.9.00007"},
		part{name: "build", body: "7"}, part{name: "update", file: "tlinuxupd7", body: "pkg"}); code != http.StatusBadRequest {
		t.Errorf("no installer: %d", code)
	}
	if code, _ = upload(t, srv, token, part{name: "platform", body: "linux"}, part{name: "version", body: "7.0.9.00007"},
		part{name: "build", body: "7"}, part{name: "installer", file: "Pluma.tar.xz", body: "xz"},
		part{name: "update", file: "tlinuxupd6", body: "pkg"}); code != http.StatusBadRequest {
		t.Errorf("a package of another version: %d", code)
	}
	if code, body = upload(t, srv, token, part{name: "platform", body: "windows"}, part{name: "version", body: "7.0.9.00006"},
		part{name: "build", body: "6"}, part{name: "installer", file: "Pluma-x64.zip", body: "zip"},
		part{name: "update", file: "tx64upd6", body: "win"}); code != http.StatusOK {
		t.Fatalf("windows: %d %s", code, body)
	}

	// The updater asks <prefix>/current4 and adds the link to the prefix.
	var current map[string]map[string]struct{ Released, Link string }
	if err := json.Unmarshal([]byte(get(t, srv, "/dl/desktop/current4").body), &current); err != nil {
		t.Fatal(err)
	}
	if len(current) != 3 || current["mac"]["stable"].Released != "7" || current["armac"]["stable"].Link != "/macos/7.0.9.00007/tmacupd7" ||
		current["win64"]["stable"].Link != "/windows/7.0.9.00006/tx64upd6" {
		t.Errorf("current4: %+v", current)
	}
	if a := get(t, srv, "/dl/desktop"+current["win64"]["stable"].Link); a.code != http.StatusOK || a.body != "win" {
		t.Errorf("the package: %d %q", a.code, a.body)
	}
	if a := get(t, srv, "/dl/desktop/current2"); a.code != http.StatusNotFound {
		t.Errorf("another updater: %d", a.code)
	}
}

// TestInterruptedUpload: the files of a version left without its row (an
// upload cut off between them) give way to the next upload of the version;
// a released version is not taken again under another build.
func TestInterruptedUpload(t *testing.T) {
	h, _, srv := newHandler(t)
	left := filepath.Join(h.DataDir, "files", "linux", "7.0.9.00020")
	if err := os.MkdirAll(left, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(left, "stale.tar.xz"), []byte("stale"), 0o640); err != nil {
		t.Fatal(err)
	}
	if code, body := upload(t, srv, token, part{name: "platform", body: "linux"}, part{name: "version", body: "7.0.9.00020"},
		part{name: "build", body: "11"}, part{name: "installer", file: "Pluma.tar.xz", body: "xz"}); code != http.StatusOK {
		t.Fatalf("upload over the leftover: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(left, "stale.tar.xz")); !os.IsNotExist(err) {
		t.Errorf("the leftover file is still there: %v", err)
	}
	if a := get(t, srv, "/dl/linux/7.0.9.00020/Pluma.tar.xz"); a.code != http.StatusOK || a.body != "xz" {
		t.Errorf("the file: %d %q", a.code, a.body)
	}
	if code, body := upload(t, srv, token, part{name: "platform", body: "linux"}, part{name: "version", body: "7.0.9.00020"},
		part{name: "build", body: "12"}, part{name: "installer", file: "Pluma.tar.xz", body: "other"}); code != http.StatusUnprocessableEntity {
		t.Errorf("the same version as another build: %d %s", code, body)
	}
	if a := get(t, srv, "/dl/linux/7.0.9.00020/Pluma.tar.xz"); a.body != "xz" {
		t.Errorf("the released file changed: %q", a.body)
	}
	entries, err := os.ReadDir(filepath.Join(h.DataDir, "work"))
	if err != nil || len(entries) != 0 {
		t.Errorf("work directory: %v %v", entries, err)
	}
}

// TestAdminRoutes: the panel's routes are closed without their token, list
// the builds of a platform, and a rollback changes which is current.
func TestAdminRoutes(t *testing.T) {
	h, _, srv := newHandler(t)
	call := func(method, path, auth string) (int, string) {
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	if code, _ := call(http.MethodGet, "/internal/releases", ""); code != http.StatusServiceUnavailable {
		t.Errorf("no admin token configured: %d", code)
	}
	h.AdminToken = token + "-admin"
	if code, _ := call(http.MethodGet, "/internal/releases", "wrong"); code != http.StatusForbidden {
		t.Errorf("a wrong token: %d", code)
	}
	ctx := context.Background()
	for build := int32(101); build <= 102; build++ {
		rel := &appreleases.Release{Platform: appreleases.Linux, Version: "7.0.9." + strconv.Itoa(int(build)), Build: build, Files: []appreleases.File{}, CreatedAt: time.Now().Unix()}
		if err := appreleases.Insert(ctx, h.DB, rel); err != nil {
			t.Fatal(err)
		}
	}
	code, body := call(http.MethodGet, "/internal/releases", h.AdminToken)
	if code != http.StatusOK || !strings.Contains(body, `"platform":"linux"`) || !strings.Contains(body, `"current":102`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body = call(http.MethodPost, "/internal/releases/linux/102/withdraw", h.AdminToken); code != http.StatusOK || !strings.Contains(body, `"current":101`) {
		t.Fatalf("withdraw: %d %s", code, body)
	}
	if code, body = call(http.MethodPost, "/internal/releases/linux/102/restore", h.AdminToken); code != http.StatusOK || !strings.Contains(body, `"current":102`) {
		t.Fatalf("restore: %d %s", code, body)
	}
	if code, _ = call(http.MethodPost, "/internal/releases/linux/999/withdraw", h.AdminToken); code != http.StatusNotFound {
		t.Errorf("an unknown build: %d", code)
	}
	if code, _ = call(http.MethodPost, "/internal/releases/bada/1/withdraw", h.AdminToken); code != http.StatusBadRequest {
		t.Errorf("an unknown platform: %d", code)
	}
}

// TestMandatoryUpload: CI makes a release mandatory at its upload, stored
// with the release; an optional repeat keeps it mandatory, a mandatory
// repeat of an optional build is refused (422), and a can_not_skip that is
// neither 1 nor 0, or 1 for a desktop platform, is refused and stores
// nothing.
func TestMandatoryUpload(t *testing.T) {
	h, _, srv := newHandler(t)
	ctx := context.Background()
	android := func(flag string) []part {
		return []part{{name: "platform", body: "android"}, {name: "version", body: "12.9.1.301"}, {name: "build", body: "301"},
			{name: "can_not_skip", body: flag}, {name: "installer", file: "Pluma-12.9.1.301.apk", body: "PK 301"}}
	}
	if code, body := upload(t, srv, token, android("1")...); code != http.StatusOK {
		t.Fatalf("a mandatory upload: %d %s", code, body)
	}
	if rel, err := appreleases.ByBuild(ctx, h.DB, appreleases.Android, 301); err != nil || rel == nil || !rel.CanNotSkip {
		t.Fatalf("the mandatory release %+v, %v", rel, err)
	}
	if code, body := upload(t, srv, token, android("0")...); code != http.StatusConflict {
		t.Errorf("an optional repeat of a mandatory build: %d %s", code, body)
	}
	if rel, err := appreleases.ByBuild(ctx, h.DB, appreleases.Android, 301); err != nil || rel == nil || !rel.CanNotSkip {
		t.Errorf("after an optional repeat %+v, %v", rel, err)
	}

	ios := func(build, flag string) []part {
		return []part{{name: "platform", body: "ios"}, {name: "version", body: "12.9.2"}, {name: "build", body: build},
			{name: "can_not_skip", body: flag}}
	}
	if code, body := upload(t, srv, token, ios("71", "")...); code != http.StatusOK {
		t.Fatalf("an optional iOS upload: %d %s", code, body)
	}
	if code, body := upload(t, srv, token, ios("71", "1")...); code != http.StatusUnprocessableEntity || !strings.Contains(body, "pluma-admin release mandatory") {
		t.Errorf("a mandatory repeat of an optional build: %d %s", code, body)
	}
	if rel, err := appreleases.ByBuild(ctx, h.DB, appreleases.IOS, 71); err != nil || rel == nil || rel.CanNotSkip {
		t.Errorf("after a refused mandatory repeat %+v, %v", rel, err)
	}
	if code, body := upload(t, srv, token, ios("72", "true")...); code != http.StatusBadRequest {
		t.Errorf("can_not_skip=true: %d %s", code, body)
	}
	if rel, err := appreleases.ByBuild(ctx, h.DB, appreleases.IOS, 72); err != nil || rel != nil {
		t.Errorf("a refused upload stored %+v, %v", rel, err)
	}
	if code, body := upload(t, srv, token, part{name: "platform", body: "macos"}, part{name: "version", body: "7.0.9.00301"},
		part{name: "build", body: "301"}, part{name: "can_not_skip", body: "1"},
		part{name: "installer", file: "Pluma-7.0.9.00301-macos-universal.zip", body: "zip"}); code != http.StatusBadRequest || !strings.Contains(body, "no mandatory updates") {
		t.Errorf("a mandatory macOS upload: %d %s", code, body)
	}
	if rel, err := appreleases.ByBuild(ctx, h.DB, appreleases.MacOS, 301); err != nil || rel != nil {
		t.Errorf("a refused macOS upload stored %+v, %v", rel, err)
	}
}
