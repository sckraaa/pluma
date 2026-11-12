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
	"maps"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/ghost"
	"github.com/teamgram/teamgram-server/pkg/sendas"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The discussion group of a broadcast channel (task 048,
// core.telegram.org/api/discussion): a supergroup linked to it, both ways
// by linked_chat_id, where each post gets a copy whose thread of replies
// is the post's comments.

type linkRow struct {
	Id        int64 `db:"id"`
	Broadcast bool  `db:"broadcast"`
	Megagroup bool  `db:"megagroup"`
	Hidden    bool  `db:"hidden_prehistory"`
	Linked    int64 `db:"linked_chat_id"`
	Deleted   bool  `db:"deleted"`
	Forum     bool  `db:"forum"`
}

func linkRows(b *userbox.Box, ids ...int64) (map[int64]linkRow, error) {
	out := map[int64]linkRow{}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		var rows []linkRow
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT id, broadcast, megagroup, hidden_prehistory, linked_chat_id, deleted, forum FROM channels WHERE id = ?", id); err != nil {
			return nil, err
		}
		if len(rows) > 0 && !rows[0].Deleted {
			out[id] = rows[0]
		}
	}
	return out, nil
}

// lockAscending opens the channels ids in b's transaction in ascending
// order, as channelbox requires; 0 and repeats are left out.
func lockAscending(b *userbox.Box, ids ...int64) error {
	ids = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id int64) bool { return id == 0 })
	for _, id := range ids {
		if _, err := channelbox.Open(b, id); err != nil {
			return err
		}
	}
	return nil
}

// SetDiscussionGroup links the supergroup groupId to the broadcast channel
// channelId as its discussion group, or with groupId 0 unlinks the one it
// has, by b's owner, who may change the info of both (channelperm,
// ChangeInfo). A channel or a group linked elsewhere before is unlinked
// there. Errors as Telegram's (core.telegram.org/method/
// channels.setDiscussionGroup): BROADCAST_ID_INVALID for a channel that is
// not a broadcast one, MEGAGROUP_ID_INVALID for a group that is not a
// supergroup, MEGAGROUP_PREHISTORY_HIDDEN for one that hides its history
// from new members, LINK_NOT_MODIFIED for the link it has. It returns the
// channels whose members are to load them again.
func (d *Dao) SetDiscussionGroup(b *userbox.Box, channelId, groupId int64) ([]int64, error) {
	// The links as they are now, read to know what to lock; checked again
	// once locked.
	before, err := linkRows(b, channelId, groupId)
	if err != nil {
		return nil, err
	}
	locks := []int64{channelId, groupId, before[channelId].Linked, before[groupId].Linked}
	if err = lockAscending(b, locks...); err != nil {
		return nil, err
	}
	rows, err := linkRows(b, locks...)
	if err != nil {
		return nil, err
	}
	ch, ok := rows[channelId]
	if !ok || !ch.Broadcast {
		return nil, mtproto.ErrBroadcastIdInvalid
	}
	if ch.Linked != before[channelId].Linked || groupId != 0 && rows[groupId].Linked != before[groupId].Linked {
		// Changed between the read and the lock: a retry reads again.
		return nil, mtproto.ErrInternalServerError
	}
	if ch.Linked == groupId {
		return nil, mtproto.ErrLinkNotModified
	}
	for _, id := range []int64{channelId, groupId} {
		if id == 0 {
			continue
		}
		a, err := rightsActor(b, id)
		if err != nil {
			return nil, err
		}
		if err = a.can(channelperm.ChangeInfo, b.Date()); err != nil {
			return nil, err
		}
	}
	if groupId != 0 {
		g, ok := rows[groupId]
		switch {
		case !ok || !g.Megagroup, g.Forum:
			// A forum cannot be a discussion group
			// (CHAT_DISCUSSION_UNALLOWED, channels.toggleForum).
			return nil, mtproto.ErrMegagroupIdInvalid
		case g.Hidden:
			return nil, mtproto.ErrMegagroupPrehistoryHidden
		}
	}
	changed := []int64{channelId}
	unlink := func(id int64) error {
		if id == 0 {
			return nil
		}
		changed = append(changed, id)
		_, err := b.Tx().Exec("UPDATE channels SET linked_chat_id = 0 WHERE id = ?", id)
		return err
	}
	if err = unlink(ch.Linked); err != nil {
		return nil, err
	}
	if groupId != 0 {
		if err = unlink(rows[groupId].Linked); err != nil {
			return nil, err
		}
		changed = append(changed, groupId)
		if _, err = b.Tx().Exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", channelId, groupId); err != nil {
			return nil, err
		}
	}
	if _, err = b.Tx().Exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", groupId, channelId); err != nil {
		return nil, err
	}
	// Each channel whose link changed keeps it in its recent actions, those
	// unlinked on the way too: the clients show a change and a removal
	// from the new value (tdesktop history_admin_log_item.cpp
	// createChangeLinkedChat, Android MessageObject.java EventLogChangedLinked*).
	type linkChange struct{ channelId, prev, next int64 }
	links := []linkChange{{channelId, ch.Linked, groupId}, {ch.Linked, channelId, 0}}
	if groupId != 0 {
		links = append(links, linkChange{groupId, rows[groupId].Linked, channelId}, linkChange{rows[groupId].Linked, groupId, 0})
	}
	for _, l := range links {
		if l.channelId == 0 {
			continue
		}
		if err = adminlog.Write(b, l.channelId, mtproto.MakeTLChannelAdminLogEventActionChangeLinkedChat(&mtproto.ChannelAdminLogEventAction{
			PrevValue_INT64: l.prev, NewValue_INT64: l.next,
		}).To_ChannelAdminLogEventAction(), ""); err != nil {
			return nil, err
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(changed))), nil
}

// discussionLink is a post of a broadcast channel and its copy in the
// discussion group.
type discussionLink struct {
	ChannelId      int64 `db:"channel_id"`
	PostId         int32 `db:"post_id"`
	GroupId        int64 `db:"group_id"`
	GroupMessageId int32 `db:"group_message_id"`
}

// discussionLinks are the links of messages ids of channelId, as posts or
// as copies.
func discussionLinks(b *userbox.Box, channelId int64, ids []int32) ([]discussionLink, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	in := "(?" + strings.Repeat(", ?", len(ids)-1) + ")"
	const columns = "SELECT channel_id, post_id, group_id, group_message_id FROM channel_discussion_posts WHERE "
	var posts, copies []discussionLink
	if err := b.Tx().QueryRowsPartial(&posts, columns+"channel_id = ? AND post_id IN "+in, args...); err != nil {
		return nil, err
	}
	if err := b.Tx().QueryRowsPartial(&copies, columns+"group_id = ? AND group_message_id IN "+in, args...); err != nil {
		return nil, err
	}
	return append(posts, copies...), nil
}

// openWithLinked locks channelId and the other channel of each link in b's
// transaction in ascending order, and returns channelId's box.
func openWithLinked(b *userbox.Box, channelId int64, links []discussionLink) (*channelbox.Channel, error) {
	ids := []int64{channelId}
	for _, l := range links {
		ids = append(ids, l.ChannelId, l.GroupId)
	}
	slices.Sort(ids)
	var ch *channelbox.Channel
	for _, id := range slices.Compact(ids) {
		c, err := openChannel(b, id)
		if err != nil {
			return nil, err
		}
		if id == channelId {
			ch = c
		}
	}
	return ch, nil
}

// removeLinked deletes what goes with the messages out.Ids of channelId:
// the copies of deleted posts (the comments stay in the group, in their
// thread), and the comments of the posts of deleted copies. The channels
// of links are locked.
func removeLinked(b *userbox.Box, channelId int64, links []discussionLink, out *Deleted) error {
	copies := map[int64][]int32{}
	for _, l := range links {
		if _, err := b.Tx().Exec("DELETE FROM channel_discussion_posts WHERE channel_id = ? AND post_id = ?", l.ChannelId, l.PostId); err != nil {
			return err
		}
		if l.ChannelId == channelId {
			copies[l.GroupId] = append(copies[l.GroupId], l.GroupMessageId)
			continue
		}
		off, err := postCommentsOff(b, l.ChannelId, l.PostId)
		if err != nil {
			return err
		}
		if off != nil {
			out.Posts = append(out.Posts, *off)
		}
	}
	for _, groupId := range slices.Sorted(maps.Keys(copies)) {
		del, err := removeCopies(b, groupId, copies[groupId])
		if err != nil {
			return err
		}
		if del != nil {
			out.Copies = append(out.Copies, del)
		}
	}
	return nil
}

// removeCopies deletes the copies ids from the locked discussion group, as
// a deletion of its own with its pts: nil when none is there any more.
func removeCopies(b *userbox.Box, groupId int64, ids []int32) (*Deleted, error) {
	args := []any{groupId}
	for _, id := range ids {
		args = append(args, id)
	}
	del := &Deleted{ChannelId: groupId}
	if err := b.Tx().QueryRowsPartial(&del.Ids,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") ORDER BY id FOR UPDATE", args...); err != nil {
		return nil, err
	}
	if len(del.Ids) == 0 {
		return nil, nil
	}
	group, err := channelbox.Open(b, groupId)
	if err != nil {
		return nil, err
	}
	if err = removeMessages(b, group, del); err != nil {
		return nil, err
	}
	return del, nil
}

// postCommentsOff takes the comments off the post of the locked channel:
// each view of a post carries them (SendChannel), and the clients take a
// post without replies as one without the button (tdesktop
// history_item.cpp:2307-2314, clearReplies). The post changes with a pts,
// no edit date: nobody edited it. A post stored without replies, written
// before posts carried them, changes too: the history showed its comments
// by the link. Nil for a post that is gone.
func postCommentsOff(b *userbox.Box, channelId int64, postId int32) (*PostCommentsOff, error) {
	var data []string
	if err := b.Tx().QueryRowsPartial(&data,
		"SELECT message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", channelId, postId); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var m *mtproto.Message
	if err := jsonx.UnmarshalFromString(data[0], &m); err != nil {
		return nil, err
	}
	ch, err := channelbox.Open(b, channelId)
	if err != nil {
		return nil, err
	}
	m.Replies = nil
	ch.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	stored, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_messages SET message_data = ? WHERE channel_id = ? AND id = ?", stored, channelId, postId); err != nil {
		return nil, err
	}
	return &PostCommentsOff{ChannelId: channelId, Message: m, Pts: ch.Pts()}, nil
}

// GroupsForDiscussion are the supergroups userId may link to a channel: in
// it, may change its info, a supergroup not linked to a channel already
// (core.telegram.org/method/channels.getGroupsForDiscussion). One that
// hides its history is listed too: the method's page tells to open it
// first, as Android (ChatLinkActivity.java:565) and Web
// (appChatsManager.setDiscussionGroup) do before linking; until then
// setDiscussionGroup is MEGAGROUP_PREHISTORY_HIDDEN. Basic groups are biz
// chat's.
func (d *Dao) GroupsForDiscussion(ctx context.Context, userId int64) ([]int64, error) {
	var rows []struct {
		Id           int64 `db:"id"`
		Default      int32 `db:"default_banned_rights"`
		Role         int32 `db:"role"`
		AdminRights  int32 `db:"admin_rights"`
		BannedRights int32 `db:"banned_rights"`
		UntilDate    int32 `db:"until_date"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT c.id, c.default_banned_rights, p.role, p.admin_rights, p.banned_rights, p.until_date FROM channel_participants p"+
			" JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.user_id = ? AND p.state = 0 AND p.role > 0 AND c.megagroup = 1 AND c.deleted = 0 AND c.linked_chat_id = 0 AND c.forum = 0"+
			" ORDER BY c.id DESC", userId); err != nil {
		return nil, err
	}
	var out []int64
	for _, r := range rows {
		p := channelperm.Participant{Role: r.Role, State: channelperm.StateIn, AdminRights: r.AdminRights, BannedRights: r.BannedRights, UntilDate: r.UntilDate}
		if channelperm.Can(channelperm.Channel{Default: r.Default}, p, channelperm.ChangeInfo, 0) == nil {
			out = append(out, r.Id)
		}
	}
	return out, nil
}

// Thread is the thread of replies of a supergroup's message: the comments
// of a post are the thread of its copy in the discussion group.
type Thread struct {
	GroupId int64
	TopId   int32
	// The post the thread comments on, when it is a post's copy.
	ChannelId int64
	PostId    int32
	// MaxId is the newest reply, Count the replies, Repliers the authors
	// of the newest ones.
	MaxId    int32
	Count    int32
	Repliers []sendas.AuthorRow
	// Pts is messageReplies.replies_pts: the pts of the post's channel when
	// counted.
	Pts int32
}

// RepliersMax is the most authors of recent_repliers
// (core.telegram.org/constructor/messageReplies: the clients show three).
const RepliersMax = 3

// ThreadOf resolves a message to its thread: a post of a broadcast channel
// to its copy in the discussion group, a message of a supergroup to the
// thread it starts or is in, even with the message or the start of the
// thread deleted, whose replies stay. MSG_ID_INVALID for a post without a
// copy (comments off, Android answers it with ChannelPostDeleted,
// ChatActivity.java:35593) or a message that never was, and for any message
// of a monoforum (task 084): its messages start no threads here, and a
// thread would be read across the sublists.
func (d *Dao) ThreadOf(ctx context.Context, channelId int64, msgId int32) (*Thread, error) {
	var posts []struct {
		GroupId        int64 `db:"group_id"`
		GroupMessageId int32 `db:"group_message_id"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &posts,
		"SELECT group_id, group_message_id FROM channel_discussion_posts WHERE channel_id = ? AND post_id = ?", channelId, msgId); err != nil {
		return nil, err
	}
	t := &Thread{}
	if len(posts) > 0 {
		t.GroupId, t.TopId, t.ChannelId, t.PostId = posts[0].GroupId, posts[0].GroupMessageId, channelId, msgId
	} else {
		var tops []struct {
			TopId     int32 `db:"top_id"`
			Broadcast bool  `db:"broadcast"`
			Monoforum bool  `db:"monoforum"`
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &tops,
			"SELECT IF(m.reply_top_id <> 0, m.reply_top_id, m.id) AS top_id, c.broadcast, c.monoforum FROM channel_messages m JOIN channels c ON c.id = m.channel_id WHERE m.channel_id = ? AND m.id = ?",
			channelId, msgId); err != nil {
			return nil, err
		}
		if len(tops) == 0 || tops[0].Broadcast || tops[0].Monoforum {
			return nil, mtproto.ErrMsgIdInvalid
		}
		t.GroupId, t.TopId = channelId, tops[0].TopId
		var post []struct {
			ChannelId int64 `db:"channel_id"`
			PostId    int32 `db:"post_id"`
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &post,
			"SELECT channel_id, post_id FROM channel_discussion_posts WHERE group_id = ? AND group_message_id = ?", t.GroupId, t.TopId); err != nil {
			return nil, err
		}
		if len(post) > 0 {
			t.ChannelId, t.PostId = post[0].ChannelId, post[0].PostId
		}
	}
	return t, d.threadCounts(ctx, t)
}

func (d *Dao) threadCounts(ctx context.Context, t *Thread) error {
	var counts []struct {
		Count int32 `db:"n"`
		MaxId int32 `db:"max_id"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &counts,
		"SELECT COUNT(*) AS n, COALESCE(MAX(id), 0) AS max_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0",
		t.GroupId, t.TopId); err != nil {
		return err
	}
	t.Count, t.MaxId = counts[0].Count, counts[0].MaxId
	// One who wrote as a channel is that channel (task 079).
	return d.Mysql.QueryRowsPartial(ctx, &t.Repliers,
		"SELECT sender_user_id, send_as_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0"+
			" GROUP BY sender_user_id, send_as_id ORDER BY MAX(id) DESC LIMIT ?", t.GroupId, t.TopId, RepliersMax)
}

// ThreadRead is how far userId read the thread, 0 for not at all.
func (d *Dao) ThreadRead(ctx context.Context, userId int64, t *Thread) (readMax, unread int32, err error) {
	var reads []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &reads,
		"SELECT read_max_id FROM channel_thread_reads WHERE user_id = ? AND channel_id = ? AND top_id = ?", userId, t.GroupId, t.TopId); err != nil {
		return 0, 0, err
	}
	readMax = first(reads)
	var n []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &n,
		"SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0 AND id > ? AND sender_user_id <> ?",
		t.GroupId, t.TopId, readMax, userId); err != nil {
		return 0, 0, err
	}
	return readMax, first(n), nil
}

// ThreadOutboxRead is how far others read userId's replies in the thread:
// its newest reply someone else read, 0 for none
// (messages.discussionMessage.read_outbox_max_id, which the clients keep
// as the thread's read mark of outgoing messages: tdesktop
// window_session_controller.cpp:1312-1315). A ghost's read is none of them
// (ADR 0048, p. 5).
func (d *Dao) ThreadOutboxRead(ctx context.Context, userId int64, t *Thread) (int32, error) {
	var outbox []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &outbox,
		"SELECT COALESCE(MAX(m.id), 0) FROM channel_messages m WHERE m.channel_id = ? AND m.reply_top_id = ? AND m.sender_user_id = ? AND m.deleted = 0"+
			" AND m.id <= (SELECT COALESCE(MAX(r.read_max_id), 0) FROM channel_thread_reads r WHERE r.channel_id = ? AND r.top_id = ? AND r.user_id <> ?"+
			" AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = r.user_id AND u.ghost = 1))",
		t.GroupId, t.TopId, userId, t.GroupId, t.TopId, userId); err != nil {
		return 0, err
	}
	return first(outbox), nil
}

// ReadThread moves how far b's owner read the thread forward to readMax,
// at most its last message (messages.readDiscussion). The caller has
// checked that the owner may read the group: a member, or a reader through
// its channel, who comments without joining (task 048). The owner's
// devices get updateReadChannelDiscussionInbox with seq, naming the post
// for a thread of comments, and the authors of the replies read get
// updateReadChannelDiscussionOutbox by push
// (core.telegram.org/constructor/updateReadChannelDiscussionInbox,
// updateReadChannelDiscussionOutbox; tdesktop api_updates.cpp:2508-2538,
// Android MessagesController.java:20522-20533). A read that moves nothing
// writes nothing. A ghost's read (ADR 0048, p. 5) is its own: the authors
// learn nothing of it.
func (d *Dao) ReadThread(b *userbox.Box, t *Thread, readMax int32) error {
	readMax = min(readMax, max(t.MaxId, t.TopId))
	var reads []int32
	if err := b.Tx().QueryRowsPartial(&reads,
		"SELECT read_max_id FROM channel_thread_reads WHERE user_id = ? AND channel_id = ? AND top_id = ? FOR UPDATE", b.UserId(), t.GroupId, t.TopId); err != nil {
		return err
	}
	was := first(reads)
	if readMax <= was {
		return nil
	}
	if _, err := b.Tx().Exec("INSERT INTO channel_thread_reads (user_id, channel_id, top_id, read_max_id) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE read_max_id = VALUES(read_max_id)",
		b.UserId(), t.GroupId, t.TopId, readMax); err != nil {
		return err
	}
	inbox := &mtproto.Update{ChannelId: t.GroupId, TopMsgId_INT32: t.TopId, ReadMaxId: readMax}
	if t.ChannelId != 0 {
		inbox.BroadcastId = mtproto.MakeFlagsInt64(t.ChannelId)
		inbox.BroadcastPost = mtproto.MakeFlagsInt32(t.PostId)
	}
	if err := d.EmitSeqUpdate(b, mtproto.MakeTLUpdateReadChannelDiscussionInbox(inbox).To_Update(), nil, nil); err != nil {
		return err
	}
	if hidden, err := ghost.IsGhost(b.Tx(), b.UserId()); err != nil || hidden {
		return err
	}
	var authors []struct {
		UserId int64 `db:"sender_user_id"`
		MaxId  int32 `db:"max_id"`
	}
	if err := b.Tx().QueryRowsPartial(&authors,
		"SELECT sender_user_id, MAX(id) AS max_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND id > ? AND id <= ? AND sender_user_id <> ? AND deleted = 0"+
			" GROUP BY sender_user_id ORDER BY max_id DESC LIMIT ?",
		t.GroupId, t.TopId, was, readMax, b.UserId(), readAuthorsMax); err != nil {
		return err
	}
	for _, a := range authors {
		in := &sync.TLSyncPushUpdates{UserId: a.UserId, Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateReadChannelDiscussionOutbox(&mtproto.Update{
			ChannelId: t.GroupId, TopMsgId_INT32: t.TopId, ReadMaxId: a.MaxId,
		}).To_Update())}
		data, err := jsonx.Marshal(in)
		if err != nil {
			return err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: a.UserId, Method: string(proto.MessageName(in)), Data: data})
	}
	return nil
}

// withReplies sets on the posts of a broadcast channel with comments their
// messageReplies: the count, the discussion group, the newest reply and
// its recent authors, and how far selfId read them (read_max_id, without
// which tdesktop never marks comments unread, history_item.cpp:1367-1375;
// core.telegram.org/constructor/messageReplies).
func (d *Dao) withReplies(ctx context.Context, selfId, channelId int64, broadcast bool, messages []*mtproto.Message) error {
	if !broadcast || len(messages) == 0 {
		return nil
	}
	args := []any{channelId}
	marks := ""
	for _, m := range messages {
		if m.GetPredicateName() == mtproto.Predicate_message {
			if marks != "" {
				marks += ", "
			}
			marks += "?"
			args = append(args, m.GetId())
		}
	}
	if marks == "" {
		return nil
	}
	var posts []struct {
		PostId         int32 `db:"post_id"`
		GroupId        int64 `db:"group_id"`
		GroupMessageId int32 `db:"group_message_id"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &posts,
		"SELECT post_id, group_id, group_message_id FROM channel_discussion_posts WHERE channel_id = ? AND post_id IN ("+marks+")", args...); err != nil {
		return err
	}
	// replies_pts is the pts of the channel the post is in ("PTS of the
	// message that started this thread", core.telegram.org/constructor/
	// messageReplies): tdesktop takes the counts of an answer only when it
	// is not older than the channel's pts it has (history_item.cpp:
	// 4825-4832), and counts the comments it receives itself in between.
	var pts []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &pts, "SELECT pts FROM channels WHERE id = ?", channelId); err != nil {
		return err
	}
	byPost := map[int32]*Thread{}
	readMax := map[int32]int32{}
	for _, p := range posts {
		t := &Thread{GroupId: p.GroupId, TopId: p.GroupMessageId, ChannelId: channelId, PostId: p.PostId}
		if err := d.threadCounts(ctx, t); err != nil {
			return err
		}
		byPost[p.PostId] = t
		var reads []int32
		if err := d.Mysql.QueryRowsPartial(ctx, &reads,
			"SELECT read_max_id FROM channel_thread_reads WHERE user_id = ? AND channel_id = ? AND top_id = ?", selfId, p.GroupId, p.GroupMessageId); err != nil {
			return err
		}
		readMax[p.PostId] = first(reads)
	}
	for _, m := range messages {
		if t, ok := byPost[m.GetId()]; ok {
			t.Pts = first(pts)
			m.Replies = MessageReplies(t, readMax[m.GetId()])
		}
	}
	return nil
}

// MessageReplies is the messageReplies of a post's comments, as of a
// reader who read the thread up to readMax (0: not given).
func MessageReplies(t *Thread, readMax int32) *mtproto.MessageReplies {
	r := &mtproto.MessageReplies{
		Comments:       true,
		Replies:        t.Count,
		RepliesPts:     t.Pts,
		ChannelId:      mtproto.MakeFlagsInt64(t.GroupId),
		RecentRepliers: []*mtproto.Peer{},
	}
	for _, a := range t.Repliers {
		r.RecentRepliers = append(r.RecentRepliers, a.Peer())
	}
	if t.MaxId != 0 {
		r.MaxId = mtproto.MakeFlagsInt32(t.MaxId)
	}
	if readMax != 0 {
		r.ReadMaxId = mtproto.MakeFlagsInt32(readMax)
	}
	return mtproto.MakeTLMessageReplies(r).To_MessageReplies()
}
