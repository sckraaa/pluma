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
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"google.golang.org/grpc/status"
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
