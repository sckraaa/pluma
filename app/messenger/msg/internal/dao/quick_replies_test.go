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

package dao

import (
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The names the clients let one type, and no other
// (tdesktop settings_quick_replies.cpp:166-180, Android
// QuickRepliesActivity.java:359-361).
func TestValidShortcutName(t *testing.T) {
	for _, name := range []string{
		"hello", "away", "Hello_World", "привет", "a1", "٣", "x‌y", "a·b", "අ්",
		strings.Repeat("a", 32), strings.Repeat("ж", 32),
		"𝒜", // a letter outside the BMP (Android \p{L})
	} {
		if !ValidShortcutName(name) {
			t.Errorf("%q refused", name)
		}
	}
	for _, name := range []string{
		"", strings.Repeat("a", 33), strings.Repeat("ж", 33),
		strings.Repeat("𝒜", 17), // 34 UTF-16 code units
		"hello world", "hi!", "a-b", "a.b", "/start", "emoji\U0001F600", "tab\t", "\xff",
	} {
		if ValidShortcutName(name) {
			t.Errorf("%q taken", name)
		}
	}
}

// Names are one shortcut whatever their case, letter by letter
// (tdesktop Qt::CaseInsensitive).
func TestShortcutKey(t *testing.T) {
	for _, c := range [][2]string{
		{"Hello", "hello"}, {"HELLO", "hello"}, {"ПРИВЕТ", "привет"}, {"ΟΔΟΣ", "οδος"}, {"οδοΣ", "οδος"},
	} {
		if ShortcutKey(c[0]) != ShortcutKey(c[1]) {
			t.Errorf("%q and %q are two shortcuts", c[0], c[1])
		}
	}
	for _, c := range [][2]string{{"straße", "strasse"}, {"cafe", "café"}, {"a1", "a2"}} {
		if ShortcutKey(c[0]) == ShortcutKey(c[1]) {
			t.Errorf("%q and %q are one shortcut", c[0], c[1])
		}
	}
}

// The hashes are the clients' own, computed for these data with Android's
// MediaDataController.calcHash on Java longs (the shortcuts as
// QuickRepliesController.java:203-215 hashes them, the messages as tdesktop
// countListHash, newest first).
func TestQuickReplyHashes(t *testing.T) {
	replies := []*QuickReply{
		{Id: 3, Shortcut: "hello", Count: 2, Top: &QuickReplyRow{Id: 17}},
		{Id: 5, Shortcut: "away", Count: 1, Top: &QuickReplyRow{Id: 21, EditDate: 1700000000}},
	}
	if got, want := QuickRepliesHash(replies), int64(-4427633655086730597); got != want {
		t.Errorf("QuickRepliesHash %d, want %d", got, want)
	}
	if QuickRepliesHash(nil) != 0 {
		t.Errorf("QuickRepliesHash of none is not 0")
	}
	rows := []*QuickReplyRow{{Id: 17}, {Id: 18, EditDate: 1700000100}, {Id: 20}}
	if got, want := QuickReplyMessagesHash(rows), int64(1858701457987777051); got != want {
		t.Errorf("QuickReplyMessagesHash %d, want %d", got, want)
	}
}

// A message of a shortcut is the owner's outgoing message in its Saved
// Messages with the shortcut's id, whatever the stored JSON says.
func TestQuickReplyRowMessage(t *testing.T) {
	r := &QuickReplyRow{Id: 9, UserId: 42, ShortcutId: 3, Date: 1700000000, EditDate: 1700000500,
		MessageData: `{"predicate_name":"message","message":"hi","saved_peer_id":{"predicate_name":"peerUser","user_id":42}}`}
	m, err := r.Message()
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case m.Id != 9 || m.Date != 1700000000 || m.GetEditDate().GetValue() != 1700000500:
		t.Errorf("id %d, date %d, edit date %v", m.Id, m.Date, m.GetEditDate())
	case !m.Out || m.GetPeerId().GetUserId() != 42 || m.GetFromId().GetUserId() != 42:
		t.Errorf("out %v, peer %v, from %v", m.Out, m.GetPeerId(), m.GetFromId())
	case m.SavedPeerId != nil:
		t.Errorf("saved peer %v", m.SavedPeerId)
	case m.GetQuickReplyShortcutId().GetValue() != 3:
		t.Errorf("shortcut %v", m.GetQuickReplyShortcutId())
	case m.Message != "hi" || m.PredicateName != mtproto.Predicate_message:
		t.Errorf("message %q (%s)", m.Message, m.PredicateName)
	}
	r.EditDate = 0
	if m, err = r.Message(); err != nil || m.EditDate != nil {
		t.Errorf("never edited: edit date %v, %v", m.GetEditDate(), err)
	}
}
