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

package twofa

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func newTestKV(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the
	// failing connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

type testEnv struct {
	s     *Service
	store *MemStore
	mr    *miniredis.Miniredis
	now   *time.Time
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	mr, store := newTestKV(t)
	e := &testEnv{store: NewMemStore(), mr: mr}
	e.s = NewService(e.store, store)
	now := time.Unix(1_800_000_000, 0)
	e.now = &now
	e.s.now = func() time.Time { return *e.now }
	return e
}

// advance moves the clock and Redis by d.
func (e *testEnv) advance(d time.Duration) {
	*e.now = e.now.Add(d)
	e.mr.FastForward(d)
}

func errorIs(err error, message string) bool {
	st, ok := status.FromError(err)
	return ok && st.Message() == message
}

func mustError(t *testing.T, err error, message string) {
	t.Helper()
	if !errorIs(err, message) {
		t.Fatalf("got %v, want %s", err, message)
	}
}

func mustFloodWait(t *testing.T, err error) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || !strings.HasPrefix(st.Message(), "FLOOD_WAIT_") {
		t.Fatalf("got %v, want FLOOD_WAIT_X", err)
	}
}

const (
	alice  = int64(1001)
	bob    = int64(1002)
	keyOne = int64(5001)
	keyTwo = int64(5002)
)

// newSettings is what a client sends to set password under the new_algo of
// account.getPassword.
func newSettings(t *testing.T, pw *mtproto.Account_Password, password, hint string) *mtproto.Account_PasswordInputSettings {
	t.Helper()
	algo, err := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
	if err != nil {
		t.Fatal(err)
	}
	hash, err := mtclient.PasswordHash(algo, password)
	if err != nil {
		t.Fatal(err)
	}
	return mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo:         algo,
		NewPasswordHash: hash,
		Hint:            wrapperspb.String(hint),
	}).To_Account_PasswordInputSettings()
}

func removeSettings() *mtproto.Account_PasswordInputSettings {
	return mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo:         mtproto.MakeTLPasswordKdfAlgoUnknown(nil).To_PasswordKdfAlgo(),
		NewPasswordHash: []byte{},
		Hint:            wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()
}

func (e *testEnv) password(t *testing.T, userId int64) *mtproto.Account_Password {
	t.Helper()
	pw, err := e.s.Password(context.Background(), userId)
	if err != nil {
		t.Fatalf("Password(%d): %v", userId, err)
	}
	return pw
}

// proof returns the client's proof of password for a fresh srp_id of userId.
func (e *testEnv) proof(t *testing.T, userId int64, password string) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	in, err := mtclient.CheckPassword(e.password(t, userId), password)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// wrongProof returns a well-formed proof for a fresh srp_id of userId that
// no password matches (without the client's PBKDF2).
func (e *testEnv) wrongProof(t *testing.T, userId int64) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	pw := e.password(t, userId)
	a, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 2048))
	if err != nil {
		t.Fatal(err)
	}
	A := new(big.Int).Exp(big.NewInt(srpG), a, srpPInt).Bytes()
	M1 := make([]byte, 32)
	_, _ = rand.Read(M1)
	return mtproto.MakeTLInputCheckPasswordSRP(&mtproto.InputCheckPasswordSRP{
		SrpId: pw.GetSrpId().GetValue(),
		A:     A,
		M1:    M1,
	}).To_InputCheckPasswordSRP()
}

// setPassword sets password of userId, which has none.
func (e *testEnv) setPassword(t *testing.T, userId int64, password, hint string) {
	t.Helper()
	pw := e.password(t, userId)
	if pw.GetHasPassword() {
		t.Fatalf("user %d already has a password", userId)
	}
	if err := e.s.Update(context.Background(), userId, keyOne, mtclient.NoPassword(), newSettings(t, pw, password, hint)); err != nil {
		t.Fatalf("set the password: %v", err)
	}
}

func TestNoPassword(t *testing.T) {
	e := newTestEnv(t)
	pw := e.password(t, alice)
	if pw.GetPredicateName() != mtproto.Predicate_account_password {
		t.Fatalf("predicate %s", pw.GetPredicateName())
	}
	if pw.GetHasPassword() || pw.GetHasRecovery() || pw.GetCurrentAlgo() != nil || pw.GetSrpId() != nil || pw.GetSrp_B() != nil {
		t.Fatalf("a user without a password: %v", pw)
	}
	algo := pw.GetNewAlgo()
	p, g := SRPGroup()
	if algo.GetPredicateName() != mtproto.Predicate_passwordKdfAlgoModPow || algo.GetG() != g || !bytes.Equal(algo.GetP(), p) ||
		len(algo.GetSalt1()) != newSalt1PrefixLen || len(algo.GetSalt2()) != salt2Len {
		t.Fatalf("new_algo: %v", algo)
	}
	if len(pw.GetSecureRandom()) != 256 || pw.GetNewSecureAlgo().GetPredicateName() != mtproto.Predicate_securePasswordKdfAlgoPBKDF2 {
		t.Fatalf("secure_random %d bytes, new_secure_algo %v", len(pw.GetSecureRandom()), pw.GetNewSecureAlgo())
	}
	// The salts stay while the user sets a password.
	if again := e.password(t, alice).GetNewAlgo(); !bytes.Equal(again.GetSalt1(), algo.GetSalt1()) || !bytes.Equal(again.GetSalt2(), algo.GetSalt2()) {
		t.Fatal("new_algo changed between two account.getPassword")
	}
	if other := e.password(t, bob).GetNewAlgo(); bytes.Equal(other.GetSalt1(), algo.GetSalt1()) {
		t.Fatal("two users got the same new_algo salt")
	}

	ctx := context.Background()
	if has, err := e.s.HasPassword(ctx, alice); err != nil || has {
		t.Fatalf("HasPassword: %v %v", has, err)
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, mtclient.NoPassword()), "PASSWORD_EMPTY")
	if _, err := e.s.Password(ctx, 0); !errorIs(err, "AUTH_KEY_UNREGISTERED") {
		t.Fatalf("Password of user 0: %v", err)
	}
}

func TestSetCheckChangeRemove(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "correct horse", "the horse")

	pw := e.password(t, alice)
	if !pw.GetHasPassword() || pw.GetHint().GetValue() != "the horse" || pw.GetHasRecovery() ||
		len(pw.GetSrp_B()) != 256 || pw.GetSrpId().GetValue() == 0 ||
		len(pw.GetCurrentAlgo().GetSalt1()) != newSalt1PrefixLen+32 {
		t.Fatalf("account.password with a password: %v", pw)
	}
	if has, _ := e.s.HasPassword(ctx, alice); !has {
		t.Fatal("HasPassword after setting it")
	}

	if err := e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "correct horse")); err != nil {
		t.Fatalf("the right password: %v", err)
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "wrong horse")), "PASSWORD_HASH_INVALID")
	mustError(t, e.s.Check(ctx, alice, keyOne, mtclient.NoPassword()), "PASSWORD_HASH_INVALID")
	mustError(t, e.s.Check(ctx, alice, keyOne, nil), "PASSWORD_HASH_INVALID")
	if _, err := e.s.Settings(ctx, alice, keyOne, e.proof(t, alice, "correct horse")); err != nil {
		t.Fatalf("Settings: %v", err)
	}

	// Change: the current password and the new one.
	pw = e.password(t, alice)
	current, err := mtclient.CheckPassword(pw, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.s.Update(ctx, alice, keyOne, current, newSettings(t, pw, "battery staple", "")); err != nil {
		t.Fatalf("change: %v", err)
	}
	if pw = e.password(t, alice); pw.GetHint() != nil {
		t.Fatalf("an empty hint is returned: %v", pw.GetHint())
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "correct horse")), "PASSWORD_HASH_INVALID")
	if err = e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "battery staple")); err != nil {
		t.Fatalf("the new password: %v", err)
	}

	// A change needs the current password.
	pw = e.password(t, alice)
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), newSettings(t, pw, "x", "")), "PASSWORD_HASH_INVALID")

	// Remove.
	mustError(t, e.s.Update(ctx, alice, keyOne, e.wrongProof(t, alice), removeSettings()), "PASSWORD_HASH_INVALID")
	if err = e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "battery staple"), removeSettings()); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if e.password(t, alice).GetHasPassword() {
		t.Fatal("the password is still set")
	}
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), removeSettings()), "NEW_SETTINGS_EMPTY")
}

func TestSRPIdIsOneUse(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "pw-alice", "")
	e.setPassword(t, bob, "pw-bob", "")

	// Right or wrong, an srp_id checks one proof.
	in := e.proof(t, alice, "pw-alice")
	if err := e.s.Check(ctx, alice, keyOne, in); err != nil {
		t.Fatal(err)
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, in), "SRP_ID_INVALID")
	wrong := e.wrongProof(t, alice)
	mustError(t, e.s.Check(ctx, alice, keyOne, wrong), "PASSWORD_HASH_INVALID")
	mustError(t, e.s.Check(ctx, alice, keyOne, wrong), "SRP_ID_INVALID")

	// An srp_id of Bob checks nothing of Alice.
	mustError(t, e.s.Check(ctx, alice, keyOne, e.proof(t, bob, "pw-bob")), "SRP_ID_INVALID")
	// An unknown one, and one that expired.
	unknown := e.wrongProof(t, alice)
	unknown.SrpId++
	mustError(t, e.s.Check(ctx, alice, keyOne, unknown), "SRP_ID_INVALID")
	in = e.proof(t, alice, "pw-alice")
	e.advance(SRPTTL + time.Second)
	mustError(t, e.s.Check(ctx, alice, keyOne, in), "SRP_ID_INVALID")
}

func TestSRPPasswordChanged(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "first", "")
	stale := e.proof(t, alice, "first")

	pw := e.password(t, alice)
	current, err := mtclient.CheckPassword(pw, "first")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.s.Update(ctx, alice, keyOne, current, newSettings(t, pw, "second", "")); err != nil {
		t.Fatal(err)
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, stale), "SRP_PASSWORD_CHANGED")
}

func TestBadProofs(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "pw", "")
	p, _ := SRPGroup()
	pMinus1 := new(big.Int).Sub(srpPInt, big.NewInt(1)).Bytes()
	for name, f := range map[string]func(in *mtproto.InputCheckPasswordSRP){
		"A = 0":         func(in *mtproto.InputCheckPasswordSRP) { in.A = []byte{0} },
		"A = 1":         func(in *mtproto.InputCheckPasswordSRP) { in.A = []byte{1} },
		"A = p - 1":     func(in *mtproto.InputCheckPasswordSRP) { in.A = pMinus1 },
		"A = p":         func(in *mtproto.InputCheckPasswordSRP) { in.A = p },
		"A too long":    func(in *mtproto.InputCheckPasswordSRP) { in.A = append([]byte{1}, in.A...) },
		"A empty":       func(in *mtproto.InputCheckPasswordSRP) { in.A = nil },
		"M1 short":      func(in *mtproto.InputCheckPasswordSRP) { in.M1 = in.M1[:31] },
		"M1 flipped":    func(in *mtproto.InputCheckPasswordSRP) { in.M1[0] ^= 1 },
		"A of another":  func(in *mtproto.InputCheckPasswordSRP) { in.A = e.proof(t, alice, "pw").A },
		"M1 of another": func(in *mtproto.InputCheckPasswordSRP) { in.M1 = e.proof(t, alice, "pw").M1 },
	} {
		in := e.proof(t, alice, "pw")
		f(in)
		if err := e.s.Check(ctx, alice, keyTwo, in); !errorIs(err, "PASSWORD_HASH_INVALID") {
			t.Errorf("%s: %v", name, err)
		}
		// The limits allow 5 a hour.
		e.advance(time.Hour)
	}
}

func TestWrongPasswordLimits(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "pw", "")

	// Five wrong passwords an hour per user, from any key.
	for i := range CheckPerUser.Limit {
		mustError(t, e.s.Check(ctx, alice, keyOne+i, e.wrongProof(t, alice)), "PASSWORD_HASH_INVALID")
		// A right one in between does not reset the count.
		if err := e.s.Check(ctx, alice, keyOne+i, e.proof(t, alice, "pw")); err != nil && i < CheckPerUser.Limit-1 {
			t.Fatalf("the right password after %d wrong: %v", i+1, err)
		}
	}
	mustFloodWait(t, e.s.Check(ctx, alice, keyTwo+100, e.wrongProof(t, alice)))
	// Past the limit the right password waits too.
	mustFloodWait(t, e.s.Check(ctx, alice, keyTwo+100, e.proof(t, alice, "pw")))

	// The hour ends; the day limit (20) still counts.
	e.advance(time.Hour)
	if err := e.s.Check(ctx, alice, keyTwo, e.proof(t, alice, "pw")); err != nil {
		t.Fatalf("an hour later: %v", err)
	}
	for range 3 {
		for range CheckPerUser.Limit {
			mustError(t, e.s.Check(ctx, alice, 0, e.wrongProof(t, alice)), "PASSWORD_HASH_INVALID")
		}
		e.advance(time.Hour)
	}
	// 5 + 15 wrong today.
	mustFloodWait(t, e.s.Check(ctx, alice, 0, e.wrongProof(t, alice)))
	e.advance(24 * time.Hour)
	mustError(t, e.s.Check(ctx, alice, 0, e.wrongProof(t, alice)), "PASSWORD_HASH_INVALID")
}

func TestWrongPasswordLimitPerKey(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	for u := range int64(CheckPerAuthKey.Limit + 1) {
		e.setPassword(t, 2000+u, "pw", "")
	}
	// One key guessing the passwords of several users.
	for u := range int64(CheckPerAuthKey.Limit) {
		mustError(t, e.s.Check(ctx, 2000+u, keyOne, e.wrongProof(t, 2000+u)), "PASSWORD_HASH_INVALID")
	}
	last := 2000 + int64(CheckPerAuthKey.Limit)
	mustFloodWait(t, e.s.Check(ctx, last, keyOne, e.wrongProof(t, last)))
	// The refused check counted for no one: the user's own key still checks.
	if err := e.s.Check(ctx, last, keyTwo, e.proof(t, last, "pw")); err != nil {
		t.Fatalf("another key: %v", err)
	}
}

func TestNewAlgoMustBeIssued(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	pw := e.password(t, alice)

	for name, f := range map[string]func(s *mtproto.Account_PasswordInputSettings){
		"salt1 prefix": func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.Salt1[0] ^= 1 },
		"salt1 short":  func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.Salt1 = s.NewAlgo.Salt1[:newSalt1PrefixLen] },
		"salt1 long": func(s *mtproto.Account_PasswordInputSettings) {
			s.NewAlgo.Salt1 = append(s.NewAlgo.Salt1, make([]byte, 256)...)
		},
		"salt2":         func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.Salt2[0] ^= 1 },
		"salt2 dropped": func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.Salt2 = nil },
	} {
		s := newSettings(t, pw, "pw", "")
		f(s)
		if err := e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s); !errorIs(err, "NEW_SALT_INVALID") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, f := range map[string]func(s *mtproto.Account_PasswordInputSettings){
		"g":          func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.G = 2 },
		"p":          func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.P = s.NewAlgo.P[1:] },
		"hash empty": func(s *mtproto.Account_PasswordInputSettings) { s.NewPasswordHash = nil },
		"hash 1":     func(s *mtproto.Account_PasswordInputSettings) { s.NewPasswordHash = []byte{1} },
		"hash long":  func(s *mtproto.Account_PasswordInputSettings) { s.NewPasswordHash = make([]byte, 257) },
		"hint": func(s *mtproto.Account_PasswordInputSettings) {
			s.Hint = wrapperspb.String(strings.Repeat("ж", MaxHintLength+1))
		},
		"algo": func(s *mtproto.Account_PasswordInputSettings) { s.NewAlgo.PredicateName = "passwordKdfAlgoOther" },
		"remove with a hash": func(s *mtproto.Account_PasswordInputSettings) {
			s.NewAlgo = mtproto.MakeTLPasswordKdfAlgoUnknown(nil).To_PasswordKdfAlgo()
		},
	} {
		s := newSettings(t, pw, "pw", "")
		f(s)
		if err := e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s); !errorIs(err, "NEW_SETTINGS_INVALID") {
			t.Errorf("%s: %v", name, err)
		}
	}
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), nil), "NEW_SETTINGS_INVALID")

	// Salts that expired, or that were used for a password, are not accepted.
	s := newSettings(t, pw, "pw", "")
	e.advance(NewAlgoTTL + time.Second)
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s), "NEW_SALT_INVALID")
	pw = e.password(t, alice)
	s = newSettings(t, pw, "pw", "")
	if err := e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s); err != nil {
		t.Fatal(err)
	}
	if next := e.password(t, alice).GetNewAlgo(); bytes.Equal(next.GetSalt1(), pw.GetNewAlgo().GetSalt1()) {
		t.Fatal("new_algo is the same after a password was set with it")
	}
	if e.password(t, bob).GetHasPassword() {
		t.Fatal("bob has a password")
	}
}

func TestRecoveryEmail(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	pw := e.password(t, alice)

	s := newSettings(t, pw, "pw", "hint")
	s.Email = wrapperspb.String("alice@example.com")
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s), "EMAIL_INVALID")
	if e.password(t, alice).GetHasPassword() {
		t.Fatal("a password was set with the refused email")
	}
	// Skipping the email: an empty one.
	s.Email = wrapperspb.String(" ")
	if err := e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), s); err != nil {
		t.Fatalf("an empty email: %v", err)
	}

	// No new password and no email change: nothing to do, after the password.
	only := mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		Email: wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), only), "PASSWORD_HASH_INVALID")
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), only); err != nil {
		t.Fatal(err)
	}
	mustError(t, e.s.Update(ctx, bob, keyOne, mtclient.NoPassword(), only), "NEW_SETTINGS_EMPTY")

	// A refused email leaves the srp_id usable: it is checked first.
	in := e.proof(t, alice, "pw")
	only.Email = wrapperspb.String("alice@example.com")
	mustError(t, e.s.Update(ctx, alice, keyOne, in, only), "EMAIL_INVALID")
	only.Email = nil
	if err := e.s.Update(ctx, alice, keyOne, in, only); err != nil {
		t.Fatalf("the srp_id after EMAIL_INVALID: %v", err)
	}
}

func TestReset(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	r, err := e.s.Reset(ctx, alice)
	if err != nil || r.GetPredicateName() != mtproto.Predicate_account_resetPasswordOk {
		t.Fatalf("reset without a password: %v %v", r, err)
	}
	mustError(t, e.s.DeclineReset(ctx, alice), "RESET_REQUEST_MISSING")

	e.setPassword(t, alice, "pw", "")
	until := e.now.Add(ResetWait).Unix()
	for range 2 {
		r, err = e.s.Reset(ctx, alice)
		if err != nil || r.GetPredicateName() != mtproto.Predicate_account_resetPasswordRequestedWait || int64(r.GetUntilDate()) != until {
			t.Fatalf("reset: %v %v, want the wait until %d", r, err, until)
		}
		if got := e.password(t, alice).GetPendingResetDate(); got.GetValue() != int32(until) {
			t.Fatalf("pending_reset_date %v", got)
		}
		e.advance(time.Hour)
	}

	// Declined, it starts again.
	if err = e.s.DeclineReset(ctx, alice); err != nil {
		t.Fatal(err)
	}
	mustError(t, e.s.DeclineReset(ctx, alice), "RESET_REQUEST_MISSING")
	if e.password(t, alice).GetPendingResetDate() != nil {
		t.Fatal("pending_reset_date after the decline")
	}
	until = e.now.Add(ResetWait).Unix()
	if r, _ = e.s.Reset(ctx, alice); int64(r.GetUntilDate()) != until {
		t.Fatalf("a new reset waits until %d, want %d", r.GetUntilDate(), until)
	}

	// The right password does not cancel a reset the user asked for.
	if err = e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "pw")); err != nil {
		t.Fatal(err)
	}
	e.advance(ResetWait - time.Second)
	if r, _ = e.s.Reset(ctx, alice); r.GetPredicateName() != mtproto.Predicate_account_resetPasswordRequestedWait {
		t.Fatalf("a second early: %v", r)
	}
	e.advance(time.Second)
	if r, err = e.s.Reset(ctx, alice); err != nil || r.GetPredicateName() != mtproto.Predicate_account_resetPasswordOk {
		t.Fatalf("after the wait: %v %v", r, err)
	}
	if e.password(t, alice).GetHasPassword() {
		t.Fatal("the password is still set")
	}

	// A new password drops a pending reset.
	e.setPassword(t, alice, "pw", "")
	if _, err = e.s.Reset(ctx, alice); err != nil {
		t.Fatal(err)
	}
	pw := e.password(t, alice)
	current, _ := mtclient.CheckPassword(pw, "pw")
	if err = e.s.Update(ctx, alice, keyOne, current, newSettings(t, pw, "pw2", "")); err != nil {
		t.Fatal(err)
	}
	if e.password(t, alice).GetPendingResetDate() != nil {
		t.Fatal("pending_reset_date after a new password")
	}
}

func TestCheckDeletion(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	if started, err := e.s.CheckDeletion(ctx, alice, keyOne, nil); err != nil || started {
		t.Fatalf("without a password: %v %v", started, err)
	}

	e.setPassword(t, alice, "pw", "")
	// With the password: now.
	if _, err := e.s.CheckDeletion(ctx, alice, keyOne, e.proof(t, alice, "pw")); err != nil {
		t.Fatalf("with the password: %v", err)
	}
	if _, err := e.s.CheckDeletion(ctx, alice, keyOne, e.wrongProof(t, alice)); !errorIs(err, "PASSWORD_HASH_INVALID") {
		t.Fatalf("a wrong password: %v", err)
	}

	// Without it: a week.
	started, err := e.s.CheckDeletion(ctx, alice, keyOne, mtclient.NoPassword())
	mustError(t, err, "2FA_CONFIRM_WAIT_604800")
	if !started {
		t.Fatal("the first request did not start the wait")
	}
	e.advance(time.Hour)
	started, err = e.s.CheckDeletion(ctx, alice, keyTwo, nil)
	mustError(t, err, "2FA_CONFIRM_WAIT_601200")
	if started {
		t.Fatal("a repeated request started the wait")
	}

	// The owner gives the password: the deletion is cancelled.
	if err = e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "pw")); err != nil {
		t.Fatal(err)
	}
	e.advance(DeleteWait)
	if started, err = e.s.CheckDeletion(ctx, alice, keyTwo, nil); !started || !errorIs(err, "2FA_CONFIRM_WAIT_604800") {
		t.Fatalf("after the cancel: %v %v", started, err)
	}
	e.advance(DeleteWait)
	if _, err = e.s.CheckDeletion(ctx, alice, keyTwo, nil); err != nil {
		t.Fatalf("after the wait: %v", err)
	}
	if err = e.s.Forget(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if e.password(t, alice).GetHasPassword() {
		t.Fatal("Forget left the password")
	}
}

func TestLogin(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	if l, err := e.s.WaitingLogin(ctx, keyOne); err != nil || l != nil {
		t.Fatalf("no login: %v %v", l, err)
	}
	if err := e.s.StartLogin(ctx, keyOne, Login{UserId: alice, Phone: "12015550101"}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.StartLogin(ctx, keyTwo, Login{UserId: bob}); err != nil {
		t.Fatal(err)
	}
	if l, err := e.s.WaitingLogin(ctx, keyOne); err != nil || l == nil || l.UserId != alice || l.Phone != "12015550101" {
		t.Fatalf("login of key one: %v %v", l, err)
	}
	e.s.EndLogin(ctx, keyOne)
	if l, _ := e.s.WaitingLogin(ctx, keyOne); l != nil {
		t.Fatalf("after EndLogin: %v", l)
	}
	e.advance(LoginTTL + time.Second)
	if l, _ := e.s.WaitingLogin(ctx, keyTwo); l != nil {
		t.Fatalf("after LoginTTL: %v", l)
	}
	if err := e.s.StartLogin(ctx, 0, Login{UserId: alice}); err == nil {
		t.Fatal("a login without a key")
	}
}

// Redis and MySQL failures are INTERNAL_SERVER_ERROR: a check never passes
// because a store is down.
func TestFailClosed(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "pw", "")
	in := e.proof(t, alice, "pw")

	e.store.err = errors.New("mysql down")
	mustError(t, e.s.Check(ctx, alice, keyOne, in), "INTERNAL_SERVER_ERROR")
	if _, err := e.s.Password(ctx, alice); !errorIs(err, "INTERNAL_SERVER_ERROR") {
		t.Fatalf("Password: %v", err)
	}
	e.store.err = nil

	e.mr.Close()
	mustError(t, e.s.Check(ctx, alice, keyOne, in), "INTERNAL_SERVER_ERROR")
	if _, err := e.s.WaitingLogin(ctx, keyOne); !errorIs(err, "INTERNAL_SERVER_ERROR") {
		t.Fatalf("WaitingLogin: %v", err)
	}
}
