// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
//  All rights reserved.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package chat

import (
	"strings"

	"github.com/teamgram/marmota/pkg/random2"
	"github.com/teamgram/marmota/pkg/utils"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

func GetChatTypeByInviteHash(hash string) int {
	if len(hash) != 20 {
		return mtproto.PEER_UNKNOWN
	}

	if utils.IsLetter(hash[0]) {
		return mtproto.PEER_CHANNEL
	} else if utils.IsNumber(hash[0]) {
		return mtproto.PEER_CHAT
	} else {
		return mtproto.PEER_UNKNOWN
	}
}

func IsChatInviteHash(hash string) bool {
	if len(hash) != 20 {
		return false
	}

	return utils.IsNumber(hash[0])
}

func IsChannelInviteHash(hash string) bool {
	if len(hash) != 20 {
		return false
	}
	return utils.IsLetter(hash[0])
}

func GenChatInviteHash() string {
	return random2.RandomNumeric(1) + random2.RandomAlphanumeric(19)
}

func GenChannelInviteHash() string {
	return random2.RandomAlphabetic(1) + random2.RandomAlphanumeric(19)
}

// GetInviteHashByLink returns the hash of an invite link: the server's
// host and Telegram's (links handed out before task 026 were t.me, and
// clients send them back), with or without the scheme, in the forms "/+"
// and "/joinchat/" (core.telegram.org/api/links). Anything else is taken
// as the hash itself.
func GetInviteHashByLink(link string) string {
	rest := strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
	for _, host := range []string{env2.TDotMe, "t.me", "telegram.me", "telegram.dog"} {
		after, ok := strings.CutPrefix(rest, host+"/")
		if !ok {
			continue
		}
		if hash, ok := strings.CutPrefix(after, "+"); ok {
			return hash
		}
		if hash, ok := strings.CutPrefix(after, "joinchat/"); ok {
			return hash
		}
	}
	return link
}

func (m *ChatInviteExt) ToChatInvite(selfId int64, cb func(idList []int64) []*mtproto.User) *mtproto.ChatInvite {
	switch m.GetPredicateName() {
	case Predicate_chatInviteAlready:
		return mtproto.MakeTLChatInviteAlready(&mtproto.ChatInvite{
			Chat: m.Chat.ToUnsafeChat(selfId),
		}).To_ChatInvite()
	case Predicate_chatInvite:
		chatInvite := mtproto.MakeTLChatInvite(&mtproto.ChatInvite{
			Channel:           false,
			Broadcast:         false,
			Public:            false,
			Megagroup:         false,
			RequestNeeded:     m.RequestNeeded,
			Title:             m.Title,
			About:             m.About,
			Photo:             m.Photo,
			ParticipantsCount: m.ParticipantsCount,
			Participants:      []*mtproto.User{},
		}).To_ChatInvite()
		if cb != nil {
			chatInvite.Participants = cb(m.Participants)
		}
		return chatInvite
	case Predicate_chatInvitePeek:
		return mtproto.MakeTLChatInvitePeek(&mtproto.ChatInvite{
			Chat:    m.Chat.ToUnsafeChat(selfId),
			Expires: m.Expires,
		}).To_ChatInvite()
	default:
		return nil
	}
}
