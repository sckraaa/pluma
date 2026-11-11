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
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/ghost"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// The counters of the posts of broadcast channels (task 046). A view or a
// forward moves neither the channel's pts nor its journal, and does not
// take the channel's lock: it is the most frequent write of a channel and
// checks nothing that a concurrent change of rights could make wrong. The
// rows of channel_message_views and channel_message_forwards count each
// user's view and each forward once, under any concurrency.

const (
	// countersMax is the most posts one call counts: the clients ask for
	// the posts on the screen. The rest of a longer list is answered, not
	// counted.
	countersMax = 100
	// counterPushWindow is the least time between two pushes of the
	// counters of one channel. Android gathers its views for 5 seconds
	// (MessagesController.java:10579); a shorter one would show nothing
	// more to it.
	counterPushWindow = 5 * time.Second
	// counterPushTimeout bounds one push, which outlives the request that
	// caused it.
	counterPushTimeout = 10 * time.Second
)

// placeholders is the list of n placeholders of an IN clause.
func placeholders(n int) string {
	return "?" + strings.Repeat(", ?", n-1)
}

// IncrementViews counts selfId's view of the posts ids of channelId: once
// per user and post, not for a post of its own (the initial 1 is that
// view), not for a message of a supergroup, which has no views, and not
// for a ghost (ADR 0048, p. 5), whose views nobody sees. A channel selfId
// may not read is CHANNEL_PRIVATE.
func (d *Dao) IncrementViews(ctx context.Context, selfId, channelId int64, ids []int32) error {
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return err
	}
	if !v.canRead {
		return mtproto.ErrChannelPrivate
	}
	if !v.broadcast {
		return nil
	}
	if hidden, err := ghost.IsGhost(ghost.OnDB(ctx, d.Mysql.DB), selfId); err != nil || hidden {
		return err
	}
	ids = countedIds(ids)
	if len(ids) == 0 {
		return nil
	}
	// Most calls find nothing new: the clients repeat them. They stop here,
	// with no transaction.
	args := []any{selfId, channelId, v.availableMinId, selfId, mtproto.Predicate_message}
	for _, id := range ids {
		args = append(args, id)
	}
	var fresh []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &fresh,
		"SELECT m.id FROM channel_messages m LEFT JOIN channel_message_views v ON v.channel_id = m.channel_id AND v.message_id = m.id AND v.user_id = ?"+
			" WHERE m.channel_id = ? AND m.deleted = 0 AND m.id >= ? AND m.sender_user_id <> ? AND v.user_id IS NULL"+
			" AND JSON_UNQUOTE(JSON_EXTRACT(m.message_data, '$.predicate_name')) = ? AND m.id IN ("+placeholders(len(ids))+") ORDER BY m.id",
		args...); err != nil {
		return err
	}
	if len(fresh) == 0 {
		return nil
	}
	var counted []int32
	if _, err = d.RunBox(ctx, selfId, func(b *userbox.Box) error {
		// A transaction that is run again starts over.
		counted = counted[:0]
		for _, id := range fresh {
			res, err := b.Tx().Exec("INSERT IGNORE INTO channel_message_views (channel_id, message_id, user_id, `date`) VALUES (?, ?, ?, ?)",
				channelId, id, selfId, b.Date())
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				counted = append(counted, id)
			}
		}
		return addCounter(b, channelId, "views", counted)
	}); err != nil {
		return err
	}
	d.pushes.mark(channelId, selfId, counted, nil)
	return nil
}

// AddForwards counts the forwards of the posts ids of channelId that selfId
// made with randomIds, one for one: a forward that is asked for again with
// its random_id counts once. Messages of supergroups and service messages
// have no counter.
func (d *Dao) AddForwards(ctx context.Context, selfId, channelId int64, ids []int32, randomIds []int64) error {
	if len(ids) != len(randomIds) {
		return mtproto.ErrInputRequestInvalid
	}
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return err
	}
	if !v.canRead {
		return mtproto.ErrChannelPrivate
	}
	if !v.broadcast || len(ids) == 0 {
		return nil
	}
	want := ids
	if len(want) > countersMax {
		want = want[:countersMax]
	}
	args := []any{channelId, v.availableMinId, mtproto.Predicate_message}
	for _, id := range want {
		args = append(args, id)
	}
	var posts []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &posts,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id >= ?"+
			" AND JSON_UNQUOTE(JSON_EXTRACT(message_data, '$.predicate_name')) = ? AND id IN ("+placeholders(len(want))+")",
		args...); err != nil {
		return err
	}
	if len(posts) == 0 {
		return nil
	}
	var counted []int32
	if _, err = d.RunBox(ctx, selfId, func(b *userbox.Box) error {
		counted = counted[:0]
		for i, id := range want {
			if !slices.Contains(posts, id) {
				continue
			}
			res, err := b.Tx().Exec("INSERT IGNORE INTO channel_message_forwards (channel_id, message_id, user_id, random_id, `date`) VALUES (?, ?, ?, ?, ?)",
				channelId, id, selfId, randomIds[i], b.Date())
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n > 0 {
				counted = append(counted, id)
			}
		}
		slices.Sort(counted)
		counted = slices.Compact(counted)
		return addCounter(b, channelId, "forwards", counted)
	}); err != nil {
		return err
	}
	d.pushes.mark(channelId, selfId, nil, counted)
	return nil
}

// countedIds are the ids one call counts: sorted, without duplicates, at
// most countersMax of them.
func countedIds(ids []int32) []int32 {
	if len(ids) > countersMax {
		ids = ids[:countersMax]
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// addCounter adds one to column of the posts ids. One statement takes the
// rows in the order of the primary key, so that two of them never deadlock.
func addCounter(b *userbox.Box, channelId int64, column string, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := b.Tx().Exec("UPDATE channel_messages SET "+column+" = "+column+" + 1 WHERE channel_id = ? AND id IN ("+placeholders(len(ids))+")", args...)
	return err
}

type postCounterRow struct {
	Id       int32 `db:"id"`
	Views    int32 `db:"views"`
	Forwards int32 `db:"forwards"`
}

func (d *Dao) postCounters(ctx context.Context, channelId int64, ids []int32) ([]postCounterRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []postCounterRow
	err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT id, views, forwards FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN ("+placeholders(len(ids))+") ORDER BY id", args...)
	return rows, err
}

// WithPostCounters sets on the posts among messages their counters as they
// are now: the stored copies and the journal hold them as they were.
func (d *Dao) WithPostCounters(ctx context.Context, channelId int64, messages []*mtproto.Message) error {
	byId := map[int32]*mtproto.Message{}
	ids := make([]int32, 0, len(messages))
	for _, m := range messages {
		if m.GetPredicateName() == mtproto.Predicate_message && m.GetPost() {
			if _, ok := byId[m.GetId()]; !ok {
				ids = append(ids, m.GetId())
			}
			byId[m.GetId()] = m
		}
	}
	rows, err := d.postCounters(ctx, channelId, ids)
	if err != nil {
		return err
	}
	for _, r := range rows {
		channel.SetPostCounters(byId[r.Id], r.Views, r.Forwards)
	}
	return nil
}

// sendCounters pushes the counters of the posts views and forwards as they
// are now, updateChannelMessageViews and updateChannelMessageForwards, to
// the channel's members (ADR 0034, p. 5): without pts and not in the
// journal, as Telegram's updates have none. The event is written in a box
// transaction of the last user counted; the box is only the way to the
// outbox. A push that fails is logged: a client that missed it reads the
// counters with getMessagesViews and with the history.
func (d *Dao) sendCounters(channelId, userId int64, views, forwards []int32) {
	ctx, cancel := context.WithTimeout(context.Background(), counterPushTimeout)
	defer cancel()
	if err := d.pushCounters(ctx, channelId, userId, views, forwards); err != nil {
		logx.WithContext(ctx).Errorf("channel: push the counters of channel %d: %v", channelId, err)
	}
}

func (d *Dao) pushCounters(ctx context.Context, channelId, userId int64, views, forwards []int32) error {
	all := append(slices.Clone(views), forwards...)
	slices.Sort(all)
	rows, err := d.postCounters(ctx, channelId, slices.Compact(all))
	if err != nil {
		return err
	}
	updates := make([]*mtproto.Update, 0, len(rows))
	for _, r := range rows {
		if slices.Contains(views, r.Id) {
			updates = append(updates, mtproto.MakeTLUpdateChannelMessageViews(&mtproto.Update{ChannelId: channelId, Id_INT32: r.Id, Views: r.Views}).To_Update())
		}
		if slices.Contains(forwards, r.Id) {
			updates = append(updates, mtproto.MakeTLUpdateChannelMessageForwards(&mtproto.Update{ChannelId: channelId, Id_INT32: r.Id, Forwards: r.Forwards}).To_Update())
		}
	}
	if len(updates) == 0 {
		return nil
	}
	in := &sync.TLSyncPushChannelUpdates{ChannelId: channelId, Updates: mtproto.MakeUpdatesByUpdates(updates...)}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	_, err = d.RunBox(ctx, userId, func(b *userbox.Box) error {
		b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
		return nil
	})
	return err
}
