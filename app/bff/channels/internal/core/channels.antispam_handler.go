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

// ChannelsToggleAntiSpam
// channels.toggleAntiSpam#68f3e4eb channel:InputChannel enabled:Bool = Updates;
//
// The antispam of a supergroup on or off (task 082, ADR 0050), by an admin
// who may delete the messages of others; it comes on in a group of
// antispam.MembersMin members (telegram_antispam_group_size_min). From then
// on a message that breaks a rule of pkg/antispam is deleted by the
// antispam account, which the recent actions name.
func (c *ChannelsCore) ChannelsToggleAntiSpam(in *mtproto.TLChannelsToggleAntiSpam) (*mtproto.Updates, error) {
	return c.toggle(in.Channel, channelpb.SettingAntispam, mtproto.FromBool(in.Enabled))
}

// ChannelsReportAntiSpamFalsePositive
// channels.reportAntiSpamFalsePositive#a850a693 channel:InputChannel msg_id:int = Bool;
//
// An admin says the antispam deleted the message msg_id wrongly: the id of
// the deleted message, which the clients take from the recent action
// (tdesktop history_admin_log_inner.cpp:1352-1356, Android
// ChannelAdminLogActivity.java:2296-2308, iOS
// ChatRecentActionsControllerNode.swift:1410-1425). Its sender is no longer
// checked in the group and the moderators get a report; the message stays
// deleted, as the clients tell the admin (Android
// ChannelAntiSpamFalsePositiveReported). false for a message the antispam
// did not delete.
func (c *ChannelsCore) ChannelsReportAntiSpamFalsePositive(in *mtproto.TLChannelsReportAntiSpamFalsePositive) (*mtproto.Bool, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.MsgId <= 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return c.svcCtx.Channel.ChannelAntispamFalsePositive(c.ctx, &channelpb.TLChannelAntispamFalsePositive{
		SelfId:    c.MD.UserId,
		ChannelId: mc.GetChannel().GetId(),
		MsgId:     in.MsgId,
	})
}
