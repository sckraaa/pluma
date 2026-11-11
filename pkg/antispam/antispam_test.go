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

package antispam

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

const (
	group = int64(5000)
	now   = int64(1_800_000_000)
)

func text(s string, entities ...*mtproto.MessageEntity) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Message: s, Entities: entities}).To_Message()
}

func entity(predicate string, offset, length int32) *mtproto.MessageEntity {
	return &mtproto.MessageEntity{PredicateName: predicate, Offset: offset, Length: length}
}

func mentionName(id int64) *mtproto.MessageEntity {
	return &mtproto.MessageEntity{PredicateName: mtproto.Predicate_messageEntityMentionName, UserId_INT64: id}
}

func forwarded(from *mtproto.Peer) *mtproto.Message {
	m := text("look")
	m.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: from}).To_MessageFwdHeader()
	return m
}

var (
	newcomer = Sender{Joined: now - 3600}
	oldtimer = Sender{Joined: now - NewPeriod - 1}
)

func TestNew(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Sender
		want bool
	}{
		{"joined an hour ago", newcomer, true},
		{"joined just under 72 hours ago", Sender{Joined: now - NewPeriod + 1}, true},
		{"joined 72 hours ago", Sender{Joined: now - NewPeriod}, false},
		{"a guest of the discussion", Sender{Guest: true}, true},
		{"new with four messages kept", Sender{Joined: now - 60, Kept: NewMessages - 1}, true},
		{"new with five messages kept", Sender{Joined: now - 60, Kept: NewMessages}, false},
		{"a guest with five messages kept", Sender{Guest: true, Kept: NewMessages}, false},
	} {
		if got := c.s.New(now); got != c.want {
			t.Errorf("%s: new %v, want %v", c.name, got, c.want)
		}
	}
}

// TestEvaluate: each rule, for whom it holds, and what stays.
func TestEvaluate(t *testing.T) {
	url := text("see example.com", entity(mtproto.Predicate_messageEntityUrl, 4, 11))
	textUrl := text("see here", entity(mtproto.Predicate_messageEntityTextUrl, 4, 4))
	card := text("see")
	card.Media = mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{}).To_MessageMedia()
	many := text("hi", mentionName(1), mentionName(2), mentionName(3), mentionName(4), mentionName(5))
	four := text("hi", mentionName(1), mentionName(2), mentionName(3), mentionName(4))
	for _, c := range []struct {
		name       string
		s          Sender
		m          *mtproto.Message
		crossPosts int
		want       Rule
	}{
		{"a new member's link", newcomer, url, 1, RuleLink},
		{"a new member's text link", newcomer, textUrl, 1, RuleLink},
		{"a new member's link card", newcomer, card, 1, RuleLink},
		{"an old member's link", oldtimer, url, 1, RuleNone},
		{"an admin's link", Sender{Exempt: true, Joined: now}, url, 1, RuleNone},
		{"a new member's forward from a channel", newcomer, forwarded(mtproto.MakePeerChannel(7)), 1, RuleForward},
		{"a new member's forward from a basic group", newcomer, forwarded(mtproto.MakePeerChat(7)), 1, RuleForward},
		{"a new member's forward from the group itself", newcomer, forwarded(mtproto.MakePeerChannel(group)), 1, RuleNone},
		{"a new member's forward from a user", newcomer, forwarded(mtproto.MakePeerUser(7)), 1, RuleNone},
		{"an old member's forward from a channel", oldtimer, forwarded(mtproto.MakePeerChannel(7)), 1, RuleNone},
		{"a new member mentions five", newcomer, many, 1, RuleMentions},
		{"a new member mentions four", newcomer, four, 1, RuleNone},
		{"an old member mentions five", oldtimer, many, 1, RuleNone},
		{"an old member posts to three groups", oldtimer, text("the same long text sent to many groups at once"), 3, RuleCrossPost},
		{"an old member posts to two groups", oldtimer, text("the same long text sent to many groups at once"), 2, RuleNone},
		{"an exempt member posts to three groups", Sender{Exempt: true}, text("the same long text sent to many groups at once"), 3, RuleNone},
		{"a new member's plain text", newcomer, text("hello everyone"), 1, RuleNone},
	} {
		got, hit := Evaluate(Input{ChannelId: group, Sender: c.s, Message: c.m, CrossPosts: c.crossPosts, Now: now})
		if got != c.want || hit != (c.want != RuleNone) {
			t.Errorf("%s: rule %d (%v), want %d", c.name, got, hit, c.want)
		}
	}
}

// TestMentions: different users count once, by id or by username, letter
// case aside; a mention out of the text counts for none.
func TestMentions(t *testing.T) {
	s := "@Ann @ann @bob ✨ @Eve"
	m := text(s,
		entity(mtproto.Predicate_messageEntityMention, 0, 4),
		entity(mtproto.Predicate_messageEntityMention, 5, 4),
		entity(mtproto.Predicate_messageEntityMention, 10, 4),
		// The emoji is one UTF-16 unit: @Eve starts at 17.
		entity(mtproto.Predicate_messageEntityMention, 17, 4),
		entity(mtproto.Predicate_messageEntityMention, 40, 4),
		mentionName(9), mentionName(9),
		&mtproto.MessageEntity{PredicateName: mtproto.Predicate_inputMessageEntityMentionName,
			UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 10}).To_InputUser()},
	)
	if got := Mentions(m); got != 5 {
		t.Errorf("mentions %d, want 5 (ann, bob, eve, 9, 10)", got)
	}
}

// TestContentHash: the same text in another case and spacing, or the same
// file, hashes the same; a different one differs; a short text alone has
// none.
func TestContentHash(t *testing.T) {
	long := "Buy cheap followers today, the best offer in town"
	a := ContentHash(text(long))
	if a == nil {
		t.Fatal("a long text has no hash")
	}
	if b := ContentHash(text("  buy CHEAP followers\ttoday,  the best offer in town ")); !bytes.Equal(a, b) {
		t.Error("case and spacing change the hash")
	}
	if b := ContentHash(text(long + "!")); bytes.Equal(a, b) {
		t.Error("another text has the same hash")
	}
	if h := ContentHash(text("ok, thanks")); h != nil {
		t.Error("a short text has a hash")
	}
	if h := ContentHash(text("x.io", entity(mtproto.Predicate_messageEntityUrl, 0, 4))); h == nil {
		t.Error("a short link has no hash")
	}
	photo := func(id int64) *mtproto.Message {
		m := text("")
		m.Media = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
			Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: id}).To_Photo(),
		}).To_MessageMedia()
		return m
	}
	if h := ContentHash(photo(1)); h == nil || !bytes.Equal(h, ContentHash(photo(1))) || bytes.Equal(h, ContentHash(photo(2))) {
		t.Error("a photo is not hashed by its id")
	}
	doc := text("")
	doc.Media = mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Document: mtproto.MakeTLDocument(&mtproto.Document{Id: 1}).To_Document(),
	}).To_MessageMedia()
	if h := ContentHash(doc); h == nil || bytes.Equal(h, ContentHash(photo(1))) {
		t.Error("a document is hashed as the photo of its id")
	}
}

// TestAppConfig: the clients find the antispam account and lock the switch
// by the app config; it must name the account and the minimum the server
// keeps. The id is a string: iOS (AccountContext.swift:1977-1983) and
// tdesktop read only that.
func TestAppConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if v, ok := config["telegram_antispam_user_id"].(string); !ok || v != strconv.FormatInt(UserId, 10) {
		t.Errorf("telegram_antispam_user_id of app_config.json %#v, the antispam account %d", config["telegram_antispam_user_id"], UserId)
	}
	if v, ok := config["telegram_antispam_group_size_min"].(float64); !ok || v != MembersMin {
		t.Errorf("telegram_antispam_group_size_min of app_config.json %v, the server's minimum %d", config["telegram_antispam_group_size_min"], MembersMin)
	}
}

// TestAccountRow: the database gets the antispam account as a service
// account (user_type 4) with the id the server and the app config use.
func TestAccountRow(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "teamgramd", "deploy", "sql", "z_pluma-antispam-user.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "VALUES (" + strconv.FormatInt(UserId, 10) + ", 4, "; !bytes.Contains(data, []byte(want)) {
		t.Errorf("z_pluma-antispam-user.sql does not insert %q", want)
	}
}
