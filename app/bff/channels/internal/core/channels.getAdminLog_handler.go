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
	"slices"
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
// channel.getAdminLog, pkg/adminlog), with the users and the linked chats
// they name.
func (c *ChannelsCore) ChannelsGetAdminLog(in *mtproto.TLChannelsGetAdminLog) (*mtproto.Channels_AdminLogResults, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	channelId := mc.GetChannel().GetId()
	var admins []int64
	for _, u := range in.GetAdmins() {
		admins = append(admins, mtproto.FromInputUser(c.MD.UserId, u).PeerId)
	}
	r, err := c.svcCtx.Channel.ChannelGetAdminLog(c.ctx, &channelpb.TLChannelGetAdminLog{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
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
	var linked []int64
	for _, e := range r.GetDatas() {
		a := e.GetAction()
		h.AppendUsers(e.GetUserId(), a.GetUserId(), a.GetApprovedBy())
		for _, inv := range []*mtproto.ExportedChatInvite{a.GetInvite(), a.GetPrevInvite(), a.GetNewInvite()} {
			if inv != nil {
				h.AppendUsers(inv.GetAdminId())
			}
		}
		for _, t := range []*mtproto.ForumTopic{a.GetTopic(), a.GetPrevTopic(), a.GetNewTopic()} {
			if t != nil {
				h.PickByPeer(t.GetFromId())
			}
		}
		// The linked chat is named by its id: the clients find it among
		// the chats (tdesktop history_admin_log_item.cpp createChangeLinkedChat
		// reads it as removed when it is not loaded).
		if a.GetPredicateName() == mtproto.Predicate_channelAdminLogEventActionChangeLinkedChat {
			for _, id := range []int64{a.GetPrevValue_INT64(), a.GetNewValue_INT64()} {
				if id != 0 && id != channelId && !slices.Contains(linked, id) {
					linked = append(linked, id)
				}
			}
		}
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
	chats := []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}
	if len(linked) > 0 {
		l, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: linked})
		if err != nil {
			c.Logger.Errorf("channels.getAdminLog - error: %v", err)
			return nil, err
		}
		chats = append(chats, l.GetChannelListByIdList(c.MD.UserId)...)
	}
	return mtproto.MakeTLChannelsAdminLogResults(&mtproto.Channels_AdminLogResults{
		Events: r.GetDatas(),
		Chats:  chats,
		Users:  users.GetUserListByIdList(c.MD.UserId, h.UserIdList...),
	}).To_Channels_AdminLogResults(), nil
}
