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
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// messages.deleteHistory for a channel: the days of a calendar are not for
// channels (CHAT_REVOKE_DATE_UNSUPPORTED), and without them the clients use
// channels.deleteHistory.
func TestDeleteHistoryChannel(t *testing.T) {
	ctx := context.Background()
	c := &MessagesCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 7}}
	peer := mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 9, AccessHash: 1}).To_InputPeer()
	message := func(in *mtproto.TLMessagesDeleteHistory) string {
		_, err := c.MessagesDeleteHistory(in)
		st, _ := status.FromError(err)
		return st.Message()
	}

	if got := message(&mtproto.TLMessagesDeleteHistory{Peer: peer, MinDate: wrapperspb.Int32(100)}); got != "CHAT_REVOKE_DATE_UNSUPPORTED" {
		t.Errorf("a range of dates: %s", got)
	}
	if got := message(&mtproto.TLMessagesDeleteHistory{Peer: peer}); got != "PEER_ID_INVALID" {
		t.Errorf("no dates: %s", got)
	}
}
