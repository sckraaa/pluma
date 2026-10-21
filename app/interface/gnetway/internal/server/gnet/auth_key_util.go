// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
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

package gnet

import (
	"sync/atomic"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
)

// tempAuthKeyRecheckInterval bounds how long the gateway uses a cached
// temporary key before it asks authsession again whether the key is still
// valid (it may have expired) and what it is bound to.
const tempAuthKeyRecheckInterval = 60 // seconds

// maxUnboundTempKeyReads bounds how many times the gateway reads a temporary
// key from authsession while it stays unbound. Every message of an unbound
// key is read again (only authsession learns that auth.bindTempAuthKey
// succeeded), and each read costs authsession its MySQL queries; nothing else
// limits how long a client may talk unbound. Official clients bind a new
// temporary key with their first message, so a key still unbound after this
// many reads gets transport error -404: an honest client creates a new key,
// and making the gateway query the database takes a new handshake every
// maxUnboundTempKeyReads messages.
const maxUnboundTempKeyReads = 64

type AuthKeyUtil = authKeyUtil

var NewAuthKeyUtil = newAuthKeyUtil

// authKeyUtil is an auth key as the gateway caches it. It is not modified
// after creation; a refreshed key replaces it.
type authKeyUtil struct {
	keyData *mtproto.AuthKeyInfo
	key     *crypto.AuthKey

	// expiresAt: unix time a temporary key stops being valid, when the
	// gateway knows it (the key was created by its own handshake); 0 when
	// unknown or the key does not expire.
	expiresAt int64
	// recheckAt: unix time after which a temporary key must be read again
	// from authsession; 0 means never (permanent keys).
	recheckAt int64
	// unboundReads counts the reads of an unbound temporary key from
	// authsession (see maxUnboundTempKeyReads). It is shared by every
	// refreshed copy of the key, so concurrent connections count together.
	unboundReads *atomic.Int32
}

func newAuthKeyUtil(k *mtproto.AuthKeyInfo) *authKeyUtil {
	return &authKeyUtil{
		keyData: k,
		key:     crypto.NewAuthKey(k.AuthKeyId, k.AuthKey),
	}
}

// newCheckedAuthKeyUtil wraps key data just read from authsession (or created
// by a handshake) at unix time now. A temporary key is re-read after
// tempAuthKeyRecheckInterval; an unbound one on every message (needsRecheck).
func newCheckedAuthKeyUtil(k *mtproto.AuthKeyInfo, now, expiresAt int64) *authKeyUtil {
	u := newAuthKeyUtil(k)
	if u.isTemp() {
		u.expiresAt = expiresAt
		u.recheckAt = now + tempAuthKeyRecheckInterval
	}
	u.unboundReads = new(atomic.Int32)
	return u
}

func (k *authKeyUtil) isTemp() bool {
	t := k.keyData.GetAuthKeyType()
	return t == mtproto.AuthKeyTypeTemp || t == mtproto.AuthKeyTypeMediaTemp
}

// isUnboundTemp reports a temporary key that auth.bindTempAuthKey has not
// bound to a permanent key yet, according to authsession.
func (k *authKeyUtil) isUnboundTemp() bool {
	return k.isTemp() && k.keyData.GetPermAuthKeyId() == 0
}

// unboundReadsExhausted reports an unbound temporary key read from
// authsession more than maxUnboundTempKeyReads times.
func (k *authKeyUtil) unboundReadsExhausted() bool {
	return k.isUnboundTemp() && k.unboundReads != nil && k.unboundReads.Load() > maxUnboundTempKeyReads
}

func (k *authKeyUtil) expired(now int64) bool {
	return k.expiresAt != 0 && now >= k.expiresAt
}

// needsRecheck reports whether the key must be read again from authsession
// before a message is forwarded. An unbound temporary key always is: the
// binding becomes known only to authsession, and only a verified binding may
// route the key's traffic to a permanent key.
func (k *authKeyUtil) needsRecheck(now int64) bool {
	return k.isUnboundTemp() || (k.recheckAt != 0 && now >= k.recheckAt)
}

// sessionRoute returns the key session traffic is dispatched by and the
// perm_auth_key_id reported to the session (0 for an unbound temporary key);
// see session.RouteKey.
func (k *authKeyUtil) sessionRoute() (routeKey int64, permAuthKeyId int64) {
	routeKey, unboundTemp := session.RouteKey(k.AuthKeyId(), k.keyData.GetAuthKeyType(), k.keyData.GetPermAuthKeyId())
	if unboundTemp {
		return routeKey, 0
	}
	return routeKey, routeKey
}

func (k *authKeyUtil) Equal(o *authKeyUtil) bool {
	return k.keyData.AuthKeyId == o.keyData.AuthKeyId
}

func (k *authKeyUtil) AuthKeyId() int64 {
	return k.keyData.AuthKeyId
}

func (k *authKeyUtil) AuthKeyType() int {
	return int(k.keyData.AuthKeyType)
}

func (k *authKeyUtil) PermAuthKeyId() int64 {
	return k.keyData.PermAuthKeyId
}

func (k *authKeyUtil) TempAuthKeyId() int64 {
	return k.keyData.TempAuthKeyId
}

func (k *authKeyUtil) MediaTempAuthKeyId() int64 {
	return k.keyData.MediaTempAuthKeyId
}

func (k *authKeyUtil) AuthKey() []byte {
	return k.key.AuthKey()
}

func (k *authKeyUtil) AesIgeEncrypt(rawData []byte) ([]byte, []byte, error) {
	return k.key.AesIgeEncrypt(rawData)
}

func (k *authKeyUtil) AesIgeDecrypt(msgKey, rawData []byte) ([]byte, error) {
	return k.key.AesIgeDecrypt(msgKey, rawData)
}

// checkQueriedAuthKey checks key data that authsession returned for
// authKeyId and wraps it. known is the copy of the key the gateway had (nil
// if none): its expiry, which the gateway may know better, and its count of
// unbound reads carry over. authsession itself reports an expired key as
// AUTH_KEY_UNREGISTERED.
func checkQueriedAuthKey(authKeyId int64, keyInfo *mtproto.AuthKeyInfo, now int64, known *authKeyUtil) (*authKeyUtil, error) {
	if keyInfo == nil || keyInfo.GetAuthKeyId() != authKeyId || len(keyInfo.GetAuthKey()) != 256 {
		return nil, mtproto.ErrInternalServerError
	}

	var knownExpiresAt int64
	if known != nil && known.AuthKeyId() == authKeyId {
		knownExpiresAt = known.expiresAt
	}
	key := newCheckedAuthKeyUtil(keyInfo, now, knownExpiresAt)
	if key.expired(now) {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	if known != nil && known.AuthKeyId() == authKeyId && known.unboundReads != nil {
		key.unboundReads = known.unboundReads
	}
	if key.isUnboundTemp() {
		key.unboundReads.Add(1)
	}

	return key, nil
}
