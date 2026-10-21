// Package serverkey loads, validates and fingerprints the MTProto server RSA
// key: the key clients use in req_DH_params to encrypt p_q_inner_data, whose
// public part is built into every (patched) client.
//
// The private key never lives in the repository. It is generated once with
// tools/genserverkey, kept in a 0600 file outside git and referenced from the
// gnetway/httpserver config together with its fingerprint; Load refuses to
// return a key that does not match that fingerprint.
package serverkey

import (
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// Bits is the only RSA key size official Telegram clients work with: the
// handshake encrypts exactly 256 bytes (RSA_PAD) with the server key.
const Bits = 2048

// PEM block types.
const (
	pemPrivatePKCS1 = "RSA PRIVATE KEY"
	pemPrivatePKCS8 = "PRIVATE KEY"
	pemPublicPKCS1  = "RSA PUBLIC KEY"
	pemPublicPKIX   = "PUBLIC KEY"
)

// Key is a server private key loaded and verified by Load.
type Key struct {
	// Path is the file the key was read from.
	Path string
	// Private is the parsed key.
	Private *rsa.PrivateKey
	// PEM is the PKCS#1 PEM exactly as read from Path
	// (for crypto.NewRSACryptorByKeyData).
	PEM []byte
	// Fingerprint is the MTProto fingerprint of the public part.
	Fingerprint uint64
}

// Fingerprint returns the MTProto fingerprint of pub, computed the way
// Telegram clients compute it: the lower 64 bits of SHA1 over the bare TL
// object rsa_public_key n:bytes e:bytes (both numbers big-endian, serialized
// as TL bytes), i.e. the last 8 bytes of the digest read as a little-endian
// long. See https://core.telegram.org/mtproto/auth_key.
func Fingerprint(pub *rsa.PublicKey) uint64 {
	x := mtproto.NewEncodeBuf(512)
	x.StringBytes(pub.N.Bytes())
	x.StringBytes(big.NewInt(int64(pub.E)).Bytes())
	sum := sha1.Sum(x.GetBuf())
	return binary.LittleEndian.Uint64(sum[12:20])
}

// ParseFingerprint parses a fingerprint as it may appear in a config: an
// unsigned decimal (the form gnetway.yaml has always used), a signed decimal
// (the TL long value) or a 0x-prefixed hex number (the form Android tgnet uses).
func ParseFingerprint(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("fingerprint is empty")
	}

	var (
		v   uint64
		err error
	)
	switch {
	case strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X"):
		v, err = strconv.ParseUint(s[2:], 16, 64)
	case strings.HasPrefix(s, "-"):
		var i int64
		i, err = strconv.ParseInt(s, 10, 64)
		v = uint64(i)
	default:
		v, err = strconv.ParseUint(s, 10, 64)
	}
	if err != nil {
		return 0, fmt.Errorf("invalid fingerprint %q: want an unsigned decimal, a signed decimal or a 0x-prefixed hex number", s)
	}

	return v, nil
}

// CheckPublicKey reports whether pub can serve as an MTProto server key.
func CheckPublicKey(pub *rsa.PublicKey) error {
	if pub == nil || pub.N == nil {
		return errors.New("empty RSA key")
	}
	if n := pub.N.BitLen(); n != Bits {
		return fmt.Errorf("RSA key is %d bits; Telegram clients require exactly %d", n, Bits)
	}
	if pub.E < 3 || pub.E%2 == 0 {
		return fmt.Errorf("invalid RSA public exponent %d", pub.E)
	}

	return nil
}

// ParsePrivateKeyPEM parses a PKCS#1 ("RSA PRIVATE KEY") PEM private key, the
// only format crypto.RSACryptor accepts, and checks it with CheckPublicKey.
func ParsePrivateKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	switch block.Type {
	case pemPrivatePKCS1:
	case pemPrivatePKCS8:
		return nil, errors.New("key is PKCS#8 (BEGIN PRIVATE KEY); convert it to PKCS#1 with: openssl rsa -in <key> -traditional -out <key.pkcs1>")
	default:
		return nil, fmt.Errorf("unexpected PEM block %q, want %q (PKCS#1)", block.Type, pemPrivatePKCS1)
	}
	if strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
		return nil, errors.New("encrypted PEM keys are not supported; protect the file with permissions instead")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#1 private key: %w", err)
	}
	if err = key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid RSA private key: %w", err)
	}
	if err = CheckPublicKey(&key.PublicKey); err != nil {
		return nil, err
	}

	return key, nil
}

// ParsePublicKeyPEM returns the public key from a PEM block in any of the
// forms in use: PKCS#1 "RSA PUBLIC KEY" (what clients embed), PKIX
// "PUBLIC KEY", or a PKCS#1 private key (its public part).
func ParsePublicKeyPEM(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}

	var pub *rsa.PublicKey
	switch block.Type {
	case pemPublicPKCS1:
		k, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1 public key: %w", err)
		}
		pub = k
	case pemPublicPKIX:
		k, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKIX public key: %w", err)
		}
		rk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is %T, not RSA", k)
		}
		pub = rk
	case pemPrivatePKCS1:
		k, err := ParsePrivateKeyPEM(data)
		if err != nil {
			return nil, err
		}
		pub = &k.PublicKey
	default:
		return nil, fmt.Errorf("unexpected PEM block %q", block.Type)
	}

	if err := CheckPublicKey(pub); err != nil {
		return nil, err
	}

	return pub, nil
}

// Load reads the server private key at path and verifies everything the
// handshake depends on: path is a regular file that other users cannot
// access, it holds a valid 2048-bit PKCS#1 RSA key, and the key's fingerprint
// equals expectedFingerprint (as written in the config, see ParseFingerprint).
func Load(path, expectedFingerprint string) (*Key, error) {
	if path == "" {
		return nil, errors.New("server RSA key: KeyFile is empty")
	}

	want, err := ParseFingerprint(expectedFingerprint)
	if err != nil {
		return nil, fmt.Errorf("server RSA key %s: KeyFingerprint: %w", path, err)
	}

	data, err := readPrivateFile(path)
	if err != nil {
		return nil, err
	}

	priv, err := ParsePrivateKeyPEM(data)
	if err != nil {
		return nil, fmt.Errorf("server RSA key %s: %w", path, err)
	}

	got := Fingerprint(&priv.PublicKey)
	if got != want {
		return nil, fmt.Errorf("server RSA key %s: fingerprint mismatch: the key has %d, KeyFingerprint is %d; "+
			"either the wrong key file is used or KeyFingerprint was not updated after the key was generated", path, got, want)
	}

	return &Key{
		Path:        path,
		Private:     priv,
		PEM:         data,
		Fingerprint: got,
	}, nil
}

// readPrivateFile reads a file holding secret material, refusing files that
// other users can access.
func readPrivateFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("server RSA key %s does not exist; generate one with tools/genserverkey (see docs/ops/security-setup.md)", path)
		}
		return nil, fmt.Errorf("server RSA key %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("server RSA key %s is not a regular file (mode %s)", path, fi.Mode())
	}
	// Windows has no POSIX permission bits to check.
	if perm := fi.Mode().Perm(); runtime.GOOS != "windows" && perm&0o007 != 0 {
		return nil, fmt.Errorf("server RSA key %s is accessible by other users (mode %04o); restrict it with: chmod 600 %s", path, perm, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("server RSA key %s is not readable: %w", path, err)
	}

	return data, nil
}
