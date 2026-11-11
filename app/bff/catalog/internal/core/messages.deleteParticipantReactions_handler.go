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

// MessagesDeleteParticipantReactions
// messages.deleteParticipantReactions#a0b80cf8 peer:InputPeer participant:InputPeer = Bool;
//
// An admin takes away all reactions of a member in a supergroup (msg
// deleteParticipantReactions). Basic groups are not served: their reactions
// live in every member's box.
func (c *CatalogCore) MessagesDeleteParticipantReactions(in *mtproto.TLMessagesDeleteParticipantReactions) (*mtproto.Bool, error) {
	if _, err := c.deleteParticipantReactions(in.GetPeer(), in.GetParticipant(), 0); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// MessagesDeleteParticipantReaction
// messages.deleteParticipantReaction#e3b7f82c peer:InputPeer msg_id:int participant:InputPeer = Updates;
//
// The same for one message; the reply holds its reactions as the admin sees
// them.
func (c *CatalogCore) MessagesDeleteParticipantReaction(in *mtproto.TLMessagesDeleteParticipantReaction) (*mtproto.Updates, error) {
	if in.GetMsgId() <= 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	return c.deleteParticipantReactions(in.GetPeer(), in.GetParticipant(), in.GetMsgId())
}

func (c *CatalogCore) deleteParticipantReactions(inPeer, inParticipant *mtproto.InputPeer, msgId int32) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, inPeer)
	if !peer.IsChannel() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	participant := mtproto.FromInputPeer2(c.MD.UserId, inParticipant)
	if !participant.IsUser() {
		return nil, mtproto.ErrParticipantIdInvalid
	}
	r, err := c.svcCtx.Msg.MsgDeleteParticipantReactions(c.ctx, &msgpb.TLMsgDeleteParticipantReactions{
		UserId:        c.MD.UserId,
		AuthKeyId:     c.MD.PermAuthKeyId,
		ChannelId:     peer.PeerId,
		ParticipantId: participant.PeerId,
		MsgId:         msgId,
	})
	if err != nil {
		c.Logger.Errorf("messages.deleteParticipantReaction(s) - error: %v", err)
		return nil, err
	}
	return r, nil
}
