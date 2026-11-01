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

package main

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestResignRealIPA re-signs a real build of Pluma with the real rcodesign
// and a throwaway certificate (openssl), as the service does with the
// team's: every bundle gets its profile and a signature with its
// entitlements, and rcodesign reads the result back.
//
//	IOSINSTALL_TEST_IPA=<a Pluma IPA> RCODESIGN=<rcodesign> go test -tags integration ./app/ops/iosinstall/
func TestResignRealIPA(t *testing.T) {
	ipa, rc := os.Getenv("IOSINSTALL_TEST_IPA"), os.Getenv("RCODESIGN")
	if ipa == "" || rc == "" {
		t.Skip("IOSINSTALL_TEST_IPA and RCODESIGN not set")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "key.pem")
	cert := filepath.Join(dir, "cert.pem")
	p12 := filepath.Join(dir, "test.p12")
	pw := filepath.Join(dir, "pw")
	if err := os.WriteFile(pw, []byte("throwaway"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", cert, "-days", "2", "-subj", "/CN=Throwaway Distribution/OU=TEST123456"},
		// The ciphers of a .p12 the Keychain exports (3DES, SHA-1 MAC);
		// rcodesign reads no other.
		{"pkcs12", "-export", "-inkey", key, "-in", cert, "-out", p12, "-passout", "file:" + pw,
			"-certpbe", "PBE-SHA1-3DES", "-keypbe", "PBE-SHA1-3DES", "-macalg", "sha1"},
	} {
		if out, err := exec.Command("openssl", args...).CombinedOutput(); err != nil {
			t.Fatalf("openssl %s: %v\n%s", args[0], err, out)
		}
	}

	paths, err := ipaBundles(ipa)
	if err != nil {
		t.Fatal(err)
	}
	b := &Build{ID: "test"}
	for p := range paths {
		b.Bundles = append(b.Bundles, Bundle{Path: p, BundleID: "chat.pluma.app" + strings.TrimSuffix(strings.TrimPrefix(p, "PlugIns/"), ".appex"),
			Entitlements: `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>application-identifier</key><string>TEST123456.` + p + `</string></dict></plist>`})
	}
	sort.Slice(b.Bundles, func(i, j int) bool { return b.Bundles[i].Path < b.Bundles[j].Path })
	for _, d := range []string{"builds", "signed", "work"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(ipa)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builds", "test.ipa"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &signer{dir: dir, rcodesign: rc, p12: p12, p12Password: pw, logf: t.Logf}
	profiles := map[string][]byte{}
	for _, bundle := range b.Bundles {
		profiles[bundle.Path] = []byte("profile of " + bundle.Path)
	}
	name, err := s.resign(context.Background(), b, profiles)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "signed", name)
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	app := ""
	seen := map[string]bool{}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".app/Info.plist") && strings.Count(f.Name, "/") == 2 {
			app = strings.TrimSuffix(f.Name, "/Info.plist")
		}
		if strings.HasSuffix(f.Name, "/embedded.mobileprovision") {
			seen[f.Name] = true
		}
	}
	for _, bundle := range b.Bundles {
		p := filepath.ToSlash(filepath.Join(app, bundle.Path, "embedded.mobileprovision"))
		if !seen[p] {
			t.Errorf("no profile in %s", p)
		}
	}

	// rcodesign reads every bundle's signature back: the throwaway
	// certificate and the bundle's entitlements.
	unpacked := filepath.Join(dir, "check")
	if err := os.MkdirAll(unpacked, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(unpacked)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := unzip(out, root); err != nil {
		t.Fatal(err)
	}
	for _, bundle := range b.Bundles {
		target := filepath.Join(unpacked, filepath.FromSlash(app), filepath.FromSlash(bundle.Path))
		info, err := exec.Command(rc, "print-signature-info", target).CombinedOutput()
		if err != nil {
			t.Fatalf("print-signature-info %s: %v\n%s", bundle.Path, err, info)
		}
		text := string(info)
		if !strings.Contains(text, "Throwaway Distribution") || !strings.Contains(text, "TEST123456."+bundle.Path) {
			t.Errorf("%q: not signed as asked:\n%s", bundle.Path, lastLines(text, 40))
		}
	}
	t.Logf("signed %d bundles into %s (%d bytes)", len(b.Bundles), name, fileSize(out))
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}
