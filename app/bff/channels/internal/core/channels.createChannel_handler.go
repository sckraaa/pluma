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
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
)

// Limits of a channel's title and description, in characters, as the
// clients enforce them (Android ChannelCreateActivity: 128 and 255).
const (
	titleMax = 128
	aboutMax = 255
)

// createdChannel is the id of the channel in the answer of its creation.
func createdChannel(r *mtproto.Updates) int64 {
	for _, chat := range r.GetChats() {
		if chat.GetPredicateName() == mtproto.Predicate_channel {
			return chat.GetId()
		}
	}
	return 0
}

// ChannelsCreateChannel
// channels.createChannel#91006707 flags:# broadcast:flags.0?true megagroup:flags.1?true for_import:flags.3?true forum:flags.5?true title:string about:string geo_point:flags.2?InputGeoPoint address:flags.2?string ttl_period:flags.4?int = Updates;
//
// A channel or a supergroup of the account (task 038); biz channel makes it
// in one transaction and answers it as the clients take it, the account
// added to users. Forums, imports and geo groups are not served yet.
func (c *ChannelsCore) ChannelsCreateChannel(in *mtproto.TLChannelsCreateChannel) (*mtproto.Updates, error) {
	if in.Forum || in.ForImport || in.GeoPoint != nil {
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, mtproto.ErrChatTitleEmpty
	}
	if utf8.RuneCountInString(title) > titleMax {
		title = string([]rune(title)[:titleMax])
	}
	if utf8.RuneCountInString(in.About) > aboutMax {
		return nil, mtproto.ErrChatAboutTooLong
	}
	ttl := in.GetTtlPeriod().GetValue()
	if in.GetTtlPeriod() == nil {
		// The clients send the account's default period for a supergroup
		// (tdesktop add_contact_box.cpp); for a channel Android sends none
		// (ChannelCreateActivity), and the default applies to all new chats
		// (core.telegram.org/method/messages.setDefaultHistoryTTL).
		def, err := c.svcCtx.User.UserGetDefaultHistoryTTL(c.ctx, &userpb.TLUserGetDefaultHistoryTTL{UserId: c.MD.UserId})
		if err != nil {
			c.Logger.Errorf("channels.createChannel - error: %v", err)
			return nil, err
		}
		ttl = def.GetPeriod()
	}
	if !historyttl.Valid(ttl) {
		return nil, mtproto.ErrTtlPeriodInvalid
	}
	broadcast := !in.Megagroup
	r, err := c.svcCtx.Channel.ChannelCreateChannel(c.ctx, &channelpb.TLChannelCreateChannel{
		CreatorId: c.MD.UserId,
		Title:     title,
		About:     in.About,
		Broadcast: mtproto.ToBool(broadcast),
		Megagroup: mtproto.ToBool(!broadcast),
	})
	if err != nil {
		c.Logger.Errorf("channels.createChannel - error: %v", err)
		return nil, err
	}
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId}, To: []int64{c.MD.UserId}})
	if err != nil {
		c.Logger.Errorf("channels.createChannel - error: %v", err)
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, c.MD.UserId)
	if ttl > 0 {
		// The channel stands by now: a period that fails is logged and the
		// user sets it again, rather than creating a second channel.
		set, err := c.svcCtx.Channel.ChannelSetHistoryTTL(c.ctx, &channelpb.TLChannelSetHistoryTTL{
			SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: createdChannel(r), TtlPeriod: ttl,
		})
		if err != nil {
			c.Logger.Errorf("channels.createChannel - setHistoryTTL: %v", err)
		} else {
			r.Updates = append(r.Updates, set.GetUpdates()...)
		}
	}
	return r, nil
}
