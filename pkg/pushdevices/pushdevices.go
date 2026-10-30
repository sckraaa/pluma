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

// Package pushdevices keeps the devices that receive push notifications
// (task 009, ADR 0017): what account.registerDevice registered, one row per
// authorization and token type. bff writes them, the push service reads
// them. The secret of a device, the key its pushes are encrypted with, is
// sealed in the database (Sealer).
package pushdevices

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
)

// Token types of account.registerDevice that the server keeps
// (https://core.telegram.org/api/push-updates).
const (
	TokenAPNs    int32 = 1  // iOS alerts, token is the hex of the APNs device token
	TokenFCM     int32 = 2  // Android, token is the FCM registration token
	TokenVoIP    int32 = 9  // iOS PushKit, for incoming calls only
	TokenWebPush int32 = 10 // Web K, token is the JSON of a PushSubscription
)

// TokenMTProto is the token type of Android's MTProto push session
// ("MTProto separate session", https://core.telegram.org/api/push-updates):
// tgnet keeps a second connection with a session of its own and registers
// that session's id as the token (DrKLO/Telegram aac1efcb
// TMessagesProj/jni/tgnet/ConnectionsManager.cpp:2398-2419). It is not
// stored here: the session service binds the push session when the
// registration passes through it and wakes the client in it (ADR 0024).
const TokenMTProto int32 = 7

// PushSessionId is the session id a TokenMTProto token names. tgnet writes
// the 64-bit session id as an unsigned decimal (Defines.h:190-194,
// ConnectionsManager.cpp:2406), so about half of the ids are over the
// int64 range as text; the id is the same 64 bits. ok is false for anything
// else, and for 0.
func PushSessionId(token string) (id int64, ok bool) {
	u, err := strconv.ParseUint(token, 10, 64)
	if err != nil || u == 0 {
		return 0, false
	}
	return int64(u), true
}

// Limits of a registration.
const (
	MaxTokenLength = 4096
	SecretLength   = 256 // the clients' secret, used as an MTProto auth key
	MaxOtherUids   = 64
)

// ErrTokenInvalidated is what Register returns for a token the provider
// rejected since it was registered: the row is gone, and the client is told
// (TOKEN_WAS_INVALIDATED) to get a new token.
var ErrTokenInvalidated = errors.New("pushdevices: the token was invalidated")

// Device is a registered device.
type Device struct {
	Id        int64
	UserId    int64
	AuthKeyId int64 // the permanent key of the authorization that registered it
	TokenType int32
	Token     string
	// AppSandbox selects the APNs sandbox gateway (development builds).
	AppSandbox bool
	// NoMuted: no background notification for muted chats either.
	NoMuted bool
	// Secret is the 256-byte key the payloads are encrypted with; empty
	// only for web push without encryption.
	Secret []byte
	// OtherUids are the other accounts of the same app, as the client said.
	OtherUids []int64
	// LangCode is the language of the authorization (initConnection), for
	// the texts the server renders.
	LangCode string
	// LockedPeriod is the inactivity after which texts are hidden
	// (account.updateDeviceLocked), seconds; 0 if not set.
	LockedPeriod int32
	CreatedAt    int64 // unix seconds
	UpdatedAt    int64 // unix seconds
}

// Store keeps the devices.
type Store interface {
	// Register adds d or replaces the device of the same authorization and
	// token type, and the same token of the same user (a new login of the
	// device). d.Id, CreatedAt and UpdatedAt are set by the store. It
	// returns ErrTokenInvalidated, and forgets the token, if the provider
	// rejected it.
	Register(ctx context.Context, d *Device) error
	// Unregister removes the token of userId; removed is false if there was
	// none.
	Unregister(ctx context.Context, userId int64, tokenType int32, token string) (removed bool, err error)
	// SetLockedPeriod sets the locked period of the devices of an
	// authorization.
	SetLockedPeriod(ctx context.Context, userId, authKeyId int64, period int32) error
	// List returns the usable devices of userId, secrets opened; a device
	// whose secret does not open is left out.
	List(ctx context.Context, userId int64) ([]*Device, error)
	// DeleteAuthorizations removes the devices of the given authorizations
	// of userId.
	DeleteAuthorizations(ctx context.Context, userId int64, authKeyIds ...int64) (removed int64, err error)
	// Delete removes one device.
	Delete(ctx context.Context, id int64) error
	// Invalidate marks a device the provider rejected: it is not listed,
	// and its next registration gets ErrTokenInvalidated.
	Invalidate(ctx context.Context, id int64) error
}

// tokenHash identifies a token in a unique key: tokens are too long to be
// indexed themselves.
func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
