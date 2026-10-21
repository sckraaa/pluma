package serverkey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// revokedTeamgramPublicKey is the public part of teamgramd/bin/server_pkcs1.key,
// the upstream teamgram key that used to be committed to this repository
// (revoked: its private part is public). Its fingerprint was configured in
// teamgramd/etc/gnetway.yaml as KeyFingerprint "12240908862933197005".
const revokedTeamgramPublicKey = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEAvKLEOWTzt9Hn3/9Kdp/RdHcEhzmd8xXeLSpHIIzaXTLJDw8BhJy1
jR/iqeG8Je5yrtVabqMSkA6ltIpgylH///FojMsX1BHu4EPYOXQgB0qOi6kr08iX
ZIH9/iOPQOWDsL+Lt8gDG0xBy+sPe/2ZHdzKMjX6O9B4sOsxjFrk5qDoWDrioJor
AJ7eFAfPpOBf2w73ohXudSrJE0lbQ8pCWNpMY8cB9i8r+WBitcvouLDAvmtnTX7a
khoDzmKgpJBYliAY4qA73v7u5UIepE8QgV0jCOhxJCPubP8dg+/PlLLVKyxU5Cdi
QtZj2EMy4s9xlNKzX8XezE0MHEa6bQpnFwIDAQAB
-----END RSA PUBLIC KEY-----
`

// telegramProductionPublicKey is Telegram's production server key as embedded
// in the official Android client (TMessagesProj/jni/tgnet/Handshake.cpp),
// which lists its fingerprint as 0xd09d1d85de64fd85.
const telegramProductionPublicKey = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEA6LszBcC1LGzyr992NzE0ieY+BSaOW622Aa9Bd4ZHLl+TuFQ4lo4g
5nKaMBwK/BIb9xUfg0Q29/2mgIR6Zr9krM7HjuIcCzFvDtr+L0GQjae9H0pRB2OO
62cECs5HKhT5DZ98K33vmWiLowc621dQuwKWSQKjWf50XYFw42h21P2KXUGyp2y/
+aEyZ+uVgLLQbRA1dEjSDZ2iGRy12Mk5gpYc397aYp438fsJoHIgJ2lgMv5h7WY9
t6N/byY9Nw9p21Og3AoXSL2q/2IJ1WRUhebgAdGVMlV1fkuOQoEzR7EdpqtQD9Cs
5+bfo3Nhmcyvk5ftB0WkJ9z6bNZ7yxrP8wIDAQAB
-----END RSA PUBLIC KEY-----
`

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
)

// generatedKey returns a 2048-bit key shared by the tests (generation is slow).
func generatedKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, Bits)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

func pkcs1PEM(k *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
}

func TestFingerprint(t *testing.T) {
	tests := []struct {
		name string
		pem  string
		want uint64
	}{
		{"revoked teamgram key as configured in gnetway.yaml", revokedTeamgramPublicKey, 12240908862933197005},
		{"telegram production key as listed by Android tgnet", telegramProductionPublicKey, 0xd09d1d85de64fd85},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub, err := ParsePublicKeyPEM([]byte(tt.pem))
			if err != nil {
				t.Fatalf("ParsePublicKeyPEM: %v", err)
			}
			if got := Fingerprint(pub); got != tt.want {
				t.Fatalf("Fingerprint = %d (%#x), want %d (%#x)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestParseFingerprint(t *testing.T) {
	const want uint64 = 12240908862933197005
	tests := []struct {
		in      string
		want    uint64
		wantErr bool
	}{
		{in: "12240908862933197005", want: want},
		{in: "  12240908862933197005\n", want: want},
		{in: "-6205835210776354611", want: want},
		{in: "0xa9e071c1771060cd", want: want},
		{in: "0XA9E071C1771060CD", want: want},
		{in: "0", want: 0},
		{in: "", wantErr: true},
		{in: "0x", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "18446744073709551616", wantErr: true},
		{in: "-9223372036854775809", wantErr: true},
		{in: "${SERVER_RSA_KEY_FINGERPRINT}", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseFingerprint(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseFingerprint(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseFingerprint(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParsePublicKeyPEM(t *testing.T) {
	k := generatedKey(t)
	pkix, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"pkcs1 public", pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.PublicKey)}), ""},
		{"pkix public", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}), ""},
		{"pkcs1 private", pkcs1PEM(k), ""},
		{"garbage", []byte("not a key"), "no PEM block"},
		{"certificate", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}}), "unexpected PEM block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub, err := ParsePublicKeyPEM(tt.data)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if Fingerprint(pub) != Fingerprint(&k.PublicKey) {
				t.Fatal("fingerprint differs from the source key")
			}
		})
	}
}

func TestLoad(t *testing.T) {
	k := generatedKey(t)
	good := pkcs1PEM(k)
	fp := strconv.FormatUint(Fingerprint(&k.PublicKey), 10)

	small, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := pem.EncodeToMemory(&pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-256-CBC,00000000000000000000000000000000"},
		Bytes:   []byte{0},
	})

	tests := []struct {
		name    string
		data    []byte // nil: do not create the file
		mode    os.FileMode
		dir     bool
		fp      string
		wantErr string
	}{
		{name: "ok 0600", data: good, mode: 0o600, fp: fp},
		{name: "ok 0400", data: good, mode: 0o400, fp: fp},
		{name: "ok group readable", data: good, mode: 0o640, fp: fp},
		{name: "ok hex fingerprint", data: good, mode: 0o600, fp: "0x" + strconv.FormatUint(Fingerprint(&k.PublicKey), 16)},
		{name: "missing file", mode: 0o600, fp: fp, wantErr: "does not exist"},
		{name: "world readable", data: good, mode: 0o644, fp: fp, wantErr: "accessible by other users"},
		{name: "world writable", data: good, mode: 0o602, fp: fp, wantErr: "accessible by other users"},
		{name: "directory", dir: true, fp: fp, wantErr: "not a regular file"},
		{name: "fingerprint mismatch", data: good, mode: 0o600, fp: "12240908862933197005", wantErr: "fingerprint mismatch"},
		{name: "empty fingerprint", data: good, mode: 0o600, fp: "", wantErr: "KeyFingerprint"},
		{name: "1024-bit key", data: pkcs1PEM(small), mode: 0o600, fp: strconv.FormatUint(Fingerprint(&small.PublicKey), 10), wantErr: "1024 bits"},
		{name: "pkcs8 key", data: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), mode: 0o600, fp: fp, wantErr: "PKCS#8"},
		{name: "public key", data: []byte(revokedTeamgramPublicKey), mode: 0o600, fp: fp, wantErr: "unexpected PEM block"},
		{name: "encrypted key", data: encrypted, mode: 0o600, fp: fp, wantErr: "encrypted"},
		{name: "garbage", data: []byte("garbage"), mode: 0o600, fp: fp, wantErr: "no PEM block"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server_pkcs1.key")
			switch {
			case tt.dir:
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case tt.data != nil:
				if err := os.WriteFile(path, tt.data, 0o600); err != nil {
					t.Fatal(err)
				}
				// Chmod explicitly: WriteFile's mode is filtered by the umask.
				if err := os.Chmod(path, tt.mode); err != nil {
					t.Fatal(err)
				}
			}

			key, err := Load(path, tt.fp)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if key.Fingerprint != Fingerprint(&k.PublicKey) || key.Path != path || string(key.PEM) != string(good) {
				t.Fatalf("unexpected key: path %s fingerprint %d", key.Path, key.Fingerprint)
			}
		})
	}
}

func TestLoadUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read 0000 files")
	}
	k := generatedKey(t)
	path := filepath.Join(t.TempDir(), "server_pkcs1.key")
	if err := os.WriteFile(path, pkcs1PEM(k), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, strconv.FormatUint(Fingerprint(&k.PublicKey), 10))
	if err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("Load error = %v, want 'not readable'", err)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	if _, err := Load("", "1"); err == nil || !strings.Contains(err.Error(), "KeyFile is empty") {
		t.Fatalf("Load error = %v, want 'KeyFile is empty'", err)
	}
}
