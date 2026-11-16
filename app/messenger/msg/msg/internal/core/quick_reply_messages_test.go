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

package core

import (
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// shortcutRow is a message of shortcut 5 of user 1 as the table keeps it.
func shortcutRow(t *testing.T, id int32, groupedId int64, m *mtproto.Message) *dao.QuickReplyRow {
	t.Helper()
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		t.Fatal(err)
	}
	return &dao.QuickReplyRow{Id: id, UserId: 1, ShortcutId: 5, GroupedId: groupedId, MessageData: data, Date: 100, EditDate: 150}
}

// The random ids of a send of a shortcut: matched to the rows by the id
// each was asked with (Android), in the order of the rows when no ids are
// named, made by the server when the client sends none (tdesktop, iOS);
// random_id that does not match id one to one is RANDOM_ID_INVALID.
func TestQuickReplyRandomIds(t *testing.T) {
	rows := []*dao.QuickReplyRow{{Id: 3}, {Id: 4}, {Id: 9}}
	// Android takes the ids from getQuickReplyMessages, newest first.
	got, err := quickReplyRandomIds(rows, []int32{9, 4, 3}, []int64{90, 40, 30})
	if err != nil || !slices.Equal(got, []int64{30, 40, 90}) {
		t.Errorf("by id: %v, %v", got, err)
	}
	got, err = quickReplyRandomIds(rows, nil, []int64{1, 2, 3})
	if err != nil || !slices.Equal(got, []int64{1, 2, 3}) {
		t.Errorf("in order: %v, %v", got, err)
	}
	got, err = quickReplyRandomIds(rows, nil, nil)
	if err != nil || len(got) != 3 || slices.Contains(got, 0) || got[0] == got[1] || got[1] == got[2] {
		t.Errorf("made: %v, %v", got, err)
	}
	if _, err = quickReplyRandomIds(rows, nil, []int64{1, 2}); !errors.Is(err, mtproto.ErrRandomIdInvalid) {
		t.Errorf("two random ids for three messages: %v", err)
	}
	if _, err = quickReplyRandomIds(rows, nil, []int64{1, 1, 2}); !errors.Is(err, mtproto.ErrRandomIdDuplicate) {
		t.Errorf("a random id twice: %v", err)
	}
}

// A shortcut's messages go to the chat as new messages of the owner: in
// that chat, without the shortcut, the id, the edit date, the forward
// header, the reply and the counters they had; an album of the shortcut is
// a new album, one grouped_id for all its messages.
func TestQuickReplyOutbox(t *testing.T) {
	const owner, peer = int64(1), int64(2)
	text := mtproto.MakeTLMessage(&mtproto.Message{
		Out: true, Message: "hello", PeerId: mtproto.MakePeerUser(owner), FromId: mtproto.MakePeerUser(owner),
		FwdFrom:  mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerUser(9), Date: 50}).To_MessageFwdHeader(),
		ReplyTo:  mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToMsgId: 1}).To_MessageReplyHeader(),
		ViaBotId: wrapperspb.Int64(77),
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 5}).To_MessageEntity()},
	}).To_Message()
	photo := func(caption string) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{
			Out: true, Message: caption, PeerId: mtproto.MakePeerUser(owner), FromId: mtproto.MakePeerUser(owner),
			GroupedId: wrapperspb.Int64(500),
			Media:     mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 8}).To_Photo()}).To_MessageMedia(),
		}).To_Message()
	}
	rows := []*dao.QuickReplyRow{
		shortcutRow(t, 3, 0, text),
		shortcutRow(t, 4, 500, photo("one")),
		shortcutRow(t, 5, 500, photo("two")),
	}
	groups := int64(1000)
	out, err := quickReplyOutbox(rows, []int64{31, 41, 51}, owner, peer, 200, func() int64 { groups++; return groups })
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("%d messages", len(out))
	}
	for i, o := range out {
		m := o.GetMessage()
		if o.GetRandomId() != []int64{31, 41, 51}[i] {
			t.Errorf("%d: random id %d", i, o.GetRandomId())
		}
		if m.GetId() != 0 || m.GetDate() != 200 || m.GetEditDate() != nil || m.GetQuickReplyShortcutId() != nil || !m.GetOut() {
			t.Errorf("%d: id %d, date %d, edit date %v, shortcut %v, out %v", i, m.GetId(), m.GetDate(), m.GetEditDate(), m.GetQuickReplyShortcutId(), m.GetOut())
		}
		if m.GetPeerId().GetUserId() != peer || m.GetFromId().GetUserId() != owner || m.GetSavedPeerId() != nil {
			t.Errorf("%d: peer %v, from %v, saved_peer_id %v", i, m.GetPeerId(), m.GetFromId(), m.GetSavedPeerId())
		}
		if m.GetFwdFrom() != nil || m.GetReplyTo() != nil {
			t.Errorf("%d: fwd_from %v, reply_to %v", i, m.GetFwdFrom(), m.GetReplyTo())
		}
	}
	first := out[0].GetMessage()
	if first.GetMessage() != "hello" || len(first.GetEntities()) != 1 || first.GetViaBotId().GetValue() != 77 || first.GetGroupedId() != nil {
		t.Errorf("text: %v", first)
	}
	if g1, g2 := out[1].GetMessage().GetGroupedId().GetValue(), out[2].GetMessage().GetGroupedId().GetValue(); g1 != 1001 || g2 != 1001 {
		t.Errorf("album: grouped ids %d and %d, want 1001", g1, g2)
	}
	if out[1].GetMessage().GetMedia().GetPhoto_FLAGPHOTO().GetId() != 8 || out[2].GetMessage().GetMessage() != "two" {
		t.Errorf("album: %v, %v", out[1].GetMessage(), out[2].GetMessage())
	}
}
