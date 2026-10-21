// pluma: tests for the checks AesIgeDecrypt and AesIgeDecryptV1 make on
// untrusted input (lengths, padding, msg_key).

package crypto

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func testKeys(t *testing.T) (server, client *AuthKey) {
	t.Helper()
	data := GenerateNonce(256)
	return NewAuthKey(0, data), NewClientAuthKey(0, data)
}

// plaintext is salt, session_id, msg_id, seq_no, message_data_length
// (declaredLen), dataLen bytes of data and paddingLen bytes of padding.
func plaintext(declaredLen uint32, dataLen, paddingLen int) []byte {
	b := make([]byte, 32+dataLen+paddingLen)
	copy(b, GenerateNonce(len(b)))
	binary.LittleEndian.PutUint32(b[28:], declaredLen)
	return b
}

// encryptV2 encrypts plain as a client does in MTProto 2.0, with a valid
// msg_key whatever plain contains (plain must be 16-byte aligned).
func encryptV2(t *testing.T, client *AuthKey, plain []byte) (msgKey, data []byte) {
	t.Helper()
	h := sha256.New()
	h.Write(client.partForMsgKey(true))
	h.Write(plain)
	sum := h.Sum(nil)
	msgKey = sum[8:24]
	aesKey, aesIV := client.prepareAES(msgKey, true)
	data, err := NewAES256IGECryptor(aesKey, aesIV).Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	return msgKey, data
}

// encryptV1 encrypts plain in MTProto 1.0, msg_key being the SHA1 of its
// first 32+messageLen bytes.
func encryptV1(t *testing.T, client *AuthKey, plain []byte, messageLen int) (msgKey, data []byte) {
	t.Helper()
	sum := sha1.Sum(plain[:32+messageLen])
	msgKey = sum[4:20]
	aesKey, aesIV := client.prepareAESV1(msgKey, true)
	data, err := NewAES256IGECryptor(aesKey, aesIV).Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	return msgKey, data
}

func TestAesIgeDecryptRoundTrip(t *testing.T) {
	server, client := testKeys(t)
	raw := plaintext(40, 40, 0)[:72] // header and data; AesIgeEncrypt pads
	msgKey, data, err := client.AesIgeEncrypt(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := server.AesIgeDecrypt(msgKey, data)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got[:len(raw)]) != string(raw) {
		t.Fatal("plaintext differs")
	}
}

func TestAesIgeDecryptChecks(t *testing.T) {
	server, client := testKeys(t)
	cases := []struct {
		name  string
		plain []byte
		ok    bool
	}{
		{"padding 12", plaintext(4, 4, 12), true},
		{"padding 16", plaintext(16, 16, 16), true},
		{"padding 1024", plaintext(16, 16, 1024), true},
		{"empty data, padding 16", plaintext(0, 0, 16), true},
		{"padding 8", plaintext(24, 24, 8), false},
		{"padding 0", plaintext(32, 32, 0), false},
		{"padding 1040", plaintext(16, 16, 1040), false},
		{"length not divisible by 4", plaintext(18, 18, 14), false},
		{"length beyond plaintext", plaintext(64, 16, 16), false},
		{"length overflows uint32 sum", plaintext(0xffffffe0, 16, 16), false},
		{"negative length", plaintext(0x80000000, 16, 16), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgKey, data := encryptV2(t, client, tc.plain)
			_, err := server.AesIgeDecrypt(msgKey, data)
			if tc.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrDecryptMessage) {
				t.Fatalf("err = %v, want ErrDecryptMessage", err)
			}
		})
	}

	t.Run("wrong msg_key", func(t *testing.T) {
		msgKey, data := encryptV2(t, client, plaintext(16, 16, 16))
		bad := append([]byte(nil), msgKey...)
		bad[0] ^= 1
		if _, err := server.AesIgeDecrypt(bad, data); !errors.Is(err, ErrDecryptMessage) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("other key", func(t *testing.T) {
		other, _ := testKeys(t)
		msgKey, data := encryptV2(t, client, plaintext(16, 16, 16))
		if _, err := other.AesIgeDecrypt(msgKey, data); !errors.Is(err, ErrDecryptMessage) {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestAesIgeDecryptShortInput feeds lengths that used to panic (slicing
// x[28:] or rawData[:64]) and misaligned ones.
func TestAesIgeDecryptShortInput(t *testing.T) {
	server, _ := testKeys(t)
	for _, n := range []int{0, 1, 15, 16, 31, 32, 33, 47, 48, 63, 64, 65} {
		for _, keyLen := range []int{0, 15, 16} {
			msgKey, data := make([]byte, keyLen), GenerateNonce(n)
			if _, err := server.AesIgeDecrypt(msgKey, data); err == nil {
				t.Errorf("V2: %d bytes, msg_key %d bytes: accepted", n, keyLen)
			}
			if _, err := server.AesIgeDecryptV1(msgKey, data); err == nil {
				t.Errorf("V1: %d bytes, msg_key %d bytes: accepted", n, keyLen)
			}
		}
	}
}

func TestAesIgeDecryptV1Checks(t *testing.T) {
	server, client := testKeys(t)
	cases := []struct {
		name       string
		plain      []byte
		messageLen int // for msg_key
		ok         bool
	}{
		{"no padding", plaintext(32, 32, 0), 32, true},
		{"padding 12", plaintext(36, 36, 12), 36, true},
		{"padding 16", plaintext(16, 16, 16), 16, false},
		{"length not divisible by 4", plaintext(30, 30, 0)[:62], 30, false},
		{"length beyond plaintext", plaintext(64, 32, 0), 32, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plain := tc.plain
			if len(plain)%16 != 0 {
				plain = append(plain, make([]byte, 16-len(plain)%16)...)
			}
			msgKey, data := encryptV1(t, client, plain, tc.messageLen)
			_, err := server.AesIgeDecryptV1(msgKey, data)
			if tc.ok && err != nil {
				t.Fatalf("rejected: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrDecryptMessage) {
				t.Fatalf("err = %v, want ErrDecryptMessage", err)
			}
		})
	}
}

func FuzzAesIgeDecrypt(f *testing.F) {
	data := GenerateNonce(256)
	server := NewAuthKey(0, data)
	f.Add(make([]byte, 16), make([]byte, 48))
	f.Add(make([]byte, 16), make([]byte, 31))
	f.Add([]byte{}, []byte{})
	f.Fuzz(func(t *testing.T, msgKey, raw []byte) {
		_, _ = server.AesIgeDecrypt(msgKey, raw)
		_, _ = server.AesIgeDecryptV1(msgKey, raw)
	})
}
