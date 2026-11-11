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

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/conf"
)

type fakeAccounts struct {
	byId       map[int64]*account
	byUsername map[string]*account
	verified   map[int64]bool
	set        []int64
	failSet    error
}

func (f *fakeAccounts) ById(_ context.Context, id int64) (*account, error) {
	return f.byId[id], nil
}

func (f *fakeAccounts) ByUsername(_ context.Context, username string) (*account, error) {
	return f.byUsername[strings.ToLower(username)], nil
}

func (f *fakeAccounts) SetVerified(_ context.Context, id int64, verified bool) (bool, error) {
	f.set = append(f.set, id)
	if f.failSet != nil {
		return false, f.failSet
	}
	changed := f.verified[id] != verified
	f.verified[id] = verified
	return changed, nil
}

func newFakeAccounts() *fakeAccounts {
	support := &account{Id: 136817688, FirstName: "Pluma", LastName: "Support", Username: "support"}
	plain := &account{Id: 136817700}
	return &fakeAccounts{
		byId:       map[int64]*account{support.Id: support, plain.Id: plain},
		byUsername: map[string]*account{"support": support},
		verified:   map[int64]bool{},
	}
}

func TestVerifyUser(t *testing.T) {
	f := newFakeAccounts()
	for _, c := range []struct {
		ref      string
		verified bool
		want     string
	}{
		{"@support", true, "user 136817688 (@support, Pluma Support) is verified now; its devices and those who see it get updateUser.\n"},
		{"Support", true, "user 136817688 (@support, Pluma Support) is already verified.\n"},
		{"136817688", false, "user 136817688 (@support, Pluma Support) is not verified now; its devices and those who see it get updateUser.\n"},
		{"136817688", false, "user 136817688 (@support, Pluma Support) is already not verified.\n"},
		{"136817700", true, "user 136817700 is verified now; its devices and those who see it get updateUser.\n"},
	} {
		var out bytes.Buffer
		if err := verifyUser(context.Background(), f, c.ref, c.verified, &out); err != nil {
			t.Fatalf("%s %v: %v", c.ref, c.verified, err)
		}
		if out.String() != c.want {
			t.Errorf("%s %v: %q, want %q", c.ref, c.verified, out.String(), c.want)
		}
	}
	if !f.verified[136817700] || f.verified[136817688] {
		t.Errorf("badges %v", f.verified)
	}
}

// An unknown account or an id that cannot be one changes nothing.
func TestVerifyUserUnknown(t *testing.T) {
	f := newFakeAccounts()
	for ref, want := range map[string]string{
		"@nobody":   "no account @nobody",
		"nobody":    "no account nobody",
		"42":        "no account 42",
		"0":         "invalid user id 0",
		"-5":        "invalid user id -5",
		"+15550100": "an account is named by @username or user id, not by phone",
	} {
		var out bytes.Buffer
		err := verifyUser(context.Background(), f, ref, true, &out)
		if err == nil || err.Error() != want || out.Len() != 0 {
			t.Errorf("%s: %v, %q; want %q", ref, err, out.String(), want)
		}
	}
	if len(f.set) != 0 {
		t.Errorf("changed %v", f.set)
	}
}

// A failure of the user service is the command's failure, with no report
// of a change.
func TestVerifyUserServiceFails(t *testing.T) {
	f := newFakeAccounts()
	f.failSet = mtproto.ErrUserIdInvalid
	var out bytes.Buffer
	if err := verifyUser(context.Background(), f, "@support", true, &out); !errors.Is(err, mtproto.ErrUserIdInvalid) || out.Len() != 0 {
		t.Errorf("%v, %q", err, out.String())
	}
}

func TestUserUsage(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"user"},
		{"user", "verify"},
		{"user", "verify", "@support"},
		{"user", "verify", "@support", "yes"},
		{"user", "verify", "@support", "on", "extra"},
		{"user", "verify", "@", "on"},
		{"user", "unverify", "@support", "on"},
	} {
		if code, _, _ := h.run(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

// TestLoadsUserConfig: user verify finds the database and the user service
// in both bff configs.
func TestLoadsUserConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("REDIS_PASSWORD", "rpw")
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	t.Setenv("LOGIN_REVIEW_PHONE", "")
	t.Setenv("LOGIN_REVIEW_CODE", "")
	t.Setenv("LOGIN_CODES_SENDER_URL", "")
	t.Setenv("LOGIN_CODES_SENDER_TOKEN", "")
	t.Setenv("TELEGRAM_API_ID", "")
	t.Setenv("TELEGRAM_API_HASH", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("PLUMA_RELEASES_URL", "")
	t.Setenv("PLUMA_IOS_UPDATE_SECRET", "")
	t.Setenv("CALLS_ICE_SERVERS", "")
	t.Setenv("CALLS_TURN_SECRET", "")
	t.Setenv("CALLS_QUALITY", "")
	t.Setenv("PUSH_SECRET_ENCRYPTION_KEY", testHmacKey)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c userConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.Mysql.DSN == "" || c.BizServiceClient.Etcd.Key != "service.biz_service" {
			t.Errorf("%s: database set %v, user service %q", f, c.Mysql.DSN != "", c.BizServiceClient.Etcd.Key)
		}
	}
}
