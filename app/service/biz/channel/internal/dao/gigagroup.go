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

package dao

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// GigagroupMin is the fewest members a supergroup becomes a broadcast group
// with. The four clients offer the conversion to the creator of a
// supergroup of at least megagroup_size_max - 1000 members (Android
// ChatUsersActivity.java:434-436, tdesktop edit_peer_permissions_box.cpp:
// 1300-1305, iOS ChannelPermissionsController.swift:735, Web K
// groupPermissions.tsx:201-205); megagroup_size_max of help.getConfig is
// MembersMax.
const GigagroupMin = MembersMax - 1000

// ConvertToGigagroup turns a supergroup into a broadcast group for good, by
// b's owner, its creator (core.telegram.org/api/channel#gigagroups): only
// its admins write from then on, as channelperm keeps for a gigagroup, and
// the default restrictions forbid every kind of message, which the clients
// read to show members that they cannot write (tdesktop CanSendAnyOf,
// data_chat_participant_status.cpp:284-290). The schema has no recent action
// of the conversion; the change of the default restrictions is logged as
// one. Errors as core.telegram.org/method/channels.convertToGigagroup:
// CHANNEL_INVALID for a broadcast channel, CHAT_ADMIN_REQUIRED for anyone but
// the creator, FORUM_ENABLED for a forum, PARTICIPANTS_TOO_FEW under
// GigagroupMin; CHAT_NOT_MODIFIED for a broadcast group already.
func (d *Dao) ConvertToGigagroup(b *userbox.Box, channelId int64) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if a.ch.Broadcast {
		return nil, mtproto.ErrChannelInvalid
	}
	// The creator may do everything but in a blocked channel.
	if err = a.can(channelperm.ChangeInfo, b.Date()); err != nil {
		return nil, err
	}
	if a.p.Role != channelperm.RoleCreator {
		return nil, mtproto.ErrChatAdminRequired
	}
	var rows []struct {
		Forum             bool  `db:"forum"`
		ParticipantsCount int32 `db:"participants_count"`
	}
	if err = b.Tx().QueryRowsPartial(&rows, "SELECT forum, participants_count FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, mtproto.ErrChannelInvalid
	case a.ch.Gigagroup:
		return nil, mtproto.ErrChatNotModified
	case rows[0].Forum:
		return nil, mtproto.ErrForumEnabled
	case rows[0].ParticipantsCount < GigagroupMin:
		return nil, mtproto.ErrParticipantsTooFew
	}
	rights := a.ch.Default | channelperm.SendBanned
	if _, err = b.Tx().Exec("UPDATE channels SET gigagroup = 1, default_banned_rights = ?, version = version + 1 WHERE id = ?",
		rights, channelId); err != nil {
		return nil, err
	}
	if rights != a.ch.Default {
		if err = adminlog.Write(b, channelId, mtproto.MakeTLChannelAdminLogEventActionDefaultBannedRights(&mtproto.ChannelAdminLogEventAction{
			PrevBannedRights: mtproto.BannedRights(a.ch.Default).ToChatBannedRights(),
			NewBannedRights:  mtproto.BannedRights(rights).ToChatBannedRights(),
		}).To_ChannelAdminLogEventAction(), ""); err != nil {
			return nil, err
		}
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}
