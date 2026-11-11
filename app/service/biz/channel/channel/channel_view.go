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
	"github.com/teamgram/teamgram-server/pkg/channelperm"

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
	// StateKicked is one removed with a ban (chatBannedRights with
	// view_messages, task 042): out of the channel, and kept out until the
	// ban's until_date. A restricted member (banned_rights without
	// view_messages) stays StateIn.
	StateKicked int32 = 2
	// StateLinked is never stored: a user not in a discussion group who
	// reads it through its channel, of which the user is a member. The
	// subscribers of a channel read its comments and, unless the group asks
	// them to join (join_to_send), write them without joining
	// (core.telegram.org/constructor/channel: join_to_send "can be false
	// only for discussion groups"; task 048).
	StateLinked int32 = 3
)

// Restricted reports whether p's own restriction is in effect at now: set,
// and its until_date not passed; 0 or Int32.max is for good (the clients
// show neither as a date: iOS ChatRestrictedInputPanelNode.swift:128).
func (p *ImmutableChannelParticipant) Restricted(now int32) bool {
	return p.GetState() == StateIn && channelperm.Restricted(p.Perm(), now)
}

// Kicked reports a ban with view_messages in effect at now: the user may not
// see the channel at all and may not join it again (chatBannedRights
// view_messages, core.telegram.org/constructor/chatBannedRights). A ban that
// ran out leaves the user out of the channel until they join again.
func (p *ImmutableChannelParticipant) Kicked(now int32) bool {
	return p.kicked(now)
}

func (p *ImmutableChannelParticipant) kicked(now int32) bool {
	if p.GetState() != StateKicked {
		return false
	}
	u := p.GetUntilDate()
	return u == 0 || u == math.MaxInt32 || now < u
}

// Member reports whether p is in the channel.
func (p *ImmutableChannelParticipant) Member() bool {
	// No row: never joined (the zero state is StateIn).
	return p != nil && p.GetState() == StateIn
}

// Perm is p as channelperm reads it; nil is one never joined (left).
func (p *ImmutableChannelParticipant) Perm() channelperm.Participant {
	if p == nil {
		return channelperm.Participant{State: channelperm.StateLeft}
	}
	return channelperm.Participant{
		Role:         p.GetRole(),
		State:        p.GetState(),
		AdminRights:  p.GetAdminRights(),
		BannedRights: p.GetBannedRights(),
		UntilDate:    p.GetUntilDate(),
	}
}

// Perm is the channel as channelperm reads it.
func (m *ImmutableChannel) Perm() channelperm.Channel {
	return channelperm.Channel{Broadcast: m.GetBroadcast(), Default: m.GetDefaultBannedRights(), Blocked: m.GetBlocked()}
}

// Can answers whether the participant p may do a in the channel at now
// (channelperm.Can): nil, or the error the clients expect.
func (m *ImmutableChannel) Can(p *ImmutableChannelParticipant, a channelperm.Action, now int32) error {
	return channelperm.Can(m.Perm(), p.Perm(), a, now)
}

// SlowmodeNextSendDate is when the viewer whose row is p may send again under
// the slow mode (channelFull.slowmode_next_send_date), 0 when it is not
// waiting or the slow mode does not limit it.
func (m *ImmutableChannel) SlowmodeNextSendDate(p *ImmutableChannelParticipant, now int64) int32 {
	if p == nil || !channelperm.SlowmodeApplies(m.GetSlowmodeSeconds(), p.GetRole()) {
		return 0
	}
	return int32(channelperm.SlowmodeNextSend(m.GetSlowmodeSeconds(), p.GetLastSendDate(), now))
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
	if !member && !m.Public() && p.GetState() != StateLinked {
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
	switch p.GetRole() {
	case RoleCreator:
		// Every right: the clients let the creator grant what it holds
		// (Android ChatRightsEditActivity.java:232-236, tdesktop
		// edit_participant_box.cpp:541-554).
		c.AdminRights = mtproto.AdminRights(channelperm.CreatorRights).ToChatAdminRights()
	case RoleAdmin:
		// None for an admin of no right: iOS and Android take any
		// admin_rights for an admin's (TelegramChannel.swift:46-48,
		// ChatObject.java:1928-1930).
		c.AdminRights = mtproto.AdminRights(p.GetAdminRights()).ToChatAdminRights()
	default:
		if p.Restricted(now) {
			c.BannedRights = bannedRights(p.GetBannedRights(), p.GetUntilDate())
		}
	}
	return mtproto.MakeTLChannel(c).To_Chat()
}

// ToInvitedChat is the channel as shown to viewerId through a link of a
// shared folder (task 064, chatlists.checkChatlistInvite): a private channel
// viewerId is not in reads as a public one does, with left, so that the
// clients show it and read participants_count (iOS ChatFolders.swift:216-223);
// a member sees its own view and one banned stays out.
func (m *MutableChannel) ToInvitedChat(viewerId int64, now int32) *mtproto.Chat {
	p, _ := m.GetParticipant(viewerId)
	if p != nil && (p.Member() || p.kicked(now)) {
		return m.GetChannel().ToChat(p, now)
	}
	return m.GetChannel().ToChat(&ImmutableChannelParticipant{State: StateLinked}, now)
}

// blockedReason is the channel's restriction when the owner of the server
// blocked it: every client refuses to open it and shows the text (platform
// "all": Android MessagesController.getRestrictionReason and checkCanOpenChat,
// tdesktop UnavailableReason::Extract, iOS PeerUtils restrictionText, Web K
// isPeerRestricted; core.telegram.org/constructor/restrictionReason). The
// field has one text for every reader, and the viewer's language is not
// known here, so it carries both of the server's languages.
var blockedReason = mtproto.MakeTLRestrictionReason(&mtproto.RestrictionReason{
	Platform: "all",
	Reason:   "terms",
	Text:     "Канал заблокирован администрацией Pluma.\nThis channel has been blocked by the Pluma administration.",
}).To_RestrictionReason()

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
	var (
		restricted bool
		reasons    []*mtproto.RestrictionReason
	)
	if m.GetBlocked() {
		restricted = true
		reasons = []*mtproto.RestrictionReason{blockedReason}
	}
	c := &mtproto.Chat{
		Restricted:        restricted,
		RestrictionReason: reasons,
		Id:                m.GetId(),
		Title:             m.GetTitle(),
		Photo:             mtproto.MakeChatPhotoByPhoto(m.GetPhoto()),
		Broadcast:         m.GetBroadcast(),
		Megagroup:         m.GetMegagroup(),
		Noforwards:        m.GetNoforwards(),
		Forum:             m.GetForum(),
		ForumTabs:         m.GetForumTabs(),
		ForumSidebar:      m.GetForumSidebar(),
		JoinToSend:        m.GetJoinToSend(),
		JoinRequest:       m.GetJoinRequest(),
		// The writers set the slow mode of supergroups only.
		SlowmodeEnabled: m.GetSlowmodeSeconds() > 0,
		// A min channel carries it (core.telegram.org/constructor/channel):
		// the clients draw the posts of a channel by it (tdesktop
		// history_item.cpp:4044-4051, Android MessageObject.java:9542-9549).
		SignatureProfiles:   m.GetSignatureProfiles(),
		DefaultBannedRights: bannedRights(m.GetDefaultBannedRights(), 0),
		// How it looks (task 078): a min channel carries them too, since
		// tdesktop applies them from a min channel as well and clears what
		// is left out (data_session.cpp:1073-1077, 1229-1237).
		Color_FLAGPEERCOLOR: m.GetColor(),
		ProfileColor:        m.GetProfileColor(),
		EmojiStatus:         m.GetEmojiStatus(),
	}
	if m.Public() {
		c.Username = wrapperspb.String(m.GetUsername())
	}
	// Every username, when there are more than the editable one: the
	// clients take the list over username (tdesktop data_session.cpp:
	// 1099-1105).
	if len(m.GetUsernames()) > 1 {
		c.Usernames = m.GetUsernames()
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
