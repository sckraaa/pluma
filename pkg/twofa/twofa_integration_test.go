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

//go:build integration

package twofa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/mail"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The table of migrate-20261024-passwords.sql on the production MySQL and
// the Lua scripts on the production Redis; the unit tests run them on fakes
// and miniredis.

var (
	itMySQL *testinfra.MySQL
	itRedis *testinfra.Redis
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itMySQL, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itRedis, errs[1] = testinfra.StartRedis(ctx) }()
	wg.Wait()
	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
	}
	_ = itMySQL.Terminate(ctx)
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// itUserSeq makes user ids unique across tests and runs (go test -count=N).
var itUserSeq atomic.Int64

func itUser() int64 {
	return time.Now().UnixMilli()%1_000_000_000*100 + itUserSeq.Add(1)
}

func itStore() *MySQLStore {
	c := itMySQL.Config()
	return NewMySQLStore(sqlx.NewMySQL(&c))
}

func TestMySQLStore(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	user := itUser()

	if p, err := s.Get(ctx, user); err != nil || p != nil {
		t.Fatalf("Get of a user without a password: %v %v", p, err)
	}
	if ok, err := s.StartReset(ctx, user, 100); err != nil || ok {
		t.Fatalf("StartReset without a password: %v %v", ok, err)
	}
	want := &Password{
		UserId:   user,
		Salt1:    bytes.Repeat([]byte{0xa1}, 40),
		Salt2:    bytes.Repeat([]byte{0x00, 0xb2}, 8),
		Verifier: append([]byte{0x00}, bytes.Repeat([]byte{0xc3}, 255)...),
		Hint:     "подсказка 🐎",
		SetAt:    1_800_000_000,
	}
	if err := s.Put(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, user)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if !bytes.Equal(got.Salt1, want.Salt1) || !bytes.Equal(got.Salt2, want.Salt2) || !bytes.Equal(got.Verifier, want.Verifier) ||
		got.Hint != want.Hint || got.SetAt != want.SetAt || got.PendingResetDate != 0 || got.PendingDeleteDate != 0 {
		t.Fatalf("Get returned %+v, want %+v", got, want)
	}

	// Reset: started once, finished only after its date.
	for i, step := range []struct {
		name string
		f    func() (bool, error)
		want bool
	}{
		{"start", func() (bool, error) { return s.StartReset(ctx, user, 2_000) }, true},
		{"start again", func() (bool, error) { return s.StartReset(ctx, user, 3_000) }, false},
		{"finish early", func() (bool, error) { return s.FinishReset(ctx, user, 1_999) }, false},
		{"cancel", func() (bool, error) { return s.CancelReset(ctx, user) }, true},
		{"cancel again", func() (bool, error) { return s.CancelReset(ctx, user) }, false},
		{"finish cancelled", func() (bool, error) { return s.FinishReset(ctx, user, 5_000) }, false},
		{"start delete", func() (bool, error) { return s.StartDelete(ctx, user, 4_000) }, true},
		{"start delete again", func() (bool, error) { return s.StartDelete(ctx, user, 5_000) }, false},
		{"start reset", func() (bool, error) { return s.StartReset(ctx, user, 2_000) }, true},
	} {
		if ok, err := step.f(); err != nil || ok != step.want {
			t.Fatalf("step %d %s: %v %v, want %v", i, step.name, ok, err, step.want)
		}
	}
	if got, _ = s.Get(ctx, user); got.PendingResetDate != 2_000 || got.PendingDeleteDate != 4_000 {
		t.Fatalf("dates %d %d", got.PendingResetDate, got.PendingDeleteDate)
	}
	// A new password drops both.
	if err = s.Put(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, user); got.PendingResetDate != 0 || got.PendingDeleteDate != 0 {
		t.Fatalf("dates after Put %d %d", got.PendingResetDate, got.PendingDeleteDate)
	}
	if ok, err := s.CancelDelete(ctx, user); err != nil || ok {
		t.Fatalf("CancelDelete of none: %v %v", ok, err)
	}
	if _, err = s.StartReset(ctx, user, 2_000); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.FinishReset(ctx, user, 2_000); err != nil || !ok {
		t.Fatalf("FinishReset on the date: %v %v", ok, err)
	}
	if got, _ = s.Get(ctx, user); got != nil {
		t.Fatal("the password is still there after the reset")
	}
	if ok, err := s.Delete(ctx, user); err != nil || ok {
		t.Fatalf("Delete of none: %v %v", ok, err)
	}
}

func TestServiceOnProductionStores(t *testing.T) {
	ctx := context.Background()
	s := NewService(itStore(), kv.NewStore(itRedis.KvConf()))
	user, other := itUser(), itUser()
	key := itUser()

	pw, err := s.Password(ctx, user)
	if err != nil || pw.GetHasPassword() {
		t.Fatalf("Password: %v %v", pw, err)
	}
	if err = s.Update(ctx, user, key, mtclient.NoPassword(), newSettings(t, pw, "pw", "hint")); err != nil {
		t.Fatalf("set: %v", err)
	}

	proof := func(userId int64, password string) *mtproto.InputCheckPasswordSRP {
		t.Helper()
		pw, err := s.Password(ctx, userId)
		if err != nil {
			t.Fatal(err)
		}
		in, err := mtclient.CheckPassword(pw, password)
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	in := proof(user, "pw")
	if err = s.Check(ctx, user, key, in); err != nil {
		t.Fatalf("the right password: %v", err)
	}
	mustError(t, s.Check(ctx, user, key, in), "SRP_ID_INVALID")
	mustError(t, s.Check(ctx, other, key, proof(user, "pw")), "PASSWORD_EMPTY")

	// Parallel wrong guesses get no more comparisons than the limit.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = map[string]int{}
	)
	guesses := make([]*mtproto.InputCheckPasswordSRP, 2*CheckPerUser.Limit)
	for i := range guesses {
		guesses[i] = proof(user, fmt.Sprintf("guess %d", i))
	}
	for i := range guesses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Check(ctx, user, 0, guesses[i])
			msg := "ok"
			if err != nil {
				msg = err.Error()
				if st, ok := status.FromError(err); ok && strings.HasPrefix(st.Message(), "FLOOD_WAIT_") {
					msg = "flood"
				}
			}
			mu.Lock()
			results[msg]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if results["flood"] != int(CheckPerUser.Limit) || len(results) != 2 {
		t.Fatalf("parallel guesses: %v", results)
	}

	// The login wait.
	if err = s.StartLogin(ctx, key, Login{UserId: user, Phone: "12015550101"}); err != nil {
		t.Fatal(err)
	}
	if l, err := s.WaitingLogin(ctx, key); err != nil || l == nil || l.UserId != user {
		t.Fatalf("WaitingLogin: %v %v", l, err)
	}
	s.EndLogin(ctx, key)
	if l, err := s.WaitingLogin(ctx, key); err != nil || l != nil {
		t.Fatalf("after EndLogin: %v %v", l, err)
	}

	if removed, err := s.Remove(ctx, user); err != nil || !removed {
		t.Fatalf("Remove: %v %v", removed, err)
	}
	if has, err := s.HasPassword(ctx, user); err != nil || has {
		t.Fatalf("HasPassword after Remove: %v %v", has, err)
	}
}

// The recovery email in user_passwords (migrate-20261116-password-email.sql):
// the migration runs again on rows that have it, every change is one
// conditional statement, and whatever removes the password removes it.
func TestMySQLStoreEmail(t *testing.T) {
	ctx := context.Background()
	s := itStore()
	user := itUser()
	p := &Password{
		UserId:   user,
		Salt1:    bytes.Repeat([]byte{0xa1}, 40),
		Salt2:    bytes.Repeat([]byte{0xb2}, 16),
		Verifier: bytes.Repeat([]byte{0xc3}, 256),
		SetAt:    1_800_000_000,
	}
	if err := s.Put(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, user); got.Email != "" || got.EmailUnconfirmed != "" || got.EmailSetAt != 0 {
		t.Fatalf("a new password has an email: %+v", got)
	}

	set := func(prev, next Emails, at int64) bool {
		t.Helper()
		ok, err := s.SwapEmail(ctx, user, p.SetAt, prev, next, at)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !set(Emails{}, Emails{Unconfirmed: "a@example.com"}, 0) {
		t.Fatal("set the waiting email")
	}
	if set(Emails{}, Emails{Unconfirmed: "b@example.com"}, 0) {
		t.Fatal("a swap from a state that is gone")
	}
	if ok, err := s.SwapEmail(ctx, user, p.SetAt+1, Emails{Unconfirmed: "a@example.com"}, Emails{Email: "a@example.com"}, 5); err != nil || ok {
		t.Fatalf("a swap under another password: %v %v", ok, err)
	}

	// Two confirmations at once: one wins.
	var (
		wg   sync.WaitGroup
		wins atomic.Int32
	)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := s.SwapEmail(ctx, user, p.SetAt, Emails{Unconfirmed: "a@example.com"}, Emails{Email: "a@example.com"}, 7); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d confirmations won", wins.Load())
	}
	got, _ := s.Get(ctx, user)
	if got.Email != "a@example.com" || got.EmailUnconfirmed != "" || got.EmailSetAt != 7 {
		t.Fatalf("after the confirmation: %+v", got)
	}

	// The migration again keeps the rows.
	path, err := testinfra.SchemaFile("migrate-20261116-password-email.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = itMySQL.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", i+2, err)
		}
	}
	if got, _ = s.Get(ctx, user); got.Email != "a@example.com" {
		t.Fatalf("after the migration ran again: %+v", got)
	}

	// Put keeps the email.
	p.SetAt++
	if err = s.Put(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, user); got.Email != "a@example.com" || got.EmailSetAt != 7 {
		t.Fatalf("after Put: %+v", got)
	}

	// Replace sets the email and awaits_email, only over the row it read.
	stale := *got
	next := *got
	next.SetAt++
	next.Verifier = bytes.Repeat([]byte{0xd4}, 256)
	next.Email, next.EmailUnconfirmed, next.EmailSetAt, next.AwaitsEmail = "c@example.com", "d@example.com", 9, true
	replace := func(prev, next *Password) bool {
		t.Helper()
		ok, err := s.Replace(ctx, prev, next)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !replace(got, &next) {
		t.Fatal("Replace over the row read")
	}
	if got, _ = s.Get(ctx, user); got.Email != "c@example.com" || got.EmailUnconfirmed != "d@example.com" || got.EmailSetAt != 9 || !got.AwaitsEmail || !bytes.Equal(got.Verifier, next.Verifier) {
		t.Fatalf("after Replace: %+v", got)
	}
	again := next
	again.SetAt++
	if replace(&stale, &again) || replace(nil, &again) {
		t.Fatal("Replace over a row that changed")
	}
	if ok, err := s.RemoveIf(ctx, &stale); err != nil || ok {
		t.Fatalf("RemoveIf of a row that changed: %v %v", ok, err)
	}
	// SwapEmail does not touch a password that is not in force.
	if ok, err := s.SwapEmail(ctx, user, next.SetAt, Emails{Email: "c@example.com", Unconfirmed: "d@example.com"}, Emails{Email: "c@example.com"}, 9); err != nil || ok {
		t.Fatalf("SwapEmail of a waiting password: %v %v", ok, err)
	}
	if ok, err := s.RemoveIf(ctx, got); err != nil || !ok {
		t.Fatalf("RemoveIf of the row read: %v %v", ok, err)
	}
	// Two first passwords at once: one is inserted.
	wins.Store(0)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := s.Replace(ctx, nil, &next); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d inserts won", wins.Load())
	}
	if got, _ = s.Get(ctx, user); got.SetAt != next.SetAt || !got.AwaitsEmail {
		t.Fatalf("after the insert: %+v", got)
	}
	// Put puts a password in force.
	if err = s.Put(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, user); got.AwaitsEmail {
		t.Fatalf("after Put: %+v", got)
	}

	// The 7-day reset removes the email with the password.
	if _, err = s.StartReset(ctx, user, 10); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.FinishReset(ctx, user, 10); err != nil || !ok {
		t.Fatalf("FinishReset: %v %v", ok, err)
	}
	if err = s.Put(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, user); got.Email != "" || got.EmailUnconfirmed != "" {
		t.Fatalf("the email outlived the reset: %+v", got)
	}
}

// sentCodes is a mail provider that keeps the codes it was given.
type sentCodes struct {
	mu    sync.Mutex
	codes map[string][]string // by address
}

func (s *sentCodes) Send(_ context.Context, m mail.Message) (string, error) {
	c := codeLine.FindStringSubmatch(m.Text)
	if c == nil {
		return "", &mail.Error{Code: "no_code"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[m.To] = append(s.codes[m.To], c[1])
	return fmt.Sprintf("id-%d", len(s.codes[m.To])), nil
}

func (s *sentCodes) last(t *testing.T, address string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.codes[address]
	if len(c) == 0 {
		t.Fatalf("no code for %s", address)
	}
	return c[len(c)-1]
}

// The email flow on MySQL and Redis: the codes burn after the wrong ones,
// parallel recoveries apply one code once, the limits count in Redis, and
// Forget and Remove take the email away.
func TestRecoveryEmailOnProductionStores(t *testing.T) {
	ctx := context.Background()
	s := NewService(itStore(), kv.NewStore(itRedis.KvConf()))
	sender := &sentCodes{codes: map[string][]string{}}
	if err := s.SetMailSender(sender, strings.Repeat("k", 32), 1000); err != nil {
		t.Fatal(err)
	}
	user, key := itUser(), itUser()
	address := fmt.Sprintf("u%d@example.com", user)

	pw, err := s.Password(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	settings := newSettings(t, pw, "pw", "")
	settings.Email = wrapperspb.String(address)
	mustError(t, s.UpdateIn(ctx, user, key, "en", mtclient.NoPassword(), settings), "EMAIL_UNCONFIRMED_6")
	code := sender.last(t, address)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < EmailCodeMaxAttempts; i++ {
		mustError(t, s.ConfirmEmail(ctx, user, key, wrong), "CODE_INVALID")
	}
	mustError(t, s.ConfirmEmail(ctx, user, key, code), "EMAIL_HASH_EXPIRED")

	// One email a minute: a resend now is FLOOD_WAIT.
	err = s.ResendEmail(ctx, user, key, "en")
	if st, ok := status.FromError(err); !ok || !strings.HasPrefix(st.Message(), "FLOOD_WAIT_") {
		t.Fatalf("a resend within a minute: %v", err)
	}
	if err = s.limiter.Reset(ctx, EmailSendPerUser, PurposeSetup+"_"+formatKeyId(user)+"_"+addressKey(address)); err != nil {
		t.Fatal(err)
	}
	if err = s.ResendEmail(ctx, user, key, "en"); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmEmail(ctx, user, key, sender.last(t, address)); err != nil {
		t.Fatal(err)
	}

	// Parallel recoveries with one code: one applies it. The five wrong
	// codes above stay counted; the counters start again for this part.
	for _, r := range []struct {
		rule    logincode.Rule
		subject string
	}{{EmailCodePerUser, formatKeyId(user)}, {EmailCodePerUserDay, formatKeyId(user)}, {EmailCodePerAuthKey, formatKeyId(key)}} {
		if err = s.limiter.Reset(ctx, r.rule, r.subject); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.RequestRecovery(ctx, user, key, "en"); err != nil {
		t.Fatal(err)
	}
	code = sender.last(t, address)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = map[string]int{}
	)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Recover(ctx, user, key, "en", code, nil)
			msg := "ok"
			if err != nil {
				msg = err.Error()
				if st, ok := status.FromError(err); ok {
					msg = st.Message()
				}
			}
			mu.Lock()
			results[msg]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if results["ok"] != 1 || results["PASSWORD_RECOVERY_EXPIRED"]+results["PASSWORD_EMPTY"] != 5 {
		t.Fatalf("parallel recoveries: %v", results)
	}
	if has, _ := s.HasPassword(ctx, user); has {
		t.Fatal("the password is still there")
	}

	// Forget and Remove take the email with the password.
	for name, remove := range map[string]func() error{
		"Forget": func() error { return s.Forget(ctx, user) },
		"Remove": func() error { _, err := s.Remove(ctx, user); return err },
	} {
		pw, err = s.Password(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Update(ctx, user, key, mtclient.NoPassword(), newSettings(t, pw, "pw", "")); err != nil {
			t.Fatal(err)
		}
		p, _ := s.store.Get(ctx, user)
		next := *p
		next.Email = address
		if ok, err := s.store.Replace(ctx, p, &next); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if err = remove(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p, _ = s.store.Get(ctx, user); p != nil {
			t.Fatalf("%s left %+v", name, p)
		}
	}
}
