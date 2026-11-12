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

// Package channelcontents gives the messages of a channel or a supergroup,
// stored once for all (ADR 0034, p. 1), the unread contents each viewer has
// of them (task 080): media_unread ("Whether there are unread media
// attachments in this message", core.telegram.org/constructor/message) and
// mentioned.
//
// A voice or round video message is unread for a member until that member
// plays it, which the clients tell with channels.readMessageContents, for
// channels as for supergroups (Android MessagesController
// markMessageContentAsRead, tdesktop ApiWrap::markContentsRead, iOS
// ManagedSynchronizeConsumeMessageContentsOperations.swift:131). The clients
// take the flag as it comes for every kind of chat: iOS
// StoreMessage_Telegram.swift:1320-1330 (CloudChannel included), Android
// MessageObject.isContentUnread, tdesktop HistoryItem::isUnreadMedia. A
// member who joins later has not played the older ones either. The plays
// are rows of channel_contents_read, written by the reader's
// channels.readMessageContents; a message nobody played has none.
//
// An unread mention is media_unread as well: tdesktop counts a mention
// unread only with the flag (HistoryItem::isUnreadMention,
// history_item.cpp:1855) and leaves the others out of its list of unread
// mentions (history_unread_things.cpp:216). The mentions are the rows of
// channel_message_mentions.
//
// The history a group was imported with (task 083, ADR 0051) is played for
// everyone, the members who join later too: the export it came from does
// not tell who played what, and the owner chose that nothing of it is
// unread. channels.imported_max_id is the last message of that history.
package channelcontents

import (
	"context"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// Querier reads rows: the MySQL of a service.
type Querier interface {
	QueryRowsPartial(ctx context.Context, v interface{}, query string, args ...interface{}) error
}

// Consumable tells whether the contents of m are read apart from the
// message: a voice or round video message (mtproto.CheckHasMediaUnread, as
// for a private chat).
func Consumable(m *mtproto.Message) bool {
	return m.GetPredicateName() == mtproto.Predicate_message && mtproto.CheckHasMediaUnread(m)
}

type key struct {
	user int64
	id   int32
}

// State is what users have of some messages of one channel: the plays and
// the mentions. The zero State has none.
type State struct {
	read     map[key]bool
	mentions map[key]bool // true while the mention is unread
	// authors wrote the voice and round video messages (LoadAll): a push
	// comes from whoever changed a message, not always from its author.
	authors map[int32]int64
	// imported is the last message of the channel's imported history,
	// played for everyone; 0 for a channel without one.
	imported int32
}

// Load reads the state of messages of channelId for user; user 0 (no one)
// has none.
func Load(ctx context.Context, q Querier, channelId, user int64, messages []*mtproto.Message) (*State, error) {
	if user == 0 {
		return &State{}, nil
	}
	return load(ctx, q, channelId, user, messages)
}

// LoadAll reads the state of messages of channelId for every user: a push
// to all the members.
func LoadAll(ctx context.Context, q Querier, channelId int64, messages []*mtproto.Message) (*State, error) {
	return load(ctx, q, channelId, 0, messages)
}

func load(ctx context.Context, q Querier, channelId, user int64, messages []*mtproto.Message) (*State, error) {
	s := &State{read: map[key]bool{}, mentions: map[key]bool{}}
	var ids, consumable []any
	for _, m := range messages {
		if m.GetPredicateName() == mtproto.Predicate_messageEmpty || m.GetId() <= 0 {
			continue
		}
		ids = append(ids, m.GetId())
		if Consumable(m) {
			consumable = append(consumable, m.GetId())
		}
	}
	if len(ids) == 0 {
		return s, nil
	}
	byUser, userArgs := "", []any{}
	if user != 0 {
		byUser, userArgs = " AND user_id = ?", []any{user}
	}
	if len(consumable) > 0 {
		var imported []int32
		if err := q.QueryRowsPartial(ctx, &imported, "SELECT imported_max_id FROM channels WHERE id = ?", channelId); err != nil {
			return nil, err
		}
		if len(imported) > 0 {
			s.imported = imported[0]
		}
		var rows []struct {
			UserId    int64 `db:"user_id"`
			MessageId int32 `db:"message_id"`
		}
		if err := q.QueryRowsPartial(ctx, &rows,
			"SELECT user_id, message_id FROM channel_contents_read WHERE channel_id = ?"+byUser+" AND message_id IN ("+inList(len(consumable))+")",
			append(append([]any{channelId}, userArgs...), consumable...)...); err != nil {
			return nil, err
		}
		for _, r := range rows {
			s.read[key{r.UserId, r.MessageId}] = true
		}
		if user == 0 {
			var authors []struct {
				Id           int32 `db:"id"`
				SenderUserId int64 `db:"sender_user_id"`
			}
			if err := q.QueryRowsPartial(ctx, &authors,
				"SELECT id, sender_user_id FROM channel_messages WHERE channel_id = ? AND id IN ("+inList(len(consumable))+")",
				append([]any{channelId}, consumable...)...); err != nil {
				return nil, err
			}
			s.authors = make(map[int32]int64, len(authors))
			for _, r := range authors {
				s.authors[r.Id] = r.SenderUserId
			}
		}
	}
	var rows []struct {
		UserId    int64 `db:"user_id"`
		MessageId int32 `db:"message_id"`
		Read      bool  `db:"read"`
	}
	if err := q.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, message_id, `read` FROM channel_message_mentions WHERE channel_id = ?"+byUser+" AND message_id IN ("+inList(len(ids))+")",
		append(append([]any{channelId}, userArgs...), ids...)...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		s.mentions[key{r.UserId, r.MessageId}] = !r.Read
	}
	return s, nil
}

// View sets the unread contents of m, already as viewer sees it
// (channel.ViewSentMessage: out set), for viewer: own when viewer wrote it
// (sender_user_id, also a post or a message written as a channel), member
// when viewer is in the channel. It is mentioned where it mentions viewer
// or replies to it, as the push marks it (sync.pushChannelUpdates);
// media_unread while that mention is unread, and for a voice or round video
// message viewer has not played. Viewer's own messages are never unread for
// it, nor anything for one not in the channel, which cannot read them
// (channels.readMessageContents: CHANNEL_PRIVATE), nor a voice or round
// video message of the history the channel was imported with. A nil s has
// nothing played and no mentions.
func (s *State) View(m *mtproto.Message, viewer int64, own, member bool) {
	if m == nil || m.GetPredicateName() != mtproto.Predicate_message {
		return
	}
	m.MediaUnread = false
	if viewer == 0 || m.GetOut() || own {
		return
	}
	k := key{viewer, m.GetId()}
	unreadMention, mentioned := false, false
	if s != nil {
		unreadMention, mentioned = s.mentions[k]
	}
	if mentioned {
		m.Mentioned = true
	}
	if !member {
		return
	}
	m.MediaUnread = unreadMention || Consumable(m) && (s == nil || !s.read[k] && m.GetId() > s.imported)
}

// Author is who wrote the voice or round video message id (sender_user_id),
// as LoadAll read it; false when it was not read.
func (s *State) Author(id int32) (int64, bool) {
	if s == nil {
		return 0, false
	}
	a, ok := s.authors[id]
	return a, ok
}

func inList(n int) string { return "?" + strings.Repeat(", ?", n-1) }
