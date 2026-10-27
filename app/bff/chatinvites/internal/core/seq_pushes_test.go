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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/chatinvites/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/chatinvites/internal/svc"
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

// biz chat gives the creator and the admins updatePendingJoinRequests with
// seq in the transaction of a request and of its answer (ADR 0021): bff
// pushes none itself.

const (
	admin     = int64(201)
	requester = int64(202)
	other     = int64(203)
	group     = int64(9)
)

func testChat() *mtproto.MutableChat {
	return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: group, Creator: admin, Title: "group", Version: 2}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{
			ChatId: group, UserId: admin, ParticipantType: mtproto.ChatMemberCreator, State: mtproto.ChatMemberStateNormal,
		}).To_ImmutableChatParticipant()},
	}).To_MutableChat()
}

type fakeChats struct {
	chat_client.ChatClient
	left *chatpb.RecentChatInviteRequesters
	err  error
}

func (f *fakeChats) ChatImportChatInvite2(context.Context, *chatpb.TLChatImportChatInvite2) (*chatpb.ChatInviteImported, error) {
	return chatpb.MakeTLChatInviteImported(&chatpb.ChatInviteImported{Chat: testChat(), Requesters: f.left}).To_ChatInviteImported(), nil
}

func (f *fakeChats) ChatGetMutableChat(context.Context, *chatpb.TLChatGetMutableChat) (*mtproto.MutableChat, error) {
	return testChat(), nil
}

func (f *fakeChats) ChatHideChatJoinRequests(context.Context, *chatpb.TLChatHideChatJoinRequests) (*chatpb.RecentChatInviteRequesters, error) {
	return f.left, f.err
}

type pushes struct {
	sync_client.SyncClient
	n int
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
	for _, id := range in.Id {
		out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id, UserType: userpb.UserTypeRegular}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

// sent answers the service message of an approval and keeps the last one.
type sent struct {
	msg_client.MsgClient
	n    int
	last *msgpb.TLMsgSendMessageV2
}

func (s *sent) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	s.n++
	s.last = in
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{}}).To_Updates(), nil
}

func inviteCore(chats *fakeChats, push *pushes, m *sent, self int64) *ChatInvitesCore {
	ctx := context.Background()
	return &ChatInvitesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{ChatClient: chats, SyncClient: push, UserClient: users{}, MsgClient: m}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: self, PermAuthKeyId: 66},
	}
}

func requesters(ids ...int64) *chatpb.RecentChatInviteRequesters {
	return chatpb.MakeTLRecentChatInviteRequesters(&chatpb.RecentChatInviteRequesters{
		RequestsPending: int32(len(ids)), RecentRequesters: ids,
	}).To_RecentChatInviteRequesters()
}

func TestJoinRequestPushesNothing(t *testing.T) {
	push := &pushes{}
	c := inviteCore(&fakeChats{left: requesters(requester)}, push, &sent{}, requester)
	_, err := c.MessagesImportChatInviteDE91436E(&mtproto.TLMessagesImportChatInviteDE91436E{Hash: chatpb.GenChatInviteHash()})
	if !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Errorf("request: %v, want INVITE_REQUEST_SENT", err)
	}
	_, err = c.MessagesImportChatInvite6C50051C(&mtproto.TLMessagesImportChatInvite6C50051C{Hash: chatpb.GenChatInviteHash()})
	if !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Errorf("request by the old method: %v, want INVITE_REQUEST_SENT", err)
	}
	if push.n != 0 {
		t.Errorf("%d pushes of bff", push.n)
	}
}

func TestHideChatJoinRequestPushesNothing(t *testing.T) {
	hide := func(approved bool) *mtproto.TLMessagesHideChatJoinRequest {
		return &mtproto.TLMessagesHideChatJoinRequest{
			Approved: approved,
			Peer:     mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: group}).To_InputPeer(),
			UserId:   mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: requester}).To_InputUser(),
		}
	}
	push, m := &pushes{}, &sent{}
	chats := &fakeChats{left: requesters(other)}
	c := inviteCore(chats, push, m, admin)

	// Declined: the requests left, with the latest requesters.
	r, err := c.MessagesHideChatJoinRequest(hide(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetRequestsPending() != 1 || r.GetSeq() != 0 ||
		len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != other {
		t.Errorf("declined: %v; want the requests left with their requester, seq 0", r)
	}
	// Approved: the service message, then the requests left.
	if r, err = c.MessagesHideChatJoinRequest(hide(true)); err != nil {
		t.Fatal(err)
	}
	if m.n != 1 || len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updatePendingJoinRequests ||
		!slices.Equal(r.GetUpdates()[0].GetRecentRequesters(), []int64{other}) || r.GetSeq() != 0 {
		t.Errorf("approved: %d messages, reply %v", m.n, r)
	}
	// The message is the new member's, from none of its devices, as in
	// Telegram: its sender is the one the clients say was accepted.
	if m.n == 1 {
		out := m.last.GetMessage()
		if m.last.GetUserId() != requester || m.last.GetAuthKeyId() != 0 || m.last.GetPeerType() != mtproto.PEER_CHAT || m.last.GetPeerId() != group ||
			len(out) != 1 || out[0].GetMessage().GetFromId().GetUserId() != requester ||
			out[0].GetMessage().GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatJoinedByRequest {
			t.Errorf("approved: the service message %v, want messageActionChatJoinedByRequest from %d", m.last, requester)
		}
	}
	if push.n != 0 {
		t.Errorf("%d pushes of bff", push.n)
	}
	// An error of biz chat is the reply's.
	chats.err = mtproto.ErrHideRequesterMissing
	if _, err = c.MessagesHideChatJoinRequest(hide(false)); !errors.Is(err, mtproto.ErrHideRequesterMissing) {
		t.Errorf("no request: %v, want HIDE_REQUESTER_MISSING", err)
	}
}
