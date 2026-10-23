package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// revokedTeamgramPublicKey is the public part of the key that used to be
// committed as teamgramd/bin/server_pkcs1.key (revoked: its private part is
// public). teamgramd/etc/gnetway.yaml configured it as
// KeyFingerprint "12240908862933197005".
const revokedTeamgramPublicKey = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEAvKLEOWTzt9Hn3/9Kdp/RdHcEhzmd8xXeLSpHIIzaXTLJDw8BhJy1
jR/iqeG8Je5yrtVabqMSkA6ltIpgylH///FojMsX1BHu4EPYOXQgB0qOi6kr08iX
ZIH9/iOPQOWDsL+Lt8gDG0xBy+sPe/2ZHdzKMjX6O9B4sOsxjFrk5qDoWDrioJor
AJ7eFAfPpOBf2w73ohXudSrJE0lbQ8pCWNpMY8cB9i8r+WBitcvouLDAvmtnTX7a
khoDzmKgpJBYliAY4qA73v7u5UIepE8QgV0jCOhxJCPubP8dg+/PlLLVKyxU5Cdi
QtZj2EMy4s9xlNKzX8XezE0MHEa6bQpnFwIDAQAB
-----END RSA PUBLIC KEY-----
`

func TestShowRevokedKeyMatchesOldConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old_public.pem")
	if err := os.WriteFile(path, []byte(revokedTeamgramPublicKey), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"-show", path}, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, want := range []string{
		"SERVER_RSA_KEY_FINGERPRINT): 12240908862933197005\n",
		"-6205835210776354611\n",
		"0xa9e071c1771060cd\n",
		revokedTeamgramPublicKey,
		"modulus:  bca2c43964f3b7d1e7dfff4a769fd174770487399df315de2d2a47208cda5d32c90f0f01849cb58d1fe2a9e1bc25ee72aed55a6ea312900ea5b48a60ca51fffff1688ccb17d411eee043d8397420074a8e8ba92bd3c8976481fdfe238f40e583b0bf8bb7c8031b4c41cbeb0f7bfd991ddcca3235fa3bd078b0eb318c5ae4e6a0e8583ae2a09a2b009ede1407cfa4e05fdb0ef7a215ee752ac913495b43ca4258da4c63c701f62f2bf96062b5cbe8b8b0c0be6b674d7eda921a03ce62a0a49058962018e2a03bdefeeee5421ea44f10815d2308e8712423ee6cff1d83efcf94b2d52b2c54e4276242d663d84332e2cf7194d2b35fc5decc4d0c1c46ba6d0a6717\n",
		"exponent: 010001\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, out.String())
		}
	}
}

var fingerprintLine = regexp.MustCompile(`SERVER_RSA_KEY_FINGERPRINT\): (\d+)\n`)

func TestGenerate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "server_pkcs1.key")

	var out bytes.Buffer
	if err := run([]string{"-out", path}, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key mode = %04o, want 0600", perm)
	}

	m := fingerprintLine.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no fingerprint in output:\n%s", out.String())
	}
	key, err := serverkey.Load(path, m[1])
	if err != nil {
		t.Fatalf("generated key does not load with the printed fingerprint: %v", err)
	}
	if !strings.Contains(out.String(), "-----BEGIN RSA PUBLIC KEY-----") {
		t.Fatalf("no public key PEM in output:\n%s", out.String())
	}

	// -show on the private key prints the same fingerprint.
	var shown bytes.Buffer
	if err = run([]string{"-show", path}, &shown, io.Discard); err != nil {
		t.Fatalf("run -show: %v", err)
	}
	if m2 := fingerprintLine.FindStringSubmatch(shown.String()); m2 == nil || m2[1] != m[1] {
		t.Fatalf("-show fingerprint %v, want %s", m2, m[1])
	}

	// A second run must not replace the key.
	err = run([]string{"-out", path}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("second run error = %v, want refusal", err)
	}
	if _, err = serverkey.Load(path, m[1]); err != nil {
		t.Fatalf("key changed after refused overwrite: %v", err)
	}
	if key.Fingerprint == 12240908862933197005 {
		t.Fatal("generated the revoked key")
	}
}

func TestArgs(t *testing.T) {
	tests := [][]string{
		nil,
		{"-out", "a", "-show", "b"},
		{"-out", "a", "extra"},
		{"-unknown"},
	}
	for _, args := range tests {
		if err := run(args, io.Discard, io.Discard); err == nil {
			t.Errorf("run(%q) succeeded, want a usage error", args)
		}
	}
}
