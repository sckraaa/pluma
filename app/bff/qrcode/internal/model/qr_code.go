// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package model

import (
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"

	"github.com/teamgram/proto/mtproto/crypto"
)

const (
	QRCodeStateNew      = 1
	QRCodeStateAccepted = 2
	QRCodeStateSuccess  = 3
)

// LoginTokenSize is the size of the token of auth.loginToken: the permanent
// auth key id of the session that shows the QR code, big-endian, by which
// auth.acceptLoginToken finds the transaction, then a random secret.
//
// The size decides whether the QR code can be read at all. The client
// encodes "plm://login?token=" + base64url(token), and Web K draws it at
// error correction level L with the logo over the centre, which may hide 7%
// of the modules (tweb 346844bf src/helpers/qrCode/paintQrCode.ts:68-70,
// qr-code-styling 1.9.2 src/core/QRSVG.ts:98-99). A 24-byte token makes a
// version 3 code (29x29) that corrects 7 codewords, fewer than the logo
// spoils: no scanner read it. 34 bytes make a version 4 code (33x33) that
// corrects 10.
//
// The length is Telegram's: on 2026-10-29 the QR code of web.telegram.org/k
// encoded "tg://login?token=" and 46 base64url characters, 34 bytes (the
// QR login observation in docs/tasks/017-rebranding.md). Only the length is
// taken from there: how Telegram lays out its tokens is not known, and the
// layout above is this server's own.
const LoginTokenSize = 34

// loginTokenKeySize is the size of the auth key id that a login token
// starts with.
const loginTokenKeySize = 8

type QRCodeTransaction struct {
	PermAuthKeyId int64  `json:"perm_auth_key_id"`
	ServerId      string `json:"server_id"`
	AuthKeyId     int64  `json:"auth_key_id"`
	SessionId     int64  `json:"session_id"`
	ApiId         int32  `json:"api_id"`
	ApiHash       string `json:"api_hash"`
	CodeHash      string `json:"code_hash"`
	ExpireAt      int64  `json:"expire_at"`
	UserId        int64  `json:"user_id"`
	State         int    `json:"state"`
}

// NewCodeHash returns a new secret of a login token, from crypto/rand, as
// QRCodeTransaction.CodeHash keeps it: in hex.
func NewCodeHash() string {
	return crypto.GenerateStringNonce(LoginTokenSize - loginTokenKeySize)
}

// Token returns the login token of the transaction: the permanent auth key
// id, big-endian, and the secret of CodeHash. A transaction cached before
// tokens had LoginTokenSize has a shorter secret, and a code hash that is
// not hex gives the key id alone: such a token is never accepted.
func (m *QRCodeTransaction) Token() []byte {
	token := make([]byte, loginTokenKeySize, LoginTokenSize)
	binary.BigEndian.PutUint64(token, uint64(m.PermAuthKeyId))
	secret, err := hex.DecodeString(m.CodeHash)
	if err != nil {
		return token
	}
	return append(token, secret...)
}

// CheckByToken tells whether token is the login token of the transaction,
// in constant time.
func (m *QRCodeTransaction) CheckByToken(token []byte) bool {
	want := m.Token()
	return len(want) == LoginTokenSize && subtle.ConstantTimeCompare(want, token) == 1
}
