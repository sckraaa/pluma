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
	"github.com/teamgram/teamgram-server/pkg/sendas"
)

// sendAsPeer is the peer of a send_as: the account itself or a channel,
// SEND_AS_PEER_INVALID for any other.
func (c *MessagesCore) sendAsPeer(in *mtproto.InputPeer) (*mtproto.Peer, error) {
	return sendas.AsPeer(c.MD.UserId, in)
}

// sendFrom is the from_id of the messages the account sends to peer with
// send_as (task 079, sendas.From): itself without one, or the peer of
// channels.getSendAs it chose, which it may write as now
// (SEND_AS_PEER_INVALID otherwise). msg checks it again when the messages
// go out, with the channel locked: a scheduled message as well.
func (c *MessagesCore) sendFrom(peer *mtproto.PeerUtil, sendAs *mtproto.InputPeer) (*mtproto.Peer, error) {
	return c.svcCtx.SendAs.From(c.ctx, c.MD.UserId, peer, sendAs)
}
