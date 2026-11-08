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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsDeleteHistory9BAA9647
// channels.deleteHistory#9baa9647 flags:# for_everyone:flags.0?true channel:InputChannel max_id:int = Updates;
//
// The history of the channel up to max_id, for everyone or, without the
// flag, for the account alone (task 043, biz channel.deleteHistory).
func (c *ChannelsCore) ChannelsDeleteHistory9BAA9647(in *mtproto.TLChannelsDeleteHistory9BAA9647) (*mtproto.Updates, error) {
	return c.deleteHistory(in.Channel, in.ForEveryone, in.MaxId)
}

// ChannelsDeleteHistoryAF369D42
// channels.deleteHistory#af369d42 channel:InputChannel max_id:int = Bool;
//
// The old form of the method, for the account alone.
func (c *ChannelsCore) ChannelsDeleteHistoryAF369D42(in *mtproto.TLChannelsDeleteHistoryAF369D42) (*mtproto.Bool, error) {
	if _, err := c.deleteHistory(in.Channel, false, in.MaxId); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

func (c *ChannelsCore) deleteHistory(input *mtproto.InputChannel, forEveryone bool, maxId int32) (*mtproto.Updates, error) {
	if maxId < 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	mc, err := c.mutableChannel(input)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Channel.ChannelDeleteHistory(c.ctx, &channelpb.TLChannelDeleteHistory{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		ForEveryone:   mtproto.ToBool(forEveryone),
		MaxId:         maxId,
	})
}

// ChannelsDeleteParticipantHistory
// channels.deleteParticipantHistory#367544db channel:InputChannel participant:InputPeer = messages.AffectedHistory;
//
// The messages a member sent to the supergroup (task 043), a portion at a
// time. The authors of a supergroup are users (biz channel.sendMessage), so
// a channel as a participant is PARTICIPANT_ID_INVALID.
func (c *ChannelsCore) ChannelsDeleteParticipantHistory(in *mtproto.TLChannelsDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	participant := mtproto.FromInputPeer2(c.MD.UserId, in.Participant)
	if !participant.IsUser() {
		return nil, mtproto.ErrParticipantIdInvalid
	}
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Channel.ChannelDeleteParticipantHistory(c.ctx, &channelpb.TLChannelDeleteParticipantHistory{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		ParticipantId: participant.PeerId,
	})
}
