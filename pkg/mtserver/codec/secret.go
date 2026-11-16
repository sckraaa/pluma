package codec

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// A secret of the obfuscated transport, as MTProxy and the dc_options of a
// DC carry it (dcOption.secret), is 0xdd followed by 16 bytes: the
// "random padding" secret. A client given such a secret opens the
// connection with the obfuscated header and derives both obfuscation keys
// as SHA256(key + the 16 bytes), the IVs as without a secret, and always
// speaks the padded intermediate transport:
//
//   - tdesktop: TcpConnection::Protocol::Create picks VersionD (tag
//     0xdddddddd) for a 17-byte secret starting with 0xdd, and
//     Version1::prepareKey hashes the key with the 16 bytes
//     (Telegram/SourceFiles/mtproto/connection_tcp.cpp:160-165, 235-248);
//   - Android: Connection::sendData sets ProtocolTypeDD and the tag
//     0xdddddddd, and encryptKeyWithSecret hashes the key with bytes 1-16
//     (TMessagesProj/jni/tgnet/Connection.cpp:458-466, 528-532, 634-655);
//   - iOS: MTProxySecretType1 keeps the 16 bytes after 0xdd and selects the
//     intermediate format 0xdddddddd; MTTcpConnection hashes the key with them
//     (submodules/MtProtoKit/Sources/MTApiEnvironment.m:113-119,
//     MTTcpConnection.m:933-936, 1281-1283, 1302-1330).
//
// Only the "dd" form is accepted here: a bare 16-byte secret lets a client
// pick an unpadded transport, and the "ee" (fake TLS) form needs a TLS
// handshake this package does not implement.

// SecretSize is the length of the key part of a secret.
const SecretSize = 16

// secretTagPadded is the first byte of a "random padding" secret.
const secretTagPadded = 0xdd

// Secret is the key part of a "dd" secret of the obfuscated transport.
type Secret [SecretSize]byte

var errSecretFormat = errors.New("codec: the secret must be 0xdd followed by 16 bytes")

// ParseSecret parses a secret as dcOption.secret carries it: 0xdd and 16
// bytes.
func ParseSecret(b []byte) (Secret, error) {
	var s Secret
	if len(b) != 1+SecretSize || b[0] != secretTagPadded {
		return s, errSecretFormat
	}
	copy(s[:], b[1:])
	return s, nil
}

// ParseSecretHex parses a secret written in hex, "dd" and 32 hex digits, as
// proxy links and the configuration write it.
func ParseSecretHex(h string) (Secret, error) {
	b, err := hex.DecodeString(h)
	if err != nil {
		return Secret{}, fmt.Errorf("%w: %v", errSecretFormat, err)
	}
	return ParseSecret(b)
}

// Bytes returns the secret as dcOption.secret carries it: 0xdd and the 16
// bytes.
func (s Secret) Bytes() []byte {
	b := make([]byte, 0, 1+SecretSize)
	b = append(b, secretTagPadded)
	return append(b, s[:]...)
}

// CreateSecretCodec reads the header of an obfuscated connection made with
// the secret s and returns its codec. Nothing else is accepted: no plain
// transport, no header without the secret (its tag decrypts to garbage), no
// transport but the padded intermediate one. ErrUnexpectedEOF means the
// header has not fully arrived yet.
func CreateSecretCodec(conn CodecReader, s Secret) (Codec, error) {
	rData, _ := conn.Peek(-1)
	if len(rData) < obfuscatedHeaderSize {
		return nil, ErrUnexpectedEOF
	}
	return createObfuscatedCodec(conn, rData[:obfuscatedHeaderSize], s[:])
}

// obfuscationKey returns the AES-256-CTR key of one direction of an
// obfuscated connection: key itself without a secret, SHA256(key + secret)
// with one.
func obfuscationKey(key, secret []byte) []byte {
	if secret == nil {
		return key
	}
	h := sha256.New()
	h.Write(key)
	h.Write(secret)
	return h.Sum(nil)
}
