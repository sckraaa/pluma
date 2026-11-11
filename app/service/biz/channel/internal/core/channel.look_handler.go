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

// ChannelUpdateColor
// channel.updateColor self_id:long perm_auth_key_id:long channel_id:long for_profile:Bool color:int background_emoji_id:long = Updates;
//
// self_id sets the color of the channel's name or profile (dao.UpdateColor);
// the members load the channel again. The answer holds updateChannel and
// the channel.
func (c *ChannelCore) ChannelUpdateColor(in *channel.TLChannelUpdateColor) (*mtproto.Updates, error) {
	return c.editRights("channel.updateColor", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.UpdateColor(b, in.ChannelId, mtproto.FromBool(in.ForProfile), in.Color, in.BackgroundEmojiId)
	})
}

// ChannelUpdateEmojiStatus
// channel.updateEmojiStatus self_id:long perm_auth_key_id:long channel_id:long document_id:long until:int = Updates;
//
// self_id sets the emoji status of the channel (dao.UpdateEmojiStatus); the
// members load the channel again.
func (c *ChannelCore) ChannelUpdateEmojiStatus(in *channel.TLChannelUpdateEmojiStatus) (*mtproto.Updates, error) {
	return c.editRights("channel.updateEmojiStatus", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.UpdateEmojiStatus(b, in.ChannelId, in.DocumentId, in.Until)
	})
}

// ChannelEditUsernames
// channel.editUsernames self_id:long perm_auth_key_id:long channel_id:long action:int username:string active:Bool order:Vector<string> = Bool;
//
// self_id shows, hides or orders the usernames of the channel
// (dao.EditUsernames); on a change the members, the editor's other devices
// among them, get updateChannel by push and load the channel again.
func (c *ChannelCore) ChannelEditUsernames(in *channel.TLChannelEditUsernames) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		change, err := c.svcCtx.Dao.EditUsernames(b, in.ChannelId, in.Action, in.Username, mtproto.FromBool(in.Active), in.Order)
		if err != nil || change == nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.editUsernames", err)
	}
	return mtproto.BoolTrue, nil
}
