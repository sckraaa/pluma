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

// Package adminlog keeps the recent actions of a channel or a supergroup
// (channels.getAdminLog, task 078): one row per action in the box
// transaction that made it, kept for Keep, the period the clients name
// ("in the last 48 hours": tdesktop lng_admin_log_about_text, Android
// EventLogInfoDetail).
package adminlog

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/encoding/protojson"
)

// Keep is how long an action is kept, in seconds.
const Keep = 48 * 60 * 60

// The kinds of actions, one per flag of channelAdminLogEventsFilter
// (core.telegram.org/constructor/channelAdminLogEventsFilter).
const (
	Join int64 = 1 << iota
	Leave
	Invite
	Ban
	Unban
	Kick
	Unkick
	Promote
	Demote
	Info
	Settings
	Pinned
	Edit
	Delete
	GroupCall
	Invites
	Send
	Forums
	SubExtend
	EditRank
)

// Mask is the kinds a filter asks for; every kind without one.
func Mask(f *mtproto.ChannelAdminLogEventsFilter) int64 {
	if f == nil {
		return -1
	}
	var m int64
	for bit, on := range map[int64]bool{
		Join: f.Join, Leave: f.Leave, Invite: f.Invite, Ban: f.Ban, Unban: f.Unban, Kick: f.Kick, Unkick: f.Unkick,
		Promote: f.Promote, Demote: f.Demote, Info: f.Info, Settings: f.Settings, Pinned: f.Pinned, Edit: f.Edit,
		Delete: f.Delete, GroupCall: f.GroupCall, Invites: f.Invites, Send: f.Send, Forums: f.Forums,
		SubExtend: f.SubExtend, EditRank: f.EditRank,
	} {
		if on {
			m |= bit
		}
	}
	if m == 0 {
		return -1
	}
	return m
}

// kindOf is the kind of an action, as the clients file it under the filter.
func kindOf(a *mtproto.ChannelAdminLogEventAction) int64 {
	switch a.GetPredicateName() {
	case mtproto.Predicate_channelAdminLogEventActionParticipantJoin,
		mtproto.Predicate_channelAdminLogEventActionParticipantJoinByInvite,
		mtproto.Predicate_channelAdminLogEventActionParticipantJoinByRequest:
		return Join
	case mtproto.Predicate_channelAdminLogEventActionParticipantLeave:
		return Leave
	case mtproto.Predicate_channelAdminLogEventActionParticipantInvite:
		return Invite
	case mtproto.Predicate_channelAdminLogEventActionParticipantToggleBan:
		return Ban | Unban | Kick | Unkick
	case mtproto.Predicate_channelAdminLogEventActionParticipantToggleAdmin:
		return Promote | Demote
	case mtproto.Predicate_channelAdminLogEventActionParticipantEditRank:
		return EditRank
	case mtproto.Predicate_channelAdminLogEventActionChangeTitle,
		mtproto.Predicate_channelAdminLogEventActionChangeAbout,
		mtproto.Predicate_channelAdminLogEventActionChangeUsername,
		mtproto.Predicate_channelAdminLogEventActionChangePhoto:
		return Info
	case mtproto.Predicate_channelAdminLogEventActionUpdatePinned:
		return Pinned
	case mtproto.Predicate_channelAdminLogEventActionEditMessage:
		return Edit
	case mtproto.Predicate_channelAdminLogEventActionDeleteMessage:
		return Delete
	default:
		return Settings
	}
}

// Write records action, made by b's owner in the channel, and drops the
// channel's actions older than Keep. text is what a search finds it by.
func Write(b *userbox.Box, channelId int64, action *mtproto.ChannelAdminLogEventAction, text string) error {
	data, err := marshal(action)
	if err != nil {
		return err
	}
	if len(text) > 512 {
		text = text[:512]
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_admin_log (channel_id, user_id, `date`, kind, `text`, action) VALUES (?, ?, ?, ?, ?, ?)",
		channelId, b.UserId(), b.Date(), kindOf(action), text, data); err != nil {
		return err
	}
	_, err = b.Tx().Exec("DELETE FROM channel_admin_log WHERE channel_id = ? AND `date` < ?", channelId, b.Date()-Keep)
	return err
}

func marshal(a *mtproto.ChannelAdminLogEventAction) ([]byte, error) {
	return protojson.Marshal(a)
}

// Decode reads back an action Write stored.
func Decode(data []byte) (*mtproto.ChannelAdminLogEventAction, error) {
	a := &mtproto.ChannelAdminLogEventAction{}
	return a, protojson.Unmarshal(data, a)
}
