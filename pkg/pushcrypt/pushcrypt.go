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

// Package pushcrypt encrypts push payloads for the official clients (task
// 009, https://core.telegram.org/api/push-updates): MTProto 2.0 from the
// server to the client (x = 8) with the device's 256-byte secret as the auth
// key. The result goes into the field "p" of the push, base64url:
//
//	p = key_id(8) ‖ msg_key(16) ‖ AES-256-IGE(LE32(len(json)) ‖ json ‖ padding)
//
// key_id is the last 8 bytes of SHA1(secret), as DrKLO/Telegram
// (PushListenerController) and Telegram-iOS (Account.swift) compute it.
package pushcrypt

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // the key id of the protocol, not a security hash
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"

	"github.com/teamgram/proto/mtproto/crypto"
)

// SecretLength is the length of a device secret.
const SecretLength = 256

// ErrDecrypt is the only error Decrypt returns for a payload that does not
// decrypt with the secret.
var ErrDecrypt = errors.New("pushcrypt: the payload does not decrypt")

// KeyId returns the key id of secret: the last 8 bytes of its SHA1.
func KeyId(secret []byte) []byte {
	h := sha1.Sum(secret) //nolint:gosec // see the import
	return h[12:20]
}

// Encrypt returns the encrypted payload for a device with secret.
func Encrypt(secret, payload []byte) ([]byte, error) {
	if len(secret) != SecretLength {
		return nil, errors.New("pushcrypt: the secret is not 256 bytes")
	}
	plain := make([]byte, 4, 4+len(payload))
	binary.LittleEndian.PutUint32(plain, uint32(len(payload)))
	plain = append(plain, payload...)
	msgKey, encrypted, err := crypto.NewAuthKey(0, secret).AesIgeEncrypt(plain)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 8+len(msgKey)+len(encrypted))
	out = append(out, KeyId(secret)...)
	out = append(out, msgKey...)
	return append(out, encrypted...), nil
}

// EncodeP encrypts payload and returns the field "p": padded base64url,
// which Android (Base64.URL_SAFE) and iOS read.
func EncodeP(secret, payload []byte) (string, error) {
	b, err := Encrypt(secret, payload)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Decrypt reverses Encrypt as a client does (Account.swift,
// decryptedNotificationPayload): it checks the key id and msg_key. For the
// tests here and the end-to-end tests.
func Decrypt(secret, p []byte) ([]byte, error) {
	if len(secret) != SecretLength || len(p) < 8+16+16 || (len(p)-24)%16 != 0 || !bytes.Equal(p[:8], KeyId(secret)) {
		return nil, ErrDecrypt
	}
	msgKey, data := p[8:24], p[24:]
	const x = 8 // from the server
	a := sha256.Sum256(append(append([]byte{}, msgKey...), secret[x:x+36]...))
	b := sha256.Sum256(append(append([]byte{}, secret[40+x:40+x+36]...), msgKey...))
	key := append(append(append([]byte{}, a[:8]...), b[8:24]...), a[24:32]...)
	iv := append(append(append([]byte{}, b[:8]...), a[8:24]...), b[24:32]...)
	plain, err := crypto.NewAES256IGECryptor(key, iv).Decrypt(data)
	if err != nil {
		return nil, ErrDecrypt
	}
	check := sha256.Sum256(append(append([]byte{}, secret[88+x:88+x+32]...), plain...))
	if subtle.ConstantTimeCompare(check[8:24], msgKey) != 1 || len(plain) < 4 {
		return nil, ErrDecrypt
	}
	n := binary.LittleEndian.Uint32(plain)
	if uint64(n) > uint64(len(plain)-4) {
		return nil, ErrDecrypt
	}
	return plain[4 : 4+n], nil
}

// DecodeP decodes the field "p" (padded or not) and decrypts it.
func DecodeP(secret []byte, p string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(string(bytes.TrimRight([]byte(p), "=")))
	if err != nil {
		return nil, ErrDecrypt
	}
	return Decrypt(secret, b)
}
