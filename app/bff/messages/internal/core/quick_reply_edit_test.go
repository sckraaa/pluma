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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// shortcutMsg holds one shortcut (5) of the account with one message (3)
// and records the edits and the adds given to msg.
type shortcutMsg struct {
	msg_client.MsgClient
	edited *msgpb.TLMsgEditQuickReplyMessage
	added  *msgpb.TLMsgAddQuickReplyMessages
	other  bool
}

func (m *shortcutMsg) MsgGetQuickReplyMessages(_ context.Context, in *msgpb.TLMsgGetQuickReplyMessages) (*mtproto.Messages_Messages, error) {
	if in.UserId != codesUserId || in.ShortcutId != 5 {
		return nil, mtproto.ErrShortcutInvalid
	}
	out := &mtproto.Messages_Messages{}
	for _, id := range in.Id {
		if id == 3 {
			out.Messages = append(out.Messages, mtproto.MakeTLMessage(&mtproto.Message{
				Id: 3, Out: true, Message: "old",
				PeerId:               mtproto.MakePeerUser(codesUserId),
				FromId:               mtproto.MakePeerUser(codesUserId),
				QuickReplyShortcutId: wrapperspb.Int32(5),
				Entities:             []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity()},
			}).To_Message())
		}
	}
	return mtproto.MakeTLMessagesMessages(out).To_Messages_Messages(), nil
}

func (m *shortcutMsg) MsgEditQuickReplyMessage(_ context.Context, in *msgpb.TLMsgEditQuickReplyMessage) (*mtproto.Updates, error) {
	m.edited = in
	return mtproto.MakeEmptyUpdates(), nil
}

func (m *shortcutMsg) MsgAddQuickReplyMessages(_ context.Context, in *msgpb.TLMsgAddQuickReplyMessages) (*mtproto.Updates, error) {
	m.added = in
	return mtproto.MakeEmptyUpdates(), nil
}

func (m *shortcutMsg) MsgEditMessageV2(context.Context, *msgpb.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	m.other = true
	return mtproto.MakeEmptyUpdates(), nil
}

func (m *shortcutMsg) MsgSendMessageV2(context.Context, *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	m.other = true
	return mtproto.MakeEmptyUpdates(), nil
}

func shortcutEnv() (*codesEnv, *shortcutMsg) {
	e := newCodesEnv()
	m := &shortcutMsg{}
	e.svc.Dao.MsgClient = m
	return e, m
}

// An edit with quick_reply_shortcut_id changes the message of the shortcut,
// not a message of Saved Messages: the new text with the client's
// entities, and invert_media as the edit says.
func TestEditQuickReplyMessage(t *testing.T) {
	e, m := shortcutEnv()
	_, err := e.core(false).MessagesEditMessage(&mtproto.TLMessagesEditMessage{
		Peer: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(), Id: 3, NoWebpage: true, InvertMedia: true,
		Message:              wrapperspb.String("new text"),
		Entities:             []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityItalic(&mtproto.MessageEntity{Offset: 4, Length: 4}).To_MessageEntity()},
		QuickReplyShortcutId: wrapperspb.Int32(5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.other || m.edited == nil {
		t.Fatalf("edit of a chat %v, edit of a shortcut %v", m.other, m.edited)
	}
	if m.edited.UserId != codesUserId || m.edited.ShortcutId != 5 || !m.edited.GetMessage().GetNoWebpage() {
		t.Errorf("edit %v", m.edited)
	}
	got := m.edited.GetMessage().GetMessage()
	if got.GetId() != 3 || got.GetMessage() != "new text" || !got.GetInvertMedia() || got.GetEditDate() == nil {
		t.Errorf("message %v", got)
	}
	if len(got.GetEntities()) != 1 || got.GetEntities()[0].GetPredicateName() != mtproto.Predicate_messageEntityItalic {
		t.Errorf("entities %v", got.GetEntities())
	}
}

// The edits of a shortcut refused: by a bot, in another chat, at a later
// time, of a poll or a checklist, of a message the shortcut does not hold,
// of another account's shortcut. None reaches an edit in msg.
func TestEditQuickReplyMessageRefused(t *testing.T) {
	self := mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
	edit := func(peer *mtproto.InputPeer, id, shortcut int32) *mtproto.TLMessagesEditMessage {
		return &mtproto.TLMessagesEditMessage{Peer: peer, Id: id, Message: wrapperspb.String("x"), QuickReplyShortcutId: wrapperspb.Int32(shortcut)}
	}
	later := edit(self, 3, 5)
	later.ScheduleDate = wrapperspb.Int32(2000000000)
	poll := edit(self, 3, 5)
	poll.Media = mtproto.MakeTLInputMediaPoll(&mtproto.InputMedia{}).To_InputMedia()
	for _, tc := range []struct {
		name string
		bot  bool
		in   *mtproto.TLMessagesEditMessage
		want error
	}{
		{"a bot", true, edit(self, 3, 5), mtproto.ErrBotMethodInvalid},
		{"another chat", false, edit(toUser(8), 3, 5), mtproto.ErrPeerIdInvalid},
		{"a later time", false, later, mtproto.ErrScheduleDateInvalid},
		{"a poll", false, poll, mtproto.ErrMediaInvalid},
		{"no such message", false, edit(self, 4, 5), mtproto.ErrMessageIdInvalid},
		{"another account's shortcut", false, edit(self, 3, 6), mtproto.ErrShortcutInvalid},
	} {
		e, m := shortcutEnv()
		if _, err := e.core(tc.bot).MessagesEditMessage(tc.in); !errors.Is(err, tc.want) || m.edited != nil || m.other {
			t.Errorf("%s: %v, want %v; edited %v, other %v", tc.name, err, tc.want, m.edited, m.other)
		}
	}
}

// A forward into a shortcut goes to it as a forward from its author, not
// as a message saved from a chat; to another chat a shortcut is refused.
func TestForwardToQuickReply(t *testing.T) {
	shortcut := mtproto.MakeTLInputQuickReplyShortcut(&mtproto.InputQuickReplyShortcut{Shortcut: "hello"}).To_InputQuickReplyShortcut()
	e, m := shortcutEnv()
	e.box.messages[11] = &mtproto.MessageBox{
		UserId: codesUserId, MessageId: 11, SenderUserId: codesUserId, PeerType: mtproto.PEER_USER, PeerId: 8,
		Message: mtproto.MakeTLMessage(&mtproto.Message{Id: 11, Out: true, Message: "to forward", Date: 100,
			PeerId: mtproto.MakePeerUser(8), FromId: mtproto.MakePeerUser(codesUserId)}).To_Message(),
	}
	forward := func(to *mtproto.InputPeer) error {
		_, err := e.core(false).MessagesForwardMessages(&mtproto.TLMessagesForwardMessages{
			FromPeer: toUser(8), Id: []int32{11}, RandomId: []int64{77}, ToPeer: to, QuickReplyShortcut: shortcut,
		})
		return err
	}
	if err := forward(mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()); err != nil {
		t.Fatal(err)
	}
	if m.other || m.added == nil || m.added.GetShortcut().GetShortcut() != "hello" || len(m.added.GetMessage()) != 1 {
		t.Fatalf("sent to a chat %v, added %v", m.other, m.added)
	}
	o := m.added.GetMessage()[0]
	got := o.GetMessage()
	if o.GetRandomId() != 77 || got.GetMessage() != "to forward" || got.GetFwdFrom().GetFromId().GetUserId() != codesUserId {
		t.Errorf("forward %v", got)
	}
	if got.GetFwdFrom().GetSavedFromPeer() != nil || got.GetFwdFrom().GetSavedFromMsgId() != nil || got.GetSavedPeerId() != nil {
		t.Errorf("saved from %v/%v, saved_peer_id %v", got.GetFwdFrom().GetSavedFromPeer(), got.GetFwdFrom().GetSavedFromMsgId(), got.GetSavedPeerId())
	}

	m.added = nil
	if err := forward(toUser(9)); !errors.Is(err, mtproto.ErrPeerIdInvalid) || m.added != nil || m.other {
		t.Errorf("to another chat: %v; added %v, other %v", err, m.added, m.other)
	}
}
