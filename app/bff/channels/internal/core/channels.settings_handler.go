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

// The settings of a channel (task 044, biz channel.toggleSetting).
func (c *ChannelsCore) toggle(in *mtproto.InputChannel, setting int32, enabled bool) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelToggleSetting(c.ctx, &channelpb.TLChannelToggleSetting{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		Setting:       setting,
		Enabled:       mtproto.ToBool(enabled),
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

// ChannelsToggleSignatures
// channels.toggleSignatures#418d549c flags:# signatures_enabled:flags.0?true profiles_enabled:flags.1?true channel:InputChannel = Updates;
// channels.toggleSignatures#1f69b606 channel:InputChannel enabled:Bool = Updates;
//
// The posts of a channel signed with their authors. Signatures that link
// to the authors' profiles (profiles_enabled) are not kept: a post shows
// its author's name.
func (c *ChannelsCore) ChannelsToggleSignatures(in *mtproto.TLChannelsToggleSignatures) (*mtproto.Updates, error) {
	enabled := in.SignaturesEnabled
	if in.GetEnabled() != nil {
		enabled = mtproto.FromBool(in.GetEnabled())
	}
	return c.toggle(in.Channel, channelpb.SettingSignatures, enabled)
}

// ChannelsTogglePreHistoryHidden
// channels.togglePreHistoryHidden#eabbb94c channel:InputChannel enabled:Bool = Updates;
//
// The history of a supergroup hidden from new members: they see it from
// their join on (available_min_id, task 041).
func (c *ChannelsCore) ChannelsTogglePreHistoryHidden(in *mtproto.TLChannelsTogglePreHistoryHidden) (*mtproto.Updates, error) {
	return c.toggle(in.Channel, channelpb.SettingPrehistoryHidden, mtproto.FromBool(in.Enabled))
}

// ChannelsToggleParticipantsHidden
// channels.toggleParticipantsHidden#6a6e7854 channel:InputChannel enabled:Bool = Updates;
//
// The members of a supergroup hidden from members who are not admins.
func (c *ChannelsCore) ChannelsToggleParticipantsHidden(in *mtproto.TLChannelsToggleParticipantsHidden) (*mtproto.Updates, error) {
	return c.toggle(in.Channel, channelpb.SettingParticipantsHidden, mtproto.FromBool(in.Enabled))
}
