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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The title and the description of a channel (task 044): written in the
// editor's box transaction with the channel locked; the members load the
// channel again on updateChannel (ADR 0034, p. 3).

type infoRow struct {
	Title               string `db:"title"`
	About               string `db:"about"`
	Broadcast           bool   `db:"broadcast"`
	DefaultBannedRights int32  `db:"default_banned_rights"`
	Role                int32  `db:"role"`
	State               int32  `db:"state"`
	AdminRights         int32  `db:"admin_rights"`
	BannedRights        int32  `db:"banned_rights"`
	UntilDate           int32  `db:"until_date"`
}

// infoEditor reads the channel, which the transaction has locked, with b's
// owner's row and checks that it may change the channel's info: the creator, an
// admin with change_info, or in a supergroup a member when neither the
// default rights nor its own restriction ban change_info
// (core.telegram.org/constructor/chatBannedRights). CHAT_ADMIN_REQUIRED
// otherwise.
func infoEditor(b *userbox.Box, channelId int64) (*infoRow, error) {
	var rows []infoRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT c.title, c.about, c.broadcast, c.default_banned_rights, p.role, p.state, p.admin_rights, p.banned_rights, p.until_date"+
			" FROM channels c JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ? AND c.deleted = 0",
		b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelPrivate
	}
	r := &rows[0]
	p := &channel.ImmutableChannelParticipant{Role: r.Role, State: r.State, BannedRights: r.BannedRights, UntilDate: r.UntilDate}
	now := int32(b.Date())
	switch {
	case !p.Member():
		return nil, mtproto.ErrChannelPrivate
	case r.Role == channel.RoleCreator, r.Role == channel.RoleAdmin && r.AdminRights&mtproto.ADMIN_CHANGE_INFO != 0:
		return r, nil
	case r.Broadcast, r.DefaultBannedRights&mtproto.BANNED_CHANGE_INFO != 0,
		r.State == channel.StateBanned && (r.UntilDate == 0 || now < r.UntilDate) && r.BannedRights&mtproto.BANNED_CHANGE_INFO != 0:
		return nil, mtproto.ErrChatAdminRequired
	}
	return r, nil
}

// EditTitle gives the channel title: the service message
// messageActionChatEditTitle and updateChannel for the members.
// CHAT_NOT_MODIFIED for the title it has.
func (d *Dao) EditTitle(b *userbox.Box, channelId int64, title string) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	if r.Title == title {
		return nil, mtproto.ErrChatNotModified
	}
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	action := mtproto.MakeTLMessageActionChatEditTitle(&mtproto.MessageAction{Title_STRING: title}).To_MessageAction()
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, r.Broadcast, action); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET title = ?, top_message = ? WHERE id = ?", title, change.Message.GetId(), channelId); err != nil {
		return nil, err
	}
	return change, nil
}

// EditAbout gives the channel description about; the members load the
// channel again. The schema has no service message for it (no
// messageAction of the description). CHAT_ABOUT_NOT_MODIFIED for the
// description it has.
func (d *Dao) EditAbout(b *userbox.Box, channelId int64, about string) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	if r.About == about {
		return nil, mtproto.ErrChatAboutNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET about = ? WHERE id = ?", about, channelId); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}
