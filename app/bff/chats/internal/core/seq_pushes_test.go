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
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"github.com/zeromicro/go-zero/core/logx"
)

// biz chat gives the members of a basic group their updates with seq in the
// transaction of a change (ADR 0021): bff pushes none of them itself.

const (
	me     = int64(101)
	member = int64(102)
	group  = int64(7)
)

// testChat is the group of me (creator) and member.
func testChat() *mtproto.MutableChat {
	p := func(userId int64, kind int32) *mtproto.ImmutableChatParticipant {
		return mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{
			ChatId: group, UserId: userId, ParticipantType: kind, State: mtproto.ChatMemberStateNormal,
		}).To_ImmutableChatParticipant()
	}
	return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{
			Id: group, Creator: me, Title: "group", Version: 3, ParticipantsCount: 2,
			Photo:               mtproto.MakeTLPhotoEmpty(nil).To_Photo(),
			DefaultBannedRights: mtproto.MakeDefaultBannedRights(),
		}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{p(me, mtproto.ChatMemberCreator), p(member, mtproto.ChatMemberNormal)},
	}).To_MutableChat()
}

// fakeChats answers every change with testChat.
type fakeChats struct {
	chat_client.ChatClient
	calls int
}

func (f *fakeChats) ChatEditChatAdmin(context.Context, *chatpb.TLChatEditChatAdmin) (*mtproto.MutableChat, error) {
	f.calls++
	return testChat(), nil
}

func (f *fakeChats) ChatEditChatDefaultBannedRights(context.Context, *chatpb.TLChatEditChatDefaultBannedRights) (*mtproto.MutableChat, error) {
	f.calls++
	return testChat(), nil
}

func (f *fakeChats) ChatEditChatParticipantRank(context.Context, *chatpb.TLChatEditChatParticipantRank) (*mtproto.MutableChat, error) {
	f.calls++
	return testChat(), nil
}

func (f *fakeChats) ChatDeleteChat(context.Context, *chatpb.TLChatDeleteChat) (*mtproto.MutableChat, error) {
	f.calls++
	return testChat(), nil
}

// pushes counts what bff pushes itself.
type pushes struct {
	sync_client.SyncClient
	n int
}

func (p *pushes) SyncUpdatesMe(context.Context, *sync.TLSyncUpdatesMe) (*mtproto.Void, error) {
	p.n++
	return mtproto.EmptyVoid, nil
}

func (p *pushes) SyncUpdatesNotMe(context.Context, *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	p.n++
	return mtproto.EmptyVoid, nil
}

func (p *pushes) SyncPushUpdates(context.Context, *sync.TLSyncPushUpdates) (*mtproto.Void, error) {
	p.n++
	return mtproto.EmptyVoid, nil
}

type users struct{ user_client.UserClient }

func (users) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range append([]int64{me}, in.Id...) {
		out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id, UserType: userpb.UserTypeRegular}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

func seqCore(chats *fakeChats, push *pushes, m msg_client.MsgClient) *ChatsCore {
	ctx := context.Background()
	return &ChatsCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{
			ChatClient: chat_client.NewChatClientHelperOf(chats),
			SyncClient: push,
			UserClient: users{},
			MsgClient:  m,
		}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: me, PermAuthKeyId: 55},
	}
}

func TestChatChangesPushNothing(t *testing.T) {
	chats, push := &fakeChats{}, &pushes{}
	c := seqCore(chats, push, &portionMsg{offsets: []int32{0, 0}})

	r, err := c.MessagesEditChatAdmin(&mtproto.TLMessagesEditChatAdmin{
		ChatId: group, UserId: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: member}).To_InputUser(), IsAdmin: mtproto.BoolTrue,
	})
	if err != nil || !mtproto.FromBool(r) {
		t.Errorf("editChatAdmin: %v, %v", r, err)
	}

	rights, err := c.MessagesEditChatDefaultBannedRights(&mtproto.TLMessagesEditChatDefaultBannedRights{
		Peer:         mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: group}).To_InputPeer(),
		BannedRights: mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{SendMedia: true}).To_ChatBannedRights(),
	})
	if err != nil || len(rights.GetUpdates()) != 0 || len(rights.GetChats()) != 1 || rights.GetSeq() != 0 {
		t.Errorf("editChatDefaultBannedRights: %v, %v; want the chat alone, seq 0", rights, err)
	}

	ranked, err := c.MessagesEditChatParticipantRank(&mtproto.TLMessagesEditChatParticipantRank{
		Peer:        mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: group}).To_InputPeer(),
		Participant: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: member}).To_InputPeer(),
		Rank:        "helper",
	})
	if err != nil || len(ranked.GetUpdates()) != 1 || ranked.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChatParticipants ||
		ranked.GetSeq() != 0 || len(ranked.GetUsers()) != 2 {
		t.Errorf("editChatParticipantRank: %v, %v; want the members with their users, seq 0", ranked, err)
	}

	if r, err = c.MessagesDeleteChat(&mtproto.TLMessagesDeleteChat{ChatId: group}); err != nil || !mtproto.FromBool(r) {
		t.Errorf("deleteChat: %v, %v", r, err)
	}

	if chats.calls != 4 || push.n != 0 {
		t.Errorf("%d changes, %d pushes of bff; want 4 and none", chats.calls, push.n)
	}
}

// removals answers chat.deleteChatUser with the chat after the removal.
type removals struct {
	chat_client.ChatClient
	removed int64
}

func (r *removals) ChatDeleteChatUser(_ context.Context, in *chatpb.TLChatDeleteChatUser) (*mtproto.MutableChat, error) {
	c := testChat()
	for _, p := range c.ChatParticipants {
		if p.UserId == in.DeleteUserId {
			p.State = mtproto.ChatMemberStateKicked
			if in.OperatorId == in.DeleteUserId {
				p.State = mtproto.ChatMemberStateLeft
			}
		}
	}
	r.removed = in.DeleteUserId
	return c, nil
}

// sends records the messages sent.
type sends struct {
	msg_client.MsgClient
	sent []*msgpb.TLMsgSendMessageV2
}

func (s *sends) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	s.sent = append(s.sent, in)
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{
		mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: in.Message[0].Message}).To_Update(),
	}}).To_Updates(), nil
}

// messages.deleteChatUser behaves the same for every client: the service
// message is sent, the removed user's dialog stays (no dialog client: a
// call would panic), the reply carries the members, or the forbidden list
// for one who left.
func TestDeleteChatUserAsTelegram(t *testing.T) {
	for _, client := range []string{"ios", "android", "tdesktop"} {
		for _, leave := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s leave %v", client, leave), func(t *testing.T) {
				chats, m := &removals{}, &sends{}
				self := me
				if leave {
					self = member
				}
				c := seqCore(&fakeChats{}, &pushes{}, m)
				c.svcCtx.Dao.ChatClient = chat_client.NewChatClientHelperOf(chats)
				c.MD = &metadata.RpcMetadata{UserId: self, PermAuthKeyId: 55, Client: client}
				r, err := c.MessagesDeleteChatUser(&mtproto.TLMessagesDeleteChatUser{
					ChatId: group, UserId: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: member}).To_InputUser(),
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(m.sent) != 1 || m.sent[0].UserId != self ||
					m.sent[0].Message[0].Message.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatDeleteUser ||
					m.sent[0].Message[0].Message.GetAction().GetUserId() != member {
					t.Fatalf("messages sent %v", m.sent)
				}
				want := mtproto.Predicate_chatParticipants
				if leave {
					want = mtproto.Predicate_chatParticipantsForbidden
				}
				if n := len(r.GetUpdates()); n != 2 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateNewMessage ||
					r.GetUpdates()[1].GetParticipants_CHATPARTICIPANTS().GetPredicateName() != want {
					t.Errorf("reply %v; want the message and %s", r.GetUpdates(), want)
				}
			})
		}
	}
}
