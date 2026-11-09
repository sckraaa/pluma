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
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Forums and topics of supergroups (task 060, core.telegram.org/api/forum).
// A topic is the id of its messageActionTopicCreate; the General topic, id
// 1, is every message of the forum that is in no other topic. A topic's
// row, like the channel's, changes in the box transaction of its author
// with the channel locked.

const (
	// GeneralTopicTitle is the title of the General topic.
	GeneralTopicTitle = "General"
	// TopicTitleMax is the longest title of a topic, in characters
	// (core.telegram.org/method/messages.editForumTopic).
	TopicTitleMax = 128
	// TopicsPinnedLimit is topics_pinned_limit (core.telegram.org/api/config).
	TopicsPinnedLimit = 5
	// TopicsPageMax is the most topics of one page.
	TopicsPageMax = 100
	// TopicDeleteBatch is the most messages one messages.deleteTopicHistory
	// deletes; the clients repeat it while offset is above 0
	// (tdesktop window_peer_menu.cpp:2193-2200, Android
	// TopicsController.java:733-741).
	TopicDeleteBatch = 100
	// MyTopicDeleteMax is the most messages, the root aside, its creator may
	// delete a topic with (tdesktop data_replies_list.cpp:30,1028-1047);
	// beyond it, as with another's messages in it, the right to delete
	// messages is needed.
	MyTopicDeleteMax = 10
)

// Errors of the topics the schema's list has no entry for
// (Android TopicsController.java:882-897).
var (
	ErrPinnedTooMuch         = status.Error(mtproto.ErrBadRequest, "PINNED_TOO_MUCH")
	ErrPinnedTopicNotChanged = status.Error(mtproto.ErrBadRequest, "PINNED_TOPIC_NOT_MODIFIED")
)

// topicColors are the icon colors of a topic
// (core.telegram.org/method/messages.createForumTopic).
var topicColors = []int32{0x6FB9F0, 0xFFD67E, 0xCB86DB, 0x8EEE98, 0xFF93B2, 0xFB6F5F}

type topicRow struct {
	TopicId       int32  `db:"topic_id"`
	CreatorUserId int64  `db:"creator_user_id"`
	Title         string `db:"title"`
	TitleMissing  bool   `db:"title_missing"`
	IconColor     int32  `db:"icon_color"`
	IconEmojiId   int64  `db:"icon_emoji_id"`
	Date          int64  `db:"date"`
	Closed        bool   `db:"closed"`
	Hidden        bool   `db:"hidden"`
	PinnedOrder   int32  `db:"pinned_order"`
	TopMessage    int32  `db:"top_message"`
	Deleted       bool   `db:"deleted"`
}

const topicColumns = "topic_id, creator_user_id, title, title_missing, icon_color, icon_emoji_id, date, closed, hidden, pinned_order, top_message, deleted"

// EnsureGeneralTopic adds the General topic of a channel that is, or is
// becoming, a forum: from its creator, with what it had so far as its
// messages. A topic it has from before stays.
func EnsureGeneralTopic(b *userbox.Box, channelId int64) error {
	_, err := b.Tx().Exec("INSERT IGNORE INTO channel_forum_topics (channel_id, topic_id, creator_user_id, title, icon_color, `date`, top_message)"+
		" SELECT id, ?, creator_user_id, ?, ?, `date`, top_message FROM channels WHERE id = ?",
		channelbox.GeneralTopicId, GeneralTopicTitle, topicColors[0], channelId)
	return err
}

// forumActor opens the channel, checks that it is a forum and reads b's
// owner of it. CHANNEL_FORUM_MISSING for a supergroup that is no forum.
func forumActor(b *userbox.Box, channelId int64) (*channelbox.Channel, *actor, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, nil, err
	}
	var forum []bool
	if err = b.Tx().QueryRowsPartial(&forum, "SELECT forum FROM channels WHERE id = ?", channelId); err != nil {
		return nil, nil, err
	}
	if len(forum) == 0 || !forum[0] {
		return nil, nil, mtproto.ErrChannelForumMissing
	}
	return ch, a, nil
}

// forumTopic reads a topic of the channel, which b holds locked:
// TOPIC_ID_INVALID for one that never was, TOPIC_DELETED for one deleted.
func forumTopic(b *userbox.Box, channelId int64, topicId int32) (*topicRow, error) {
	var rows []topicRow
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT "+topicColumns+" FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", channelId, topicId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrTopicIdInvalid
	}
	if rows[0].Deleted {
		return nil, mtproto.ErrTopicDeleted
	}
	return &rows[0], nil
}

// manages tells whether a may manage all the topics: the creator, an admin
// with manage_topics (Android ChatObject.canManageTopics,
// tdesktop ForumTopic::canTogglePinned).
func (a *actor) manages() bool {
	return a.p.Role == channelperm.RoleCreator ||
		a.p.Role == channelperm.RoleAdmin && a.can(channelperm.ManageTopics, 0) == nil
}

// topicReplyHeader is reply_to of a service message of a topic: the topic
// itself, none in General (tdesktop history_item.cpp:2648-2659).
func topicReplyHeader(topicId int32) *mtproto.MessageReplyHeader {
	if topicId == channelbox.GeneralTopicId {
		return nil
	}
	return mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ForumTopic:             true,
		ReplyToMsgId:           topicId,
		ReplyToMsgId_INT32:     topicId,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(topicId),
	}).To_MessageReplyHeader()
}

// addTopicService writes a service message of b's owner to a topic of the
// forum, with the next id and pts of the channel; with topicId 0 the
// message makes a new topic, whose id it is.
func addTopicService(b *userbox.Box, ch *channelbox.Channel, channelId int64, topicId int32, action *mtproto.MessageAction, randomId int64) (*mtproto.Message, int32, error) {
	id := ch.NextMessageId()
	create := topicId == 0
	if create {
		topicId = id
	}
	m := mtproto.MakeTLMessageService(&mtproto.Message{
		Id:     id,
		PeerId: mtproto.MakePeerChannel(channelId),
		FromId: mtproto.MakePeerUser(b.UserId()),
		Date:   int32(b.Date()),
		Action: action,
	}).To_Message()
	if !create {
		m.ReplyTo = topicReplyHeader(topicId)
	}
	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, 0, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, reply_top_id, topic_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)",
		channelId, id, b.UserId(), randomId, ch.Pts(), mtproto.GetMediaType(m), data, channelbox.TopicColumn(topicId), b.Date()); err != nil {
		return nil, 0, err
	}
	if err = channelbox.ForumMoved(b, channelId, topicId, id); err != nil {
		return nil, 0, err
	}
	InvalidateDialog(b, channelId)
	return m, ch.Pts(), nil
}

// topicTitle checks a title: TOPIC_TITLE_EMPTY for a blank one; a longer
// one than TopicTitleMax is cut, as a channel's title is.
func topicTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", mtproto.ErrTopicTitleEmpty
	}
	if utf8.RuneCountInString(title) > TopicTitleMax {
		title = string([]rune(title)[:TopicTitleMax])
	}
	return title, nil
}

// ToggleForum turns the forum of a supergroup on or off by its creator
// (core.telegram.org/api/forum: "Only owners"), with the tabbed layout or
// the list. Errors as Telegram's (core.telegram.org/method/
// channels.toggleForum): CHAT_ADMIN_REQUIRED, CHANNEL_INVALID for a
// broadcast channel, CHAT_DISCUSSION_UNALLOWED for a discussion group,
// CHAT_NOT_MODIFIED for what it is already. Messages keep their topics
// when the forum goes off and find them again when it comes back.
func (d *Dao) ToggleForum(b *userbox.Box, channelId int64, enabled, tabs, sidebar bool) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if a.p.Role != channelperm.RoleCreator {
		return nil, mtproto.ErrChatAdminRequired
	}
	var rows []struct {
		Megagroup bool  `db:"megagroup"`
		Linked    int64 `db:"linked_chat_id"`
		Forum     bool  `db:"forum"`
		Tabs      bool  `db:"forum_tabs"`
		Sidebar   bool  `db:"forum_sidebar"`
	}
	if err = b.Tx().QueryRowsPartial(&rows, "SELECT megagroup, linked_chat_id, forum, forum_tabs, forum_sidebar FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || !rows[0].Megagroup {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	switch {
	case enabled && r.Linked != 0:
		return nil, mtproto.ErrChatDiscussionUnallowed
	case !enabled:
		tabs, sidebar = false, false
	}
	if r.Forum == enabled && r.Tabs == tabs && r.Sidebar == sidebar {
		return nil, mtproto.ErrChatNotModified
	}
	if enabled {
		if err = EnsureGeneralTopic(b, channelId); err != nil {
			return nil, err
		}
	} else if r.Forum {
		if err = keepTopicReads(b, channelId); err != nil {
			return nil, err
		}
	}
	if _, err = b.Tx().Exec("UPDATE channels SET forum = ?, forum_tabs = ?, forum_sidebar = ?, version = version + 1 WHERE id = ?", enabled, tabs, sidebar, channelId); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// keepTopicReads moves what the members read in topics of the forum, about
// to be turned off, into the read mark of their dialogs: without topics only
// that mark counts. It goes as far as everything before it is read, so no
// unread message becomes read; a message read in a topic past the first
// unread one of another topic counts as unread again. The channel is
// locked: dialogs are changed under it, as a send changes the sender's.
// What Telegram does here is not documented (UNVERIFIED).
func keepTopicReads(b *userbox.Box, channelId int64) error {
	var members []int64
	if err := b.Tx().QueryRowsPartial(&members,
		"SELECT DISTINCT user_id FROM channel_thread_reads WHERE channel_id = ?", channelId); err != nil || len(members) == 0 {
		return err
	}
	if _, err := b.Tx().Exec("UPDATE dialogs g SET g.read_inbox_max_id = GREATEST(g.read_inbox_max_id, COALESCE(("+
		"SELECT MIN(m.id) - 1 FROM channel_messages m LEFT JOIN channel_thread_reads t ON t.user_id = g.user_id AND t.channel_id = m.channel_id AND t.top_id = IF(m.topic_id = 0, 1, m.topic_id)"+
		" WHERE m.channel_id = ? AND m.deleted = 0 AND m.sender_user_id <> g.user_id AND m.id > GREATEST(g.read_inbox_max_id, COALESCE(t.read_max_id, 0))"+
		"), (SELECT top_message FROM channels WHERE id = ?)))"+
		" WHERE g.peer_type = ? AND g.peer_id = ? AND g.deleted = 0 AND g.user_id IN (SELECT user_id FROM channel_thread_reads WHERE channel_id = ?)",
		channelId, channelId, mtproto.PEER_CHANNEL, channelId, channelId); err != nil {
		return err
	}
	for _, id := range members {
		b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(id, mtproto.PEER_CHANNEL, channelId))
	}
	return nil
}

// ToggleViewForumAsMessages sets whether b's owner reads the forum as one
// feed of messages, per account (core.telegram.org/api/forum); its devices
// get updateChannelViewForumAsMessages with seq.
// CHANNEL_FORUM_MISSING for a supergroup that is no forum.
func (d *Dao) ToggleViewForumAsMessages(b *userbox.Box, channelId int64, enabled bool) (*mtproto.Update, error) {
	if err := readerIn(b, channelId); err != nil {
		return nil, err
	}
	var forum []bool
	if err := b.Tx().QueryRowsPartial(&forum, "SELECT forum FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	if len(forum) == 0 || !forum[0] {
		return nil, mtproto.ErrChannelForumMissing
	}
	if _, err := b.Tx().Exec("UPDATE dialogs SET view_forum_as_messages = ? WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		enabled, b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	InvalidateDialog(b, channelId)
	u := mtproto.MakeTLUpdateChannelViewForumAsMessages(&mtproto.Update{ChannelId: channelId, Enabled: mtproto.ToBool(enabled)}).To_Update()
	return u, d.EmitSeqUpdate(b, u, nil, nil)
}

// TopicChange is what a change of a topic wrote: its service message, and
// the updates for the members beside it.
type TopicChange struct {
	ChannelChange
	RandomId int64
	// Repeat: the answer to a repeated request, already pushed.
	Repeat bool
}

// CreateForumTopic creates a topic by b's owner, who may manage topics
// (Android ChatObject.canCreateTopic: the group's default rights count for
// a member). Its id is that of its messageActionTopicCreate, and a request
// repeated with its random_id gets the first answer.
func (d *Dao) CreateForumTopic(b *userbox.Box, channelId int64, title string, color int32, emojiId int64, titleMissing bool, randomId int64) (*TopicChange, error) {
	ch, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = a.can(channelperm.ManageTopics, b.Date()); err != nil {
		return nil, err
	}
	if randomId != 0 {
		var sent []struct {
			Id          int32  `db:"id"`
			Pts         int32  `db:"pts"`
			MessageData string `db:"message_data"`
		}
		if err = b.Tx().QueryRowsPartial(&sent, "SELECT id, pts, message_data FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND out_random_id = ?",
			channelId, b.UserId(), randomId); err != nil {
			return nil, err
		}
		if len(sent) > 0 {
			var m mtproto.Message
			if err = jsonx.UnmarshalFromString(sent[0].MessageData, &m); err != nil {
				return nil, err
			}
			return &TopicChange{ChannelChange: ChannelChange{ChannelId: channelId, Message: &m, Pts: sent[0].Pts}, RandomId: randomId, Repeat: true}, nil
		}
	}
	if title, err = topicTitle(title); err != nil {
		return nil, err
	}
	if color == 0 {
		color = topicColors[0]
	} else if !slices.Contains(topicColors, color) {
		return nil, mtproto.ErrInputRequestInvalid
	}
	action := &mtproto.MessageAction{TitleMissing: titleMissing, Title_STRING: title, IconColor: color}
	if emojiId != 0 {
		action.IconEmojiId = wrapperspb.Int64(emojiId)
	}
	m, pts, err := addTopicService(b, ch, channelId, 0, mtproto.MakeTLMessageActionTopicCreate(action).To_MessageAction(), randomId)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_forum_topics (channel_id, topic_id, creator_user_id, title, title_missing, icon_color, icon_emoji_id, `date`, top_message) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		channelId, m.Id, b.UserId(), title, titleMissing, color, emojiId, b.Date(), m.Id); err != nil {
		return nil, err
	}
	return &TopicChange{ChannelChange: ChannelChange{ChannelId: channelId, Message: m, Pts: pts}, RandomId: randomId}, nil
}

// TopicEdit is what messages.editForumTopic changes: the fields of
// channel.TopicField* that are set.
type TopicEdit struct {
	Fields      int32
	Title       string
	IconEmojiId int64
	Closed      bool
	Hidden      bool
}

// EditForumTopic changes a topic by b's owner: its creator or one who
// manages topics (tdesktop ForumTopic::canEdit, Android
// ChatObject.canManageTopic). closed and hidden go alone
// (TOPIC_CLOSE_SEPARATELY, core.telegram.org/method/messages.editForumTopic);
// hidden is of the General topic only, and hiding it closes it (Android
// TopicsController.java:832-842). TOPIC_NOT_MODIFIED for what it is already.
func (d *Dao) EditForumTopic(b *userbox.Box, channelId int64, topicId int32, e TopicEdit) (*TopicChange, error) {
	ch, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if e.Fields&channel.TopicFieldClosed != 0 && e.Fields != channel.TopicFieldClosed ||
		e.Fields&channel.TopicFieldHidden != 0 && e.Fields != channel.TopicFieldHidden {
		return nil, mtproto.ErrTopicCloseSeparately
	}
	t, err := forumTopic(b, channelId, topicId)
	if err != nil {
		return nil, err
	}
	my := t.CreatorUserId == b.UserId() && topicId != channelbox.GeneralTopicId
	switch {
	case e.Fields&channel.TopicFieldHidden != 0:
		if topicId != channelbox.GeneralTopicId {
			return nil, mtproto.ErrTopicIdInvalid
		}
		if !a.manages() {
			return nil, mtproto.ErrChatAdminRequired
		}
	case !my && !a.manages():
		return nil, mtproto.ErrChatAdminRequired
	}
	action := &mtproto.MessageAction{}
	changed := false
	if e.Fields&channel.TopicFieldTitle != 0 {
		if e.Title, err = topicTitle(e.Title); err != nil {
			return nil, err
		}
		if e.Title != t.Title {
			t.Title, changed = e.Title, true
			action.Title_FLAGSTRING = wrapperspb.String(e.Title)
		}
	}
	if e.Fields&channel.TopicFieldIcon != 0 && e.IconEmojiId != t.IconEmojiId {
		t.IconEmojiId, changed = e.IconEmojiId, true
		action.IconEmojiId = wrapperspb.Int64(e.IconEmojiId)
	}
	if e.Fields&channel.TopicFieldClosed != 0 && e.Closed != t.Closed {
		t.Closed, changed = e.Closed, true
		action.Closed = mtproto.ToBool(e.Closed)
	}
	if e.Fields&channel.TopicFieldHidden != 0 && e.Hidden != t.Hidden {
		t.Hidden, t.Closed, changed = e.Hidden, e.Hidden, true
		action.Hidden = mtproto.ToBool(e.Hidden)
	}
	if !changed {
		return nil, mtproto.ErrTopicNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channel_forum_topics SET title = ?, icon_emoji_id = ?, closed = ?, hidden = ? WHERE channel_id = ? AND topic_id = ?",
		t.Title, t.IconEmojiId, t.Closed, t.Hidden, channelId, topicId); err != nil {
		return nil, err
	}
	m, pts, err := addTopicService(b, ch, channelId, topicId, mtproto.MakeTLMessageActionTopicEdit(action).To_MessageAction(), 0)
	if err != nil {
		return nil, err
	}
	return &TopicChange{ChannelChange: ChannelChange{ChannelId: channelId, Message: m, Pts: pts}}, nil
}

// pinnedTopics are the pinned topics of the channel, first shown first.
func pinnedTopics(b *userbox.Box, channelId int64) ([]int32, error) {
	var ids []int32
	err := b.Tx().QueryRowsPartial(&ids, "SELECT topic_id FROM channel_forum_topics WHERE channel_id = ? AND deleted = 0 AND pinned_order > 0 ORDER BY pinned_order DESC", channelId)
	return ids, err
}

// writePinned makes order the pinned topics of the channel, first shown
// first, and the rest unpinned.
func writePinned(b *userbox.Box, channelId int64, order []int32) error {
	if _, err := b.Tx().Exec("UPDATE channel_forum_topics SET pinned_order = 0 WHERE channel_id = ? AND pinned_order > 0", channelId); err != nil {
		return err
	}
	for i, id := range order {
		if _, err := b.Tx().Exec("UPDATE channel_forum_topics SET pinned_order = ? WHERE channel_id = ? AND topic_id = ?", len(order)-i, channelId, id); err != nil {
			return err
		}
	}
	return nil
}

// UpdatePinnedForumTopic pins a topic, first in the list, or unpins it, by
// one who manages topics (tdesktop ForumTopic::canTogglePinned), and tells
// the members. PINNED_TOPIC_NOT_MODIFIED for what it is already and
// PINNED_TOO_MUCH beyond TopicsPinnedLimit, as Android expects
// (TopicsController.java:882-897).
func (d *Dao) UpdatePinnedForumTopic(b *userbox.Box, channelId int64, topicId int32, pinned bool) (*TopicChange, error) {
	_, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if !a.manages() {
		return nil, mtproto.ErrChatAdminRequired
	}
	t, err := forumTopic(b, channelId, topicId)
	if err != nil {
		return nil, err
	}
	if (t.PinnedOrder > 0) == pinned {
		return nil, ErrPinnedTopicNotChanged
	}
	order, err := pinnedTopics(b, channelId)
	if err != nil {
		return nil, err
	}
	order = slices.DeleteFunc(order, func(id int32) bool { return id == topicId })
	if pinned {
		if len(order) >= TopicsPinnedLimit {
			return nil, ErrPinnedTooMuch
		}
		order = slices.Insert(order, 0, topicId)
	}
	if err = writePinned(b, channelId, order); err != nil {
		return nil, err
	}
	u := mtproto.MakeTLUpdatePinnedForumTopic(&mtproto.Update{Pinned: pinned, Peer_PEER: mtproto.MakePeerChannel(channelId), TopicId_INT32: topicId}).To_Update()
	return &TopicChange{ChannelChange: ChannelChange{ChannelId: channelId, Updates: []*mtproto.Update{u}}}, nil
}

// reorderPinned is the list of pinned topics after a reorder of cur by
// order: with force the list is order; otherwise the topics of cur named in
// order take, in that order, the places of those named, and the others
// stay (core.telegram.org/method/messages.reorderPinnedForumTopics).
func reorderPinned(cur, order []int32, force bool) []int32 {
	seen := map[int32]bool{}
	named := make([]int32, 0, len(order))
	for _, id := range order {
		if !seen[id] && (force || slices.Contains(cur, id)) {
			seen[id] = true
			named = append(named, id)
		}
	}
	if force {
		return named
	}
	out := slices.Clone(cur)
	next := 0
	for i, id := range out {
		if seen[id] {
			out[i] = named[next]
			next++
		}
	}
	return out
}

// ReorderPinnedForumTopics orders the pinned topics by one who manages
// topics and tells the members. force names the whole list
// (core.telegram.org/method/messages.reorderPinnedForumTopics); its topics
// must all exist and fit TopicsPinnedLimit.
func (d *Dao) ReorderPinnedForumTopics(b *userbox.Box, channelId int64, order []int32, force bool) (*TopicChange, error) {
	_, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if !a.manages() {
		return nil, mtproto.ErrChatAdminRequired
	}
	cur, err := pinnedTopics(b, channelId)
	if err != nil {
		return nil, err
	}
	next := reorderPinned(cur, order, force)
	if force {
		if len(next) > TopicsPinnedLimit {
			return nil, ErrPinnedTooMuch
		}
		for _, id := range next {
			if _, err = forumTopic(b, channelId, id); err != nil {
				return nil, err
			}
		}
	}
	if err = writePinned(b, channelId, next); err != nil {
		return nil, err
	}
	u := mtproto.MakeTLUpdatePinnedForumTopics(&mtproto.Update{Peer_PEER: mtproto.MakePeerChannel(channelId), Order_VECTORINT32: next}).To_Update()
	return &TopicChange{ChannelChange: ChannelChange{ChannelId: channelId, Updates: []*mtproto.Update{u}}}, nil
}

// DeleteTopicHistory deletes a portion of a topic, newest first, by one
// who may delete others' messages, or by its creator while it holds
// only that creator's messages, at most MyTopicDeleteMax of them
// (tdesktop ForumTopic::canDelete, RepliesList::canDeleteMyTopic). The
// root message goes with the last portion and the topic with it
// (core.telegram.org/api/forum: clients see the root among the deleted
// ids). General is not deleted (TOPIC_ID_INVALID). more tells the client to
// repeat.
func (d *Dao) DeleteTopicHistory(b *userbox.Box, channelId int64, topicId int32) (del *Deleted, more bool, err error) {
	ch, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, false, err
	}
	if topicId == channelbox.GeneralTopicId {
		return nil, false, mtproto.ErrTopicIdInvalid
	}
	t, err := forumTopic(b, channelId, topicId)
	if err != nil {
		return nil, false, err
	}
	if a.can(channelperm.DeleteOthers, b.Date()) != nil {
		var counts []struct {
			All    int32 `db:"n"`
			Others int32 `db:"others"`
		}
		if err = b.Tx().QueryRowsPartial(&counts,
			"SELECT COUNT(*) AS n, COALESCE(SUM(sender_user_id <> ?), 0) AS others FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND deleted = 0",
			b.UserId(), channelId, topicId); err != nil {
			return nil, false, err
		}
		if t.CreatorUserId != b.UserId() || counts[0].Others != 0 || counts[0].All-1 > MyTopicDeleteMax {
			return nil, false, mtproto.ErrMessageDeleteForbidden
		}
	}
	var ids []int32
	if err = b.Tx().QueryRowsPartial(&ids,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND deleted = 0 ORDER BY id DESC LIMIT ? FOR UPDATE",
		channelId, topicId, TopicDeleteBatch+1); err != nil {
		return nil, false, err
	}
	more = len(ids) > TopicDeleteBatch
	if more {
		ids = ids[:TopicDeleteBatch]
	}
	del = &Deleted{ChannelId: channelId}
	if len(ids) > 0 {
		slices.Sort(ids)
		del.Ids = ids
		if err = removeMessages(b, ch, del); err != nil {
			return nil, false, err
		}
	}
	if more {
		return del, true, refreshTopicTops(b, channelId, []int32{topicId})
	}
	if _, err = b.Tx().Exec("UPDATE channel_forum_topics SET deleted = 1, pinned_order = 0 WHERE channel_id = ? AND topic_id = ?", channelId, topicId); err != nil {
		return nil, false, err
	}
	if _, err = b.Tx().Exec("DELETE FROM user_topic_notify_settings WHERE channel_id = ? AND top_msg_id = ?", channelId, topicId); err != nil {
		return nil, false, err
	}
	_, err = b.Tx().Exec("DELETE FROM channel_thread_reads WHERE channel_id = ? AND top_id = ?", channelId, topicId)
	return del, false, err
}

// topicsOf returns those of ids that are topics of the channel other than
// General. In a forum that was turned off they are ordinary messages.
func topicsOf(b *userbox.Box, channelId int64, ids []int32) ([]int32, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	var out []int32
	err := b.Tx().QueryRowsPartial(&out, "SELECT t.topic_id FROM channel_forum_topics t JOIN channels c ON c.id = t.channel_id AND c.forum = 1 WHERE t.channel_id = ? AND t.topic_id > 1 AND t.topic_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")", args...)
	return out, err
}

// refreshTopicTops moves the top_message of the topics that lost their
// newest messages.
func refreshTopicTops(b *userbox.Box, channelId int64, topics []int32) error {
	for _, c := range slices.Compact(slices.Sorted(slices.Values(topics))) {
		var top []int32
		if err := b.Tx().QueryRowsPartial(&top, "SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND deleted = 0", channelId, c); err != nil {
			return err
		}
		id := c
		if id == 0 {
			id = channelbox.GeneralTopicId
		}
		if _, err := b.Tx().Exec("UPDATE channel_forum_topics SET top_message = ? WHERE channel_id = ? AND topic_id = ?", first(top), channelId, id); err != nil {
			return err
		}
	}
	return nil
}

// ReadTopic moves how far b's owner read a topic forward to maxId (0 or
// above its last message: up to its last message), from any point not past
// what it read before: messages.readDiscussion in a forum (Android
// MessagesController.java:14501-14508, tdesktop data_replies_list.cpp:1008).
// Its mentions in the topic up to it are read. Its devices get
// updateReadChannelDiscussionInbox with seq, and the authors of what it
// read updateReadChannelDiscussionOutbox. Reading the whole channel
// (channels.readHistory) also counts for its topics, see topicReadFloor.
func (d *Dao) ReadTopic(b *userbox.Box, channelId int64, topicId, maxId int32) error {
	if err := readerIn(b, channelId); err != nil {
		return err
	}
	column := channelbox.TopicColumn(topicId)
	var tops []int32
	if err := b.Tx().QueryRowsPartial(&tops, "SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND deleted = 0", channelId, column); err != nil {
		return err
	}
	top := first(tops)
	if maxId <= 0 || maxId > top {
		maxId = top
	}
	floor, err := topicReadFloorTx(b, channelId, topicId)
	if err != nil {
		return err
	}
	if maxId <= floor {
		return nil
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_thread_reads (user_id, channel_id, top_id, read_max_id) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE read_max_id = GREATEST(read_max_id, VALUES(read_max_id))",
		b.UserId(), channelId, topicId, maxId); err != nil {
		return err
	}
	if _, err = b.Tx().Exec("UPDATE channel_message_mentions mm JOIN channel_messages m ON m.channel_id = mm.channel_id AND m.id = mm.message_id"+
		" SET mm.read = 1 WHERE mm.channel_id = ? AND mm.user_id = ? AND mm.message_id <= ? AND mm.read = 0 AND m.topic_id = ?",
		channelId, b.UserId(), maxId, column); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	if err = d.EmitSeqUpdate(b, mtproto.MakeTLUpdateReadChannelDiscussionInbox(&mtproto.Update{
		ChannelId: channelId, TopMsgId_INT32: topicId, ReadMaxId: maxId,
	}).To_Update(), nil, nil); err != nil {
		return err
	}
	var authors []int64
	if err = b.Tx().QueryRowsPartial(&authors,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND id > ? AND id <= ? AND sender_user_id <> ? AND deleted = 0"+
			" GROUP BY sender_user_id ORDER BY MAX(id) DESC LIMIT ?",
		channelId, column, floor, maxId, b.UserId(), readAuthorsMax); err != nil {
		return err
	}
	outbox := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateReadChannelDiscussionOutbox(&mtproto.Update{
		ChannelId: channelId, TopMsgId_INT32: topicId, ReadMaxId: maxId,
	}).To_Update())
	for _, author := range authors {
		in := &sync.TLSyncPushUpdates{UserId: author, Updates: outbox}
		data, err := jsonx.Marshal(in)
		if err != nil {
			return err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: author, Method: string(proto.MessageName(in)), Data: data})
	}
	return nil
}

// topicReadFloorTx is how far b's owner read the topic: the further of what
// it read of the topic and of the whole channel (channels.readHistory, the
// mark a join leaves, the feed of messages).
func topicReadFloorTx(b *userbox.Box, channelId int64, topicId int32) (int32, error) {
	var marks []int32
	err := b.Tx().QueryRowsPartial(&marks,
		"SELECT GREATEST(g.read_inbox_max_id, COALESCE(r.read_max_id, 0)) FROM dialogs g"+
			" LEFT JOIN channel_thread_reads r ON r.user_id = g.user_id AND r.channel_id = g.peer_id AND r.top_id = ?"+
			" WHERE g.user_id = ? AND g.peer_type = ? AND g.peer_id = ?",
		topicId, b.UserId(), mtproto.PEER_CHANNEL, channelId)
	return first(marks), err
}

// ReadTopicMentions reads every unread mention of b's owner in a topic; its
// devices get updateChannelReadMessagesContents with seq, as ReadMentions.
func (d *Dao) ReadTopicMentions(b *userbox.Box, channelId int64, topicId int32) error {
	if err := readerIn(b, channelId); err != nil {
		return err
	}
	column := channelbox.TopicColumn(topicId)
	var ids []int32
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT mm.message_id FROM channel_message_mentions mm JOIN channel_messages m ON m.channel_id = mm.channel_id AND m.id = mm.message_id"+
			" WHERE mm.channel_id = ? AND mm.user_id = ? AND mm.read = 0 AND m.topic_id = ? ORDER BY mm.message_id FOR UPDATE OF mm",
		channelId, b.UserId(), column); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{channelId, b.UserId()}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := b.Tx().Exec("UPDATE channel_message_mentions SET `read` = 1 WHERE channel_id = ? AND user_id = ? AND message_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")", args...); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateChannelReadMessagesContents(&mtproto.Update{ChannelId: channelId, Messages: ids}).To_Update(), nil, nil)
}

// UnpinAllTopic unpins every pinned message of a topic: one
// updatePinnedChannelMessages with them all, nil when none is pinned; the
// check is that of a pin.
func (d *Dao) UnpinAllTopic(b *userbox.Box, channelId int64, topicId int32) (*mtproto.Update, int32, error) {
	ch, a, err := forumActor(b, channelId)
	if err != nil {
		return nil, 0, err
	}
	if err = a.can(channelperm.PinMessages, b.Date()); err != nil {
		return nil, 0, err
	}
	var rows []struct {
		Id          int32  `db:"id"`
		MessageData string `db:"message_data"`
	}
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT id, message_data FROM channel_messages WHERE channel_id = ? AND topic_id = ? AND pinned = 1 AND deleted = 0 ORDER BY id FOR UPDATE",
		channelId, channelbox.TopicColumn(topicId)); err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, nil
	}
	ids := make([]int32, 0, len(rows))
	for _, r := range rows {
		var m *mtproto.Message
		if err = jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, 0, err
		}
		m.Pinned = false
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, 0, err
		}
		if _, err = b.Tx().Exec("UPDATE channel_messages SET pinned = 0, message_data = ? WHERE channel_id = ? AND id = ?", data, channelId, r.Id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, r.Id)
	}
	u := mtproto.MakeTLUpdatePinnedChannelMessages(&mtproto.Update{ChannelId: channelId, Messages: ids}).To_Update()
	ch.AddPtsUpdate(u, 1)
	return u, ch.Pts(), nil
}

// topicCounts is what a page of topics needs of selfId and the messages.
type topicCounts struct {
	floor, unread, mentions, top, outbox, reactions, pollVotes map[int32]int32
	notify                                                     map[int32]*mtproto.PeerNotifySettings
}

func topicIn(ids []int32) (string, []any) {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return "(?" + strings.Repeat(", ?", len(ids)-1) + ")", args
}

// countTopics reads, for the topics rows of the channel, how far selfId read
// each, what it has not read and the newest message.
func (d *Dao) countTopics(ctx context.Context, selfId, channelId int64, availableMinId int32, rows []topicRow) (*topicCounts, error) {
	out := &topicCounts{reactions: map[int32]int32{}, pollVotes: map[int32]int32{}, notify: map[int32]*mtproto.PeerNotifySettings{}, floor: map[int32]int32{}, unread: map[int32]int32{}, mentions: map[int32]int32{}, top: map[int32]int32{}, outbox: map[int32]int32{}}
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int32, 0, len(rows))
	columns := make([]int32, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.TopicId)
		columns = append(columns, channelbox.TopicColumn(r.TopicId))
	}
	columnsIn, columnArgs := topicIn(columns)
	idsIn, idArgs := topicIn(ids)
	type count struct {
		Topic int32 `db:"topic"`
		N     int32 `db:"n"`
	}
	asTopic := func(c int32) int32 {
		if c == 0 {
			return channelbox.GeneralTopicId
		}
		return c
	}
	var dialogRead []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &dialogRead, "SELECT read_inbox_max_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0",
		selfId, mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	// One with no dialog (a public forum it is not in) has nothing unread.
	reader := len(dialogRead) > 0
	base := first(dialogRead)
	for _, r := range rows {
		out.floor[r.TopicId] = base
	}
	var reads []count
	if err := d.Mysql.QueryRowsPartial(ctx, &reads,
		"SELECT top_id AS topic, read_max_id AS n FROM channel_thread_reads WHERE user_id = ? AND channel_id = ? AND top_id IN "+idsIn,
		append([]any{selfId, channelId}, idArgs...)...); err != nil {
		return nil, err
	}
	for _, r := range reads {
		out.floor[r.Topic] = max(out.floor[r.Topic], r.N)
	}
	var tops []count
	if err := d.Mysql.QueryRowsPartial(ctx, &tops,
		"SELECT topic_id AS topic, MAX(id) AS n FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id >= ? AND topic_id IN "+columnsIn+" GROUP BY topic_id",
		append([]any{channelId, availableMinId}, columnArgs...)...); err != nil {
		return nil, err
	}
	for _, c := range tops {
		out.top[asTopic(c.Topic)] = c.N
	}
	var unread []count
	if reader {
		if err := d.Mysql.QueryRowsPartial(ctx, &unread,
			"SELECT m.topic_id AS topic, COUNT(*) AS n FROM channel_messages m"+
				" LEFT JOIN channel_thread_reads r ON r.user_id = ? AND r.channel_id = m.channel_id AND r.top_id = IF(m.topic_id = 0, 1, m.topic_id)"+
				" WHERE m.channel_id = ? AND m.deleted = 0 AND m.sender_user_id <> ? AND m.id >= ? AND m.id > GREATEST(?, COALESCE(r.read_max_id, 0))"+
				" AND m.topic_id IN "+columnsIn+" GROUP BY m.topic_id",
			append([]any{selfId, channelId, selfId, availableMinId, base}, columnArgs...)...); err != nil {
			return nil, err
		}
	} else {
		for _, r := range rows {
			out.floor[r.TopicId] = out.top[r.TopicId]
		}
	}
	for _, c := range unread {
		out.unread[asTopic(c.Topic)] = c.N
	}
	var mentions []count
	if err := d.Mysql.QueryRowsPartial(ctx, &mentions,
		"SELECT m.topic_id AS topic, COUNT(*) AS n FROM channel_message_mentions mm"+
			" JOIN channel_messages m ON m.channel_id = mm.channel_id AND m.id = mm.message_id AND m.deleted = 0"+
			" WHERE mm.channel_id = ? AND mm.user_id = ? AND mm.read = 0 AND mm.message_id >= ? AND m.topic_id IN "+columnsIn+" GROUP BY m.topic_id",
		append([]any{channelId, selfId, availableMinId}, columnArgs...)...); err != nil {
		return nil, err
	}
	for _, c := range mentions {
		out.mentions[asTopic(c.Topic)] = c.N
	}
	var reactionCounts []count
	if err := d.Mysql.QueryRowsPartial(ctx, &reactionCounts,
		"SELECT m.topic_id AS topic, COUNT(DISTINCT r.message_id) AS n FROM channel_message_reactions r"+
			" JOIN channel_messages m ON m.channel_id = r.channel_id AND m.id = r.message_id AND m.deleted = 0"+
			" WHERE r.channel_id = ? AND r.unread = 1 AND m.sender_user_id = ? AND r.message_id >= ? AND m.topic_id IN "+columnsIn+" GROUP BY m.topic_id",
		append([]any{channelId, selfId, availableMinId}, columnArgs...)...); err != nil {
		return nil, err
	}
	for _, c := range reactionCounts {
		out.reactions[asTopic(c.Topic)] = c.N
	}
	var voteCounts []count
	if err := d.Mysql.QueryRowsPartial(ctx, &voteCounts,
		"SELECT m.topic_id AS topic, COUNT(*) AS n FROM polls p"+
			" JOIN channel_messages m ON m.channel_id = p.peer_id AND m.id = p.message_id AND m.deleted = 0"+
			" WHERE p.creator_user_id = ? AND p.has_unread = 1 AND p.peer_type = ? AND p.peer_id = ? AND m.topic_id IN "+columnsIn+" GROUP BY m.topic_id",
		append([]any{selfId, mtproto.PEER_CHANNEL, channelId}, columnArgs...)...); err != nil {
		return nil, err
	}
	for _, c := range voteCounts {
		out.pollVotes[asTopic(c.Topic)] = c.N
	}
	var settings []userpb.TopicNotifyRow
	if err := d.Mysql.QueryRowsPartial(ctx, &settings,
		"SELECT "+userpb.TopicNotifyColumns+" FROM user_topic_notify_settings WHERE user_id = ? AND channel_id = ? AND top_msg_id IN "+idsIn,
		append([]any{selfId, channelId}, idArgs...)...); err != nil {
		return nil, err
	}
	for i := range settings {
		out.notify[settings[i].TopMsgId] = settings[i].Settings()
	}
	var others []count
	if err := d.Mysql.QueryRowsPartial(ctx, &others,
		"SELECT top_id AS topic, MAX(read_max_id) AS n FROM channel_thread_reads WHERE channel_id = ? AND user_id <> ? AND top_id IN "+idsIn+" GROUP BY top_id",
		append([]any{channelId, selfId}, idArgs...)...); err != nil {
		return nil, err
	}
	var dialogsOut []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &dialogsOut,
		"SELECT COALESCE(MAX(read_inbox_max_id), 0) FROM dialogs WHERE peer_type = ? AND peer_id = ? AND user_id <> ? AND deleted = 0",
		mtproto.PEER_CHANNEL, channelId, selfId); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out.outbox[r.TopicId] = first(dialogsOut)
	}
	for _, c := range others {
		out.outbox[c.Topic] = max(out.outbox[c.Topic], c.N)
	}
	return out, nil
}

// viewTopic is the forumTopic of r as selfId sees it. Without settings of
// its own for the topic they are the empty ones: the forum's apply.
func viewTopic(channelId, selfId int64, r *topicRow, c *topicCounts) *mtproto.ForumTopic {
	t := &mtproto.ForumTopic{
		Id:              r.TopicId,
		My:              r.CreatorUserId == selfId && r.TopicId != channelbox.GeneralTopicId,
		Closed:          r.Closed,
		Pinned:          r.PinnedOrder > 0,
		Hidden:          r.Hidden,
		TitleMissing:    r.TitleMissing,
		Date:            int32(r.Date),
		Peer:            mtproto.MakePeerChannel(channelId),
		Title:           r.Title,
		IconColor:       r.IconColor,
		TopMessage:      c.top[r.TopicId],
		ReadInboxMaxId:  c.floor[r.TopicId],
		ReadOutboxMaxId: c.outbox[r.TopicId],
		UnreadCount:     c.unread[r.TopicId],
		FromId:          mtproto.MakePeerUser(r.CreatorUserId),
		NotifySettings:  mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{}).To_PeerNotifySettings(),
	}
	t.UnreadMentionsCount = c.mentions[r.TopicId]
	t.UnreadReactionsCount = c.reactions[r.TopicId]
	t.UnreadPollVotesCount = c.pollVotes[r.TopicId]
	if s := c.notify[r.TopicId]; s != nil {
		t.NotifySettings = s
	}
	if r.IconEmojiId != 0 {
		t.IconEmojiId = wrapperspb.Int64(r.IconEmojiId)
	}
	return mtproto.MakeTLForumTopic(t).To_ForumTopic()
}

// forumViewer is viewerOf for a forum: CHANNEL_PRIVATE, CHANNEL_FORUM_MISSING.
func (d *Dao) forumViewer(ctx context.Context, selfId, channelId int64) (viewer, error) {
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return viewer{}, err
	}
	if !v.canRead {
		return viewer{}, mtproto.ErrChannelPrivate
	}
	mc, err := d.GetMutableChannel(ctx, channelId, nil)
	if err != nil {
		return viewer{}, err
	}
	if !mc.GetChannel().GetForum() {
		return viewer{}, mtproto.ErrChannelForumMissing
	}
	return v, nil
}

// topicsResult is messages.forumTopics of rows, with the root and the
// newest message of each, which Android takes from it
// (TopicsController.java:278-297); chats and users are the caller's.
func (d *Dao) topicsResult(ctx context.Context, selfId, channelId int64, v viewer, rows []topicRow, count int32) (*mtproto.Messages_ForumTopics, error) {
	counts, err := d.countTopics(ctx, selfId, channelId, v.availableMinId, rows)
	if err != nil {
		return nil, err
	}
	topics := make([]*mtproto.ForumTopic, 0, len(rows))
	ids := make([]int32, 0, 2*len(rows))
	for i := range rows {
		r := &rows[i]
		topics = append(topics, viewTopic(channelId, selfId, r, counts))
		if t := counts.top[r.TopicId]; t != 0 {
			ids = append(ids, t)
		}
		ids = append(ids, r.TopicId)
	}
	messages, err := d.Messages(ctx, selfId, channelId, slices.Compact(slices.Sorted(slices.Values(ids))))
	if err != nil {
		return nil, err
	}
	pts, err := d.ChannelPts(ctx, channelId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesForumTopics(&mtproto.Messages_ForumTopics{
		Count:    count,
		Topics:   topics,
		Messages: messages,
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
		Pts:      pts,
	}).To_Messages_ForumTopics(), nil
}

// ForumTopics is a page of the topics of the channel: on the first page all
// the pinned ones first, then by their newest message. The offsets are
// those of the last topic of the previous page
// (core.telegram.org/method/messages.getForumTopics); after a pinned one
// the page starts from the newest of the rest.
func (d *Dao) ForumTopics(ctx context.Context, selfId, channelId int64, q string, offsetId, offsetTopic, limit int32) (*mtproto.Messages_ForumTopics, error) {
	v, err := d.forumViewer(ctx, selfId, channelId)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > TopicsPageMax {
		limit = TopicsPageMax
	}
	where := "channel_id = ? AND deleted = 0"
	args := []any{channelId}
	if q != "" {
		where += " AND title LIKE ? ESCAPE '!'"
		args = append(args, "%"+likeEscaper.Replace(q)+"%")
	}
	var count []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*) FROM channel_forum_topics WHERE "+where, args...); err != nil {
		return nil, err
	}
	var rows []topicRow
	firstPage := offsetId == 0 && offsetTopic == 0
	if q == "" {
		if firstPage {
			if err = d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+topicColumns+" FROM channel_forum_topics WHERE "+where+" AND pinned_order > 0 ORDER BY pinned_order DESC", args...); err != nil {
				return nil, err
			}
		}
		where += " AND pinned_order = 0"
	}
	var pinned []int32
	if offsetTopic != 0 {
		if err = d.Mysql.QueryRowsPartial(ctx, &pinned, "SELECT pinned_order FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", channelId, offsetTopic); err != nil {
			return nil, err
		}
	}
	// A search has no pinned ones apart: its order holds them as the rest.
	if offsetId != 0 && (q != "" || first(pinned) == 0) {
		where += " AND (top_message < ? OR (top_message = ? AND topic_id < ?))"
		args = append(args, offsetId, offsetId, offsetTopic)
	}
	if rest := limit - int32(len(rows)); rest > 0 {
		var next []topicRow
		if err = d.Mysql.QueryRowsPartial(ctx, &next,
			"SELECT "+topicColumns+" FROM channel_forum_topics WHERE "+where+" ORDER BY top_message DESC, topic_id DESC LIMIT ?", append(args, rest)...); err != nil {
			return nil, err
		}
		rows = append(rows, next...)
	}
	return d.topicsResult(ctx, selfId, channelId, v, rows, first(count))
}

// ForumTopicsByID are the topics ids, forumTopicDeleted for the ones not
// there (core.telegram.org/api/forum).
func (d *Dao) ForumTopicsByID(ctx context.Context, selfId, channelId int64, ids []int32) (*mtproto.Messages_ForumTopics, error) {
	if len(ids) == 0 {
		return nil, mtproto.ErrTopicsEmpty
	}
	if len(ids) > TopicsPageMax {
		ids = ids[:TopicsPageMax]
	}
	v, err := d.forumViewer(ctx, selfId, channelId)
	if err != nil {
		return nil, err
	}
	in, args := topicIn(ids)
	var rows []topicRow
	if err = d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+topicColumns+" FROM channel_forum_topics WHERE channel_id = ? AND deleted = 0 AND topic_id IN "+in,
		append([]any{channelId}, args...)...); err != nil {
		return nil, err
	}
	r, err := d.topicsResult(ctx, selfId, channelId, v, rows, int32(len(ids)))
	if err != nil {
		return nil, err
	}
	byId := map[int32]*mtproto.ForumTopic{}
	for _, t := range r.Topics {
		byId[t.GetId()] = t
	}
	r.Topics = make([]*mtproto.ForumTopic, 0, len(ids))
	for _, id := range ids {
		if t, ok := byId[id]; ok {
			r.Topics = append(r.Topics, t)
		} else {
			r.Topics = append(r.Topics, mtproto.MakeTLForumTopicDeleted(&mtproto.ForumTopic{Id: id}).To_ForumTopic())
		}
	}
	return r, nil
}

// TopicsOfMessages are the topics the messages of a forum are in, as
// selfId sees them, for messages.channelMessages.topics (tdesktop
// api_messages_search.cpp:176, iOS Holes.swift:268).
func (d *Dao) TopicsOfMessages(ctx context.Context, selfId, channelId int64, messages []*mtproto.Message) ([]*mtproto.ForumTopic, error) {
	seen := map[int32]bool{}
	var ids []int32
	for _, m := range messages {
		id := int32(channelbox.GeneralTopicId)
		if h := m.GetReplyTo(); h.GetForumTopic() {
			id = h.GetReplyToTopId().GetValue()
			if id == 0 {
				id = h.GetReplyToMsgId()
			}
		}
		if m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionTopicCreate {
			id = m.GetId()
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []*mtproto.ForumTopic{}, nil
	}
	r, err := d.ForumTopicsByID(ctx, selfId, channelId, ids)
	if err != nil {
		return nil, err
	}
	return r.Topics, nil
}

// IsForum tells whether the channel is a forum now.
func (d *Dao) IsForum(ctx context.Context, channelId int64) (bool, error) {
	var forum []bool
	if err := d.Mysql.QueryRowsPartial(ctx, &forum, "SELECT forum FROM channels WHERE id = ?", channelId); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return len(forum) > 0 && forum[0], nil
}

// TopicPage is a page of the messages of a topic as selfId sees them,
// service messages included: its first is messageActionTopicCreate, which
// the clients do not add themselves (tdesktop data_replies_list.cpp:306-311:
// the root of a topic is not injected).
func (d *Dao) TopicPage(ctx context.Context, selfId, channelId int64, topicId, offsetId, offsetDate, addOffset, limit, maxId, minId int32) ([]*mtproto.Message, int32, error) {
	if err := d.topicExists(ctx, channelId, topicId); err != nil {
		return nil, 0, err
	}
	return d.page(ctx, selfId, channelId, pageQuery{where: " AND topic_id = ?", args: []any{channelbox.TopicColumn(topicId)}}, offsetId, offsetDate, addOffset, limit, maxId, minId)
}

func (d *Dao) topicExists(ctx context.Context, channelId int64, topicId int32) error {
	var deleted []bool
	if err := d.Mysql.QueryRowsPartial(ctx, &deleted, "SELECT deleted FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", channelId, topicId); err != nil {
		return err
	}
	if len(deleted) == 0 {
		return mtproto.ErrTopicIdInvalid
	}
	if deleted[0] {
		return mtproto.ErrTopicDeleted
	}
	return nil
}

// TopicThread is what messages.getDiscussionMessage answers for a topic:
// its newest message, how far selfId read it, what it has not read
// (tdesktop RepliesList::requestUnreadCount).
func (d *Dao) TopicThread(ctx context.Context, selfId, channelId int64, topicId int32) (root, maxId, readMax, unread int32, err error) {
	if err = d.topicExists(ctx, channelId, topicId); err != nil {
		return 0, 0, 0, 0, err
	}
	v, err := d.forumViewer(ctx, selfId, channelId)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	var rows []topicRow
	if err = d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+topicColumns+" FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", channelId, topicId); err != nil {
		return 0, 0, 0, 0, err
	}
	c, err := d.countTopics(ctx, selfId, channelId, v.availableMinId, rows)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return topicId, c.top[topicId], c.floor[topicId], c.unread[topicId], nil
}
