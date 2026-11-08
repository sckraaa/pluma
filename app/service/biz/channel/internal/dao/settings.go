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
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The settings of a channel (task 044): each a flag of the channel's row,
// changed by one who may change its info (channelperm.ChangeInfo), in its
// box transaction with the channel locked; the members load the channel
// again on updateChannel.

// HiddenMembersMin is the fewest members a supergroup hides its members
// with: hidden_members_group_size_min of the app config, 100
// (core.telegram.org/api/config; Android MessagesController.java:3788).
const HiddenMembersMin = 100

var settingColumns = map[int32]string{
	channel.SettingSignatures:         "signatures",
	channel.SettingPrehistoryHidden:   "hidden_prehistory",
	channel.SettingParticipantsHidden: "participants_hidden",
	channel.SettingNoforwards:         "noforwards",
	channel.SettingJoinToSend:         "join_to_send",
}

type settingsRow struct {
	Broadcast          bool  `db:"broadcast"`
	Linked             int64 `db:"linked_chat_id"`
	ParticipantsCount  int32 `db:"participants_count"`
	Signatures         bool  `db:"signatures"`
	HiddenPrehistory   bool  `db:"hidden_prehistory"`
	ParticipantsHidden bool  `db:"participants_hidden"`
	Noforwards         bool  `db:"noforwards"`
	JoinToSend         bool  `db:"join_to_send"`
}

func (r *settingsRow) value(setting int32) bool {
	switch setting {
	case channel.SettingSignatures:
		return r.Signatures
	case channel.SettingPrehistoryHidden:
		return r.HiddenPrehistory
	case channel.SettingParticipantsHidden:
		return r.ParticipantsHidden
	case channel.SettingNoforwards:
		return r.Noforwards
	}
	return r.JoinToSend
}

// ToggleSetting turns setting of the channel on or off by b's owner.
// Errors as Telegram's: CHAT_NOT_MODIFIED for the value it has;
// CHANNEL_INVALID for a setting the kind of channel has not (signatures
// of a broadcast channel; the history, the members and joining to comment
// of a supergroup, the last of a discussion group only);
// CHAT_LINK_EXISTS for hiding the history of a discussion group, whose
// commenters read it; PARTICIPANTS_TOO_FEW for hiding the members of a
// group of fewer than HiddenMembersMin.
func (d *Dao) ToggleSetting(b *userbox.Box, channelId int64, setting int32, enabled bool) (*ChannelChange, error) {
	column, ok := settingColumns[setting]
	if !ok {
		return nil, mtproto.ErrInputRequestInvalid
	}
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
	var rows []settingsRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT broadcast, linked_chat_id, participants_count, signatures, hidden_prehistory, participants_hidden, noforwards, join_to_send FROM channels WHERE id = ?",
		channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	switch {
	case setting == channel.SettingSignatures && !r.Broadcast,
		setting == channel.SettingPrehistoryHidden && r.Broadcast,
		setting == channel.SettingParticipantsHidden && r.Broadcast,
		setting == channel.SettingJoinToSend && (r.Broadcast || r.Linked == 0):
		return nil, mtproto.ErrChannelInvalid
	case r.value(setting) == enabled:
		return nil, mtproto.ErrChatNotModified
	case setting == channel.SettingPrehistoryHidden && enabled && r.Linked != 0:
		return nil, mtproto.ErrChatLinkExists
	case setting == channel.SettingParticipantsHidden && enabled && r.ParticipantsCount < HiddenMembersMin:
		return nil, mtproto.ErrParticipantsTooFew
	}
	if _, err = b.Tx().Exec("UPDATE channels SET `"+column+"` = ?, version = version + 1 WHERE id = ?", enabled, channelId); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// SetAvailableReactions sets the reactions the members of the channel may
// use (mtproto.ChatReactionsType*: none, all, all with custom emoji, some),
// by b's owner, as biz chat keeps them for a basic group; at most
// reactions.InChatMax of some (REACTIONS_TOO_MANY).
func (d *Dao) SetAvailableReactions(b *userbox.Box, channelId int64, t int32, list []string) (*ChannelChange, error) {
	if t != mtproto.ChatReactionsTypeSome {
		list = nil
	}
	if len(list) > reactions.InChatMax {
		return nil, mtproto.ErrReactionsTooMany
	}
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
	var data any
	if list != nil {
		s, err := jsonx.MarshalToString(list)
		if err != nil {
			return nil, err
		}
		data = s
	}
	if _, err = b.Tx().Exec("UPDATE channels SET available_reactions_type = ?, available_reactions = ?, version = version + 1 WHERE id = ?",
		t, data, channelId); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}
