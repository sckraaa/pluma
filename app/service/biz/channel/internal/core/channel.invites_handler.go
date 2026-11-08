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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// ChannelToggleJoinRequest
// channel.toggleJoinRequest self_id:long perm_auth_key_id:long channel_id:long enabled:Bool apply_to_invites:Bool = Updates;
//
// self_id turns the approval of those who join on or off (dao.ToggleJoinRequest);
// the members load the channel again, join_request being one of its flags.
func (c *ChannelCore) ChannelToggleJoinRequest(in *channel.TLChannelToggleJoinRequest) (*mtproto.Updates, error) {
	return c.editRights("channel.toggleJoinRequest", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.ToggleJoinRequest(b, in.ChannelId, mtproto.FromBool(in.Enabled), mtproto.FromBool(in.ApplyToInvites))
	})
}

// ChannelExportChatInvite
// channel.exportChatInvite self_id:long channel_id:long legacy_revoke_permanent:Bool request_needed:Bool expire_date:int usage_limit:int title:string = ExportedChatInvite;
func (c *ChannelCore) ChannelExportChatInvite(in *channel.TLChannelExportChatInvite) (*mtproto.ExportedChatInvite, error) {
	var row *dao.InviteRow
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		row, err = c.svcCtx.Dao.ExportInvite(b, in.ChannelId, mtproto.FromBool(in.LegacyRevokePermanent), mtproto.FromBool(in.RequestNeeded), in.ExpireDate, in.UsageLimit, in.Title)
		return err
	}); err != nil {
		return nil, c.membershipError("channel.exportChatInvite", err)
	}
	return row.Exported(), nil
}

// linkReader reads self_id's row in the channel and checks that it may read
// the links: a link admin (dao linkAdmin's rule).
func (c *ChannelCore) linkReader(selfId, channelId int64) (*channel.ImmutableChannelParticipant, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, channelId, []int64{selfId})
	if err != nil {
		return nil, err
	}
	me, _ := mc.GetParticipant(selfId)
	switch {
	case mc.GetChannel().GetDeleted():
		return nil, mtproto.ErrChannelInvalid
	case !me.Member():
		return nil, mtproto.ErrChannelPrivate
	case me.GetRole() == channelperm.RoleMember:
		return nil, mtproto.ErrChatAdminRequired
	}
	return me, mc.GetChannel().Can(me, channelperm.InviteUsers, int32(time.Now().Unix()))
}

// readableInvite is the link of the channel, which me may read: its own or,
// for the creator, any (the rule of dao.actor.linksOf).
func (c *ChannelCore) readableInvite(me *channel.ImmutableChannelParticipant, channelId int64, link string) (*dao.InviteRow, error) {
	row, err := c.svcCtx.Dao.InviteOf(c.ctx, channelId, chatpb.GetInviteHashByLink(link))
	if err != nil {
		return nil, err
	}
	switch {
	case row == nil:
		return nil, mtproto.ErrInviteHashExpired
	case row.AdminId != me.GetUserId() && me.GetRole() != channelperm.RoleCreator:
		return nil, mtproto.ErrChatAdminRequired
	}
	return row, nil
}

// ChannelGetExportedChatInvite
// channel.getExportedChatInvite self_id:long channel_id:long link:string = ExportedChatInvite;
func (c *ChannelCore) ChannelGetExportedChatInvite(in *channel.TLChannelGetExportedChatInvite) (*mtproto.ExportedChatInvite, error) {
	me, err := c.linkReader(in.SelfId, in.ChannelId)
	if err != nil {
		return nil, err
	}
	row, err := c.readableInvite(me, in.ChannelId, in.Link)
	if err != nil {
		return nil, err
	}
	return row.Exported(), nil
}

// ChannelGetExportedChatInvites
// channel.getExportedChatInvites self_id:long channel_id:long admin_id:long revoked:Bool offset_date:int offset_link:string limit:int = messages.ExportedChatInvites;
//
// A page of admin_id's links. The offset_date of the method goes with
// offset_link, which alone says where the page starts.
func (c *ChannelCore) ChannelGetExportedChatInvites(in *channel.TLChannelGetExportedChatInvites) (*mtproto.Messages_ExportedChatInvites, error) {
	me, err := c.linkReader(in.SelfId, in.ChannelId)
	if err != nil {
		return nil, err
	}
	if in.AdminId != in.SelfId && me.GetRole() != channelperm.RoleCreator {
		return nil, mtproto.ErrChatAdminRequired
	}
	rows, count, err := c.svcCtx.Dao.Invites(c.ctx, in.ChannelId, in.AdminId, mtproto.FromBool(in.Revoked), chatpb.GetInviteHashByLink(in.OffsetLink), in.Limit)
	if err != nil {
		c.Logger.Errorf("channel.getExportedChatInvites - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	invites := make([]*mtproto.ExportedChatInvite, 0, len(rows))
	for i := range rows {
		invites = append(invites, rows[i].Exported())
	}
	return mtproto.MakeTLMessagesExportedChatInvites(&mtproto.Messages_ExportedChatInvites{
		Count:   count,
		Invites: invites,
		Users:   []*mtproto.User{},
	}).To_Messages_ExportedChatInvites(), nil
}

// ChannelEditExportedChatInvite
// channel.editExportedChatInvite self_id:long channel_id:long link:string revoked:Bool fields:int expire_date:int usage_limit:int request_needed:Bool title:string = messages.ExportedChatInvite;
//
// A permanent link revoked is replaced: the answer names both
// (messages.exportedChatInviteReplaced, ManageLinksActivity.java:1472-1474).
func (c *ChannelCore) ChannelEditExportedChatInvite(in *channel.TLChannelEditExportedChatInvite) (*mtproto.Messages_ExportedChatInvite, error) {
	var edited, replaced *dao.InviteRow
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		edited, replaced, err = c.svcCtx.Dao.EditInvite(b, in.ChannelId, chatpb.GetInviteHashByLink(in.Link), dao.InviteEdit{
			Revoked:       mtproto.FromBool(in.Revoked),
			Fields:        in.Fields,
			ExpireDate:    in.ExpireDate,
			UsageLimit:    in.UsageLimit,
			RequestNeeded: mtproto.FromBool(in.RequestNeeded),
			Title:         in.Title,
		})
		return err
	}); err != nil {
		return nil, c.membershipError("channel.editExportedChatInvite", err)
	}
	if replaced != nil {
		return mtproto.MakeTLMessagesExportedChatInviteReplaced(&mtproto.Messages_ExportedChatInvite{
			Invite:    edited.Exported(),
			NewInvite: replaced.Exported(),
			Users:     []*mtproto.User{},
		}).To_Messages_ExportedChatInvite(), nil
	}
	return mtproto.MakeTLMessagesExportedChatInvite(&mtproto.Messages_ExportedChatInvite{
		Invite: edited.Exported(),
		Users:  []*mtproto.User{},
	}).To_Messages_ExportedChatInvite(), nil
}

// ChannelDeleteExportedChatInvite
// channel.deleteExportedChatInvite self_id:long channel_id:long link:string = Bool;
func (c *ChannelCore) ChannelDeleteExportedChatInvite(in *channel.TLChannelDeleteExportedChatInvite) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.DeleteInvite(b, in.ChannelId, chatpb.GetInviteHashByLink(in.Link))
	}); err != nil {
		return nil, c.membershipError("channel.deleteExportedChatInvite", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelDeleteRevokedExportedChatInvites
// channel.deleteRevokedExportedChatInvites self_id:long channel_id:long admin_id:long = Bool;
func (c *ChannelCore) ChannelDeleteRevokedExportedChatInvites(in *channel.TLChannelDeleteRevokedExportedChatInvites) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.DeleteRevokedInvites(b, in.ChannelId, in.AdminId)
	}); err != nil {
		return nil, c.membershipError("channel.deleteRevokedExportedChatInvites", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelGetAdminsWithInvites
// channel.getAdminsWithInvites self_id:long channel_id:long = messages.ChatAdminsWithInvites;
//
// The creator's list: tdesktop loads it only for one
// (edit_peer_invite_links.cpp, AdminsController::prepare).
func (c *ChannelCore) ChannelGetAdminsWithInvites(in *channel.TLChannelGetAdminsWithInvites) (*mtproto.Messages_ChatAdminsWithInvites, error) {
	me, err := c.linkReader(in.SelfId, in.ChannelId)
	if err != nil {
		return nil, err
	}
	if me.GetRole() != channelperm.RoleCreator {
		return nil, mtproto.ErrChatAdminRequired
	}
	rows, err := c.svcCtx.Dao.AdminsWithInvites(c.ctx, in.ChannelId)
	if err != nil {
		c.Logger.Errorf("channel.getAdminsWithInvites - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	admins := make([]*mtproto.ChatAdminWithInvites, 0, len(rows))
	for _, r := range rows {
		admins = append(admins, mtproto.MakeTLChatAdminWithInvites(&mtproto.ChatAdminWithInvites{
			AdminId:             r.AdminId,
			InvitesCount:        r.Active,
			RevokedInvitesCount: r.Revoked,
		}).To_ChatAdminWithInvites())
	}
	return mtproto.MakeTLMessagesChatAdminsWithInvites(&mtproto.Messages_ChatAdminsWithInvites{
		Admins: admins,
		Users:  []*mtproto.User{},
	}).To_Messages_ChatAdminsWithInvites(), nil
}

// ChannelGetChatInviteImporters
// channel.getChatInviteImporters self_id:long channel_id:long requested:Bool link:string q:string offset_date:int offset_user:long limit:int = messages.ChatInviteImporters;
//
// A page of those who joined by a link or ask to join (dao.Importers): of a
// named link, for its owner or the creator; the pending requests, for any
// link admin. SEARCH_WITH_LINK_NOT_SUPPORTED for q and a link together
// (core.telegram.org/method/messages.getChatInviteImporters).
func (c *ChannelCore) ChannelGetChatInviteImporters(in *channel.TLChannelGetChatInviteImporters) (*mtproto.Messages_ChatInviteImporters, error) {
	if in.Q != "" && in.Link != "" {
		return nil, mtproto.ErrSearchWithLinkNotSupported
	}
	me, err := c.linkReader(in.SelfId, in.ChannelId)
	if err != nil {
		return nil, err
	}
	requested := mtproto.FromBool(in.Requested)
	link := ""
	if in.Link != "" {
		row, err := c.readableInvite(me, in.ChannelId, in.Link)
		if err != nil {
			return nil, err
		}
		link = row.Link
	}
	rows, count, err := c.svcCtx.Dao.Importers(c.ctx, in.ChannelId, requested, link, in.Q, in.OffsetDate, in.OffsetUser, in.Limit)
	if err != nil {
		c.Logger.Errorf("channel.getChatInviteImporters - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	importers := make([]*mtproto.ChatInviteImporter, 0, len(rows))
	for _, r := range rows {
		importers = append(importers, mtproto.MakeTLChatInviteImporter(&mtproto.ChatInviteImporter{
			Requested:  requested,
			UserId:     r.UserId,
			Date:       int32(r.Date),
			ApprovedBy: mtproto.MakeFlagsInt64(r.ApprovedBy),
		}).To_ChatInviteImporter())
	}
	return mtproto.MakeTLMessagesChatInviteImporters(&mtproto.Messages_ChatInviteImporters{
		Count:     count,
		Importers: importers,
		Users:     []*mtproto.User{},
	}).To_Messages_ChatInviteImporters(), nil
}

// ChannelHideChatJoinRequests
// channel.hideChatJoinRequests self_id:long perm_auth_key_id:long channel_id:long approved:Bool all:Bool link:string user_id:long = Updates;
//
// self_id answers a request to join, or every one of a link or the channel
// (dao.HideRequests), in its box transaction with the channel locked; the
// admins get the requests left, the members the service messages. The
// answer holds them, the requests left and the channel: Android reads the
// channel from the answer to load it again (MemberRequestsDelegate.java:483-487).
func (c *ChannelCore) ChannelHideChatJoinRequests(in *channel.TLChannelHideChatJoinRequests) (*mtproto.Updates, error) {
	var hidden *dao.Hidden
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		hidden, err = c.svcCtx.Dao.HideRequests(b, in.ChannelId, mtproto.FromBool(in.Approved), mtproto.FromBool(in.All), in.Link, in.UserId)
		if err != nil {
			return err
		}
		for _, change := range hidden.Changes {
			if err = c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, c.membershipError("channel.hideChatJoinRequests", err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, []int64{in.SelfId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	updates := []*mtproto.Update{hidden.Pending}
	for _, change := range hidden.Changes {
		shown := proto.Clone(change.Message).(*mtproto.Message)
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update())
	}
	if len(hidden.Approved) > 0 {
		updates = append(updates, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: in.ChannelId}).To_Update())
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{mc.ToChat(in.SelfId, now)},
		Date:    now,
	}).To_Updates(), nil
}

// ChannelGetPendingJoinRequests
// channel.getPendingJoinRequests self_id:long channel_id:long = Update;
//
// The requests pending as updatePendingJoinRequests names them, for those
// who may answer them (dao linkAdmin's rule).
func (c *ChannelCore) ChannelGetPendingJoinRequests(in *channel.TLChannelGetPendingJoinRequests) (*mtproto.Update, error) {
	if _, err := c.linkReader(in.SelfId, in.ChannelId); err != nil {
		return nil, err
	}
	return c.svcCtx.Dao.PendingRequests(c.ctx, in.ChannelId)
}

// ChannelGetLeftChannels
// channel.getLeftChannels self_id:long offset:int = messages.Chats;
//
// A page of the channels self_id left, messages.chats while all fit and
// messages.chatsSlice otherwise, each as it sees the channel: left, or
// channelForbidden for a private one (tdesktop takes both,
// export_data_types.cpp ParseLeftChannelsInfo).
func (c *ChannelCore) ChannelGetLeftChannels(in *channel.TLChannelGetLeftChannels) (*mtproto.Messages_Chats, error) {
	ids, count, err := c.svcCtx.Dao.LeftChannels(c.ctx, in.SelfId, in.Offset)
	if err != nil {
		c.Logger.Errorf("channel.getLeftChannels - user %d: %v", in.SelfId, err)
		return nil, err
	}
	channels, err := c.svcCtx.Dao.GetChannelListByIdList(c.ctx, in.SelfId, ids)
	if err != nil {
		c.Logger.Errorf("channel.getLeftChannels - user %d: %v", in.SelfId, err)
		return nil, err
	}
	now := int32(time.Now().Unix())
	chats := make([]*mtproto.Chat, 0, len(channels))
	for _, mc := range channels {
		chats = append(chats, mc.ToChat(in.SelfId, now))
	}
	if in.Offset == 0 && int(count) <= len(chats) {
		return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: chats}).To_Messages_Chats(), nil
	}
	return mtproto.MakeTLMessagesChatsSlice(&mtproto.Messages_Chats{Count: count, Chats: chats}).To_Messages_Chats(), nil
}
