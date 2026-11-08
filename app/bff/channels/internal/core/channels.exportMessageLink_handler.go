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
	"strconv"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// ChannelsExportMessageLink
// channels.exportMessageLink#e63fadeb flags:# grouped:flags.0?true thread:flags.1?true channel:InputChannel id:int = ExportedMessageLink;
//
// The link of a message of a channel the account may read (task 026), on
// the server's link host, as core.telegram.org/api/links#message-links
// builds it: <username>/<id> for a public channel, c/<channel>/<id> for a
// private one; with thread, <thread>/<id> for a message of a thread; one
// medium of an album, unless grouped, gets ?single. No embed code: html is
// empty, as the server has no embed widget.
func (c *ChannelsCore) ChannelsExportMessageLink(in *mtproto.TLChannelsExportMessageLink) (*mtproto.ExportedMessageLink, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if err = readable(mc, c.MD.UserId); err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: ch.GetId(), Id: []int32{in.Id}})
	if err != nil {
		c.Logger.Errorf("channels.exportMessageLink - error: %v", err)
		return nil, err
	}
	var msg *mtproto.Message
	for _, m := range messages.GetDatas() {
		if m.GetId() == in.Id && m.GetPredicateName() == mtproto.Predicate_message {
			msg = m
		}
	}
	if msg == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return mtproto.MakeTLExportedMessageLink(&mtproto.ExportedMessageLink{
		Link: messageLink(ch.GetUsername(), ch.GetId(), msg, in.Grouped, in.Thread),
		Html: "",
	}).To_ExportedMessageLink(), nil
}

// messageLink builds the link of msg in the channel username (or, without
// one, channelId).
func messageLink(username string, channelId int64, msg *mtproto.Message, grouped, thread bool) string {
	link := "https://" + env2.TDotMe + "/"
	if username != "" {
		link += username + "/"
	} else {
		link += "c/" + strconv.FormatInt(channelId, 10) + "/"
	}
	if r := msg.GetReplyTo(); thread && r != nil {
		// Without reply_to_top_id the message replied to starts the thread
		// (tdesktop history_item.cpp:2502; a comment directly to a post).
		top := r.GetReplyToTopId().GetValue()
		if top == 0 {
			top = r.GetReplyToMsgId()
		}
		if top != 0 {
			link += strconv.Itoa(int(top)) + "/"
		}
	}
	link += strconv.Itoa(int(msg.GetId()))
	if !grouped && msg.GetGroupedId() != nil && msg.GetGroupedId().GetValue() != 0 {
		link += "?single"
	}
	return link
}
