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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/readmarks"
)

const (
	readMe    = int64(7)
	readChat  = int64(42)
	readMsgId = int32(100)
)

// readMessages is the message service of a group where readMe sent msg:
// dates are its readers' dates as message.getOutboxReadDate gives them.
type readMessages struct {
	message_client.MessageClient
	msg   *mtproto.MessageBox
	dates []*mtproto.ReadParticipantDate
	asked *message.TLMessageGetOutboxReadDate
}

func (m *readMessages) MessageGetUserMessage(_ context.Context, in *message.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	if m.msg == nil || in.UserId != m.msg.UserId || in.Id != m.msg.MessageId {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return m.msg, nil
}

func (m *readMessages) MessageGetOutboxReadDate(_ context.Context, in *message.TLMessageGetOutboxReadDate) (*message.Vector_ReadParticipantDate, error) {
	m.asked = in
	return &message.Vector_ReadParticipantDate{Datas: m.dates}, nil
}

type readMembers struct {
	chat_client.ChatClient
	members []int64
}

func (c *readMembers) ChatGetChatParticipantIdList(_ context.Context, in *chatpb.TLChatGetChatParticipantIdList) (*chatpb.Vector_Long, error) {
	if in.ChatId != readChat {
		return &chatpb.Vector_Long{Datas: []int64{}}, nil
	}
	return &chatpb.Vector_Long{Datas: c.members}, nil
}

func readDate(userId int64, date int32) *mtproto.ReadParticipantDate {
	return mtproto.MakeTLReadParticipantDate(&mtproto.ReadParticipantDate{UserId: userId, Date: date}).To_ReadParticipantDate()
}

// readGroup is a group of readMe, 8 and 9 where readMe sent readMsgId an
// hour ago; 8 read it, and so did 10, who left the group since.
func readGroup() (*readMessages, *readMembers) {
	sent := int32(time.Now().Add(-time.Hour).Unix())
	return &readMessages{
			msg: &mtproto.MessageBox{
				UserId: readMe, MessageId: readMsgId, SenderUserId: readMe, PeerType: mtproto.PEER_CHAT, PeerId: readChat,
				Message: mtproto.MakeTLMessage(&mtproto.Message{Id: readMsgId, Out: true, Date: sent}).To_Message(),
			},
			dates: []*mtproto.ReadParticipantDate{readDate(10, sent+60), readDate(8, sent+30)},
		},
		&readMembers{members: []int64{readMe, 8, 9}}
}

func readCore(m *readMessages, c *readMembers) *ChatsCore {
	core := New(context.Background(), &svc.ServiceContext{Dao: &dao.Dao{
		MessageClient: m,
		ChatClient:    chat_client.NewChatClientHelperOf(c),
	}})
	core.MD = &metadata.RpcMetadata{UserId: readMe}
	return core
}

func chatPeer(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: id}).To_InputPeer()
}

// The members who read the message with the dates they read it, as the
// message service orders them; one who left is not listed.
func TestReadParticipants(t *testing.T) {
	m, c := readGroup()
	readers, err := readCore(m, c).readParticipants(chatPeer(readChat), readMsgId)
	if err != nil {
		t.Fatal(err)
	}
	if len(readers) != 1 || readers[0].GetUserId() != 8 || readers[0].GetDate() != m.dates[1].GetDate() {
		t.Errorf("readers %v, want 8 at %d", readers, m.dates[1].GetDate())
	}
	if a := m.asked; a.UserId != readMe || a.PeerType != mtproto.PEER_CHAT || a.PeerId != readChat || a.MsgId != readMsgId {
		t.Errorf("asked the message service %+v", a)
	}
}

func TestReadParticipantsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(m *readMessages, c *readMembers)
		peer   *mtproto.InputPeer
		want   error
	}{
		{"another's message", func(m *readMessages, _ *readMembers) { m.msg.SenderUserId = 8 }, chatPeer(readChat), mtproto.ErrMsgIdInvalid},
		{"no such message", func(m *readMessages, _ *readMembers) { m.msg = nil }, chatPeer(readChat), mtproto.ErrMsgIdInvalid},
		{"a message of another chat", nil, chatPeer(readChat + 1), mtproto.ErrMsgIdInvalid},
		{"too old", func(m *readMessages, _ *readMembers) {
			m.msg.Message.Date = int32(time.Now().Unix() - readmarks.ChatExpirePeriod - 60)
		}, chatPeer(readChat), mtproto.ErrMsgTooOld},
		{"not a member", func(_ *readMessages, c *readMembers) { c.members = []int64{8, 9} }, chatPeer(readChat), mtproto.ErrPeerIdInvalid},
		{"too big", func(_ *readMessages, c *readMembers) {
			for i := range readmarks.ChatSizeThreshold {
				c.members = append(c.members, int64(1000+i))
			}
		}, chatPeer(readChat), mtproto.ErrChatTooBig},
		{"a private chat", nil, mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 8}).To_InputPeer(), mtproto.ErrPeerIdInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, c := readGroup()
			if tc.change != nil {
				tc.change(m, c)
			}
			if _, err := readCore(m, c).readParticipants(tc.peer, readMsgId); !errors.Is(err, tc.want) {
				t.Errorf("error %v, want %v", err, tc.want)
			}
			if m.asked != nil {
				t.Errorf("asked for the dates: %+v", m.asked)
			}
		})
	}
}
