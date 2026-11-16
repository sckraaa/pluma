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

package urlauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func miniKV(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the
	// failing connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

func testRequest() Request {
	return Request{
		Kind:        KindOIDC,
		BotId:       1001,
		Domain:      "site.example",
		RedirectURI: "https://site.example/cb",
		State:       "st",
		Scopes:      []string{ScopeOpenID, ScopeProfile, ScopeBotAccess},
		Browser:     "Firefox 121",
		Platform:    "Linux",
		IP:          "192.0.2.7",
		Region:      "Amsterdam, Netherlands",
	}
}

// wrongOf is a code of the request that is not the right one.
func wrongOf(e *Entry) string {
	for _, c := range e.MatchCodes {
		if c != e.Code {
			return c
		}
	}
	panic("no wrong code")
}

// storeSuite runs the behaviour of Store on a Redis, miniredis here and a
// real server in the integration test.
func storeSuite(t *testing.T, s *Store) {
	ctx := context.Background()

	t.Run("create and get", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, e.Token)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusPending || got.Code != e.Code || got.Domain != "site.example" || got.IP != "192.0.2.7" ||
			len(got.MatchCodes) != MatchCodeCount || got.UserId != 0 || got.Attempts != 0 || got.CreatedAt == 0 {
			t.Fatalf("%+v", got)
		}
		if !got.WantsWrite() || got.WantsPhone() {
			t.Fatalf("scopes %v: write %v, phone %v", got.Scopes, got.WantsWrite(), got.WantsPhone())
		}
		if _, err := s.Get(ctx, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, ErrExpired) {
			t.Fatalf("missing: %v", err)
		}
		if _, err := s.Get(ctx, "bad token"); !errors.Is(err, ErrExpired) {
			t.Fatalf("bad token: %v", err)
		}
	})

	t.Run("incomplete request", func(t *testing.T) {
		r := testRequest()
		r.Domain = ""
		if _, err := s.Create(ctx, r); err == nil {
			t.Fatal("created")
		}
	})

	t.Run("check then accept", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		if r, err := s.CheckCode(ctx, e.Token, wrongOf(e)); err != nil || r != CodeWrong {
			t.Fatalf("wrong check: %v, %v", r, err)
		}
		if r, err := s.CheckCode(ctx, e.Token, e.Code); err != nil || r != CodeRight {
			t.Fatalf("right check: %v, %v", r, err)
		}
		a := Acceptance{UserId: 77, Code: e.Code, WriteAllowed: true}
		if r, err := s.Accept(ctx, e.Token, a); err != nil || r != CodeRight {
			t.Fatalf("accept: %v, %v", r, err)
		}
		// The same acceptance again is a retry; another user's is not.
		if r, err := s.Accept(ctx, e.Token, a); err != nil || r != CodeRepeated {
			t.Fatalf("repeat: %v, %v", r, err)
		}
		if _, err := s.Accept(ctx, e.Token, Acceptance{UserId: 78, Code: e.Code}); !errors.Is(err, ErrExpired) {
			t.Fatalf("another user: %v", err)
		}
		if _, err := s.CheckCode(ctx, e.Token, e.Code); !errors.Is(err, ErrExpired) {
			t.Fatalf("check after acceptance: %v", err)
		}
		if ok, err := s.Decline(ctx, e.Token); err != nil || ok {
			t.Fatalf("decline after acceptance: %v, %v", ok, err)
		}
		got, err := s.Get(ctx, e.Token)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusAccepted || got.UserId != 77 || !got.WriteAllowed || got.SharePhone || got.AcceptedAt == 0 || got.Attempts != 1 {
			t.Fatalf("%+v", got)
		}
		// The page takes the answered request once.
		taken, err := s.Take(ctx, e.Token)
		if err != nil || taken.Status != StatusAccepted || taken.UserId != 77 {
			t.Fatalf("take: %+v, %v", taken, err)
		}
		if _, err := s.Take(ctx, e.Token); !errors.Is(err, ErrExpired) {
			t.Fatalf("second take: %v", err)
		}
	})

	t.Run("pending is not taken", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			got, err := s.Take(ctx, e.Token)
			if err != nil || got.Status != StatusPending {
				t.Fatalf("take: %+v, %v", got, err)
			}
		}
	})

	t.Run("wrong codes end the request", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		wrong := wrongOf(e)
		if r, err := s.CheckCode(ctx, e.Token, wrong); err != nil || r != CodeWrong {
			t.Fatalf("1: %v, %v", r, err)
		}
		if r, err := s.Accept(ctx, e.Token, Acceptance{UserId: 5, Code: wrong}); err != nil || r != CodeWrong {
			t.Fatalf("2: %v, %v", r, err)
		}
		if r, err := s.Accept(ctx, e.Token, Acceptance{UserId: 5, Code: ""}); err != nil || r != CodeEnded {
			t.Fatalf("3: %v, %v", r, err)
		}
		if _, err := s.Accept(ctx, e.Token, Acceptance{UserId: 5, Code: e.Code}); !errors.Is(err, ErrExpired) {
			t.Fatalf("right code after the end: %v", err)
		}
	})

	t.Run("decline", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if ok, err := s.Decline(ctx, e.Token); err != nil || !ok {
				t.Fatalf("decline: %v, %v", ok, err)
			}
		}
		if _, err := s.Accept(ctx, e.Token, Acceptance{UserId: 5, Code: e.Code}); !errors.Is(err, ErrExpired) {
			t.Fatalf("accept after decline: %v", err)
		}
		taken, err := s.Take(ctx, e.Token)
		if err != nil || taken.Status != StatusDeclined {
			t.Fatalf("take: %+v, %v", taken, err)
		}
		if _, err := s.Decline(ctx, e.Token); !errors.Is(err, ErrExpired) {
			t.Fatalf("decline after take: %v", err)
		}
	})

	t.Run("an overlong code is a wrong one", func(t *testing.T) {
		e, err := s.Create(ctx, testRequest())
		if err != nil {
			t.Fatal(err)
		}
		long := e.Code
		for len(long) <= maxMatchCode {
			long += e.Code
		}
		if r, err := s.CheckCode(ctx, e.Token, long); err != nil || r != CodeWrong {
			t.Fatalf("%v, %v", r, err)
		}
	})
}

func TestStore(t *testing.T) {
	_, store := miniKV(t)
	storeSuite(t, NewStore(store))
}

// The request lives RequestTTL from its creation, answered or not.
func TestStoreExpiry(t *testing.T) {
	mr, store := miniKV(t)
	s := NewStore(store)
	ctx := context.Background()
	e, err := s.Create(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(key(e.Token)); ttl != RequestTTL {
		t.Fatalf("ttl %v", ttl)
	}
	if _, err := s.Accept(ctx, e.Token, Acceptance{UserId: 3, Code: e.Code}); err != nil {
		t.Fatal(err)
	}
	if ttl := mr.TTL(key(e.Token)); ttl != RequestTTL {
		t.Fatalf("ttl after acceptance %v", ttl)
	}
	mr.FastForward(RequestTTL + time.Second)
	if _, err := s.Get(ctx, e.Token); !errors.Is(err, ErrExpired) {
		t.Fatalf("after ttl: %v", err)
	}
}

// A damaged record is OAUTH_REQUEST_INVALID, not a crash or a login.
func TestStoreInvalid(t *testing.T) {
	mr, store := miniKV(t)
	s := NewStore(store)
	ctx := context.Background()
	for name, fields := range map[string][]string{
		"json":  {"req", "{", "code", "🐶", "state", "pending", "attempts", "0", "user_id", "0", "accepted_at", "0"},
		"state": {"req", `{"kind":1,"bot_id":1,"domain":"a.example","match_codes":["🐶","🐱","🐭","🐹"]}`, "code", "🐶", "state", "lost", "attempts", "0", "user_id", "0", "accepted_at", "0"},
		"code":  {"req", `{"kind":1,"bot_id":1,"domain":"a.example","match_codes":["🐶","🐱","🐭","🐹"]}`, "code", "🦊", "state", "pending", "attempts", "0", "user_id", "0", "accepted_at", "0"},
	} {
		token := "INVALIDINVALIDINVALID" + name
		mr.HSet(key(token), fields...)
		if _, err := s.Get(ctx, token); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
