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

package nodekey

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	k, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file mode %04o, want 0600", perm)
	}
	pub, err := serverkey.ParsePublicKeyPEM(k.PublicPEM)
	if err != nil {
		t.Fatal(err)
	}
	if serverkey.Fingerprint(pub) != k.Fingerprint {
		t.Fatal("the public PEM does not match the fingerprint")
	}
	if string(k.PublicPEM[:30]) != "-----BEGIN RSA PUBLIC KEY-----" {
		t.Fatalf("not PKCS#1: %q", k.PublicPEM[:30])
	}
	left, _ := filepath.Glob(filepath.Join(dir, FileName+".*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}

	again, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Fingerprint != k.Fingerprint {
		t.Fatal("a second start made a new key")
	}
}

func TestDamagedKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("a damaged key was accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, FileName)); string(b) != "not a key" {
		t.Fatal("a damaged key was replaced")
	}
}

func TestExposedKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, FileName), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("a key readable by others was accepted")
	}
}
