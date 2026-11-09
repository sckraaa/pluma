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

package botstore

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"
)

// The scopes of a command list that name no chat.
const (
	scopeDefault    = "default"
	scopeUsers      = "users"
	scopeChats      = "chats"
	scopeChatAdmins = "chat_admins"
)

func scopePeer(peerType int32, peerId int64) string {
	return fmt.Sprintf("peer:%d:%d", peerType, peerId)
}

func scopePeerAdmins(peerType int32, peerId int64) string {
	return fmt.Sprintf("peer_admins:%d:%d", peerType, peerId)
}

func scopePeerUser(peerType int32, peerId, userId int64) string {
	return fmt.Sprintf("peer_user:%d:%d:%d", peerType, peerId, userId)
}

// ScopeKey is the key of the list a BotCommandScope of bot botId names: its
// peers are input peers of that bot.
func ScopeKey(botId int64, scope *mtproto.BotCommandScope) (string, error) {
	peer := func() (*mtproto.PeerUtil, error) {
		p := mtproto.FromInputPeer2(botId, scope.GetPeer())
		if !p.IsUser() && !p.IsChat() && !p.IsChannel() {
			return nil, mtproto.ErrPeerIdInvalid
		}
		return p, nil
	}
	switch scope.GetPredicateName() {
	case mtproto.Predicate_botCommandScopeDefault:
		return scopeDefault, nil
	case mtproto.Predicate_botCommandScopeUsers:
		return scopeUsers, nil
	case mtproto.Predicate_botCommandScopeChats:
		return scopeChats, nil
	case mtproto.Predicate_botCommandScopeChatAdmins:
		return scopeChatAdmins, nil
	case mtproto.Predicate_botCommandScopePeer:
		p, err := peer()
		if err != nil {
			return "", err
		}
		return scopePeer(p.PeerType, p.PeerId), nil
	case mtproto.Predicate_botCommandScopePeerAdmins:
		p, err := peer()
		if err != nil {
			return "", err
		}
		return scopePeerAdmins(p.PeerType, p.PeerId), nil
	case mtproto.Predicate_botCommandScopePeerUser:
		p, err := peer()
		if err != nil {
			return "", err
		}
		u := mtproto.FromInputUser(botId, scope.GetUserId())
		if !u.IsUser() {
			return "", mtproto.ErrUserIdInvalid
		}
		return scopePeerUser(p.PeerType, p.PeerId, u.PeerId), nil
	}
	return "", mtproto.ErrPeerIdInvalid
}

// scopeChain is the scopes of a viewer, the first to be used first
// (core.telegram.org/bots/api#determining-list-of-commands; the Bot API's
// scopes are, in order, botCommandScopePeer as Chat, Users as
// AllPrivateChats, Chats as AllGroupChats, ChatAdmins as
// AllChatAdministrators, PeerAdmins as ChatAdministrators, PeerUser as
// ChatMember).
func scopeChain(v Viewer) []string {
	if v.PeerType == mtproto.PEER_USER {
		return []string{scopePeer(mtproto.PEER_USER, v.UserId), scopeUsers, scopeDefault}
	}
	chain := []string{scopePeerUser(v.PeerType, v.PeerId, v.UserId)}
	if v.Admin {
		chain = append(chain, scopePeerAdmins(v.PeerType, v.PeerId))
	}
	chain = append(chain, scopePeer(v.PeerType, v.PeerId))
	if v.Admin {
		chain = append(chain, scopeChatAdmins)
	}
	return append(chain, scopeChats, scopeDefault)
}
