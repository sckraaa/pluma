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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// quickReplyMessages records what is given to msg: adds to a shortcut and
// sends to a chat.
type quickReplyMessages struct {
	msg_client.MsgClient
	added *msgpb.TLMsgAddQuickReplyMessages
	sent  *msgpb.TLMsgSendMessageV2
}

func (q *quickReplyMessages) MsgAddQuickReplyMessages(_ context.Context, in *msgpb.TLMsgAddQuickReplyMessages) (*mtproto.Updates, error) {
	q.added = in
	return mtproto.MakeEmptyUpdates(), nil
}

func (q *quickReplyMessages) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	q.sent = in
	return mtproto.MakeEmptyUpdates(), nil
}

// A result chosen in the editor of a quick reply shortcut goes to the
// shortcut, not to Saved Messages: without a sub-chat and without the
// reply the client named. A shortcut is the account's own: a result for
// one sent to another chat, or at a later time, is refused and nothing
// reaches msg.
func TestSendInlineResultToQuickReply(t *testing.T) {
	const user, bot = int64(10), int64(20)
	shortcut := mtproto.MakeTLInputQuickReplyShortcut(&mtproto.InputQuickReplyShortcut{Shortcut: "hello"}).To_InputQuickReplyShortcut()
	self := mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()

	q := &quickReplyMessages{}
	c := inlineCore(t, &dao.Dao{MsgClient: q}, user, bot)
	if _, err := c.MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{
		Peer: self, RandomId: 41, QueryId: 5, Id: "a", ClearDraft: true,
		ReplyTo:            mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 3}).To_InputReplyTo(),
		QuickReplyShortcut: shortcut,
	}); err != nil {
		t.Fatal(err)
	}
	if q.sent != nil {
		t.Fatalf("sent to a chat: %v", q.sent)
	}
	if q.added.GetUserId() != user || q.added.GetShortcut().GetShortcut() != "hello" || len(q.added.GetMessage()) != 1 {
		t.Fatalf("added %v", q.added)
	}
	o := q.added.GetMessage()[0]
	m := o.GetMessage()
	if o.GetRandomId() != 41 || m.GetMessage() != "hi" || m.GetViaBotId().GetValue() != bot {
		t.Errorf("message %v, random id %d", m, o.GetRandomId())
	}
	if m.GetSavedPeerId() != nil || m.GetReplyTo() != nil {
		t.Errorf("saved_peer_id %v, reply_to %v", m.GetSavedPeerId(), m.GetReplyTo())
	}

	for _, tc := range []struct {
		name string
		in   *mtproto.TLMessagesSendInlineBotResult
		want error
	}{
		{"another chat", &mtproto.TLMessagesSendInlineBotResult{
			Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: bot}).To_InputPeer(), RandomId: 42, QueryId: 5, Id: "a", QuickReplyShortcut: shortcut,
		}, mtproto.ErrPeerIdInvalid},
		{"a later time", &mtproto.TLMessagesSendInlineBotResult{
			Peer: self, RandomId: 43, QueryId: 5, Id: "a", ScheduleDate: wrapperspb.Int32(2000000000), QuickReplyShortcut: shortcut,
		}, mtproto.ErrScheduleDateInvalid},
	} {
		q := &quickReplyMessages{}
		c := inlineCore(t, &dao.Dao{MsgClient: q}, user, bot)
		if _, err := c.MessagesSendInlineBotResult(tc.in); !errors.Is(err, tc.want) || q.added != nil || q.sent != nil {
			t.Errorf("%s: %v, want %v; added %v, sent %v", tc.name, err, tc.want, q.added, q.sent)
		}
	}
}
