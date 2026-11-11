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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChannelToggleSetting
// channel.toggleSetting self_id:long perm_auth_key_id:long channel_id:long setting:int enabled:Bool = Updates;
//
// self_id turns a setting of the channel on or off (dao.ToggleSetting);
// the members load the channel again. The answer holds updateChannel and
// the channel. A broadcast group only comes on, and for good
// (dao.ConvertToGigagroup).
func (c *ChannelCore) ChannelToggleSetting(in *channel.TLChannelToggleSetting) (*mtproto.Updates, error) {
	return c.editRights("channel.toggleSetting", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		if in.Setting == channel.SettingGigagroup {
			if !mtproto.FromBool(in.Enabled) {
				return nil, mtproto.ErrInputRequestInvalid
			}
			return c.svcCtx.Dao.ConvertToGigagroup(b, in.ChannelId)
		}
		return c.svcCtx.Dao.ToggleSetting(b, in.ChannelId, in.Setting, mtproto.FromBool(in.Enabled))
	})
}

// ChannelToggleSignatures
// channel.toggleSignatures self_id:long perm_auth_key_id:long channel_id:long enabled:Bool profiles:Bool = Updates;
//
// self_id signs the posts of a broadcast channel with their authors
// (dao.ToggleSignatures); the members load the channel again.
func (c *ChannelCore) ChannelToggleSignatures(in *channel.TLChannelToggleSignatures) (*mtproto.Updates, error) {
	return c.editRights("channel.toggleSignatures", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.ToggleSignatures(b, in.ChannelId, mtproto.FromBool(in.Enabled), mtproto.FromBool(in.Profiles))
	})
}

// ChannelToggleSlowMode
// channel.toggleSlowMode self_id:long perm_auth_key_id:long channel_id:long seconds:int = Updates;
//
// self_id sets the slow mode of a supergroup (dao.ToggleSlowMode); the members
// load the channel again.
func (c *ChannelCore) ChannelToggleSlowMode(in *channel.TLChannelToggleSlowMode) (*mtproto.Updates, error) {
	return c.editRights("channel.toggleSlowMode", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.ToggleSlowMode(b, in.ChannelId, in.Seconds)
	})
}

// ChannelSetChatWallpaper
// channel.setChatWallpaper flags:# self_id:long perm_auth_key_id:long channel_id:long wallpaper:flags.0?WallPaper = Updates;
//
// The wallpaper of the channel, for those allowed to change its info
// (dao.SetChatWallpaper): the service message and updatePeerWallpaper for
// the members.
func (c *ChannelCore) ChannelSetChatWallpaper(in *channel.TLChannelSetChatWallpaper) (*mtproto.Updates, error) {
	return c.editRights("channel.setChatWallpaper", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.SetChatWallpaper(b, in.ChannelId, in.GetWallpaper())
	})
}

// ChannelSetChatTheme
// channel.setChatTheme self_id:long perm_auth_key_id:long channel_id:long theme_emoticon:string = Updates;
//
// The chat theme of the channel (dao.SetChatTheme); the same rights.
func (c *ChannelCore) ChannelSetChatTheme(in *channel.TLChannelSetChatTheme) (*mtproto.Updates, error) {
	return c.editRights("channel.setChatTheme", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.SetChatTheme(b, in.ChannelId, in.GetThemeEmoticon())
	})
}

// ChannelSetHistoryTTL
// channel.setHistoryTTL self_id:long perm_auth_key_id:long channel_id:long ttl_period:int = Updates;
//
// self_id sets the auto-delete period of the channel's new messages
// (dao.SetHistoryTTL), with the service message and updatePeerHistoryTTL for
// the members.
func (c *ChannelCore) ChannelSetHistoryTTL(in *channel.TLChannelSetHistoryTTL) (*mtproto.Updates, error) {
	return c.editRights("channel.setHistoryTTL", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.SetHistoryTTL(b, in.ChannelId, in.TtlPeriod)
	})
}

// ChannelSetAvailableReactions
// channel.setAvailableReactions self_id:long perm_auth_key_id:long channel_id:long reactions_type:int reactions:Vector<string> = Updates;
//
// self_id sets the reactions the members may use (dao.SetAvailableReactions);
// the members load the channel again.
func (c *ChannelCore) ChannelSetAvailableReactions(in *channel.TLChannelSetAvailableReactions) (*mtproto.Updates, error) {
	return c.editRights("channel.setAvailableReactions", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.SetAvailableReactions(b, in.ChannelId, in.ReactionsType, in.Reactions)
	})
}
