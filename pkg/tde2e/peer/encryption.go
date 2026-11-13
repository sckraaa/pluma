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

package peer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// MessageEncryption of tde2e (MessageEncryption.cpp:23-194): AES-256-CBC
// whose key and IV come from HMAC-SHA512 of a secret and the message id,
// the message id being the first 16 bytes of HMAC-SHA256 over the padded
// data, the additional data and its length.

// minPadding is the least random prefix of encrypted data
// (MessageEncryption.cpp:19-21).
const minPadding = 16

var (
	errShortData  = errors.New("tde2e: encrypted data is less than 16 bytes")
	errDataBlocks = errors.New("tde2e: encrypted data is not a whole number of blocks")
	errMsgID      = errors.New("tde2e: msg_id mismatch")
	errPrefix     = errors.New("tde2e: invalid prefix size")
	errHeaderSize = errors.New("tde2e: a header must be 32 bytes")
)

func hmacSHA512(key, message []byte) []byte {
	m := hmac.New(sha512.New, key)
	m.Write(message)
	return m.Sum(nil)
}

func hmacSHA256(key, message []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(message)
	return m.Sum(nil)
}

// cbc returns the AES-CBC key and IV of a hash of at least 48 bytes
// (calc_aes_cbc_state_from_hash, MessageEncryption.cpp:23-30).
func cbc(hash []byte) (cipher.Block, []byte) {
	block, err := aes.NewCipher(hash[:32])
	if err != nil {
		// A 32-byte key is always valid.
		panic(err)
	}
	return block, hash[32:48]
}

// padding is the prefix of data before encryption: its first byte is its
// length, the rest random or, with deterministic, zeros; it makes the data
// a whole number of blocks with at least minPadding bytes
// (gen_random_prefix and gen_deterministic_prefix,
// MessageEncryption.cpp:32-47).
func padding(dataSize int, deterministic bool) []byte {
	n := ((minPadding + 15 + dataSize) &^ 15) - dataSize
	p := make([]byte, n)
	if !deterministic {
		if _, err := rand.Read(p); err != nil {
			panic(err)
		}
	}
	p[0] = byte(n)
	return p
}

// encryptWithPrefix encrypts data that is already padded
// (encrypt_data_with_prefix, MessageEncryption.cpp:55-85) and returns the
// message and its large msg_id, which a call packet signs.
func encryptWithPrefix(data, secret, extra []byte) (out []byte, largeMsgID [32]byte) {
	large := hmacSHA512(secret, []byte("tde2e_encrypt_data"))
	encryptSecret, hmacSecret := large[:32], large[32:64]
	tail := make([]byte, 0, len(data)+len(extra)+4)
	tail = append(tail, data...)
	tail = append(tail, extra...)
	tail = binary.LittleEndian.AppendUint32(tail, uint32(len(extra)))
	copy(largeMsgID[:], hmacSHA256(hmacSecret, tail))
	msgID := largeMsgID[:16]
	out = make([]byte, 16+len(data))
	copy(out, msgID)
	block, iv := cbc(hmacSHA512(encryptSecret, msgID))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[16:], data)
	return out, largeMsgID
}

// EncryptData encrypts data with secret, authenticating extra as well
// (encrypt_data, MessageEncryption.cpp:90-97).
func EncryptData(data, secret, extra []byte) ([]byte, [32]byte) {
	return encryptWithPrefix(append(padding(len(data), false), data...), secret, extra)
}

// DecryptData opens what EncryptData made with the same secret and extra
// (decrypt_data, MessageEncryption.cpp:99-144), and returns the large
// msg_id the sender signed.
func DecryptData(encrypted, secret, extra []byte) ([]byte, [32]byte, error) {
	var largeMsgID [32]byte
	if len(encrypted) < 16 {
		return nil, largeMsgID, errShortData
	}
	if len(encrypted)%16 != 0 {
		return nil, largeMsgID, errDataBlocks
	}
	large := hmacSHA512(secret, []byte("tde2e_encrypt_data"))
	encryptSecret, hmacSecret := large[:32], large[32:64]
	msgID, body := encrypted[:16], encrypted[16:]
	buf := make([]byte, len(body), len(body)+len(extra)+4)
	block, iv := cbc(hmacSHA512(encryptSecret, msgID))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(buf, body)
	buf = append(buf, extra...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(extra)))
	copy(largeMsgID[:], hmacSHA256(hmacSecret, buf))
	if subtle.ConstantTimeCompare(largeMsgID[:16], msgID) != 1 {
		return nil, largeMsgID, errMsgID
	}
	decrypted := buf[:len(body)]
	if len(decrypted) == 0 {
		return nil, largeMsgID, errPrefix
	}
	n := int(decrypted[0])
	if n > len(decrypted) || n < minPadding {
		return nil, largeMsgID, errPrefix
	}
	return append([]byte(nil), decrypted[n:]...), largeMsgID, nil
}

// headerCipher is the AES-CBC of a 32-byte header bound to a message: the
// key comes from the secret and the message's msg_id
// (MessageEncryption.cpp:157-194).
func headerCipher(message, secret []byte) (cipher.Block, []byte, error) {
	if len(message) < 16 {
		return nil, nil, errShortData
	}
	key := hmacSHA512(secret, []byte("tde2e_encrypt_header"))[:32]
	block, iv := cbc(hmacSHA512(key, message[:16]))
	return block, iv, nil
}

// EncryptHeader encrypts a 32-byte header for the holder of secret, bound
// to an encrypted message (encrypt_header, MessageEncryption.cpp:157-174).
func EncryptHeader(header, message, secret []byte) ([]byte, error) {
	if len(header) != 32 {
		return nil, errHeaderSize
	}
	block, iv, err := headerCipher(message, secret)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 32)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, header)
	return out, nil
}

// DecryptHeader undoes EncryptHeader (decrypt_header,
// MessageEncryption.cpp:176-194). A wrong secret gives a wrong header, not
// an error: the data it opens then fails its msg_id.
func DecryptHeader(header, message, secret []byte) ([]byte, error) {
	if len(header) != 32 {
		return nil, errHeaderSize
	}
	block, iv, err := headerCipher(message, secret)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 32)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, header)
	return out, nil
}
