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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/proto"
)

// The title and the description of a channel (task 044): written in the
// editor's box transaction with the channel locked; the members load the
// channel again on updateChannel (ADR 0034, p. 3).

type infoRow struct {
	Title               string `db:"title"`
	PhotoId             int64  `db:"photo_id"`
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
// owner's row and checks that it may change the channel's info
// (channelperm.Can, ChangeInfo): the creator, an admin with change_info, or
// in a supergroup a member when neither the default rights nor its own
// restriction ban change_info. CHANNEL_PRIVATE for one out of it,
// CHAT_ADMIN_REQUIRED otherwise.
func infoEditor(b *userbox.Box, channelId int64) (*infoRow, error) {
	var rows []infoRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT c.title, c.photo_id, c.about, c.broadcast, c.default_banned_rights, p.role, p.state, p.admin_rights, p.banned_rights, p.until_date"+
			" FROM channels c JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ? AND c.deleted = 0",
		b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelPrivate
	}
	r := &rows[0]
	ch := channelperm.Channel{Broadcast: r.Broadcast, Default: r.DefaultBannedRights}
	p := channelperm.Participant{Role: r.Role, State: r.State, AdminRights: r.AdminRights, BannedRights: r.BannedRights, UntilDate: r.UntilDate}
	if err := channelperm.Can(ch, p, channelperm.ChangeInfo, int32(b.Date())); err != nil {
		return nil, err
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

// SetHistoryTTL gives the channel's new messages the auto-delete period,
// for those allowed to change its info (channelperm, ChangeInfo: tdesktop
// TTLValidator::can, canEditInformation): the service message
// messageActionSetMessagesTTL and updatePeerHistoryTTL for the members.
// CHAT_NOT_MODIFIED for the period it has.
func (d *Dao) SetHistoryTTL(b *userbox.Box, channelId int64, period int32) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	current, err := ch.TTL()
	if err != nil {
		return nil, err
	}
	if current == period {
		return nil, mtproto.ErrChatNotModified
	}
	if err = ch.SetTTL(period); err != nil {
		return nil, err
	}
	change := &ChannelChange{ChannelId: channelId, TTL: historyttl.Update(mtproto.MakePeerChannel(channelId), period)}
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, r.Broadcast, historyttl.Action(period, 0)); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
		return nil, err
	}
	return change, nil
}

// SetChatWallpaper gives the channel the wallpaper w, nil to take it away,
// for those allowed to change its info (channelperm, ChangeInfo: Android
// ChannelColorActivity and tdesktop setExistingForPeer offer it to admins with
// that right). Setting one writes the service message messageActionSetChatWallPaper
// (Android MessageObject: ActionSetWallpaperForThisChannel); taking it
// away only tells the members, as the clients show nothing for it
// (tdesktop resetForPeer has no message to show).
func (d *Dao) SetChatWallpaper(b *userbox.Box, channelId int64, w *mtproto.WallPaper) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	var data []byte
	if w != nil {
		if data, err = proto.Marshal(w); err != nil {
			return nil, fmt.Errorf("channel: encode a wallpaper: %w", err)
		}
	}
	if _, err = b.Tx().Exec("UPDATE channels SET wallpaper = ? WHERE id = ?", data, channelId); err != nil {
		return nil, err
	}
	change := &ChannelChange{
		ChannelId: channelId,
		Peer:      appearance.PeerWallpaperUpdate(mtproto.MakePeerChannel(channelId), w, false),
	}
	if w == nil {
		return change, nil
	}
	action := mtproto.MakeTLMessageActionSetChatWallPaper(&mtproto.MessageAction{Wallpaper: w}).To_MessageAction()
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, r.Broadcast, action); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
		return nil, err
	}
	return change, nil
}

// SetChatTheme gives the channel a chat theme, "" for none: the service
// message messageActionSetChatTheme and updateChannel for the members, who
// read channelFull.theme_emoticon. EMOJI_NOT_MODIFIED for the theme it has
// (core.telegram.org/method/messages.setChatTheme).
func (d *Dao) SetChatTheme(b *userbox.Box, channelId int64, emoticon string) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	var current string
	if err = b.Tx().QueryRow(&current, "SELECT theme_emoticon FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	if current == emoticon {
		return nil, mtproto.ErrEmojiNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET theme_emoticon = ? WHERE id = ?", emoticon, channelId); err != nil {
		return nil, err
	}
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	action := mtproto.MakeTLMessageActionSetChatTheme(&mtproto.MessageAction{
		Theme: mtproto.MakeTLChatTheme(&mtproto.ChatTheme{Emoticon: emoticon}).To_ChatTheme(),
	}).To_MessageAction()
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, r.Broadcast, action); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
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

// EditPhoto gives the channel the photo photo (nil: no photo): the service
// message messageActionChatEditPhoto, or messageActionChatDeletePhoto, and
// updateChannel for the members, as for a basic group
// (core.telegram.org/method/channels.editPhoto). CHAT_NOT_MODIFIED for no
// photo when it has none. The caller loads the photo before the
// transaction.
func (d *Dao) EditPhoto(b *userbox.Box, channelId int64, photo *mtproto.Photo) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	photoId := photo.GetId()
	if photoId == 0 && r.PhotoId == 0 {
		return nil, mtproto.ErrChatNotModified
	}
	action := mtproto.MakeTLMessageActionChatDeletePhoto(nil).To_MessageAction()
	if photoId != 0 {
		action = mtproto.MakeMessageActionChatEditPhoto(photo)
	}
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, r.Broadcast, action); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET photo_id = ?, top_message = ? WHERE id = ?", photoId, change.Message.GetId(), channelId); err != nil {
		return nil, err
	}
	return change, nil
}
