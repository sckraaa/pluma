// pluma: regression test for audit finding #9 — a decryption error used to
// carry aesKey, aesIV and auth_key[88:120], and gateways log these errors.

package crypto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
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

func TestDecryptErrorsCarryNoKeyMaterial(t *testing.T) {
	server, client := testKeys(t)

	type input struct {
		name         string
		msgKey, data []byte
	}
	var inputs []input
	for _, n := range []int{0, 16, 31, 48, 64, 1024} {
		inputs = append(inputs, input{"random", GenerateNonce(16), GenerateNonce(n)})
	}
	msgKey, data := encryptV2(t, client, plaintext(16, 16, 16))
	bad := append([]byte(nil), msgKey...)
	bad[3] ^= 0x80
	inputs = append(inputs,
		input{"wrong msg_key", bad, data},
		input{"bad padding", nil, nil},
	)
	inputs[len(inputs)-1].msgKey, inputs[len(inputs)-1].data = encryptV2(t, client, plaintext(32, 32, 0))

	for _, in := range inputs {
		for _, v := range []struct {
			name    string
			decrypt func(msgKey, data []byte) ([]byte, error)
			prepare func(msgKey []byte, incoming bool) ([]byte, []byte)
		}{
			{"v2", server.AesIgeDecrypt, server.prepareAES},
			{"v1", server.AesIgeDecryptV1, server.prepareAESV1},
		} {
			_, err := v.decrypt(in.msgKey, in.data)
			if err == nil {
				t.Fatalf("%s/%s: accepted", in.name, v.name)
			}
			msg := err.Error()
			secrets := map[string][]byte{"auth_key": server.AuthKey()}
			if len(in.msgKey) == 16 {
				aesKey, aesIV := v.prepare(in.msgKey, false)
				secrets["aes_key"], secrets["aes_iv"] = aesKey, aesIV
			}
			if len(in.data) >= 4 {
				secrets["data"] = in.data
			}
			for what, s := range secrets {
				if leaksBytes(msg, s) {
					t.Errorf("%s/%s: error %q contains %s", in.name, v.name, msg, what)
				}
			}
		}
	}
}

func TestLeaksBytes(t *testing.T) {
	secret := []byte{0xde, 0xad, 0xbe, 0xef, 0x01}
	for _, msg := range []string{
		"x " + hex.EncodeToString(secret[1:]),
		"x " + strings.ToUpper(hex.EncodeToString(secret)),
		"raw " + string(secret[:4]),
	} {
		if !leaksBytes(msg, secret) {
			t.Errorf("%q: leak not found", msg)
		}
	}
	if leaksBytes("aesIgeDecrypt: invalid encrypted message", secret) {
		t.Error("false positive")
	}
}
