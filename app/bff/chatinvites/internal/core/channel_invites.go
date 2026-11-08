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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// importChannelInvite joins the account to the channel of the link (task
// 041): biz channel writes the join, the users of the answer are added
// here.
func (c *ChatInvitesCore) importChannelInvite(hash string) (*mtproto.Updates, error) {
	r, err := c.svcCtx.Dao.Channel.ChannelJoinChannel(c.ctx, &channelpb.TLChannelJoinChannel{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Link:          hash,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.PickByUpdates(r.GetUpdates()...)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	return r, nil
}

// channelOf reads the channel of an input peer the account named and checks
// its access hash.
func (c *ChatInvitesCore) channelOf(peer *mtproto.PeerUtil) error {
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return err
	}
	if mc.GetChannel().GetAccessHash() != peer.AccessHash {
		return mtproto.ErrChannelInvalid
	}
	return nil
}

// userList is the users of ids and the account's, as the account sees them,
// with the rows they come from.
func (c *ChatInvitesCore) userList(ids ...int64) (*mtproto.MutableUsers, []*mtproto.User, error) {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendUsers(c.MD.UserId)
	h.AppendUsers(ids...)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      h.UserIdList,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, nil, err
	}
	return users, users.GetUserListByIdList(c.MD.UserId, h.UserIdList...), nil
}

func (c *ChatInvitesCore) exportChannelInvite(peer *mtproto.PeerUtil, in *mtproto.TLMessagesExportChatInvite) (*mtproto.ExportedChatInvite, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	return c.svcCtx.Dao.Channel.ChannelExportChatInvite(c.ctx, &channelpb.TLChannelExportChatInvite{
		SelfId:                c.MD.UserId,
		ChannelId:             peer.PeerId,
		LegacyRevokePermanent: mtproto.ToBool(in.LegacyRevokePermanent),
		RequestNeeded:         mtproto.ToBool(in.RequestNeeded),
		ExpireDate:            in.GetExpireDate().GetValue(),
		UsageLimit:            in.GetUsageLimit().GetValue(),
		Title:                 in.GetTitle().GetValue(),
	})
}

func (c *ChatInvitesCore) channelExportedInvite(peer *mtproto.PeerUtil, link string) (*mtproto.Messages_ExportedChatInvite, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	invite, err := c.svcCtx.Dao.Channel.ChannelGetExportedChatInvite(c.ctx, &channelpb.TLChannelGetExportedChatInvite{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Link: link})
	if err != nil {
		return nil, err
	}
	_, users, err := c.userList(invite.AdminId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesExportedChatInvite(&mtproto.Messages_ExportedChatInvite{Invite: invite, Users: users}).To_Messages_ExportedChatInvite(), nil
}

func (c *ChatInvitesCore) channelExportedInvites(peer *mtproto.PeerUtil, adminId int64, in *mtproto.TLMessagesGetExportedChatInvites) (*mtproto.Messages_ExportedChatInvites, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	if adminId == 0 {
		// inputUserEmpty names the account (core.telegram.org/api/invites
		// calls the filter optional).
		adminId = c.MD.UserId
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetExportedChatInvites(c.ctx, &channelpb.TLChannelGetExportedChatInvites{
		SelfId:     c.MD.UserId,
		ChannelId:  peer.PeerId,
		AdminId:    adminId,
		Revoked:    mtproto.ToBool(in.Revoked),
		OffsetDate: in.GetOffsetDate().GetValue(),
		OffsetLink: in.GetOffsetLink().GetValue(),
		Limit:      in.Limit,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(r.Invites))
	for _, i := range r.Invites {
		ids = append(ids, i.AdminId)
	}
	_, r.Users, err = c.userList(ids...)
	return r, err
}

// editChannelInvite edits a link; fields says what the request gave, and
// the users of the answer are the owner of the link.
func (c *ChatInvitesCore) editChannelInvite(peer *mtproto.PeerUtil, in *mtproto.TLMessagesEditExportedChatInvite) (*mtproto.Messages_ExportedChatInvite, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	req := &channelpb.TLChannelEditExportedChatInvite{
		SelfId:        c.MD.UserId,
		ChannelId:     peer.PeerId,
		Link:          in.Link,
		Revoked:       mtproto.ToBool(in.Revoked),
		ExpireDate:    in.GetExpireDate().GetValue(),
		UsageLimit:    in.GetUsageLimit().GetValue(),
		RequestNeeded: mtproto.ToBool(mtproto.FromBool(in.GetRequestNeeded())),
		Title:         in.GetTitle().GetValue(),
	}
	if in.GetExpireDate() != nil {
		req.Fields |= channelpb.InviteFieldExpireDate
	}
	if in.GetUsageLimit() != nil {
		req.Fields |= channelpb.InviteFieldUsageLimit
	}
	if in.GetRequestNeeded() != nil {
		req.Fields |= channelpb.InviteFieldRequestNeeded
	}
	if in.GetTitle() != nil {
		req.Fields |= channelpb.InviteFieldTitle
	}
	r, err := c.svcCtx.Dao.Channel.ChannelEditExportedChatInvite(c.ctx, req)
	if err != nil {
		return nil, err
	}
	_, r.Users, err = c.userList(r.GetInvite().GetAdminId())
	return r, err
}

func (c *ChatInvitesCore) deleteChannelInvite(peer *mtproto.PeerUtil, link string) (*mtproto.Bool, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	return c.svcCtx.Dao.Channel.ChannelDeleteExportedChatInvite(c.ctx, &channelpb.TLChannelDeleteExportedChatInvite{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Link: link})
}

func (c *ChatInvitesCore) deleteRevokedChannelInvites(peer *mtproto.PeerUtil, adminId int64) (*mtproto.Bool, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	if adminId == 0 {
		adminId = c.MD.UserId
	}
	return c.svcCtx.Dao.Channel.ChannelDeleteRevokedExportedChatInvites(c.ctx, &channelpb.TLChannelDeleteRevokedExportedChatInvites{SelfId: c.MD.UserId, ChannelId: peer.PeerId, AdminId: adminId})
}

func (c *ChatInvitesCore) channelAdminsWithInvites(peer *mtproto.PeerUtil) (*mtproto.Messages_ChatAdminsWithInvites, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetAdminsWithInvites(c.ctx, &channelpb.TLChannelGetAdminsWithInvites{SelfId: c.MD.UserId, ChannelId: peer.PeerId})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(r.Admins))
	for _, a := range r.Admins {
		ids = append(ids, a.AdminId)
	}
	_, r.Users, err = c.userList(ids...)
	return r, err
}

// channelInviteImporters is a page of those who joined or ask to, each
// requester with its bio (chatInviteImporter.about: "for users with pending
// requests", core.telegram.org/constructor/chatInviteImporter).
func (c *ChatInvitesCore) channelInviteImporters(peer *mtproto.PeerUtil, offsetUser int64, in *mtproto.TLMessagesGetChatInviteImporters) (*mtproto.Messages_ChatInviteImporters, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetChatInviteImporters(c.ctx, &channelpb.TLChannelGetChatInviteImporters{
		SelfId:     c.MD.UserId,
		ChannelId:  peer.PeerId,
		Requested:  mtproto.ToBool(in.Requested),
		Link:       in.GetLink().GetValue(),
		Q:          in.GetQ().GetValue(),
		OffsetDate: in.OffsetDate,
		OffsetUser: offsetUser,
		Limit:      in.Limit,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(r.Importers))
	for _, i := range r.Importers {
		ids = append(ids, i.UserId)
		if v := i.GetApprovedBy(); v != nil {
			ids = append(ids, v.GetValue())
		}
	}
	users, list, err := c.userList(ids...)
	if err != nil {
		return nil, err
	}
	r.Users = list
	for _, i := range r.Importers {
		if !i.Requested {
			continue
		}
		if u, ok := users.GetImmutableUser(i.UserId); ok {
			i.About = mtproto.MakeFlagsString(u.About())
		}
	}
	return r, nil
}

// hideChannelRequests answers requests to join (biz
// channel.hideChatJoinRequests) and adds the users the answer names: the
// new members and the requesters left.
func (c *ChatInvitesCore) hideChannelRequests(peer *mtproto.PeerUtil, approved, all bool, link string, userId int64) (*mtproto.Updates, error) {
	if err := c.channelOf(peer); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.Channel.ChannelHideChatJoinRequests(c.ctx, &channelpb.TLChannelHideChatJoinRequests{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     peer.PeerId,
		Approved:      mtproto.ToBool(approved),
		All:           mtproto.ToBool(all),
		Link:          chatpb.GetInviteHashByLink(link),
		UserId:        userId,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByUpdates(r.GetUpdates()...)
	ids := h.UserIdList
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updatePendingJoinRequests {
			ids = append(ids, u.GetRecentRequesters()...)
		}
	}
	_, r.Users, err = c.userList(ids...)
	return r, err
}
