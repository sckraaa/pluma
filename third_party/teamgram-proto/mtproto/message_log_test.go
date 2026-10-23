// pluma: regression test for audit finding #9 in EncryptedMessage2: its
// decryption error used to carry aesKey, aesIV, auth_key[88:120] and the
// first 64 bytes of the ciphertext.

package mtproto

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto/crypto"
)

// leaksBytes reports whether msg contains any 4-byte window of secret, raw or
// hex-encoded (either case).
func leaksBytes(msg string, secret []byte) bool {
	lower := strings.ToLower(msg)
	for i := 0; i+4 <= len(secret); i++ {
		w := secret[i : i+4]
		if bytes.Contains([]byte(msg), w) || strings.Contains(lower, hex.EncodeToString(w)) {
			return true
		}
	}
	return false
}

func TestEncryptedMessage2DecryptErrorCarriesNoKeyMaterial(t *testing.T) {
	authKey := crypto.GenerateNonce(256)

	// forged passes the length checks and fails only the msg_key check, the
	// error that used to carry the key material.
	plain := make([]byte, 64)
	copy(plain, crypto.GenerateNonce(64))
	binary.LittleEndian.PutUint32(plain[28:], 16)
	msgKey := crypto.GenerateNonce(16)
	aesKey, aesIV := generateMessageKey(msgKey, authKey, false)
	enc, err := crypto.NewAES256IGECryptor(aesKey, aesIV).Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	forged := append(append([]byte(nil), msgKey...), enc...)

	for _, tc := range []struct {
		name string
		b    []byte
	}{
		{"wrong msg_key", forged},
		{"random", crypto.GenerateNonce(16 + 64)},
		{"short", crypto.GenerateNonce(16 + 16)},
	} {
		in := NewEncryptedMessage2(1)
		err := in.decode(authKey, tc.b)
		if err == nil {
			t.Fatalf("%s: accepted", tc.name)
		}
		if !errors.Is(err, crypto.ErrDecryptMessage) {
			t.Errorf("%s: err = %v, want ErrDecryptMessage", tc.name, err)
		}
		aesKey, aesIV := generateMessageKey(tc.b[:16], authKey, false)
		for what, s := range map[string][]byte{
			"auth_key": authKey, "aes_key": aesKey, "aes_iv": aesIV, "data": tc.b[16:],
		} {
			if leaksBytes(err.Error(), s) {
				t.Errorf("%s: error %q contains %s", tc.name, err, what)
			}
		}
	}
}
