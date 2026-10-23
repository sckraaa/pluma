package gnet

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// writeKey generates a server key into dir and returns its config entry.
func writeKey(t *testing.T, dir, name string) (config.RSAKey, *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, serverkey.Bits)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	if err = os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return config.RSAKey{
		KeyFile:        path,
		KeyFingerprint: strconv.FormatUint(serverkey.Fingerprint(&k.PublicKey), 10),
	}, k
}

func TestLoadRSAKeys(t *testing.T) {
	dir := t.TempDir()
	first, k1 := writeKey(t, dir, "first.key")
	second, k2 := writeKey(t, dir, "second.key")

	exposed, _ := writeKey(t, dir, "exposed.key")
	if err := os.Chmod(exposed.KeyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	mismatch := config.RSAKey{KeyFile: first.KeyFile, KeyFingerprint: second.KeyFingerprint}
	missing := config.RSAKey{KeyFile: filepath.Join(dir, "missing.key"), KeyFingerprint: first.KeyFingerprint}
	// The fingerprint of the revoked key that used to be committed to the repo.
	revoked := config.RSAKey{KeyFile: first.KeyFile, KeyFingerprint: "12240908862933197005"}

	tests := []struct {
		name    string
		keys    []config.RSAKey
		wantErr string
	}{
		{name: "one key", keys: []config.RSAKey{first}},
		{name: "rotation with two keys", keys: []config.RSAKey{first, second}},
		{name: "no keys", keys: nil, wantErr: "RSAKey is empty"},
		{name: "duplicate", keys: []config.RSAKey{first, first}, wantErr: "duplicates"},
		{name: "world readable", keys: []config.RSAKey{first, exposed}, wantErr: "accessible by other users"},
		{name: "fingerprint mismatch", keys: []config.RSAKey{mismatch}, wantErr: "fingerprint mismatch"},
		{name: "old fingerprint left in config", keys: []config.RSAKey{revoked}, wantErr: "fingerprint mismatch"},
		{name: "missing file", keys: []config.RSAKey{missing}, wantErr: "does not exist"},
		{name: "unresolved placeholder", keys: []config.RSAKey{{KeyFile: first.KeyFile, KeyFingerprint: "${SERVER_RSA_KEY_FINGERPRINT}"}}, wantErr: "invalid fingerprint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := LoadRSAKeys(tt.keys)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != len(tt.keys) {
				t.Fatalf("got %d keys, want %d", len(keys), len(tt.keys))
			}
		})
	}

	// The handshake advertises every fingerprint in resPQ and decrypts with
	// the key the client picked.
	keys, err := LoadRSAKeys([]config.RSAKey{first, second})
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHandshake(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []*rsa.PrivateKey{k1, k2} {
		fp := int64(serverkey.Fingerprint(&k.PublicKey))
		found := false
		for _, v := range h.keyFingerprints {
			found = found || v == fp
		}
		if !found {
			t.Fatalf("fingerprint %d not advertised: %v", fp, h.keyFingerprints)
		}

		cryptor := h.getKey(fp)
		if cryptor == nil {
			t.Fatalf("no cryptor for fingerprint %d", fp)
		}
		msg := big.NewInt(0x1234567890)
		enc := new(big.Int).Exp(msg, big.NewInt(int64(k.E)), k.N)
		if got := new(big.Int).SetBytes(cryptor.Decrypt(enc.Bytes())); got.Cmp(msg) != 0 {
			t.Fatalf("cryptor for %d decrypts with the wrong key", fp)
		}
	}
	if h.getKey(12345) != nil {
		t.Fatal("unknown fingerprint must not resolve to a key")
	}
}
