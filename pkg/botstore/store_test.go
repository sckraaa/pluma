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

package botstore

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestTokens(t *testing.T) {
	token, hash, err := newToken(42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "42:") || strings.Contains(hash, token) {
		t.Fatalf("token %q, hash %q", token, hash)
	}
	id, parsed, ok := ParseToken(token)
	if !ok || id != 42 || parsed != hash {
		t.Fatalf("parsed %d %q %v", id, parsed, ok)
	}
	for _, bad := range []string{"", "42", ":abc", "42:", "x:abc", "-1:abc"} {
		if _, _, ok := ParseToken(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

// The order of core.telegram.org/bots/api#determining-list-of-commands.
func TestScopeChain(t *testing.T) {
	private := scopeChain(Viewer{UserId: 7, PeerType: mtproto.PEER_USER, PeerId: 7})
	if want := []string{"peer:2:7", "users", "default"}; !slices.Equal(private, want) {
		t.Errorf("private %v, want %v", private, want)
	}
	admin := scopeChain(Viewer{UserId: 7, PeerType: mtproto.PEER_CHANNEL, PeerId: 9, Admin: true})
	if want := []string{"peer_user:4:9:7", "peer_admins:4:9", "peer:4:9", "chat_admins", "chats", "default"}; !slices.Equal(admin, want) {
		t.Errorf("admin %v, want %v", admin, want)
	}
	member := scopeChain(Viewer{UserId: 7, PeerType: mtproto.PEER_CHAT, PeerId: 9})
	if want := []string{"peer_user:3:9:7", "peer:3:9", "chats", "default"}; !slices.Equal(member, want) {
		t.Errorf("member %v, want %v", member, want)
	}
}

func TestScopeKey(t *testing.T) {
	peer := mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 9}).To_InputPeer()
	user := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 7}).To_InputUser()
	cases := []struct {
		scope *mtproto.BotCommandScope
		want  string
	}{
		{mtproto.MakeTLBotCommandScopeDefault(nil).To_BotCommandScope(), "default"},
		{mtproto.MakeTLBotCommandScopeUsers(nil).To_BotCommandScope(), "users"},
		{mtproto.MakeTLBotCommandScopeChats(nil).To_BotCommandScope(), "chats"},
		{mtproto.MakeTLBotCommandScopeChatAdmins(nil).To_BotCommandScope(), "chat_admins"},
		{mtproto.MakeTLBotCommandScopePeer(&mtproto.BotCommandScope{Peer: peer}).To_BotCommandScope(), "peer:3:9"},
		{mtproto.MakeTLBotCommandScopePeerAdmins(&mtproto.BotCommandScope{Peer: peer}).To_BotCommandScope(), "peer_admins:3:9"},
		{mtproto.MakeTLBotCommandScopePeerUser(&mtproto.BotCommandScope{Peer: peer, UserId: user}).To_BotCommandScope(), "peer_user:3:9:7"},
	}
	for _, c := range cases {
		got, err := ScopeKey(1, c.scope)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v", c.scope.GetPredicateName(), got, err)
		}
	}
	if _, err := ScopeKey(1, mtproto.MakeTLBotCommandScopePeer(&mtproto.BotCommandScope{Peer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer()}).To_BotCommandScope()); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("empty peer: %v", err)
	}
}

func TestValidCommands(t *testing.T) {
	cmd := func(c, d string) *mtproto.BotCommand {
		return mtproto.MakeTLBotCommand(&mtproto.BotCommand{Command: c, Description: d}).To_BotCommand()
	}
	if err := ValidCommands([]*mtproto.BotCommand{cmd("start", "Start it")}); err != nil {
		t.Error(err)
	}
	for _, bad := range []*mtproto.BotCommand{cmd("Start", "x"), cmd("", "x"), cmd("a-b", "x"), cmd(strings.Repeat("a", 33), "x")} {
		if err := ValidCommands([]*mtproto.BotCommand{bad}); !errors.Is(err, mtproto.ErrBotCommandInvalid) {
			t.Errorf("%q: %v", bad.GetCommand(), err)
		}
	}
	for _, bad := range []*mtproto.BotCommand{cmd("a", ""), cmd("a", strings.Repeat("x", 257))} {
		if err := ValidCommands([]*mtproto.BotCommand{bad}); !errors.Is(err, mtproto.ErrBotCommandDescriptionInvalid) {
			t.Errorf("description %d: %v", len(bad.GetDescription()), err)
		}
	}
	over := make([]*mtproto.BotCommand, MaxCommands+1)
	for i := range over {
		over[i] = cmd("c", "d")
	}
	if err := ValidCommands(over); !errors.Is(err, mtproto.ErrBotCommandInvalid) {
		t.Errorf("too many: %v", err)
	}
}

func TestValidLangCode(t *testing.T) {
	for _, ok := range []string{"", "en", "pt-br", "zh_hans"} {
		if !ValidLangCode(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"e n", "é", strings.Repeat("a", maxLangCode+1)} {
		if ValidLangCode(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestUsernameRule(t *testing.T) {
	for _, ok := range []string{"my_bot", "MyBOT", "ab1bot"} {
		if !usernameRe.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"bot", "bo", "1abcbot", "my-bot", "myname", "a" + strings.Repeat("b", 32) + "bot"} {
		if usernameRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A menu button's link is https and fits its column.
func TestHttpsURL(t *testing.T) {
	long := "https://a/" + strings.Repeat("b", maxMenuURL-len("https://a/"))
	for u, want := range map[string]bool{
		"https://a":  true,
		long:         true,
		long + "b":   false,
		"https://":   false,
		"http://a/b": false,
	} {
		if got := httpsURL(u); got != want {
			t.Errorf("%.20q: %v", u, got)
		}
	}
}
