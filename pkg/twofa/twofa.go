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

// Package twofa is the 2FA password of Telegram (task 008,
// https://core.telegram.org/api/srp), shared by the bff modules that take
// one: account.getPassword and the password settings (app/bff/twofa), the
// login (auth.signIn, auth.checkPassword in app/bff/authorization, the QR
// login in app/bff/qrcode), account.deleteAccount and
// messages.editChatCreator.
//
//   - The algorithm is passwordKdfAlgoSHA256SHA256PBKDF2HMACSHA512iter100000SHA256ModPow
//     with Telegram's 2048-bit prime and g = 3. The server stores the salts
//     and the verifier v = g^x mod p (table user_passwords, Store); it never
//     sees the password.
//   - account.getPassword issues an srp_id: the server's ephemeral b lives
//     in Redis for SRPTTL and checks one proof (A, M1), right or wrong. M1
//     is compared in constant time.
//   - Wrong passwords are limited per user and per auth key (Rule); past a
//     limit every check answers FLOOD_WAIT_X. A right password takes back
//     its own count only.
//   - A login of a user with a password waits for it (Login, kept per
//     permanent auth key for LoginTTL): the key is bound to the user only
//     after auth.checkPassword.
//   - There is no mail on the server, so no recovery by email:
//     has_recovery is false and a recovery email is refused. A logged-in
//     user resets a forgotten password with account.resetPassword after
//     ResetWait; for others the administrator removes it
//     (pluma-admin password reset).
//
// Nothing secret is logged: no password hash, A, M1, v, b or srp_B.
package twofa

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"strconv"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

const (
	// SRPTTL is how long an srp_id of account.getPassword can be used.
	SRPTTL = 10 * time.Minute
	// NewAlgoTTL is how long the new_algo salts of a user stay the same
	// after the last account.getPassword: a client sets a password with the
	// new_algo it was given at the start of the flow.
	NewAlgoTTL = 24 * time.Hour
	// LoginTTL is how long a login waits for the password: the life of a
	// login code.
	LoginTTL = logincode.CodeTTL
	// ResetWait is how long account.resetPassword waits before it removes
	// the password, as Telegram does.
	ResetWait = 7 * 24 * time.Hour
	// DeleteWait is how long account.deleteAccount without the password
	// waits before it deletes an account protected by one (2FA_CONFIRM_WAIT_X).
	DeleteWait = 7 * 24 * time.Hour
	// MaxHintLength is the longest hint, in characters.
	MaxHintLength = 255
)

// Limits of wrong passwords (FLOOD_WAIT_X past them). Counted before the
// comparison, so parallel guesses cannot slip past them; a right password
// is refunded.
var (
	CheckPerUser       = logincode.Rule{Name: "password_user", Limit: 5, Window: time.Hour}
	CheckPerUserDay    = logincode.Rule{Name: "password_user_day", Limit: 20, Window: 24 * time.Hour}
	CheckPerAuthKey    = logincode.Rule{Name: "password_key", Limit: 5, Window: time.Hour}
	CheckPerAuthKeyDay = logincode.Rule{Name: "password_key_day", Limit: 20, Window: 24 * time.Hour}
)

// ErrNoPassword is the answer of a check for a user without a password
// (PASSWORD_EMPTY). Callers that need a password answer PASSWORD_MISSING.
var ErrNoPassword = mtproto.ErrPasswordEmpty

// Login is a login waiting for the password of UserId on a permanent auth
// key. Phone is the phone of auth.signIn, empty for the other logins (a
// future auth token, a QR code). LangCode is the lang_code of the session
// that accepted a QR code, the language of the New login message (task
// 019), empty for the other logins.
type Login struct {
	UserId   int64
	Phone    string
	LangCode string
}

// Service applies the 2FA password. It is safe for concurrent use.
type Service struct {
	store   Store
	state   *redisState
	limiter *logincode.Limiter

	now    func() time.Time
	random io.Reader
}

// NewService returns the Service on store, with its short-lived state and
// counters in kv.
func NewService(store Store, kv kv.Store) *Service {
	return &Service{
		store:   store,
		state:   &redisState{kv: kv},
		limiter: logincode.NewLimiter(kv),
		now:     time.Now,
		random:  rand.Reader,
	}
}

// MustNewService returns the Service of a bff: passwords in MySQL, the rest
// in Redis.
func MustNewService(mysql sqlx.Config, kvConf kv.KvConf) *Service {
	return NewService(NewMySQLStore(sqlx.NewMySQL(&mysql)), kv.NewStore(kvConf))
}

// HasPassword reports whether userId has a password.
func (s *Service) HasPassword(ctx context.Context, userId int64) (bool, error) {
	p, err := s.get(ctx, userId)
	return p != nil, err
}

// StartLogin makes the login of userId on the permanent key authKeyId wait
// for the password. The caller answers SESSION_PASSWORD_NEEDED.
func (s *Service) StartLogin(ctx context.Context, authKeyId int64, l Login) error {
	if authKeyId == 0 || l.UserId == 0 {
		return mtproto.ErrInternalServerError
	}
	if err := s.state.putLogin(ctx, authKeyId, l, LoginTTL); err != nil {
		logx.WithContext(ctx).Errorf("twofa: start login of user %d: %v", l.UserId, err)
		return mtproto.ErrInternalServerError
	}
	logx.WithContext(ctx).Infof("twofa: login of user %d on key %d waits for the password", l.UserId, authKeyId)
	return nil
}

// WaitingLogin returns the login waiting for the password on authKeyId, or
// nil if there is none.
func (s *Service) WaitingLogin(ctx context.Context, authKeyId int64) (*Login, error) {
	if authKeyId == 0 {
		return nil, nil
	}
	l, err := s.state.login(ctx, authKeyId)
	if err != nil {
		logx.WithContext(ctx).Errorf("twofa: login of key %d: %v", authKeyId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return l, nil
}

// EndLogin forgets the login waiting on authKeyId (it succeeded, or the key
// logged in or out otherwise). Best effort: the wait expires anyway.
func (s *Service) EndLogin(ctx context.Context, authKeyId int64) {
	if authKeyId == 0 {
		return
	}
	if err := s.state.dropLogin(ctx, authKeyId); err != nil {
		logx.WithContext(ctx).Errorf("twofa: end login of key %d: %v", authKeyId, err)
	}
}

// Forget removes everything of userId (the account was deleted).
func (s *Service) Forget(ctx context.Context, userId int64) error {
	if _, err := s.store.Delete(ctx, userId); err != nil {
		logx.WithContext(ctx).Errorf("twofa: forget user %d: %v", userId, err)
		return mtproto.ErrInternalServerError
	}
	s.dropNewAlgo(ctx, userId)
	return nil
}

// Remove removes the password of userId (pluma-admin password reset);
// removed is false if there was none.
func (s *Service) Remove(ctx context.Context, userId int64) (bool, error) {
	removed, err := s.store.Delete(ctx, userId)
	if err != nil {
		return false, err
	}
	s.dropNewAlgo(ctx, userId)
	return removed, nil
}

func (s *Service) get(ctx context.Context, userId int64) (*Password, error) {
	if userId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	p, err := s.store.Get(ctx, userId)
	if err != nil {
		logx.WithContext(ctx).Errorf("twofa: password of user %d: %v", userId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return p, nil
}

func (s *Service) dropNewAlgo(ctx context.Context, userId int64) {
	if err := s.state.dropNewAlgo(ctx, userId); err != nil {
		logx.WithContext(ctx).Errorf("twofa: drop new algo of user %d: %v", userId, err)
	}
}

func (s *Service) randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.random, b); err != nil {
		return nil, err
	}
	return b, nil
}

// newSRPId returns a random non-zero srp_id.
func (s *Service) newSRPId() (int64, error) {
	for {
		b, err := s.randomBytes(8)
		if err != nil {
			return 0, err
		}
		if id := int64(binary.LittleEndian.Uint64(b)); id != 0 {
			return id, nil
		}
	}
}

func formatKeyId(authKeyId int64) string {
	if authKeyId == 0 {
		return ""
	}
	return strconv.FormatInt(authKeyId, 10)
}

// errInternal logs err and returns INTERNAL_SERVER_ERROR.
func errInternal(ctx context.Context, what string, err error) error {
	logx.WithContext(ctx).Errorf("twofa: %s: %v", what, err)
	return mtproto.ErrInternalServerError
}
