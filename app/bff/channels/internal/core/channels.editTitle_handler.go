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
)

// ChannelsEditTitle
// channels.editTitle#566decd0 channel:InputChannel title:string = Updates;
//
// The channel renamed (task 044), with the limits of channels.createChannel.
func (c *ChannelsCore) ChannelsEditTitle(in *mtproto.TLChannelsEditTitle) (*mtproto.Updates, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, mtproto.ErrChatTitleEmpty
	}
	if utf8.RuneCountInString(title) > titleMax {
		title = string([]rune(title)[:titleMax])
	}
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelEditTitle(c.ctx, &channelpb.TLChannelEditTitle{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		Title:         title,
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}
