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
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// How a channel looks and is addressed (task 078): the colors of its name
// and of its profile, its emoji status and the order and state of its
// usernames, each changed in the editor's box transaction with the channel
// locked; the members load the channel again on updateChannel. Boosts do
// not limit any of it: what Telegram gives to boosted channels is open to
// every channel (ADR 0041).

// lookEditor checks that b's owner may change how the channel looks: its
// creator, or an admin with the right to change its info. The clients offer
// it to those only (tdesktop ChannelData::canEditEmoji, data_channel.cpp:
// 830-832, for the color button, edit_peer_info_box.cpp:1583; Android
// ChatEditActivity.java:939 canChangeChatInfo for a channel); a member of a
// supergroup whose default rights let it change the info is not one.
func lookEditor(b *userbox.Box, channelId int64) (*actor, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = a.can(channelperm.ChangeInfo, b.Date()); err != nil {
		return nil, err
	}
	if a.p.Role == channelperm.RoleMember {
		return nil, mtproto.ErrChatAdminRequired
	}
	return a, nil
}

type lookRow struct {
	Color                         int32 `db:"color"`
	ColorBackgroundEmojiId        int64 `db:"color_background_emoji_id"`
	ProfileColor                  int32 `db:"profile_color"`
	ProfileColorBackgroundEmojiId int64 `db:"profile_color_background_emoji_id"`
	EmojiStatusDocumentId         int64 `db:"emoji_status_document_id"`
	EmojiStatusUntil              int32 `db:"emoji_status_until"`
}

func readLook(b *userbox.Box, channelId int64) (*lookRow, error) {
	var rows []lookRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT color, color_background_emoji_id, profile_color, profile_color_background_emoji_id, emoji_status_document_id, emoji_status_until"+
			" FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	return &rows[0], nil
}

// logColor is a color as the recent actions show it: peerColor without
// flags for none, as the field is not optional
// (core.telegram.org/constructor/channelAdminLogEventActionChangePeerColor).
func logColor(color int32, emojiId int64) *mtproto.PeerColor {
	if c := channel.MakePeerColor(color, emojiId); c != nil {
		return c
	}
	return mtproto.MakeTLPeerColor(&mtproto.PeerColor{}).To_PeerColor()
}

// UpdateColor sets the color of the channel's name or, with forProfile, of
// its profile, with its pattern of custom emoji (emojiId 0 for none), by
// b's owner (lookEditor); color channel.NoColor is none. The caller checks
// that the color and the emoji exist. A supergroup has only the color of
// its profile ("supergroups can only change the profile palette",
// core.telegram.org/method/channels.updateColor): CHANNEL_INVALID for the
// other. CHAT_NOT_MODIFIED for what the channel has, which tdesktop and iOS
// take as done (edit_peer_color_box.cpp:557-565, UpdatePeerInfo.swift:133).
// The recent actions keep the change.
func (d *Dao) UpdateColor(b *userbox.Box, channelId int64, forProfile bool, color int32, emojiId int64) (*ChannelChange, error) {
	a, err := lookEditor(b, channelId)
	if err != nil {
		return nil, err
	}
	if !a.ch.Broadcast && !forProfile {
		return nil, mtproto.ErrChannelInvalid
	}
	r, err := readLook(b, channelId)
	if err != nil {
		return nil, err
	}
	prevColor, prevEmoji := r.Color, r.ColorBackgroundEmojiId
	columns := "color = ?, color_background_emoji_id = ?"
	if forProfile {
		prevColor, prevEmoji = r.ProfileColor, r.ProfileColorBackgroundEmojiId
		columns = "profile_color = ?, profile_color_background_emoji_id = ?"
	}
	if prevColor == color && prevEmoji == emojiId {
		return nil, mtproto.ErrChatNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET "+columns+", version = version + 1 WHERE id = ?", color, emojiId, channelId); err != nil {
		return nil, err
	}
	action := &mtproto.ChannelAdminLogEventAction{
		PrevValue_PEERCOLOR: logColor(prevColor, prevEmoji),
		NewValue_PEERCOLOR:  logColor(color, emojiId),
	}
	logged := mtproto.MakeTLChannelAdminLogEventActionChangePeerColor(action).To_ChannelAdminLogEventAction()
	if forProfile {
		logged = mtproto.MakeTLChannelAdminLogEventActionChangeProfilePeerColor(action).To_ChannelAdminLogEventAction()
	}
	if err = adminlog.Write(b, channelId, logged, ""); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// logStatus is an emoji status as the recent actions show it:
// emojiStatusEmpty for none.
func logStatus(documentId int64, until int32) *mtproto.EmojiStatus {
	if s := channel.MakeEmojiStatus(documentId, until); s != nil {
		return s
	}
	return channel.EmptyEmojiStatus()
}

// UpdateEmojiStatus sets the emoji status of the channel, a custom emoji
// until a date (0: no end), documentId 0 for none, by b's owner
// (lookEditor). The caller checks the emoji. CHAT_NOT_MODIFIED for what the
// channel has, which iOS takes as done (UpdatePeerInfo.swift:208-209). The
// recent actions keep the change.
func (d *Dao) UpdateEmojiStatus(b *userbox.Box, channelId int64, documentId int64, until int32) (*ChannelChange, error) {
	if documentId == 0 {
		until = 0
	}
	if _, err := lookEditor(b, channelId); err != nil {
		return nil, err
	}
	r, err := readLook(b, channelId)
	if err != nil {
		return nil, err
	}
	if r.EmojiStatusDocumentId == documentId && r.EmojiStatusUntil == until {
		return nil, mtproto.ErrChatNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET emoji_status_document_id = ?, emoji_status_until = ?, version = version + 1 WHERE id = ?",
		documentId, until, channelId); err != nil {
		return nil, err
	}
	action := mtproto.MakeTLChannelAdminLogEventActionChangeEmojiStatus(&mtproto.ChannelAdminLogEventAction{
		PrevValue_EMOJISTATUS: logStatus(r.EmojiStatusDocumentId, r.EmojiStatusUntil),
		NewValue_EMOJISTATUS:  logStatus(documentId, until),
	}).To_ChannelAdminLogEventAction()
	if err = adminlog.Write(b, channelId, action, ""); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// usernameRow is a row of username of a channel.
type usernameRow struct {
	Id       int64  `db:"id"`
	Username string `db:"username"`
	Editable bool   `db:"editable"`
	Active   bool   `db:"active"`
}

// activeNames are the usernames of rows shown, in their order.
func activeNames(rows []usernameRow) []string {
	names := []string{}
	for _, r := range rows {
		if r.Active {
			names = append(names, r.Username)
		}
	}
	return names
}

// EditUsernames changes the usernames of the channel by b's owner, who must
// be its creator, as for its public link (usernameEditor;
// channels.toggleUsername is for "a supergroup or channel you own",
// core.telegram.org/method/channels.toggleUsername). A channel's usernames
// are its rows of username: the editable one, its public link, and the
// others, which Telegram sells on Fragment and this server has no way to
// give; the methods work on what the rows hold. By action:
//
//   - channel.UsernamesToggle shows (active) or hides username, one of the
//     channel's: USERNAME_INVALID for another, USERNAME_NOT_MODIFIED for
//     its state, USERNAMES_ACTIVE_TOO_MUCH past maxShownUsernames shown.
//     The editable one is always shown: the channel is made
//     private by channels.updateUsername instead, so hiding it is
//     USERNAME_INVALID;
//   - channel.UsernamesReorder orders the shown usernames as order, which
//     lists each of them once ("all active usernames must be specified",
//     core.telegram.org/method/channels.reorderUsernames): USERNAME_INVALID
//     otherwise, CHAT_NOT_MODIFIED for their order;
//   - channel.UsernamesDeactivateAll hides every username but the editable
//     one ("disable all purchased usernames",
//     core.telegram.org/method/channels.deactivateAllUsernames). It is done
//     also when there is nothing to hide, with no change (nil): tdesktop
//     goes on saving the channel's settings only when it is
//     (edit_peer_info_box.cpp:2516-2528).
//
// The recent actions keep a change of the shown usernames.
func (d *Dao) EditUsernames(b *userbox.Box, channelId int64, action int32, username string, active bool, order []string) (*ChannelChange, error) {
	if err := usernameEditor(b, channelId); err != nil {
		return nil, err
	}
	var rows []usernameRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, username, editable, active FROM username WHERE peer_type = ? AND peer_id = ? ORDER BY order2, id FOR UPDATE",
		mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	before := activeNames(rows)
	switch action {
	case channel.UsernamesToggle:
		if err := toggleUsername(b, rows, username, active); err != nil {
			return nil, err
		}
	case channel.UsernamesReorder:
		if err := reorderUsernames(b, rows, order); err != nil {
			return nil, err
		}
	case channel.UsernamesDeactivateAll:
		res, err := b.Tx().Exec("UPDATE username SET active = 0 WHERE peer_type = ? AND peer_id = ? AND editable = 0 AND active = 1",
			mtproto.PEER_CHANNEL, channelId)
		if err != nil {
			return nil, err
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, err
		} else if n == 0 {
			return nil, nil
		}
	default:
		return nil, mtproto.ErrInputRequestInvalid
	}
	var now []usernameRow
	if err := b.Tx().QueryRowsPartial(&now,
		"SELECT id, username, editable, active FROM username WHERE peer_type = ? AND peer_id = ? ORDER BY order2, id",
		mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	after := activeNames(now)
	if err := adminlog.Write(b, channelId, mtproto.MakeTLChannelAdminLogEventActionChangeUsernames(&mtproto.ChannelAdminLogEventAction{
		PrevValue_VECTORSTRING: before, NewValue_VECTORSTRING: after,
	}).To_ChannelAdminLogEventAction(), strings.Join(after, " ")); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// maxShownUsernames is how many usernames a channel shows at most; the
// clients tell of it on USERNAMES_ACTIVE_TOO_MUCH (tdesktop kMaxUsernames,
// edit_peer_usernames_list.cpp:338).
const maxShownUsernames = 10

// toggleUsername shows or hides username, one of rows (EditUsernames). The
// table compares usernames without case, and so does the match. Showing one
// more than maxShownUsernames is USERNAMES_ACTIVE_TOO_MUCH
// (core.telegram.org/method/channels.toggleUsername).
func toggleUsername(b *userbox.Box, rows []usernameRow, username string, active bool) error {
	for _, r := range rows {
		if !strings.EqualFold(r.Username, username) {
			continue
		}
		switch {
		case r.Active == active:
			return mtproto.ErrUsernameNotModified
		case r.Editable:
			return mtproto.ErrUsernameInvalid
		case active && len(activeNames(rows)) >= maxShownUsernames:
			return mtproto.ErrUsernamesActiveTooMuch
		}
		_, err := b.Tx().Exec("UPDATE username SET active = ? WHERE id = ?", active, r.Id)
		return err
	}
	return mtproto.ErrUsernameInvalid
}

// usernamesOrder places the shown usernames of rows in order: the index in
// order of each, or false when order does not list each of them once.
func usernamesOrder(rows []usernameRow, order []string) (map[int64]int, bool) {
	at := make(map[int64]int, len(order))
	shown := 0
	for _, r := range rows {
		if !r.Active {
			continue
		}
		shown++
		found := false
		for i, name := range order {
			if strings.EqualFold(r.Username, name) {
				if found {
					return nil, false
				}
				at[r.Id], found = i, true
			}
		}
		if !found {
			return nil, false
		}
	}
	return at, shown == len(order)
}

// reorderUsernames orders the shown usernames of rows as order
// (EditUsernames); the hidden ones follow them, in their order.
func reorderUsernames(b *userbox.Box, rows []usernameRow, order []string) error {
	at, ok := usernamesOrder(rows, order)
	if !ok {
		return mtproto.ErrUsernameInvalid
	}
	same := true
	i := 0
	for _, r := range rows {
		if r.Active {
			same = same && at[r.Id] == i
			i++
		}
	}
	if same {
		return mtproto.ErrChatNotModified
	}
	base := b.Date() << 32
	hidden := int64(len(order))
	for _, r := range rows {
		pos := hidden
		if r.Active {
			pos = int64(at[r.Id])
		} else {
			hidden++
		}
		if _, err := b.Tx().Exec("UPDATE username SET order2 = ? WHERE id = ?", base+pos, r.Id); err != nil {
			return err
		}
	}
	return nil
}
