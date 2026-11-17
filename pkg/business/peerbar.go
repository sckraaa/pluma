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

package business

import (
	"context"
	"errors"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Users is what the bar of the connected bot needs of the user service
// (app/service/biz/user/client.UserClient): who the other side of the chat
// is, whether it is a contact, and the username of the bot.
type Users interface {
	UserGetImmutableUser(ctx context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error)
	UserCheckContact(ctx context.Context, in *userpb.TLUserCheckContact) (*mtproto.Bool, error)
}

// ServedUser tells whether the private chat of owner with u is one a
// connected bot may serve: a chat with a person, not with the account
// itself, a bot, a deleted account, the service notifications or a support
// account. These are the chats of a business: the bot "can be used to
// interact with users" of the account
// (core.telegram.org/api/bots/connected-business-bots).
func ServedUser(owner int64, u *mtproto.ImmutableUser) bool {
	return u != nil && u.Id() != owner && u.Id() != moderation.ServiceUserId &&
		!u.IsBot() && !u.Deleted() && !u.Support()
}

// PeerBar is the connected bot of a chat as peerSettings shows it: the
// bar over the chat with the bot, its state and the button that opens the
// bot (tdesktop history/view/history_view_contact_status.cpp:1255-1270,
// Android ui/Business/BusinessBotButton.java).
type PeerBar struct {
	BotID int64
	// ManageURL is business_bot_manage_url, "a deep link to the management
	// menu of the bot" (core.telegram.org/constructor/peerSettings); the
	// clients open it as a link (tdesktop
	// history_view_contact_status.cpp:1305-1312, Android
	// BusinessBotButton.java:145-147).
	ManageURL string
	Paused    bool
	// CanReply is business_bot_can_reply: the bot has the right to reply
	// and is not paused in the chat. tdesktop shows "can reply" or "can
	// only read" by it, and sets it back itself after a resume
	// (data/business/data_business_chatbots.cpp:124-131).
	CanReply bool
}

// ManageURL is the link business_bot_manage_url opens: the bot by its
// username on host (env2.TDotMe). What Telegram puts there is not
// published (probably a start parameter, not verified); the link opens the
// chat with the bot, where its menu is.
func ManageURL(host, username string) string {
	if username == "" {
		return ""
	}
	return "https://" + host + "/" + username
}

// PeerBar returns the bar of the connected bot in the private chat of
// userID with peerID, nil when no connected bot serves that chat: no bot is
// connected, the chat is not one a bot serves (ServedUser) or the
// recipients of the bot leave it out. host builds the manage link.
func (s *Store) PeerBar(ctx context.Context, users Users, host string, userID, peerID int64) (*PeerBar, error) {
	c, err := s.Connection(ctx, userID)
	if err != nil || c == nil {
		return nil, err
	}
	_, served, err := s.ChatPeer(ctx, users, c, peerID)
	if err != nil || !served {
		return nil, err
	}
	paused, err := s.Paused(ctx, c.UserID, peerID)
	if err != nil {
		return nil, err
	}
	bot, err := users.UserGetImmutableUser(ctx, &userpb.TLUserGetImmutableUser{Id: c.BotID})
	if err != nil {
		return nil, err
	}
	return &PeerBar{
		BotID:     c.BotID,
		ManageURL: ManageURL(host, bot.Username()),
		Paused:    paused,
		CanReply:  !paused && c.Rights.Has(RightReply),
	}, nil
}

// ChatPeer tells whether c serves the private chat of its account with
// peerID, and what the recipients of c know of the other side: the chat is
// one a bot serves (ServedUser) and the recipients take it in, as a new
// chat when it began after the bot was connected (ChatIsNew), as a contact
// when peerID is a contact of the account. Whether the bot is paused there
// is not asked.
func (s *Store) ChatPeer(ctx context.Context, users Users, c *Connection, peerID int64) (Peer, bool, error) {
	p := Peer{UserID: peerID}
	if peerID == c.UserID {
		return p, false, nil
	}
	u, err := users.UserGetImmutableUser(ctx, &userpb.TLUserGetImmutableUser{Id: peerID})
	if err != nil {
		if errors.Is(err, mtproto.ErrUserIdInvalid) {
			return p, false, nil
		}
		return p, false, err
	}
	if !ServedUser(c.UserID, u) {
		return p, false, nil
	}
	if p.New, err = s.ChatIsNew(ctx, c.UserID, peerID, c.CreatedAt); err != nil {
		return p, false, err
	}
	contact, err := users.UserCheckContact(ctx, &userpb.TLUserCheckContact{UserId: c.UserID, Id: peerID})
	if err != nil {
		return p, false, err
	}
	p.Contact = mtproto.FromBool(contact)
	return p, c.Recipients.Match(p), nil
}

// Apply returns settings with the business_bot_* fields of b; a nil b
// clears them. settings is not changed; nil is empty settings.
func (b *PeerBar) Apply(settings *mtproto.PeerSettings) *mtproto.PeerSettings {
	out := &mtproto.PeerSettings{}
	if settings != nil {
		out = proto.Clone(settings).(*mtproto.PeerSettings)
	}
	out.BusinessBotPaused, out.BusinessBotCanReply, out.BusinessBotId, out.BusinessBotManageUrl = false, false, nil, nil
	if b != nil {
		// business_bot_id and business_bot_manage_url are one flag
		// (flags.13): both or neither.
		out.BusinessBotPaused = b.Paused
		out.BusinessBotCanReply = b.CanReply
		out.BusinessBotId = wrapperspb.Int64(b.BotID)
		out.BusinessBotManageUrl = wrapperspb.String(b.ManageURL)
	}
	return mtproto.MakeTLPeerSettings(out).To_PeerSettings()
}
