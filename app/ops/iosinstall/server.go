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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The site's hidden page leads a person through it (pluma.chat/iphone):
//
//  1. GET  /ios/adhoc/profile   a Profile Service profile with a one-time
//     challenge; iOS asks to install it in Settings.
//  2. POST /ios/adhoc/enroll    the device's signed answer (UDID, model, iOS
//     version, the challenge); the reply sends Safari back to the page
//     with the device's token.
//  3. GET  /ios/adhoc/status    "waiting" until the IPA is signed for the
//     device, then the itms-services link, the version and the build number.
//  4. GET  /ios/adhoc/manifest  and /ios/adhoc/app.ipa, by the token.
//
// CI uploads every signed ad hoc build: POST /ios/adhoc/upload.

const (
	challengeTTL  = 30 * time.Minute
	maxEnrollBody = 64 << 10
	maxUpload     = 1 << 30
)

type server struct {
	cfg    config
	store  *store
	signer *signer
	now    func() time.Time
	verify func(der []byte) ([]byte, error)

	mu         sync.Mutex
	challenges map[string]time.Time
	perAddress map[string][]time.Time
}

func newServer(cfg config, st *store, sg *signer) *server {
	return &server{
		cfg:        cfg,
		store:      st,
		signer:     sg,
		now:        time.Now,
		verify:     verifyDeviceSignature,
		challenges: map[string]time.Time{},
		perAddress: map[string][]time.Time{},
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ios/adhoc/profile", s.profile)
	mux.HandleFunc("POST /ios/adhoc/enroll", s.enroll)
	mux.HandleFunc("GET /ios/adhoc/status", s.status)
	mux.HandleFunc("GET /ios/adhoc/manifest", s.manifest)
	mux.HandleFunc("GET /ios/adhoc/app.ipa", s.ipa)
	mux.HandleFunc("POST /ios/adhoc/upload", s.upload)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

// clientAddress is the address Caddy saw (X-Forwarded-For, last hop);
// the service listens on the internal network only.
func clientAddress(r *http.Request) string {
	if f := r.Header.Get("X-Forwarded-For"); f != "" {
		parts := strings.Split(f, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allow counts a request of addr against the limit per day.
func (s *server) allow(addr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	recent := s.perAddress[addr][:0]
	for _, t := range s.perAddress[addr] {
		if now.Sub(t) < 24*time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= s.cfg.perAddressDay {
		s.perAddress[addr] = recent
		return false
	}
	s.perAddress[addr] = append(recent, now)
	return true
}

func (s *server) newChallenge() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for c, t := range s.challenges {
		if now.Sub(t) > challengeTTL {
			delete(s.challenges, c)
		}
	}
	c := newToken()
	s.challenges[c] = now
	return c
}

// takeChallenge uses up a challenge of the last half hour.
func (s *server) takeChallenge(c string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.challenges[c]
	delete(s.challenges, c)
	return ok && s.now().Sub(t) <= challengeTTL
}

func (s *server) profile(w http.ResponseWriter, r *http.Request) {
	if !s.allow(clientAddress(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	c := s.newChallenge()
	s.signer.logf("iosinstall: a profile for %s", clientAddress(r))
	enroll := s.cfg.publicURL + "/ios/adhoc/enroll?c=" + url.QueryEscape(c)
	ru := strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ru")
	w.Header().Set("Content-Type", "application/x-apple-aspen-config")
	w.Header().Set("Content-Disposition", `attachment; filename="pluma.mobileconfig"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, profileServicePlist(enroll, c, newUUID(), ru))
}

// profileServicePlist is the profile of phase 1 of "Over-the-Air Profile
// Delivery and Configuration": the device posts the attributes, signed, to
// URL. The profile is unsigned: iOS shows it as such.
func profileServicePlist(enrollURL, challenge, uuid string, ru bool) string {
	name, desc := "Pluma: install on this iPhone",
		"Tells Pluma this iPhone's identifier (UDID) so that the app can be signed for it. Nothing is installed but the app."
	if ru {
		name, desc = "Pluma: установка на этот iPhone",
			"Сообщает Pluma идентификатор iPhone (UDID), чтобы приложение подписали для него. Устанавливается только приложение."
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<dict>
		<key>URL</key>
		<string>` + xmlEscape(enrollURL) + `</string>
		<key>DeviceAttributes</key>
		<array>
			<string>UDID</string>
			<string>PRODUCT</string>
			<string>VERSION</string>
		</array>
		<key>Challenge</key>
		<string>` + xmlEscape(challenge) + `</string>
	</dict>
	<key>PayloadOrganization</key>
	<string>Pluma</string>
	<key>PayloadDisplayName</key>
	<string>` + xmlEscape(name) + `</string>
	<key>PayloadDescription</key>
	<string>` + xmlEscape(desc) + `</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
	<key>PayloadUUID</key>
	<string>` + uuid + `</string>
	<key>PayloadIdentifier</key>
	<string>chat.pluma.install</string>
	<key>PayloadType</key>
	<string>Profile Service</string>
</dict>
</plist>
`
}

var (
	udidPattern    = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{16}$|^[0-9a-f]{40}$`)
	productPattern = regexp.MustCompile(`^(iPhone|iPod)[0-9]{1,3},[0-9]{1,3}$`)
)

func (s *server) enroll(w http.ResponseWriter, r *http.Request) {
	addr := clientAddress(r)
	body, err := io.ReadAll(io.LimitReader(r.Body, maxEnrollBody+1))
	if err != nil || len(body) > maxEnrollBody {
		s.signer.logf("iosinstall: an enrolment from %s: the body is unreadable or over %d bytes", addr, maxEnrollBody)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Every outcome is logged, with the address and the model but never
	// the UDID: the first answers of real devices are what tells a wrong
	// expectation about their format.
	content, err := s.verify(body)
	if err != nil {
		s.signer.logf("iosinstall: an enrolment from %s refused: %v", addr, err)
		http.Error(w, "the answer is not signed by an iPhone", http.StatusForbidden)
		return
	}
	attrs, err := plistStrings(content)
	if err != nil {
		s.signer.logf("iosinstall: an enrolment from %s: the attributes (%d bytes) do not parse: %v", addr, len(content), err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// The challenge is used up by any answer, a wrong one too.
	challenge := r.URL.Query().Get("c")
	if known := s.takeChallenge(challenge); !known || attrs["CHALLENGE"] != challenge {
		s.signer.logf("iosinstall: an enrolment from %s: challenge known %v, the signed one matches %v",
			addr, known, attrs["CHALLENGE"] == challenge && challenge != "")
		http.Error(w, "the profile has expired: download it again", http.StatusForbidden)
		return
	}
	udid, product := attrs["UDID"], attrs["PRODUCT"]
	if !udidPattern.MatchString(udid) || !productPattern.MatchString(product) {
		// iPads and Macs are not served: the profiles are for iPhones.
		s.signer.logf("iosinstall: an enrolment from %s refused: product %q, a UDID of %d characters", addr, truncate(product, 40), len(udid))
		http.Error(w, "only an iPhone can install Pluma this way", http.StatusForbidden)
		return
	}

	var token string
	err = s.store.update(func(st *state) error {
		if d := st.device(udid); d != nil {
			d.OS = attrs["VERSION"]
			token = d.Token
			return nil
		}
		if st.addedSince(s.now().Add(-24*time.Hour)) >= s.cfg.devicesDay {
			return errDailyLimit
		}
		token = newToken()
		st.Devices = append(st.Devices, &Device{UDID: udid, Product: product, OS: attrs["VERSION"], Token: token, Added: s.now().UTC()})
		return nil
	})
	if errors.Is(err, errDailyLimit) {
		http.Error(w, "too many new iPhones today: try tomorrow", http.StatusTooManyRequests)
		return
	}
	if err != nil {
		s.signer.logf("iosinstall: enrolment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.signer.logf("iosinstall: an enrolment from %s: %s, iOS %s", addr, product, truncate(attrs["VERSION"], 20))
	s.signer.kick()
	// Apple: answer phase 2 with a redirect; Safari opens it.
	http.Redirect(w, r, s.cfg.pageURL+"#t="+token, http.StatusMovedPermanently)
}

var errDailyLimit = errors.New("daily limit of new devices")

// plistStrings reads the string values of a flat plist dictionary.
func plistStrings(raw []byte) (map[string]string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	dec.Strict = false
	out := map[string]string{}
	var key, elem string
	inDict := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elem = t.Name.Local
			if elem == "dict" {
				inDict++
			}
		case xml.EndElement:
			if t.Name.Local == "dict" {
				inDict--
			}
			elem = ""
		case xml.CharData:
			switch {
			case inDict == 1 && elem == "key":
				key = string(t)
			case inDict == 1 && elem == "string" && key != "":
				out[key] = string(t)
				key = ""
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("plist: no strings")
	}
	return out, nil
}

// device returns the device of the request's token.
func (s *server) device(r *http.Request) (*Device, state, bool) {
	st := s.store.view()
	t := r.URL.Query().Get("t")
	if t == "" {
		return nil, st, false
	}
	d := st.byToken(t)
	return d, st, d != nil
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	d, st, ok := s.device(r)
	if !ok {
		http.Error(w, "unknown link", http.StatusNotFound)
		return
	}
	out := map[string]string{"state": "waiting"}
	switch {
	case d.Failure != "":
		out["state"] = "refused"
	case st.ready(d):
		out["state"] = "ready"
		out["version"] = st.Build.Version
		// The version is Telegram's, the same across Pluma's builds; the
		// page names the build too (ADR 0037).
		out["number"] = st.Build.Number
		out["install"] = "itms-services://?action=download-manifest&url=" +
			url.QueryEscape(s.cfg.publicURL+"/ios/adhoc/manifest?t="+d.Token)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *server) manifest(w http.ResponseWriter, r *http.Request) {
	d, st, ok := s.device(r)
	if !ok || !st.ready(d) {
		http.Error(w, "not ready", http.StatusNotFound)
		return
	}
	app := ""
	for _, b := range st.Build.Bundles {
		if b.Path == "" {
			app = b.BundleID
		}
	}
	w.Header().Set("Content-Type", "text/xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>items</key>
	<array>
		<dict>
			<key>assets</key>
			<array>
				<dict>
					<key>kind</key>
					<string>software-package</string>
					<key>url</key>
					<string>`+xmlEscape(s.cfg.publicURL+"/ios/adhoc/app.ipa?t="+url.QueryEscape(d.Token))+`</string>
				</dict>
			</array>
			<key>metadata</key>
			<dict>
				<key>bundle-identifier</key>
				<string>`+xmlEscape(app)+`</string>
				<key>bundle-version</key>
				<string>`+xmlEscape(st.Build.Version)+`</string>
				<key>kind</key>
				<string>software</string>
				<key>title</key>
				<string>Pluma</string>
			</dict>
		</dict>
	</array>
</dict>
</plist>
`)
}

func (s *server) ipa(w http.ResponseWriter, r *http.Request) {
	d, st, ok := s.device(r)
	if !ok || !st.ready(d) {
		http.Error(w, "not ready", http.StatusNotFound)
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.dataDir, "signed", st.Signed.File))
	if err != nil {
		http.Error(w, "not ready", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "Pluma.ipa", info.ModTime(), f)
}

// upload takes a build from CI: the IPA it signed (ad hoc), its version and
// number, and the bundles with their entitlements (JSON).
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(auth), []byte(s.cfg.uploadToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "multipart form expected", http.StatusBadRequest)
		return
	}
	b := &Build{Uploaded: s.now().UTC()}
	tmp, err := os.CreateTemp(filepath.Join(s.cfg.dataDir, "work"), "upload-*.ipa")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	hash := sha256.New()
	gotIPA := false
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch p.FormName() {
		case "ipa":
			if _, err := io.Copy(io.MultiWriter(tmp, hash), p); err != nil {
				http.Error(w, "upload: "+err.Error(), http.StatusBadRequest)
				return
			}
			gotIPA = true
		case "version", "number":
			v, _ := io.ReadAll(io.LimitReader(p, 64))
			if p.FormName() == "version" {
				b.Version = strings.TrimSpace(string(v))
			} else {
				b.Number = strings.TrimSpace(string(v))
			}
		case "bundles":
			if err := json.NewDecoder(io.LimitReader(p, 1<<20)).Decode(&b.Bundles); err != nil {
				http.Error(w, "bundles: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
	}
	if err := tmp.Sync(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := checkUpload(tmp.Name(), b, gotIPA); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.ID = hex.EncodeToString(hash.Sum(nil))
	dst := filepath.Join(s.cfg.dataDir, "builds", b.ID+".ipa")
	if err := os.Rename(tmp.Name(), dst); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var old string
	err = s.store.update(func(st *state) error {
		if st.Build != nil && st.Build.ID != b.ID {
			old = st.Build.ID
		}
		st.Build = b
		return nil
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if old != "" {
		_ = os.Remove(filepath.Join(s.cfg.dataDir, "builds", old+".ipa"))
	}
	s.signer.logf("iosinstall: build %s %s (%s) uploaded", short(b.ID), b.Version, b.Number)
	s.signer.kick()
	w.WriteHeader(http.StatusNoContent)
}

var versionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`)

// checkUpload: a version and number, the app and its extensions with
// bundle IDs and entitlements, all present in the IPA.
func checkUpload(ipaPath string, b *Build, gotIPA bool) error {
	if !gotIPA || !versionPattern.MatchString(b.Version) || !versionPattern.MatchString(b.Number) {
		return errors.New("an IPA, a version and a number are required")
	}
	paths := map[string]bool{}
	app := false
	for _, bundle := range b.Bundles {
		if bundle.BundleID == "" || !strings.Contains(bundle.Entitlements, "<plist") || paths[bundle.Path] {
			return fmt.Errorf("bundle %q: a bundle ID and entitlements are required, once per path", bundle.Path)
		}
		if bundle.Path != "" && (!strings.HasPrefix(bundle.Path, "PlugIns/") || !strings.HasSuffix(bundle.Path, ".appex") ||
			strings.Contains(bundle.Path, "..") || strings.Count(bundle.Path, "/") != 1) {
			return fmt.Errorf("bundle %q: an extension in PlugIns/ expected", bundle.Path)
		}
		paths[bundle.Path] = true
		app = app || bundle.Path == ""
	}
	if !app {
		return errors.New("bundles: the app itself is missing")
	}
	names, err := ipaBundles(ipaPath)
	if err != nil {
		return err
	}
	for p := range paths {
		if !names[p] {
			return fmt.Errorf("bundle %q is not in the IPA", p)
		}
	}
	for p := range names {
		if !paths[p] {
			return fmt.Errorf("the IPA has %q, which the bundles do not list", p)
		}
	}
	return nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func newUUID() string {
	t := newToken()
	h := sha256.Sum256([]byte(t))
	h[6] = h[6]&0x0f | 0x40
	h[8] = h[8]&0x3f | 0x80
	x := hex.EncodeToString(h[:16])
	return strings.ToUpper(x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32])
}
