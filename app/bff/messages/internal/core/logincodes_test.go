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
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/sendas"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	codesUserId = int64(7)
	codesPhone  = "79990000007"
)

// shownCodes records the invalidations asked of it.
type shownCodes struct {
	calls [][]string
	phone string
	err   error
}

func (s *shownCodes) Invalidate(_ context.Context, phone string, codes []string) (int, error) {
	s.phone = phone
	s.calls = append(s.calls, slices.Clone(codes))
	return len(codes), s.err
}

// codesUsers knows the one account, or fails.
type codesUsers struct {
	user_client.UserClient
	lookups int
	err     error
}

func (u *codesUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	u.lookups++
	if u.err != nil || in.Id != codesUserId {
		return nil, errors.Join(u.err, mtproto.ErrUserIdInvalid)
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: codesUserId, FirstName: "U", Phone: codesPhone}).To_UserData(),
	}).To_ImmutableUser(), nil
}

// codesMsg accepts every send and edit, or fails them all.
type codesMsg struct {
	msg_client.MsgClient
	err error
}

func (m *codesMsg) MsgSendMessageV2(context.Context, *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	if m.err != nil {
		return nil, m.err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

func (m *codesMsg) MsgEditMessageV2(context.Context, *msgpb.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	if m.err != nil {
		return nil, m.err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

// codesBox holds the account's messages by id.
type codesBox struct {
	message_client.MessageClient
	messages map[int32]*mtproto.MessageBox
}

func (b *codesBox) MessageGetUserMessageList(_ context.Context, in *message.TLMessageGetUserMessageList) (*message.Vector_MessageBox, error) {
	out := &message.Vector_MessageBox{}
	for _, id := range in.IdList {
		if m, ok := b.messages[id]; ok {
			out.Datas = append(out.Datas, m)
		}
	}
	return out, nil
}

type codesEnv struct {
	codes *shownCodes
	users *codesUsers
	msg   *codesMsg
	box   *codesBox
	svc   *svc.ServiceContext
}

func newCodesEnv() *codesEnv {
	e := &codesEnv{
		codes: &shownCodes{},
		users: &codesUsers{},
		msg:   &codesMsg{},
		box:   &codesBox{messages: map[int32]*mtproto.MessageBox{}},
	}
	e.svc = &svc.ServiceContext{
		Dao:        &dao.Dao{MsgClient: e.msg, UserClient: e.users, MessageClient: e.box},
		SendAs:     sendas.New(nil),
		LoginCodes: e.codes,
	}
	return e
}

func (e *codesEnv) core(bot bool) *MessagesCore {
	ctx := context.Background()
	return &MessagesCore{ctx: ctx, svcCtx: e.svc, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: codesUserId, IsBot: bot}}
}

func toUser(id int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id}).To_InputPeer()
}

// A login code the account sends is invalidated once msg has the message;
// a text without one costs no lookup; a bot's message, or a send msg
// refused, invalidates nothing.
func TestSentLoginCodeIsInvalidated(t *testing.T) {
	send := func(e *codesEnv, bot bool, text string) error {
		_, err := e.core(bot).MessagesSendMessage(&mtproto.TLMessagesSendMessage{Peer: toUser(8), Message: text, RandomId: 1})
		return err
	}

	e := newCodesEnv()
	if err := send(e, false, "Login code: 123456. Do not give this code to anyone"); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 1 || !slices.Equal(e.codes.calls[0], []string{"123456"}) || e.codes.phone != codesPhone {
		t.Fatalf("invalidations %q of phone %q; want [123456] of the sender's phone", e.codes.calls, e.codes.phone)
	}

	e = newCodesEnv()
	if err := send(e, false, "see you at 10:30, room 1234567"); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 0 || e.users.lookups != 0 {
		t.Fatalf("a text without a code: %d invalidations, %d lookups", len(e.codes.calls), e.users.lookups)
	}

	e = newCodesEnv()
	if err := send(e, true, "123456"); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 0 {
		t.Fatalf("a bot's message: %q invalidated", e.codes.calls)
	}

	e = newCodesEnv()
	e.msg.err = mtproto.ErrPeerIdInvalid
	if err := send(e, false, "123456"); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("a refused send: %v", err)
	}
	if len(e.codes.calls) != 0 {
		t.Fatalf("a refused send: %q invalidated", e.codes.calls)
	}
}

// The send stands when the code cannot be invalidated.
func TestSendSurvivesInvalidationFailure(t *testing.T) {
	e := newCodesEnv()
	e.users.err = errors.New("biz down")
	if _, err := e.core(false).MessagesSendMessage(&mtproto.TLMessagesSendMessage{Peer: toUser(8), Message: "123456", RandomId: 1}); err != nil {
		t.Fatalf("user lookup failed: %v", err)
	}
	if len(e.codes.calls) != 0 {
		t.Fatalf("invalidated without the phone: %q", e.codes.calls)
	}

	e = newCodesEnv()
	e.codes.err = errors.New("redis down")
	if _, err := e.core(false).MessagesSendMessage(&mtproto.TLMessagesSendMessage{Peer: toUser(8), Message: "123456", RandomId: 1}); err != nil {
		t.Fatalf("the store failed: %v", err)
	}
}

// An edit with a code in its new text invalidates it; an edit of the
// formatting alone does not look.
func TestEditedLoginCodeIsInvalidated(t *testing.T) {
	e := newCodesEnv()
	e.box.messages[5] = &mtproto.MessageBox{
		UserId: codesUserId, MessageId: 5, SenderUserId: codesUserId, PeerType: mtproto.PEER_USER, PeerId: 8,
		Message: mtproto.MakeTLMessage(&mtproto.Message{Id: 5, Message: "soon", PeerId: mtproto.MakePeerUser(8)}).To_Message(),
	}
	if _, err := e.core(false).MessagesEditMessage(&mtproto.TLMessagesEditMessage{Peer: toUser(8), Id: 5, Message: wrapperspb.String("it is 65-43-21")}); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 1 || !slices.Equal(e.codes.calls[0], []string{"654321"}) {
		t.Fatalf("invalidations %q; want [654321]", e.codes.calls)
	}
	if _, err := e.core(false).MessagesEditMessage(&mtproto.TLMessagesEditMessage{Peer: toUser(8), Id: 5}); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 1 {
		t.Fatalf("an edit without a new text: %q", e.codes.calls)
	}
}

// A forwarded message of the service notifications account invalidates
// the code it carries.
func TestForwardedLoginCodeIsInvalidated(t *testing.T) {
	e := newCodesEnv()
	e.box.messages[3] = &mtproto.MessageBox{
		UserId: codesUserId, MessageId: 3, SenderUserId: 777000, PeerType: mtproto.PEER_USER, PeerId: 777000,
		Message: mtproto.MakeTLMessage(&mtproto.Message{
			Id: 3, Message: "Login code: 246810. Do not give this code to anyone", PeerId: mtproto.MakePeerUser(777000), FromId: mtproto.MakePeerUser(777000),
		}).To_Message(),
	}
	if _, err := e.core(false).MessagesForwardMessages(&mtproto.TLMessagesForwardMessages{
		DropAuthor: true,
		FromPeer:   toUser(777000),
		Id:         []int32{3},
		RandomId:   []int64{11},
		ToPeer:     toUser(8),
	}); err != nil {
		t.Fatal(err)
	}
	if len(e.codes.calls) != 1 || !slices.Equal(e.codes.calls[0], []string{"246810"}) {
		t.Fatalf("invalidations %q; want [246810]", e.codes.calls)
	}
}
