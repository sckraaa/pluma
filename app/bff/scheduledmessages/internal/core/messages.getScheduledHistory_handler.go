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
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesGetScheduledHistory
// messages.getScheduledHistory#f516760b peer:InputPeer hash:long = messages.Messages;
func (c *ScheduledMessagesCore) MessagesGetScheduledHistory(in *mtproto.TLMessagesGetScheduledHistory) (*mtproto.Messages_Messages, error) {
	peerType, peerId, err := c.peerOf(in.GetPeer())
	if err != nil {
		return nil, err
	}
	return c.svcCtx.MsgGetScheduledHistory(c.ctx, &msgpb.TLMsgGetScheduledHistory{
		UserId:   c.MD.UserId,
		PeerType: peerType,
		PeerId:   peerId,
		Hash:     in.GetHash(),
	})
}
