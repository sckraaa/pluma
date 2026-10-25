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

package pushdevices

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestMemStore(t *testing.T) {
	testStore(t, func() Store { return NewMemStore() }, func() int64 { return 1001 })
}

func testSecret(b byte) []byte { return bytes.Repeat([]byte{b}, SecretLength) }

// testStore checks the rules of a Store; newUser gives a user id that no
// other test uses, nor the next ten ids. Authorization ids are made of it.
func testStore(t *testing.T, newStore func() Store, newUser func() int64) {
	ctx := context.Background()
	list := func(t *testing.T, s Store, user int64) []*Device {
		t.Helper()
		ds, err := s.List(ctx, user)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		return ds
	}
	register := func(t *testing.T, s Store, d Device) *Device {
		t.Helper()
		if err := s.Register(ctx, &d); err != nil {
			t.Fatalf("Register %+v: %v", d, err)
		}
		if d.Id == 0 || d.CreatedAt == 0 || d.UpdatedAt == 0 {
			t.Fatalf("Register did not set the id and dates: %+v", d)
		}
		return &d
	}

	t.Run("register and list", func(t *testing.T) {
		s, user := newStore(), newUser()
		want := register(t, s, Device{
			UserId: user, AuthKeyId: user*100 + 11, TokenType: TokenFCM, Token: "fcm-token-1",
			NoMuted: true, Secret: testSecret(0xa1), OtherUids: []int64{user + 1, user + 2}, LangCode: "ru",
		})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 12, TokenType: TokenAPNs, Token: "ab12", AppSandbox: true, Secret: testSecret(0xb2)})
		register(t, s, Device{UserId: user + 1, AuthKeyId: user*100 + 13, TokenType: TokenFCM, Token: "fcm-token-1", Secret: testSecret(0xc3)})
		got := list(t, s, user)
		if len(got) != 2 {
			t.Fatalf("List: %d devices, want 2", len(got))
		}
		d := got[0]
		if d.Id != want.Id || d.AuthKeyId != user*100+11 || d.TokenType != TokenFCM || d.Token != "fcm-token-1" || !d.NoMuted || d.AppSandbox ||
			!bytes.Equal(d.Secret, testSecret(0xa1)) || !slices.Equal(d.OtherUids, []int64{user + 1, user + 2}) || d.LangCode != "ru" {
			t.Fatalf("List[0] = %+v, want %+v", d, want)
		}
		if !got[1].AppSandbox || !bytes.Equal(got[1].Secret, testSecret(0xb2)) {
			t.Fatalf("List[1] = %+v", got[1])
		}
	})

	t.Run("one token per authorization and type", func(t *testing.T) {
		s, user := newStore(), newUser()
		first := register(t, s, Device{UserId: user, AuthKeyId: user*100 + 21, TokenType: TokenAPNs, Token: "old", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 21, TokenType: TokenVoIP, Token: "voip", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 21, TokenType: TokenAPNs, Token: "new", Secret: testSecret(2)})
		got := list(t, s, user)
		if len(got) != 2 || got[0].Token != "voip" || got[1].Token != "new" || !bytes.Equal(got[1].Secret, testSecret(2)) {
			t.Fatalf("after a new APNs token: %v", tokens(got))
		}
		// The same token again keeps the first registration date.
		again := register(t, s, Device{UserId: user, AuthKeyId: user*100 + 21, TokenType: TokenAPNs, Token: "new", Secret: testSecret(2)})
		if again.CreatedAt < first.CreatedAt || len(list(t, s, user)) != 2 {
			t.Fatalf("re-registration: %+v", again)
		}
	})

	t.Run("a token moves to the new login", func(t *testing.T) {
		s, user := newStore(), newUser()
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 31, TokenType: TokenFCM, Token: "t", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 32, TokenType: TokenFCM, Token: "t", Secret: testSecret(2)})
		got := list(t, s, user)
		if len(got) != 1 || got[0].AuthKeyId != user*100+32 || !bytes.Equal(got[0].Secret, testSecret(2)) {
			t.Fatalf("after a new login with the same token: %+v", got)
		}
	})

	t.Run("unregister", func(t *testing.T) {
		s, user := newStore(), newUser()
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 41, TokenType: TokenFCM, Token: "t", Secret: testSecret(1)})
		if ok, err := s.Unregister(ctx, user+1, TokenFCM, "t"); err != nil || ok {
			t.Fatalf("Unregister of another user: %v %v", ok, err)
		}
		if ok, err := s.Unregister(ctx, user, TokenAPNs, "t"); err != nil || ok {
			t.Fatalf("Unregister of another type: %v %v", ok, err)
		}
		if ok, err := s.Unregister(ctx, user, TokenFCM, "t"); err != nil || !ok {
			t.Fatalf("Unregister: %v %v", ok, err)
		}
		if len(list(t, s, user)) != 0 {
			t.Fatal("the device is still listed")
		}
	})

	t.Run("authorizations", func(t *testing.T) {
		s, user := newStore(), newUser()
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 51, TokenType: TokenAPNs, Token: "a", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 51, TokenType: TokenVoIP, Token: "b", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 52, TokenType: TokenFCM, Token: "c", Secret: testSecret(1)})
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 53, TokenType: TokenFCM, Token: "d", Secret: testSecret(1)})
		if err := s.SetLockedPeriod(ctx, user, user*100+51, 300); err != nil {
			t.Fatal(err)
		}
		if n, err := s.DeleteAuthorizations(ctx, user+1, user*100+51, user*100+52); err != nil || n != 0 {
			t.Fatalf("DeleteAuthorizations of another user: %d %v", n, err)
		}
		if n, err := s.DeleteAuthorizations(ctx, user, user*100+52, user*100+53); err != nil || n != 2 {
			t.Fatalf("DeleteAuthorizations: %d %v", n, err)
		}
		got := list(t, s, user)
		if len(got) != 2 || got[0].LockedPeriod != 300 || got[1].LockedPeriod != 300 {
			t.Fatalf("after DeleteAuthorizations: %+v", got)
		}
		if n, err := s.DeleteAuthorizations(ctx, user); err != nil || n != 0 {
			t.Fatalf("DeleteAuthorizations of none: %d %v", n, err)
		}
	})

	t.Run("invalidated token", func(t *testing.T) {
		s, user := newStore(), newUser()
		d := register(t, s, Device{UserId: user, AuthKeyId: user*100 + 61, TokenType: TokenAPNs, Token: "gone", Secret: testSecret(1)})
		if err := s.Invalidate(ctx, d.Id); err != nil {
			t.Fatal(err)
		}
		if len(list(t, s, user)) != 0 {
			t.Fatal("an invalidated device is listed")
		}
		err := s.Register(ctx, &Device{UserId: user, AuthKeyId: user*100 + 61, TokenType: TokenAPNs, Token: "gone", Secret: testSecret(1)})
		if !errors.Is(err, ErrTokenInvalidated) {
			t.Fatalf("Register of an invalidated token: %v, want ErrTokenInvalidated", err)
		}
		// Told once: the client got a new token, or tries this one again.
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 61, TokenType: TokenAPNs, Token: "gone", Secret: testSecret(1)})
		if len(list(t, s, user)) != 1 {
			t.Fatal("the token registered again is not listed")
		}
		if err = s.Delete(ctx, list(t, s, user)[0].Id); err != nil || len(list(t, s, user)) != 0 {
			t.Fatalf("Delete: %v", err)
		}
	})

	t.Run("web push without a secret", func(t *testing.T) {
		s, user := newStore(), newUser()
		register(t, s, Device{UserId: user, AuthKeyId: user*100 + 71, TokenType: TokenWebPush, Token: `{"endpoint":"https://push.example/x"}`})
		got := list(t, s, user)
		if len(got) != 1 || len(got[0].Secret) != 0 {
			t.Fatalf("web push device: %+v", got)
		}
	})
}

func tokens(ds []*Device) []string {
	s := make([]string, len(ds))
	for i, d := range ds {
		s[i] = d.Token
	}
	return s
}
