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
)

type fakeGhosts struct {
	accounts map[string]*ghostAccount
	chats    map[chatRef]*ghostChat
	hiddenIn map[int64]bool // chat id: the account is hidden there
	setTo    []bool
	visible  int
}

func newFakeGhosts() *fakeGhosts {
	support := &ghostAccount{Id: 136817688, FirstName: "Pluma", LastName: "Support", Username: "support"}
	group := &ghostChat{Id: 1001, Title: "Bugs"}
	return &fakeGhosts{
		accounts: map[string]*ghostAccount{"136817688": support, "support": support, "@support": support,
			"136817700": {Id: 136817700, FirstName: "Plain"}},
		chats: map[chatRef]*ghostChat{
			{Id: 1001}: group, {Username: "bugs"}: group, {Invite: "AbCd"}: group,
			{Id: 1002}: {Id: 1002, Title: "News", Broadcast: true},
		},
		hiddenIn: map[int64]bool{},
	}
}

func (f *fakeGhosts) Account(_ context.Context, ref string) (*ghostAccount, error) {
	return f.accounts[strings.ToLower(ref)], nil
}

func (f *fakeGhosts) Chat(_ context.Context, ref chatRef) (*ghostChat, error) {
	return f.chats[ref], nil
}

func (f *fakeGhosts) SetGhost(_ context.Context, userId int64, ghost bool) (bool, error) {
	for _, a := range f.accounts {
		if a.Id == userId {
			changed := a.Ghost != ghost
			a.Ghost = ghost
			return changed, nil
		}
	}
	return false, errors.New("no account")
}

func (f *fakeGhosts) SetHidden(_ context.Context, _ int64, hidden bool) (int32, error) {
	f.setTo = append(f.setTo, hidden)
	return 3, nil
}

func (f *fakeGhosts) Visible(context.Context, int64) (int, error) { return f.visible, nil }

func (f *fakeGhosts) Join(_ context.Context, _ int64, chatId int64) (bool, error) {
	if f.hiddenIn[chatId] {
		return false, nil
	}
	f.hiddenIn[chatId] = true
	return true, nil
}

func (f *fakeGhosts) Leave(_ context.Context, _ int64, chatId int64) (bool, error) {
	was := f.hiddenIn[chatId]
	delete(f.hiddenIn, chatId)
	return was, nil
}

func (f *fakeGhosts) Hidden(context.Context, int64) ([]ghostChat, error) {
	var out []ghostChat
	for id := range f.hiddenIn {
		for _, c := range f.chats {
			if c.Id == id {
				out = append(out, *c)
				break
			}
		}
	}
	return out, nil
}

func ghost(t *testing.T, f *fakeGhosts, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runGhost(context.Background(), f, append([]string{"ghost"}, args...), &out)
	return out.String(), err
}

// TestGhostSet: on makes the account a ghost and hides its rows, with the
// chats where it stays shown said; again changes nothing; off undoes it.
// The output names the account without its phone.
func TestGhostSet(t *testing.T) {
	f := newFakeGhosts()
	f.visible = 2
	out, err := ghost(t, f, "set", "@support", "on")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user 136817688 (@support, Pluma Support) is a ghost now", "hidden in 3 chats", "stays shown in 2 chats"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q lacks %q", out, want)
		}
	}
	if out, _ = ghost(t, f, "set", "136817688", "on"); !strings.Contains(out, "is a ghost already") {
		t.Errorf("again: %q", out)
	}
	if out, _ = ghost(t, f, "set", "support", "off"); !strings.Contains(out, "is no ghost now") || !strings.Contains(out, "shown again in 3 chats") {
		t.Errorf("off: %q", out)
	}
	if len(f.setTo) != 3 || !f.setTo[0] || f.setTo[2] {
		t.Errorf("rows set %v", f.setTo)
	}
	for _, bad := range [][]string{{"set", "support", "maybe"}, {"set", "support"}, {"set", "@", "on"}} {
		if _, err := ghost(t, f, bad...); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v, want usage", bad, err)
		}
	}
	if _, err := ghost(t, f, "set", "+79991234567", "on"); err == nil || !strings.Contains(err.Error(), "not by phone") {
		t.Errorf("by phone: %v", err)
	}
	if _, err := ghost(t, f, "set", "nobody", "on"); err == nil {
		t.Error("an unknown account was found")
	}
}

// TestGhostJoinLeave: a ghost joins a chat named by link, @name or id,
// once; leaves it; the list shows where it is; an account that is no ghost
// and an unknown chat are refused.
func TestGhostJoinLeave(t *testing.T) {
	f := newFakeGhosts()
	if _, err := ghost(t, f, "join", "support", "1001"); err == nil || !strings.Contains(err.Error(), "is no ghost") {
		t.Errorf("before set: %v", err)
	}
	f.accounts["support"].Ghost = true
	out, err := ghost(t, f, "join", "support", "https://t.me/+AbCd")
	if err != nil || !strings.Contains(out, `joined supergroup 1001 "Bugs" hidden`) {
		t.Fatalf("join: %q, %v", out, err)
	}
	if out, _ = ghost(t, f, "join", "support", "@bugs"); !strings.Contains(out, "already") {
		t.Errorf("again: %q", out)
	}
	if out, err = ghost(t, f, "join", "support", "t.me/c/1002/15"); err != nil || !strings.Contains(out, "channel 1002") {
		t.Errorf("by t.me/c: %q, %v", out, err)
	}
	if out, _ = ghost(t, f, "list", "support"); !strings.Contains(out, "Bugs") || !strings.Contains(out, "News") {
		t.Errorf("list: %q", out)
	}
	if out, _ = ghost(t, f, "leave", "support", "-1001001"); !strings.Contains(out, "left supergroup 1001") {
		t.Errorf("leave by a -100 id: %q", out)
	}
	if out, _ = ghost(t, f, "leave", "support", "1001"); !strings.Contains(out, "nothing to do") {
		t.Errorf("leave again: %q", out)
	}
	if _, err = ghost(t, f, "join", "support", "@nochat"); err == nil {
		t.Error("an unknown chat was joined")
	}
}

func TestParseChatRef(t *testing.T) {
	for in, want := range map[string]chatRef{
		"42":                         {Id: 42},
		"-10042":                     {Id: 42},
		"@bugs":                      {Username: "bugs"},
		"bugs":                       {Username: "bugs"},
		"https://t.me/bugs":          {Username: "bugs"},
		"t.me/bugs/17":               {Username: "bugs"},
		"https://t.me/+AbCd":         {Invite: "AbCd"},
		"https://t.me/joinchat/AbCd": {Invite: "AbCd"},
		"https://t.me/c/42/7":        {Id: 42},
		"pluma.example/c/42":         {Id: 42},
	} {
		got, err := parseChatRef(in)
		if err != nil || got != want {
			t.Errorf("parseChatRef(%q) = %+v, %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"0", "@", "https://t.me/", "https://t.me/c/x"} {
		if _, err := parseChatRef(bad); err == nil {
			t.Errorf("parseChatRef(%q) accepted", bad)
		}
	}
}
