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

// The usernames of a channel beside its public link (task 078): Telegram
// sells more on Fragment, which this server has not, so a channel here has
// its editable username alone and these methods find nothing to change in
// it; they answer as Telegram does all the same, since every client calls
// them (tdesktop api_user_names.cpp:179,232 and edit_peer_info_box.cpp:
// 2516-2535, Android ChatEditTypeActivity.java:823,1009,1260, iOS
// AddressNames.swift:221,321,471). biz channel checks that the account
// created the channel and keeps the change.

// ChannelsToggleUsername
// channels.toggleUsername#50f24105 channel:InputChannel username:string active:Bool = Bool;
//
// Shows or hides one username of the channel.
func (c *UsernamesCore) ChannelsToggleUsername(in *mtproto.TLChannelsToggleUsername) (*mtproto.Bool, error) {
	return c.editUsernames("channels.toggleUsername", in.GetChannel(), &channelpb.TLChannelEditUsernames{
		Action:   channelpb.UsernamesToggle,
		Username: in.GetUsername(),
		Active:   in.GetActive(),
	})
}

// ChannelsReorderUsernames
// channels.reorderUsernames#b45ced1d channel:InputChannel order:Vector<string> = Bool;
//
// Orders the shown usernames of the channel; order lists every one of them.
func (c *UsernamesCore) ChannelsReorderUsernames(in *mtproto.TLChannelsReorderUsernames) (*mtproto.Bool, error) {
	return c.editUsernames("channels.reorderUsernames", in.GetChannel(), &channelpb.TLChannelEditUsernames{
		Action: channelpb.UsernamesReorder,
		Order:  in.GetOrder(),
	})
}

// ChannelsDeactivateAllUsernames
// channels.deactivateAllUsernames#a245dd3 channel:InputChannel = Bool;
//
// Hides every username of the channel but the editable one; true also when
// none is shown.
func (c *UsernamesCore) ChannelsDeactivateAllUsernames(in *mtproto.TLChannelsDeactivateAllUsernames) (*mtproto.Bool, error) {
	return c.editUsernames("channels.deactivateAllUsernames", in.GetChannel(), &channelpb.TLChannelEditUsernames{
		Action: channelpb.UsernamesDeactivateAll,
	})
}

// editUsernames sends req, for the channel in, to biz channel.
func (c *UsernamesCore) editUsernames(method string, in *mtproto.InputChannel, req *channelpb.TLChannelEditUsernames) (*mtproto.Bool, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	channelId, err := c.channelOf(in)
	if err != nil {
		return nil, err
	}
	req.SelfId = c.MD.UserId
	req.PermAuthKeyId = c.MD.PermAuthKeyId
	req.ChannelId = channelId
	if req.Active == nil {
		req.Active = mtproto.BoolFalse
	}
	r, err := c.svcCtx.Dao.Channel.ChannelEditUsernames(c.ctx, req)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}
