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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"

	"github.com/zeromicro/go-zero/core/logx"
)

// peerMessages answers only the look-up a press needs; any other method of
// the client would panic on the nil interface.
type peerMessages struct {
	message_client.MessageClient
	got *message.TLMessageGetPeerUserMessage
}

func (p *peerMessages) MessageGetPeerUserMessage(_ context.Context, in *message.TLMessageGetPeerUserMessage) (*mtproto.MessageBox, error) {
	p.got = in
	return &mtproto.MessageBox{MessageId: 77}, nil
}

func testCore(d *dao.Dao, userId int64) *BotsCore {
	return &BotsCore{
		ctx:    context.Background(),
		svcCtx: &svc.ServiceContext{Dao: d},
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: userId},
	}
}

// The bot is told of a press by the id of the message in its own box.
func TestCallbackUpdateNamesTheBotsCopy(t *testing.T) {
	const user, bot = int64(10), int64(20)
	messages := &peerMessages{}
	c := testCore(&dao.Dao{MessageClient: messages}, user)
	p := &pressed{peer: mtproto.MakeUserPeerUtil(bot), botId: bot}

	u, chats, err := c.callbackUpdate(5, p, 33, []byte("go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := messages.got; got == nil || got.UserId != user || got.PeerUserId != bot || got.MsgId != 33 {
		t.Fatalf("look-up: %+v", got)
	}
	if u.GetPredicateName() != mtproto.Predicate_updateBotCallbackQuery || u.GetMsgId_INT32() != 77 || u.GetPeer_PEER().GetUserId() != user {
		t.Fatalf("update: %v", u)
	}
	if len(chats) != 0 {
		t.Fatalf("chats of a private press: %v", chats)
	}
}

// Clients send inputPeerEmpty for a query from a secret chat; the update then
// has no peer_type.
func TestInlinePeerTypeOfNoChat(t *testing.T) {
	c := testCore(&dao.Dao{}, 10)
	peer := mtproto.FromInputPeer2(10, mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer())
	pt, kind, err := c.inlinePeerType(20, peer)
	if err != nil || pt != nil || kind == "" {
		t.Fatalf("peer type %v, kind %q, error %v", pt, kind, err)
	}
}
