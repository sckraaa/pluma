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

// Package channelperm decides what a participant may do in a channel or a
// supergroup (task 042): one function, Can, for every handler that writes
// to a channel, so that the rights of an admin (chatAdminRights), the
// restrictions of a member (chatBannedRights) and the default restrictions
// of the channel are read in one place.
//
// The rules (core.telegram.org/api/rights, constructor/chatAdminRights,
// constructor/chatBannedRights):
//   - the creator may do everything;
//   - an admin does what their admin_rights allow; the restrictions of
//     members, default or their own, do not apply to admins;
//   - in a broadcast channel a member does nothing but read;
//   - in a supergroup a member does what neither the channel's default
//     restrictions nor their own restriction in effect forbid; a set flag of
//     chatBannedRights forbids, send_media forbids every kind of media, and
//     a restriction is over at its until_date (0 or Int32.max: for good).
package channelperm

import (
	"math"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/grpc/status"
)

// Roles and states of a participant, as in channel_participants.
const (
	RoleMember  int32 = 0
	RoleAdmin   int32 = 1
	RoleCreator int32 = 2

	StateIn     int32 = 0
	StateLeft   int32 = 1
	StateKicked int32 = 2
)

// CreatorRights are the admin rights the creator holds: every right but
// anonymous, which is a choice of the admin's own.
const CreatorRights = mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_POST_MESSAGES | mtproto.ADMIN_EDIT_MESSAGES |
	mtproto.ADMIN_DELETE_MESSAGES | mtproto.ADMIN_BAN_USERS | mtproto.ADMIN_INVITE_USERS | mtproto.ADMIN_PIN_MESSAGES |
	mtproto.ADMIN_ADD_ADMINS | mtproto.ADMIN_MANAGE_CALL | mtproto.ADMIN_OTHER | mtproto.ADMIN_MANAGE_TOPICS |
	mtproto.ADMIN_POST_STORIES | mtproto.ADMIN_EDIT_STORIES | mtproto.ADMIN_DELETE_STORIES |
	mtproto.ADMIN_MANAGE_DIRECT_MESSAGES | mtproto.ADMIN_MANAGE_RANKS

// Channel is what Can reads of a channel.
type Channel struct {
	Broadcast bool
	// Default is default_banned_rights: the restrictions of every member.
	Default int32
}

// Participant is what Can reads of a participant of the channel.
type Participant struct {
	Role         int32
	State        int32
	AdminRights  int32
	BannedRights int32
	// UntilDate ends BannedRights; 0 or Int32.max: for good.
	UntilDate int32
}

// Action is something a participant does in a channel.
type Action int

const (
	// Sending: plain text and each kind of media, as chatBannedRights names
	// them.
	SendPlain Action = iota
	SendPhotos
	SendVideos
	SendRoundVideos
	SendAudios
	SendVoices
	SendDocs
	SendStickers
	SendGifs
	SendGames
	SendInline
	SendPolls
	// SendMedia is a kind of media without a flag of its own (location,
	// contact): send_media forbids it.
	SendMedia
	EmbedLinks
	// EditOthers and DeleteOthers act on the messages of others (in a
	// broadcast channel, of other admins).
	EditOthers
	DeleteOthers
	PinMessages
	InviteUsers
	ChangeInfo
	BanUsers
	AddAdmins
	ManageCall
	ManageTopics
)

// sendBits are the restrictions that forbid a kind of message, and the
// error each answers with.
var sendBits = map[Action]struct {
	banned int32
	media  bool
	err    error
}{
	SendPlain:       {mtproto.BANNED_SEND_PLAIN, false, mtproto.ErrChatSendPlainForbidden},
	SendPhotos:      {mtproto.BANNED_SEND_PHOTOS, true, mtproto.ErrChatSendPhotosForbidden},
	SendVideos:      {mtproto.BANNED_SEND_VIDEOS, true, mtproto.ErrChatSendVideosForbidden},
	SendRoundVideos: {mtproto.BANNED_SEND_ROUNDVIDEOS, true, ErrChatSendRoundvideosForbidden},
	SendAudios:      {mtproto.BANNED_SEND_AUDIOS, true, mtproto.ErrChatSendAudiosForbidden},
	SendVoices:      {mtproto.BANNED_SEND_VOICES, true, mtproto.ErrChatSendVoicesForbidden},
	SendDocs:        {mtproto.BANNED_SEND_DOCS, true, mtproto.ErrChatSendDocsForbidden},
	SendStickers:    {mtproto.BANNED_SEND_STICKERS, true, mtproto.ErrChatSendStickersForbidden},
	SendGifs:        {mtproto.BANNED_SEND_GIFS, true, mtproto.ErrChatSendGifsForbidden},
	SendGames:       {mtproto.BANNED_SEND_GAMES, true, mtproto.ErrChatSendGameForbidden},
	SendInline:      {mtproto.BANNED_SEND_INLINE, true, mtproto.ErrChatSendInlineForbidden},
	SendPolls:       {mtproto.BANNED_SEND_POLLS, true, mtproto.ErrChatSendPollForbidden},
	SendMedia:       {0, true, mtproto.ErrChatSendMediaForbidden},
	EmbedLinks:      {mtproto.BANNED_EMBED_LINKS, false, mtproto.ErrChatWriteForbidden},
}

// memberBits are the restrictions of the other things a member of a
// supergroup may do.
var memberBits = map[Action]int32{
	PinMessages:  mtproto.BANNED_PIN_MESSAGES,
	InviteUsers:  mtproto.BANNED_INVITE_USERS,
	ChangeInfo:   mtproto.BANNED_CHANGE_INFO,
	ManageTopics: mtproto.BANNED_MANAGE_TOPICS,
}

// adminBits are the admin rights each action needs of an admin.
var adminBits = map[Action]int32{
	EditOthers:   mtproto.ADMIN_EDIT_MESSAGES,
	DeleteOthers: mtproto.ADMIN_DELETE_MESSAGES,
	PinMessages:  mtproto.ADMIN_PIN_MESSAGES,
	InviteUsers:  mtproto.ADMIN_INVITE_USERS,
	ChangeInfo:   mtproto.ADMIN_CHANGE_INFO,
	BanUsers:     mtproto.ADMIN_BAN_USERS,
	AddAdmins:    mtproto.ADMIN_ADD_ADMINS,
	ManageCall:   mtproto.ADMIN_MANAGE_CALL,
	ManageTopics: mtproto.ADMIN_MANAGE_TOPICS,
}

// ErrChatSendRoundvideosForbidden: the schema's error list has no entry for
// round videos (core.telegram.org/method/messages.sendMedia lists it, 403).
var ErrChatSendRoundvideosForbidden = status.Error(mtproto.ErrForbidden, "CHAT_SEND_ROUNDVIDEOS_FORBIDDEN")

// Can answers whether p may do a in ch at now: nil, or the error the
// clients expect. A participant out of the channel (left, kicked) gets
// CHANNEL_PRIVATE; the caller has already let in whom it lets read.
func Can(ch Channel, p Participant, a Action, now int32) error {
	if p.State != StateIn {
		return mtproto.ErrChannelPrivate
	}
	switch p.Role {
	case RoleCreator:
		return nil
	case RoleAdmin:
		return canAdmin(ch, p, a)
	}
	if ch.Broadcast {
		if _, send := sendBits[a]; send {
			return mtproto.ErrChatWriteForbidden
		}
		return mtproto.ErrChatAdminRequired
	}
	banned := ch.Default
	if Restricted(p, now) {
		banned |= p.BannedRights
	}
	if s, send := sendBits[a]; send {
		switch {
		case banned&mtproto.BANNED_SEND_MESSAGES != 0:
			return mtproto.ErrChatWriteForbidden
		case s.banned != 0 && banned&s.banned != 0, s.media && banned&mtproto.BANNED_SEND_MEDIA != 0:
			// The error of the kind, also for send_media: Android has a
			// text for each kind and none for CHAT_SEND_MEDIA_FORBIDDEN
			// (AlertsCreator.java:448-519 at aac1efcb).
			return s.err
		}
		return nil
	}
	if bit, ok := memberBits[a]; ok {
		if banned&bit != 0 {
			return mtproto.ErrChatAdminRequired
		}
		return nil
	}
	return mtproto.ErrChatAdminRequired
}

// canAdmin: an admin of a broadcast channel posts with post_messages; of a
// supergroup sends anything. Every other action needs its admin right; in a
// broadcast channel pinning is also allowed by edit_messages, as the
// clients allow it (Android ChatObject.java:2283-2284, iOS
// TelegramChannel.swift, Web K hasRights.ts:97-99).
func canAdmin(ch Channel, p Participant, a Action) error {
	if _, send := sendBits[a]; send {
		if ch.Broadcast && p.AdminRights&mtproto.ADMIN_POST_MESSAGES == 0 {
			return mtproto.ErrChatWriteForbidden
		}
		return nil
	}
	bit, ok := adminBits[a]
	if !ok {
		return mtproto.ErrChatAdminRequired
	}
	if p.AdminRights&bit != 0 {
		return nil
	}
	if a == PinMessages && ch.Broadcast && p.AdminRights&mtproto.ADMIN_EDIT_MESSAGES != 0 {
		return nil
	}
	return mtproto.ErrChatAdminRequired
}

// Restricted reports whether the participant's own restriction is in effect
// at now: set, and its until_date not passed (0 or Int32.max: for good).
func Restricted(p Participant, now int32) bool {
	if p.BannedRights == 0 {
		return false
	}
	return p.UntilDate == 0 || p.UntilDate == math.MaxInt32 || now < p.UntilDate
}

// UntilDate is the until_date of a restriction given at now as the clients
// send it: less than 30 seconds or more than 366 days from now is for good,
// Int32.max (core.telegram.org/constructor/chatBannedRights).
func UntilDate(until, now int32) int32 {
	if until == 0 || int64(until) < int64(now)+30 || int64(until) > int64(now)+366*86400 {
		return math.MaxInt32
	}
	return until
}
