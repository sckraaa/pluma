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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
)

// ChannelsGetAdminLog
// channels.getAdminLog#33ddf480 flags:# channel:InputChannel q:string events_filter:flags.0?ChannelAdminLogEventsFilter admins:flags.1?Vector<InputUser> max_id:long min_id:long limit:int = channels.AdminLogResults;
//
// The recent actions of the last 48 hours, for an admin (biz
// channel.getAdminLog, pkg/adminlog), with the users they name.
func (c *ChannelsCore) ChannelsGetAdminLog(in *mtproto.TLChannelsGetAdminLog) (*mtproto.Channels_AdminLogResults, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	var admins []int64
	for _, u := range in.GetAdmins() {
		admins = append(admins, mtproto.FromInputUser(c.MD.UserId, u).PeerId)
	}
	r, err := c.svcCtx.Channel.ChannelGetAdminLog(c.ctx, &channelpb.TLChannelGetAdminLog{
		SelfId:    c.MD.UserId,
		ChannelId: mc.GetChannel().GetId(),
		Q:         in.GetQ(),
		Filter:    adminlog.Mask(in.GetEventsFilter()),
		Admins:    admins,
		MaxId:     in.GetMaxId(),
		MinId:     in.GetMinId(),
		Limit:     in.GetLimit(),
	})
	if err != nil {
		c.Logger.Errorf("channels.getAdminLog - error: %v", err)
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	for _, e := range r.GetDatas() {
		a := e.GetAction()
		h.AppendUsers(e.GetUserId(), a.GetUserId())
		for _, p := range []*mtproto.ChannelParticipant{a.GetParticipant_CHANNELPARTICIPANT(), a.GetPrevParticipant(), a.GetNewParticipant()} {
			if p == nil {
				continue
			}
			h.AppendUsers(p.GetUserId(), p.GetPeer().GetUserId(), p.GetInviterId_INT64(), p.GetPromotedBy(), p.GetKickedBy())
		}
		for _, m := range []*mtproto.Message{a.GetMessage(), a.GetPrevMessage(), a.GetNewMessage()} {
			if m != nil {
				h.PickByMessages(m)
			}
		}
	}
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLChannelsAdminLogResults(&mtproto.Channels_AdminLogResults{
		Events: r.GetDatas(),
		Chats:  []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))},
		Users:  users.GetUserListByIdList(c.MD.UserId, h.UserIdList...),
	}).To_Channels_AdminLogResults(), nil
}
