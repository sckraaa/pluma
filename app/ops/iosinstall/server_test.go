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
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeApple is App Store Connect: devices, bundle IDs and ad hoc profiles.
type fakeApple struct {
	mu       sync.Mutex
	devices  map[string]string // udid -> id
	refuse   map[string]bool   // udids Apple refuses
	profiles map[string]ascProfile
	created  []string // "name|bundle|cert|devices"
	nextID   int
}

func newFakeApple() *fakeApple {
	return &fakeApple{devices: map[string]string{"OWNER-UDID": "D0"}, refuse: map[string]bool{},
		profiles: map[string]ascProfile{"P-app": {ID: "P-app", Name: "Pluma"}}}
}

func (f *fakeApple) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s%d", prefix, f.nextID)
}

func (f *fakeApple) deviceID(_ context.Context, udid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.devices[udid], nil
}

func (f *fakeApple) registerDevice(_ context.Context, name, udid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse[udid] {
		return "", &ascError{Status: 409, Body: `{"errors":[{"detail":"An invalid value '` + udid + `' was provided"}]}`}
	}
	if !strings.HasPrefix(name, "Pluma iPhone") {
		return "", fmt.Errorf("device name %q", name)
	}
	id := f.id("D")
	f.devices[udid] = id
	return id, nil
}

func (f *fakeApple) enabledDevices(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, id := range f.devices {
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *fakeApple) bundleID(_ context.Context, identifier string) (string, error) {
	return "B:" + identifier, nil
}

func (f *fakeApple) adHocProfiles(_ context.Context, bundleID string) ([]ascProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if bundleID == "B:chat.pluma.app" {
		if p, ok := f.profiles["P-app"]; ok {
			return []ascProfile{p}, nil
		}
	}
	return nil, nil
}

func (f *fakeApple) deleteProfile(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.profiles, id)
	return nil
}

func (f *fakeApple) createAdHocProfile(_ context.Context, name, bundleID, cert string, devices []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := sortedCopy(devices)
	f.created = append(f.created, fmt.Sprintf("%s|%s|%s|%s", name, bundleID, cert, strings.Join(d, ",")))
	return []byte("profile of " + bundleID + " for " + strings.Join(d, ",")), nil
}

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef"

type harness struct {
	t      *testing.T
	srv    *server
	signer *signer
	h      http.Handler
	apple  *fakeApple
	dir    string
	now    time.Time
	logs   *strings.Builder
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"builds", "signed", "work"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// rcodesign: checks the arguments the signer passes and marks every
	// bundle it signs.
	fake := filepath.Join(dir, "rcodesign")
	script := `#!/bin/sh
set -e
[ "$1" = sign ] && [ "$2" = --p12-file ] && [ "$4" = --p12-password-file ] || { echo "bad arguments: $*" >&2; exit 2; }
shift 5
ents=""
while [ "$1" = -e ]; do ents="$ents $2"; shift 2; done
app="$1"
echo "$ents" > "$app/signed-with"
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		publicURL: "https://pluma.example", pageURL: "https://pluma.example/iphone/install",
		certificateID: "CERT", p12File: "/secrets/p12", p12PasswordFile: "/secrets/p12pw",
		uploadToken: testToken, dataDir: dir, rcodesign: fake, perAddressDay: 3, devicesDay: 2,
	}
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	apple := newFakeApple()
	sg := newSigner(st, apple, cfg)
	logs := &strings.Builder{}
	sg.logf = func(format string, args ...any) { fmt.Fprintf(logs, format+"\n", args...) }
	srv := newServer(cfg, st, sg)
	h := &harness{t: t, srv: srv, signer: sg, h: srv.routes(), apple: apple, dir: dir, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), logs: logs}
	srv.now = func() time.Time { return h.now }
	// The signature is verifyDeviceSignature's test; here the answer is the
	// plist itself.
	srv.verify = func(der []byte) ([]byte, error) {
		if !bytes.HasPrefix(der, []byte("<?xml")) {
			return nil, errNotApple
		}
		return der, nil
	}
	return h
}

func (h *harness) do(method, target string, body io.Reader, header map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.RemoteAddr = "198.51.100.7:5555"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, req)
	return w
}

// testIPA is an app with one extension, as CI signs it.
func testIPA(t *testing.T, extra ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := append([]string{
		"Payload/Telegram.app/Info.plist",
		"Payload/Telegram.app/Telegram",
		"Payload/Telegram.app/Frameworks/X.framework/Info.plist",
		"Payload/Telegram.app/PlugIns/ShareExtension.appex/Info.plist",
		"Payload/Telegram.app/PlugIns/ShareExtension.appex/ShareExtension",
	}, extra...)
	for _, name := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o755)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, "content of "+name)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var testBundles = []Bundle{
	{Path: "", BundleID: "chat.pluma.app", Entitlements: "<plist>app</plist>"},
	{Path: "PlugIns/ShareExtension.appex", BundleID: "chat.pluma.app.Share", Entitlements: "<plist>share</plist>"},
}

func uploadForm(t *testing.T, ipa []byte, bundles []Bundle, version string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("version", version)
	_ = mw.WriteField("number", "21")
	b, _ := json.Marshal(bundles)
	_ = mw.WriteField("bundles", string(b))
	fw, _ := mw.CreateFormFile("ipa", "Pluma.ipa")
	_, _ = fw.Write(ipa)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func (h *harness) upload(ipa []byte, bundles []Bundle, token string) *httptest.ResponseRecorder {
	body, ct := uploadForm(h.t, ipa, bundles, "12.9.2")
	return h.do("POST", "/ios/adhoc/upload", body, map[string]string{"Authorization": "Bearer " + token, "Content-Type": ct})
}

var challengeOf = regexp.MustCompile(`<key>Challenge</key>\s*<string>([^<]+)</string>`)

// enroll downloads the profile and answers it as a device.
func (h *harness) enroll(udid, product string) *httptest.ResponseRecorder {
	h.t.Helper()
	w := h.do("GET", "/ios/adhoc/profile", nil, nil)
	if w.Code != 200 {
		return w
	}
	c := challengeOf.FindStringSubmatch(w.Body.String())[1]
	return h.answer(c, c, udid, product)
}

func (h *harness) answer(query, signed, udid, product string) *httptest.ResponseRecorder {
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>` +
		`<key>CHALLENGE</key><string>` + signed + `</string>` +
		`<key>PRODUCT</key><string>` + product + `</string>` +
		`<key>UDID</key><string>` + udid + `</string>` +
		`<key>VERSION</key><string>23B85</string></dict></plist>`
	return h.do("POST", "/ios/adhoc/enroll?c="+url.QueryEscape(query), strings.NewReader(plist), nil)
}

func tokenOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	loc := w.Header().Get("Location")
	if w.Code != http.StatusMovedPermanently || !strings.HasPrefix(loc, "https://pluma.example/iphone/install#t=") {
		t.Fatalf("enrolment: %d %q %s", w.Code, loc, w.Body)
	}
	return strings.TrimPrefix(loc, "https://pluma.example/iphone/install#t=")
}

func (h *harness) status(token string) map[string]string {
	h.t.Helper()
	w := h.do("GET", "/ios/adhoc/status?t="+url.QueryEscape(token), nil, nil)
	var out map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		h.t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	return out
}

const udid1 = "00008130-001A2B3C4D5E6F70"

func TestInstallFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// The profile asks for the attributes and posts them back here.
	w := h.do("GET", "/ios/adhoc/profile", nil, map[string]string{"Accept-Language": "ru-RU,ru"})
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-apple-aspen-config" ||
		!strings.Contains(body, "<string>Profile Service</string>") || !strings.Contains(body, "Устанавливается только приложение") ||
		!strings.Contains(body, "<string>https://pluma.example/ios/adhoc/enroll?c=") {
		t.Fatalf("profile: %d %s", w.Code, body)
	}

	// A device before any build: it waits.
	token := tokenOf(t, h.enroll(udid1, "iPhone16,1"))
	if s := h.status(token); s["state"] != "waiting" {
		t.Fatalf("before a build: %v", s)
	}
	if err := h.signer.run(ctx); err != nil {
		t.Fatal(err)
	}
	if st := h.srv.store.view(); st.device(udid1).AppleID == "" || h.apple.devices[udid1] == "" {
		t.Fatalf("the device is not registered: %+v", st.device(udid1))
	}

	// CI uploads a build; the signer issues profiles for both devices and
	// signs.
	if w := h.upload(testIPA(t), testBundles, "wrong"+testToken); w.Code != http.StatusForbidden {
		t.Fatalf("a wrong token: %d", w.Code)
	}
	if w := h.upload(testIPA(t), testBundles, testToken); w.Code != http.StatusNoContent {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if err := h.signer.run(ctx); err != nil {
		t.Fatal(err)
	}
	devs := strings.Join(sortedCopy([]string{"D0", h.apple.devices[udid1]}), ",")
	want := []string{"Pluma|B:chat.pluma.app|CERT|" + devs, "Pluma Ad Hoc chat.pluma.app.Share|B:chat.pluma.app.Share|CERT|" + devs}
	if !slices.Equal(h.apple.created, want) {
		t.Fatalf("profiles %q, want %q", h.apple.created, want)
	}
	s := h.status(token)
	if s["state"] != "ready" || s["version"] != "12.9.2" ||
		s["install"] != "itms-services://?action=download-manifest&url="+url.QueryEscape("https://pluma.example/ios/adhoc/manifest?t="+token) {
		t.Fatalf("ready: %v", s)
	}
	m := h.do("GET", "/ios/adhoc/manifest?t="+token, nil, nil).Body.String()
	if !strings.Contains(m, "<string>chat.pluma.app</string>") || !strings.Contains(m, "<string>https://pluma.example/ios/adhoc/app.ipa?t="+token+"</string>") {
		t.Fatalf("manifest: %s", m)
	}

	// The IPA: each bundle with its new profile, signed with its
	// entitlements, the files as uploaded.
	w = h.do("GET", "/ios/adhoc/app.ipa?t="+token, nil, nil)
	if w.Code != 200 {
		t.Fatalf("ipa: %d", w.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
		if f.Name == "Payload/Telegram.app/Telegram" && f.Mode().Perm()&0o111 == 0 {
			t.Errorf("the executable lost its mode: %v", f.Mode())
		}
	}
	if files["Payload/Telegram.app/embedded.mobileprovision"] != "profile of B:chat.pluma.app for "+devs ||
		files["Payload/Telegram.app/PlugIns/ShareExtension.appex/embedded.mobileprovision"] != "profile of B:chat.pluma.app.Share for "+devs ||
		files["Payload/Telegram.app/Telegram"] != "content of Payload/Telegram.app/Telegram" {
		t.Fatalf("IPA files: %v", files)
	}
	if signed := files["Payload/Telegram.app/signed-with"]; !strings.Contains(signed, "entitlements-0.plist") ||
		!strings.Contains(signed, "PlugIns/ShareExtension.appex:") {
		t.Fatalf("rcodesign arguments: %q", signed)
	}

	// Nothing new: no new profiles.
	if err := h.signer.run(ctx); err != nil || len(h.apple.created) != 2 {
		t.Fatalf("an idle run: %v, %d profiles", err, len(h.apple.created))
	}

	// The same iPhone again: the same link, no new device.
	if again := tokenOf(t, h.enroll(udid1, "iPhone16,1")); again != token || len(h.srv.store.view().Devices) != 1 {
		t.Fatalf("again: %q", again)
	}

	// Without a token nothing is served.
	for _, p := range []string{"/ios/adhoc/manifest?t=x", "/ios/adhoc/app.ipa", "/ios/adhoc/status?t=nope"} {
		if w := h.do("GET", p, nil, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, w.Code)
		}
	}

	// A new build is signed for everyone and replaces the old files.
	old := h.srv.store.view()
	if w := h.upload(testIPA(t, "Payload/Telegram.app/new.txt"), testBundles, testToken); w.Code != http.StatusNoContent {
		t.Fatalf("second upload: %d %s", w.Code, w.Body)
	}
	if s := h.status(token); s["state"] != "waiting" {
		t.Fatalf("a new build before signing: %v", s)
	}
	if err := h.signer.run(ctx); err != nil {
		t.Fatal(err)
	}
	if h.status(token)["state"] != "ready" {
		t.Fatal("the new build is not ready")
	}
	for _, p := range []string{filepath.Join(h.dir, "builds", old.Build.ID+".ipa"), filepath.Join(h.dir, "signed", old.Signed.File)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is left: %v", p, err)
		}
	}
	if strings.Contains(h.logs.String(), udid1) {
		t.Errorf("a UDID in the log: %s", h.logs)
	}
}

func TestEnrolmentRefusals(t *testing.T) {
	h := newHarness(t)

	// A challenge is used once, and must be the one the device signed.
	w := h.do("GET", "/ios/adhoc/profile", nil, nil)
	c := challengeOf.FindStringSubmatch(w.Body.String())[1]
	if w := h.answer(c, "other", udid1, "iPhone16,1"); w.Code != http.StatusForbidden {
		t.Errorf("another challenge signed: %d", w.Code)
	}
	if w := h.answer(c, c, udid1, "iPhone16,1"); w.Code != http.StatusForbidden {
		t.Errorf("a used challenge: %d", w.Code)
	}
	// An expired one.
	w = h.do("GET", "/ios/adhoc/profile", nil, nil)
	c = challengeOf.FindStringSubmatch(w.Body.String())[1]
	h.now = h.now.Add(challengeTTL + time.Minute)
	if w := h.answer(c, c, udid1, "iPhone16,1"); w.Code != http.StatusForbidden {
		t.Errorf("an expired challenge: %d", w.Code)
	}
	// Not signed by a device.
	if w := h.do("POST", "/ios/adhoc/enroll?c=x", strings.NewReader("garbage"), nil); w.Code != http.StatusForbidden {
		t.Errorf("unsigned: %d", w.Code)
	}

	// The address's limit: 3 profiles a day (two used above).
	h.now = h.now.Add(25 * time.Hour)
	for i := 0; i < 3; i++ {
		if w := h.do("GET", "/ios/adhoc/profile", nil, nil); w.Code != 200 {
			t.Fatalf("profile %d: %d", i, w.Code)
		}
	}
	if w := h.do("GET", "/ios/adhoc/profile", nil, nil); w.Code != http.StatusTooManyRequests {
		t.Errorf("over the address limit: %d", w.Code)
	}

	// iPads are not served; 2 new iPhones a day.
	h.now = h.now.Add(25 * time.Hour)
	if w := h.enroll("00008130-001A2B3C4D5E6F71", "iPad14,1"); w.Code != http.StatusForbidden {
		t.Errorf("an iPad: %d", w.Code)
	}
	h.now = h.now.Add(25 * time.Hour)
	tokenOf(t, h.enroll("00008130-001A2B3C4D5E6F72", "iPhone15,2"))
	tokenOf(t, h.enroll("00008130-001A2B3C4D5E6F73", "iPhone15,3"))
	h.now = h.now.Add(25 * time.Hour)
	tokenOf(t, h.enroll("00008130-001A2B3C4D5E6F74", "iPhone15,4"))
	h.srv.perAddress = map[string][]time.Time{}
	tokenOf(t, h.enroll("00008130-001A2B3C4D5E6F75", "iPhone15,5"))
	if w := h.enroll("00008130-001A2B3C4D5E6F76", "iPhone15,6"); w.Code != http.StatusTooManyRequests {
		t.Errorf("over the daily limit of devices: %d", w.Code)
	}
}

func TestAppleRefusesADevice(t *testing.T) {
	h := newHarness(t)
	bad := "00008130-00000000000000BA"
	h.apple.refuse[bad] = true
	token := tokenOf(t, h.enroll(bad, "iPhone16,1"))
	good := tokenOf(t, h.enroll(udid1, "iPhone16,1"))
	if w := h.upload(testIPA(t), testBundles, testToken); w.Code != http.StatusNoContent {
		t.Fatal(w.Code)
	}
	if err := h.signer.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := h.status(token); s["state"] != "refused" {
		t.Errorf("the refused device: %v", s)
	}
	if s := h.status(good); s["state"] != "ready" {
		t.Errorf("the other device: %v", s)
	}
	if strings.Contains(h.logs.String(), bad) || !strings.Contains(h.logs.String(), "HTTP 409") {
		t.Errorf("log: %s", h.logs)
	}
}

func TestUploadChecks(t *testing.T) {
	h := newHarness(t)
	for name, c := range map[string]struct {
		ipa     []byte
		bundles []Bundle
		want    string
	}{
		"an extension not listed": {testIPA(t, "Payload/Telegram.app/PlugIns/Widget.appex/Info.plist"), testBundles, `the IPA has "PlugIns/Widget.appex"`},
		"a bundle not in the IPA": {testIPA(t), append(testBundles[:2:2], Bundle{Path: "PlugIns/Widget.appex", BundleID: "w", Entitlements: "<plist/>"}), "is not in the IPA"},
		"no app":                  {testIPA(t), testBundles[1:], "the app itself is missing"},
		"a path outside":          {testIPA(t), append(testBundles[:2:2], Bundle{Path: "PlugIns/../../x.appex", BundleID: "w", Entitlements: "<plist/>"}), "an extension in PlugIns/"},
		"no entitlements":         {testIPA(t), []Bundle{{BundleID: "chat.pluma.app"}}, "entitlements are required"},
		"not an IPA":              {[]byte("zip?"), testBundles, "IPA"},
	} {
		w := h.upload(c.ipa, c.bundles, testToken)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if st := h.srv.store.view(); st.Build != nil {
		t.Errorf("a refused upload was kept: %+v", st.Build)
	}
}

func TestUnzipRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"../evil", "/abs", "Payload/../../evil"} {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		_, _ = zw.Create(name)
		_ = zw.Close()
		p := filepath.Join(dir, "x.ipa")
		_ = os.WriteFile(p, buf.Bytes(), 0o600)
		if err := unzip(p, root); err == nil {
			t.Errorf("%q unpacked", name)
		}
	}
}
