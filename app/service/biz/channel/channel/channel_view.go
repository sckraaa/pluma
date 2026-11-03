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

package channel

import (
	"math"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Roles and states of a channel_participants row
// (migrate-20261103-channels.sql, ADR 0034, p. 2).
const (
	RoleMember  int32 = 0
	RoleAdmin   int32 = 1
	RoleCreator int32 = 2

	// StateIn is a participant in the channel.
	StateIn int32 = 0
	// StateLeft is one who left (or was removed without a ban).
	StateLeft int32 = 1
	// StateBanned is one with banned_rights: with view_messages kicked, out
	// of the channel; without it restricted, and still in.
	StateBanned int32 = 2
)

// banned reports whether the ban of p is in effect at now: an until_date
// of 0 is a ban for good, as is Int32.max (the clients show neither as a
// date: iOS ChatRestrictedInputPanelNode.swift:128); a ban whose date has
// passed is over.
func (p *ImmutableChannelParticipant) banned(now int32) bool {
	if p.GetState() != StateBanned {
		return false
	}
	u := p.GetUntilDate()
	return u == 0 || now < u
}

// kicked reports a ban with view_messages in effect at now: the user may not
// see the channel at all (chatBannedRights.view_messages,
// core.telegram.org/constructor/chatBannedRights).
func (p *ImmutableChannelParticipant) kicked(now int32) bool {
	return p.banned(now) && p.GetBannedRights()&mtproto.BANNED_VIEW_MESSAGES != 0
}

// Member reports whether p is in the channel: in it, or restricted
// without view_messages. A ban with view_messages that ran out leaves the
// user out of the channel until they join again; a restriction that ran out
// leaves them a plain member.
func (p *ImmutableChannelParticipant) Member() bool {
	switch p.GetState() {
	case StateIn:
		return true
	case StateBanned:
		return p.GetBannedRights()&mtproto.BANNED_VIEW_MESSAGES == 0
	default:
		return false
	}
}

// Public reports whether the channel has a public username.
func (m *ImmutableChannel) Public() bool {
	return m.GetUsername() != ""
}

// GetParticipant returns the row of userId among m's participants.
func (m *MutableChannel) GetParticipant(userId int64) (*ImmutableChannelParticipant, bool) {
	for _, p := range m.GetParticipants() {
		if p.GetUserId() == userId {
			return p, true
		}
	}
	return nil, false
}

// ToChat is the channel as viewerId sees it at now, from viewerId's row among
// m's participants (none: never joined). See ImmutableChannel.ToChat.
func (m *MutableChannel) ToChat(viewerId int64, now int32) *mtproto.Chat {
	p, _ := m.GetParticipant(viewerId)
	return m.GetChannel().ToChat(p, now)
}

// ToChat builds the channel object for a viewer whose participant row is p
// (nil: the viewer never joined) at now (unix time):
//
//   - channelForbidden ("a channel/supergroup we can't access because we were
//     banned, or for some other reason", core.telegram.org/constructor/channelForbidden)
//     to one kicked (a ban with view_messages in effect; until_date for a
//     timed ban), to one out of a private channel (left or never joined), and
//     for a deleted channel;
//   - channel to a member (creator, admin_rights, banned_rights of a
//     restriction in effect) and, with left, to one out of a public channel.
//
// Its date is when the viewer joined, or the creation date for one who is not
// a member (core.telegram.org/constructor/channel).
func (m *ImmutableChannel) ToChat(p *ImmutableChannelParticipant, now int32) *mtproto.Chat {
	if m.GetDeleted() {
		return m.forbidden(nil)
	}
	if p != nil && p.kicked(now) {
		return m.forbidden(p)
	}
	member := p != nil && p.Member()
	if !member && !m.Public() {
		return m.forbidden(nil)
	}

	c := m.base()
	c.AccessHash_FLAGINT64 = wrapperspb.Int64(m.GetAccessHash())
	c.Signatures = m.GetSignatures()
	c.HasLink = m.GetLinkedChatId() != 0
	c.ParticipantsCount_FLAGINT32 = wrapperspb.Int32(m.GetParticipantsCount())
	if !member {
		c.Left = true
		c.Date = int32(m.GetDate())
		return mtproto.MakeTLChannel(c).To_Chat()
	}

	c.Date = int32(p.GetDate())
	c.Creator = p.GetRole() == RoleCreator
	if p.GetRole() == RoleAdmin || p.GetRole() == RoleCreator {
		c.AdminRights = mtproto.AdminRights(p.GetAdminRights()).ToChatAdminRights()
	}
	if p.banned(now) {
		c.BannedRights = bannedRights(p.GetBannedRights(), p.GetUntilDate())
	}
	return mtproto.MakeTLChannel(c).To_Chat()
}

// ToMinChat is the min form of the channel (core.telegram.org/api/min): for a
// peer one only sees referenced, without the viewer's state. It carries the
// fields a client applies over its stored copy of a min channel (the list on
// core.telegram.org/constructor/channel) and nothing about the viewer. The
// access hash is left out: the hash of a min constructor is good only for
// the peer's photo (core.telegram.org/api/peers), and the channels' one hash
// would be a full one.
func (m *ImmutableChannel) ToMinChat() *mtproto.Chat {
	c := m.base()
	c.Min = true
	c.Date = int32(m.GetDate())
	return mtproto.MakeTLChannel(c).To_Chat()
}

// base is what every channel object carries: the fields of a min channel.
func (m *ImmutableChannel) base() *mtproto.Chat {
	c := &mtproto.Chat{
		Id:          m.GetId(),
		Title:       m.GetTitle(),
		Photo:       mtproto.MakeChatPhotoByPhoto(m.GetPhoto()),
		Broadcast:   m.GetBroadcast(),
		Megagroup:   m.GetMegagroup(),
		Noforwards:  m.GetNoforwards(),
		Forum:       m.GetForum(),
		JoinToSend:  m.GetJoinToSend(),
		JoinRequest: m.GetJoinRequest(),
		// The writers set the slow mode of supergroups only.
		SlowmodeEnabled:     m.GetSlowmodeSeconds() > 0,
		DefaultBannedRights: bannedRights(m.GetDefaultBannedRights(), 0),
	}
	if m.Public() {
		c.Username = wrapperspb.String(m.GetUsername())
	}
	return c
}

// forbidden is channelForbidden; p, when set, is the kicked viewer's row,
// whose timed ban gives until_date.
func (m *ImmutableChannel) forbidden(p *ImmutableChannelParticipant) *mtproto.Chat {
	c := &mtproto.Chat{
		Id:               m.GetId(),
		AccessHash_INT64: m.GetAccessHash(),
		Title:            m.GetTitle(),
		Broadcast:        m.GetBroadcast(),
		Megagroup:        m.GetMegagroup(),
	}
	if u := p.GetUntilDate(); p != nil && u != 0 && u != math.MaxInt32 {
		c.UntilDate = wrapperspb.Int32(u)
	}
	return mtproto.MakeTLChannelForbidden(c).To_Chat()
}

// bannedRights is chatBannedRights of the flags of a channel_participants or
// channels row; nothing banned is chatBannedRights with no flags, not
// nothing.
func bannedRights(flags, untilDate int32) *mtproto.ChatBannedRights {
	if r := mtproto.BannedRights(flags).ToChatBannedRights(); r != nil {
		r.UntilDate = untilDate
		return r
	}
	return mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{UntilDate: untilDate}).To_ChatBannedRights()
}
