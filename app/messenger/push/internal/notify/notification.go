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

// Package notify turns the updates of a user into push notifications and
// encodes them as the official clients read them (task 009): loc_key and
// loc_args for Android and web (DrKLO/Telegram PushListenerController), a
// text the server renders for iOS (Telegram-iOS NotificationService). The
// keys and their arguments follow https://core.telegram.org/api/push-updates.
package notify

import (
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
)

// Kind is what a notification is about.
type Kind int

const (
	KindMessage       Kind = iota + 1 // a new message, album or forwarded messages
	KindAction                        // a service message of a basic group
	KindPinned                        // a message was pinned
	KindReadHistory                   // read on another device: remove the notifications
	KindMuted                         // a message in a muted chat: refresh, nothing shown
	KindSessionRevoke                 // the authorization was terminated: log out
)

// Content is what a message carries, as the loc_key suffix names it.
type Content string

const (
	ContentText    Content = "TEXT"
	ContentNoText  Content = "NOTEXT"
	ContentPhoto   Content = "PHOTO"
	ContentVideo   Content = "VIDEO"
	ContentRound   Content = "ROUND"
	ContentDoc     Content = "DOC"
	ContentAudio   Content = "AUDIO" // a voice message
	ContentGif     Content = "GIF"
	ContentGeo     Content = "GEO"
	ContentGeoLive Content = "GEOLIVE"
	ContentSticker Content = "STICKER"
	ContentContact Content = "CONTACT"
	ContentPoll    Content = "POLL"
	ContentQuiz    Content = "QUIZ"
)

// Action of a basic group, the loc_key after "CHAT_".
type Action string

const (
	ActionCreated      Action = "CREATED"
	ActionAddYou       Action = "ADD_YOU"
	ActionAddMember    Action = "ADD_MEMBER"
	ActionDeleteYou    Action = "DELETE_YOU"
	ActionDeleteMember Action = "DELETE_MEMBER"
	ActionLeft         Action = "LEFT"
	ActionReturned     Action = "RETURNED"
	ActionJoined       Action = "JOINED"
	ActionTitleEdited  Action = "TITLE_EDITED"
	ActionPhotoEdited  Action = "PHOTO_EDITED"
)

// MaxTextLength bounds a text in a push (runes): the providers take 4 KB.
const MaxTextLength = 512

// Notification is one push to the devices of a user.
type Notification struct {
	Kind   Kind
	UserId int64 // the recipient
	// Chat: the dialog is a basic group PeerId; else the private chat with
	// the user PeerId.
	Chat      bool
	PeerId    int64
	MsgId     int32
	FromId    int64 // the author
	FromName  string
	ChatTitle string

	Content Content
	// Text is the message text, sticker emoji, contact name or poll
	// question; for KindPinned, of the pinned message.
	Text string
	// Album or Forwarded: Count messages in one notification.
	Album     bool
	Forwarded bool
	Count     int

	Action     Action
	MemberName string // ADD_MEMBER, DELETE_MEMBER

	// PinnedId is the pinned message, whose content the caller fills in
	// (NeedsPinned).
	PinnedId int32
	MaxId    int32 // READ_HISTORY

	Mention bool
	Silent  bool

	groupedId int64 // the album being merged
}

// NeedsPinned tells whether the content of the pinned message is to be
// looked up.
func (n *Notification) NeedsPinned() bool { return n.Kind == KindPinned && n.PinnedId != 0 }

// SetPinned sets the content of the pinned message m.
func (n *Notification) SetPinned(m *mtproto.Message) {
	n.Content, n.Text = content(m)
}

// FromUpdates returns the notifications that the updates of userId call
// for: incoming messages and service messages of other users, history read
// on another device. Messages of one album, and forwarded messages that
// came together, are one notification.
func FromUpdates(userId int64, ups *mtproto.Updates) []*Notification {
	list, users, chats := unpack(ups)
	names := map[int64]string{}
	for _, u := range users {
		names[u.GetId()] = userName(u)
	}
	titles := map[int64]string{}
	for _, c := range chats {
		titles[c.GetId()] = c.GetTitle()
	}

	var out []*Notification
	for _, u := range list {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			n := fromMessage(userId, u.GetMessage_MESSAGE(), names, titles)
			if n == nil {
				continue
			}
			if last := lastOf(out); last != nil && merges(last, n, u.GetMessage_MESSAGE()) {
				last.Count++
				continue
			}
			out = append(out, n)
		case mtproto.Predicate_updateReadHistoryInbox:
			peer := u.GetPeer_PEER()
			n := &Notification{Kind: KindReadHistory, UserId: userId, MaxId: u.GetMaxId()}
			switch peer.GetPredicateName() {
			case mtproto.Predicate_peerUser:
				n.PeerId = peer.GetUserId()
			case mtproto.Predicate_peerChat:
				n.Chat, n.PeerId = true, peer.GetChatId()
			default:
				continue
			}
			out = append(out, n)
		}
	}
	return out
}

func lastOf(ns []*Notification) *Notification {
	if len(ns) == 0 {
		return nil
	}
	return ns[len(ns)-1]
}

// merges tells whether message m, notification n, belongs to last: the
// same album, or forwarded messages of the same author in the same dialog.
func merges(last, n *Notification, m *mtproto.Message) bool {
	if last.Kind != KindMessage || n.Kind != KindMessage || last.Chat != n.Chat || last.PeerId != n.PeerId || last.FromId != n.FromId {
		return false
	}
	switch {
	case last.Album && n.Album:
		return last.groupedId != 0 && last.groupedId == m.GetGroupedId().GetValue()
	case last.Forwarded && n.Forwarded:
		return true
	}
	return false
}

func unpack(ups *mtproto.Updates) ([]*mtproto.Update, []*mtproto.User, []*mtproto.Chat) {
	switch ups.GetPredicateName() {
	case mtproto.Predicate_updates, mtproto.Predicate_updatesCombined:
		return ups.GetUpdates(), ups.GetUsers(), ups.GetChats()
	case mtproto.Predicate_updateShort:
		return []*mtproto.Update{ups.GetUpdate()}, nil, nil
	}
	return nil, nil, nil
}

// fromMessage returns the notification of an incoming message, nil for an
// outgoing one or one the recipient need not be told about.
func fromMessage(userId int64, m *mtproto.Message, names, titles map[int64]string) *Notification {
	if m == nil || m.GetOut() {
		return nil
	}
	n := &Notification{
		Kind:    KindMessage,
		UserId:  userId,
		MsgId:   m.GetId(),
		Mention: m.GetMentioned(),
		Silent:  m.GetSilent(),
	}
	switch peer := m.GetPeerId(); peer.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		n.PeerId = peer.GetUserId()
		n.FromId = n.PeerId
	case mtproto.Predicate_peerChat:
		n.Chat, n.PeerId = true, peer.GetChatId()
		n.ChatTitle = titles[n.PeerId]
	default:
		return nil // channels are not served yet
	}
	if from := m.GetFromId(); from.GetPredicateName() == mtproto.Predicate_peerUser {
		n.FromId = from.GetUserId()
	}
	if n.FromId == 0 || n.FromId == userId {
		return nil
	}
	n.FromName = names[n.FromId]

	if m.GetPredicateName() == mtproto.Predicate_messageService {
		n = withAction(n, userId, m.GetAction(), names)
		if n != nil && n.Kind == KindPinned {
			n.PinnedId = m.GetReplyTo().GetReplyToMsgId()
		}
		return n
	}
	n.Content, n.Text = content(m)
	switch {
	case m.GetGroupedId() != nil && m.GetGroupedId().GetValue() != 0:
		n.Album, n.Count, n.groupedId = true, 1, m.GetGroupedId().GetValue()
	case m.GetFwdFrom() != nil:
		n.Forwarded, n.Count = true, 1
	}
	return n
}

// withAction makes n the notification of a service message, nil for an
// action that has none.
func withAction(n *Notification, userId int64, a *mtproto.MessageAction, names map[int64]string) *Notification {
	n.Kind = KindAction
	switch a.GetPredicateName() {
	case mtproto.Predicate_messageActionChatCreate:
		n.Action = ActionCreated
	case mtproto.Predicate_messageActionChatAddUser:
		users := a.GetUsers()
		switch {
		case len(users) == 1 && users[0] == n.FromId:
			n.Action = ActionReturned
		case containsId(users, userId):
			n.Action = ActionAddYou
		case len(users) > 0:
			n.Action, n.MemberName = ActionAddMember, names[users[0]]
		default:
			return nil
		}
	case mtproto.Predicate_messageActionChatDeleteUser:
		switch a.GetUserId() {
		case n.FromId:
			n.Action = ActionLeft
		case userId:
			n.Action = ActionDeleteYou
		default:
			n.Action, n.MemberName = ActionDeleteMember, names[a.GetUserId()]
		}
	case mtproto.Predicate_messageActionChatJoinedByLink, mtproto.Predicate_messageActionChatJoinedByRequest:
		n.Action = ActionJoined
	case mtproto.Predicate_messageActionChatEditTitle:
		n.Action = ActionTitleEdited
		if t := a.GetTitle_STRING(); t != "" {
			n.ChatTitle = t
		}
	case mtproto.Predicate_messageActionChatEditPhoto:
		n.Action = ActionPhotoEdited
	case mtproto.Predicate_messageActionPinMessage:
		n.Kind, n.Action = KindPinned, ""
		n.Content = ContentNoText
		return n
	default:
		return nil
	}
	if !n.Chat {
		return nil // group actions only
	}
	return n
}

// content returns what message m carries and its text.
func content(m *mtproto.Message) (Content, string) {
	text := truncate(m.GetMessage())
	media := m.GetMedia()
	switch media.GetPredicateName() {
	case "", mtproto.Predicate_messageMediaEmpty, mtproto.Predicate_messageMediaWebPage, mtproto.Predicate_messageMediaUnsupported:
		if text == "" {
			return ContentNoText, ""
		}
		return ContentText, text
	case mtproto.Predicate_messageMediaPhoto:
		return ContentPhoto, text
	case mtproto.Predicate_messageMediaDocument:
		if c := documentContent(media); c != ContentSticker {
			return c, text
		}
		return ContentSticker, stickerEmoji(m)
	case mtproto.Predicate_messageMediaGeo, mtproto.Predicate_messageMediaVenue:
		return ContentGeo, ""
	case mtproto.Predicate_messageMediaGeoLive:
		return ContentGeoLive, ""
	case mtproto.Predicate_messageMediaContact:
		return ContentContact, truncate(strings.TrimSpace(media.GetFirstName() + " " + media.GetLastName()))
	case mtproto.Predicate_messageMediaPoll:
		q := truncate(media.GetPoll().GetQuestion_TEXTWITHENTITIES().GetText())
		if media.GetPoll().GetQuiz() {
			return ContentQuiz, q
		}
		return ContentPoll, q
	case mtproto.Predicate_messageMediaDice:
		return ContentText, media.GetEmoticon()
	}
	if text != "" {
		return ContentText, text
	}
	return ContentNoText, ""
}

func documentContent(media *mtproto.MessageMedia) Content {
	switch {
	case media.GetRound():
		return ContentRound
	case media.GetVoice():
		return ContentAudio
	}
	c := ContentDoc
	for _, a := range media.GetDocument().GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeSticker:
			return ContentSticker
		case mtproto.Predicate_documentAttributeAnimated:
			c = ContentGif
		case mtproto.Predicate_documentAttributeVideo:
			if a.GetRoundMessage() {
				return ContentRound
			}
			if c != ContentGif {
				c = ContentVideo
			}
		case mtproto.Predicate_documentAttributeAudio:
			if a.GetVoice() {
				return ContentAudio
			}
		}
	}
	return c
}

// stickerEmoji is the emoji of a sticker message, the argument of
// MESSAGE_STICKER.
func stickerEmoji(m *mtproto.Message) string {
	for _, a := range m.GetMedia().GetDocument().GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeSticker {
			return a.GetAlt()
		}
	}
	return ""
}

func userName(u *mtproto.User) string {
	name := strings.TrimSpace(u.GetFirstName().GetValue() + " " + u.GetLastName().GetValue())
	if name == "" {
		name = u.GetUsername().GetValue()
	}
	return name
}

func containsId(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= MaxTextLength {
		return s
	}
	r := []rune(s)
	return string(r[:MaxTextLength-1]) + "…"
}
