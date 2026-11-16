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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func widgetMedia(canQuery bool) *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaWidget(&mtproto.MessageMedia{
		Payload: []byte{0x78, 1}, CanQuery: canQuery,
		PublicData: wrapperspb.String("p"), PublicDataHash: wrapperspb.Int64(botstore.WidgetDataHash("p")), ChatData: wrapperspb.String("c"),
	}).To_MessageMedia()
}

func dataQuery(wallet bool) *mtproto.WidgetQueryType {
	return mtproto.MakeTLWidgetQueryTypeData(&mtproto.WidgetQueryType{WithWallet: wallet}).To_WidgetQueryType()
}

// A widget's request is refused before it reaches the bot: a query type
// other than plain data, data out of 1 to 64 bytes, a message without a
// widget, a widget that may not ask, a forward.
func TestWidgetQueryRefusals(t *testing.T) {
	const user, bot = int64(10), int64(20)
	inBox := func(m *mtproto.Message) *mtproto.MessageBox {
		b := copyIn(bot, mtproto.PEER_USER, user)
		b.Message = m
		return b
	}
	fwd := mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{}).To_MessageFwdHeader()
	for _, tc := range []struct {
		name  string
		box   *mtproto.MessageBox
		query *mtproto.WidgetQueryType
		data  []byte
		want  error
	}{
		{"the wallet", inBox(&mtproto.Message{Media: widgetMedia(true)}), dataQuery(true), []byte("x"), mtproto.ErrWidgetQueryTypeUnsupported},
		{"a signature", inBox(&mtproto.Message{Media: widgetMedia(true)}), mtproto.MakeTLWidgetQueryTypeSignData(nil).To_WidgetQueryType(), []byte("x"), mtproto.ErrWidgetQueryTypeUnsupported},
		{"no data", inBox(&mtproto.Message{Media: widgetMedia(true)}), dataQuery(false), nil, mtproto.ErrWidgetQueryDataInvalid},
		{"65 bytes", inBox(&mtproto.Message{Media: widgetMedia(true)}), dataQuery(false), make([]byte, 65), mtproto.ErrWidgetQueryDataInvalid},
		{"no widget", inBox(&mtproto.Message{Media: mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia()}), dataQuery(false), []byte("x"), mtproto.ErrMessageIdInvalid},
		{"no allow_queries", inBox(&mtproto.Message{Media: widgetMedia(false)}), dataQuery(false), []byte("x"), mtproto.ErrWidgetQueryNotAllowed},
		{"a forward", inBox(&mtproto.Message{Media: widgetMedia(true), FwdFrom: fwd}), dataQuery(false), []byte("x"), mtproto.ErrWidgetQueryNotAllowed},
	} {
		c := testCore(&dao.Dao{MessageClient: &userMessages{box: tc.box}}, user)
		_, err := c.MessagesGetBotWidgetQueryAnswer(&mtproto.TLMessagesGetBotWidgetQueryAnswer{
			Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: bot}).To_InputPeer(), MsgId: 33,
			QueryType: tc.query, Data: tc.data,
		})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// The bot is told of a widget's request in a private chat by the id of the
// message in its own box, with the kind of the chat and the widget's
// object.
func TestWidgetUpdate(t *testing.T) {
	const user, bot = int64(10), int64(20)
	messages := &peerMessages{}
	c := testCore(&dao.Dao{MessageClient: messages}, user)
	media := widgetMedia(true)
	media.WidgetId = wrapperspb.Int64(5)
	p := &pressed{peer: mtproto.MakeUserPeerUtil(bot), botId: bot, message: &mtproto.Message{Media: media}}

	u, _, err := c.widgetUpdate(9, p, 33, dataQuery(false), []byte("go"))
	if err != nil {
		t.Fatal(err)
	}
	if u.GetPredicateName() != mtproto.Predicate_updateBotWidgetQuery || u.GetQueryId_INT64() != 9 || u.GetMsgId_INT32() != 77 ||
		u.GetPeer_PEER().GetUserId() != user || u.GetUserId() != user || u.GetChatType() != "private" ||
		string(u.GetData()) != "go" || u.GetWidgetId().GetValue() != 5 || u.GetQueryType().GetPredicateName() != mtproto.Predicate_widgetQueryTypeData {
		t.Fatalf("update: %v", u)
	}

	p = &pressed{peer: mtproto.MakeUserPeerUtil(bot), botId: bot, inline: true, message: &mtproto.Message{Media: widgetMedia(true)}}
	u, chats, err := c.widgetUpdate(9, p, 33, dataQuery(false), []byte("go"))
	if err != nil || u.GetPredicateName() != mtproto.Predicate_updateInlineBotWidgetQuery || u.GetInlineMessageId().GetId_INT32() != 33 ||
		u.GetInlineMessageId().GetOwnerId() != user || u.GetWidgetId() != nil || len(chats) != 0 {
		t.Fatalf("inline update: %v, %v, %v", u, chats, err)
	}
}

// A static widget has no object: its public state is never "modified".
func TestWidgetPublicDataOfAStaticWidget(t *testing.T) {
	const user, bot = int64(10), int64(20)
	b := copyIn(bot, mtproto.PEER_USER, user)
	b.Message = &mtproto.Message{Media: widgetMedia(false)}
	c := testCore(&dao.Dao{MessageClient: &userMessages{box: b}}, user)
	r, err := c.MessagesGetWidgetPublicData(&mtproto.TLMessagesGetWidgetPublicData{
		Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: bot}).To_InputPeer(), MsgId: 33,
	})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_widgetPublicDataNotModified || r.GetPeriod() != botstore.DefaultWidgetPeriod {
		t.Fatalf("answer %v, %v", r, err)
	}
}

// The bot's edit of a widget's state: only the state changes, silently,
// the automatic entities go for msg to make again; an edit that changes
// nothing, of a message without a widget or with a state too long, is
// refused.
func TestWidgetEdit(t *testing.T) {
	c := testCore(&dao.Dao{}, 20)
	stored := mtproto.MakeTLMessage(&mtproto.Message{
		Message: "see https://a.example", Media: widgetMedia(true),
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityUrl(&mtproto.MessageEntity{Offset: 4, Length: 17}).To_MessageEntity()},
	}).To_Message()

	m, err := c.widgetEdit(stored, nil, wrapperspb.String("c2"))
	if err != nil {
		t.Fatal(err)
	}
	if m.GetMedia().GetChatData().GetValue() != "c2" || m.GetMedia().GetPublicData().GetValue() != "p" || m.GetEditDate() != nil ||
		len(m.GetEntities()) != 0 || m.GetMessage() != stored.GetMessage() {
		t.Fatalf("edited: %v", m)
	}
	if stored.GetMedia().GetChatData().GetValue() != "c" {
		t.Fatal("the stored message changed")
	}
	for name, tc := range map[string]struct {
		m          *mtproto.Message
		public, ch *wrapperspb.StringValue
		want       error
	}{
		"nothing":        {stored, nil, nil, mtproto.ErrMessageNotModified},
		"the same state": {stored, wrapperspb.String("p"), wrapperspb.String("c"), mtproto.ErrMessageNotModified},
		"no widget":      {mtproto.MakeTLMessage(&mtproto.Message{Message: "x"}).To_Message(), nil, wrapperspb.String("c"), mtproto.ErrMessageIdInvalid},
		"a long state":   {stored, wrapperspb.String(string(make([]byte, 1025))), nil, mtproto.ErrWidgetDataTooLarge},
		"not text":       {stored, nil, wrapperspb.String("a\x00"), mtproto.ErrWidgetDataInvalid},
	} {
		if _, err := c.widgetEdit(tc.m, tc.public, tc.ch); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

func TestWidgetChatType(t *testing.T) {
	channel := func(broadcast bool) []*mtproto.Chat {
		return []*mtproto.Chat{mtproto.MakeTLChannel(&mtproto.Chat{Id: 5, Broadcast: broadcast}).To_Chat()}
	}
	for _, tc := range []struct {
		peer  *mtproto.PeerUtil
		chats []*mtproto.Chat
		want  string
	}{
		{mtproto.MakeUserPeerUtil(1), nil, "private"},
		{mtproto.MakeChatPeerUtil(5), nil, "group"},
		{mtproto.MakeChannelPeerUtil(5), channel(false), "supergroup"},
		{mtproto.MakeChannelPeerUtil(5), channel(true), "channel"},
	} {
		if got := widgetChatType(tc.peer, tc.chats); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.peer, got, tc.want)
		}
	}
}
