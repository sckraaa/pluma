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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
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
}

func linkRows(b *userbox.Box, ids ...int64) (map[int64]linkRow, error) {
	out := map[int64]linkRow{}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		var rows []linkRow
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT id, broadcast, megagroup, hidden_prehistory, linked_chat_id, deleted FROM channels WHERE id = ?", id); err != nil {
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
		case !ok || !g.Megagroup:
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
	return slices.Compact(slices.Sorted(slices.Values(changed))), nil
}

// GroupsForDiscussion are the supergroups userId may link to a channel: in
// it, may change its info, its history visible to new members, a
// supergroup not linked to a channel already
// (core.telegram.org/method/channels.getGroupsForDiscussion). Basic groups
// are biz chat's.
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
			" WHERE p.user_id = ? AND p.state = 0 AND p.role > 0 AND c.megagroup = 1 AND c.deleted = 0 AND c.hidden_prehistory = 0 AND c.linked_chat_id = 0"+
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
	Repliers []int64
	// Pts is the group's pts when counted (messageReplies.replies_pts).
	Pts int32
}

// RepliersMax is the most authors of recent_repliers
// (core.telegram.org/constructor/messageReplies: the clients show three).
const RepliersMax = 3

// ThreadOf resolves a message to its thread: a post of a broadcast channel
// to its copy in the discussion group, a message of a supergroup to the
// thread it starts or is in. MSG_ID_INVALID for a post without a copy
// (comments off) or a message that is not there.
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
		var tops []int32
		if err := d.Mysql.QueryRowsPartial(ctx, &tops,
			"SELECT IF(reply_top_id <> 0, reply_top_id, id) FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0", channelId, msgId); err != nil {
			return nil, err
		}
		if len(tops) == 0 {
			return nil, mtproto.ErrMsgIdInvalid
		}
		t.GroupId, t.TopId = channelId, tops[0]
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
	var pts []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &pts, "SELECT pts FROM channels WHERE id = ?", t.GroupId); err != nil {
		return err
	}
	t.Pts = first(pts)
	return d.Mysql.QueryRowsPartial(ctx, &t.Repliers,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0"+
			" GROUP BY sender_user_id ORDER BY MAX(id) DESC LIMIT ?", t.GroupId, t.TopId, RepliersMax)
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

// ReadThread moves how far b's owner read the thread topId of groupId
// forward to readMax (messages.readDiscussion); a member of the group only
// (CHANNEL_PRIVATE).
func (d *Dao) ReadThread(b *userbox.Box, groupId int64, topId, readMax int32) error {
	if err := readerIn(b, groupId); err != nil {
		return err
	}
	_, err := b.Tx().Exec("INSERT INTO channel_thread_reads (user_id, channel_id, top_id, read_max_id) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE read_max_id = GREATEST(read_max_id, VALUES(read_max_id))",
		b.UserId(), groupId, topId, readMax)
	return err
}

// withReplies sets on the posts of a broadcast channel with comments their
// messageReplies: the count, the discussion group, the newest reply and
// its recent authors (core.telegram.org/constructor/messageReplies).
func (d *Dao) withReplies(ctx context.Context, channelId int64, broadcast bool, messages []*mtproto.Message) error {
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
	byPost := map[int32]*Thread{}
	for _, p := range posts {
		t := &Thread{GroupId: p.GroupId, TopId: p.GroupMessageId, ChannelId: channelId, PostId: p.PostId}
		if err := d.threadCounts(ctx, t); err != nil {
			return err
		}
		byPost[p.PostId] = t
	}
	for _, m := range messages {
		if t, ok := byPost[m.GetId()]; ok {
			m.Replies = MessageReplies(t, 0)
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
	for _, id := range t.Repliers {
		r.RecentRepliers = append(r.RecentRepliers, mtproto.MakePeerUser(id))
	}
	if t.MaxId != 0 {
		r.MaxId = mtproto.MakeFlagsInt32(t.MaxId)
	}
	if readMax != 0 {
		r.ReadMaxId = mtproto.MakeFlagsInt32(readMax)
	}
	return mtproto.MakeTLMessageReplies(r).To_MessageReplies()
}
