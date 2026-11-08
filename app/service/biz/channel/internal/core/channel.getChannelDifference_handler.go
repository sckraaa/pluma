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

package core

import (
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// differenceLimitMax is the most journal rows of one answer: Web K
	// asks 1000 (apiUpdatesManager.ts:435-440), the others 100.
	differenceLimitMax = 1000
	// tooLongMessages is the most messages of channelDifferenceTooLong.
	tooLongMessages = 100

	// The poll timeouts of a final answer, in seconds: a member gets the
	// new posts by push, so polling is the fallback; 30 s is what iOS waits
	// without the flag, and 0 would make it poll without a pause (ADR 0034,
	// p. 4; Telegram's values are not verified, C1).
	memberTimeout = 30
	viewerTimeout = 10
)

// ChannelGetChannelDifference
// channel.getChannelDifference self_id:long channel_id:long pts:int limit:int = updates.ChannelDifference;
//
// What self_id missed in the channel since pts (ADR 0034, p. 4). The
// committed pts S of the channel is read first, then the journal rows in
// (pts, S], all committed by then:
//
//   - pts at or above S: channelDifferenceEmpty with S (a client ahead of
//     the channel, restored from a backup, takes the channel's pts);
//   - rows from pts on are gone from the journal (JournalWindow), or pts is
//     below the first pts: channelDifferenceTooLong with the dialog and the
//     last messages;
//   - more rows than limit: a slice of them, not final, with the pts of its
//     last row, so that the client always moves forward;
//   - otherwise the rows folded up to S, final.
//
// A final answer always has a timeout above zero. CHANNEL_PRIVATE for a
// channel self_id may not read. Chats and users are the caller's to add.
func (c *ChannelCore) ChannelGetChannelDifference(in *channel.TLChannelGetChannelDifference) (*mtproto.Updates_ChannelDifference, error) {
	v, err := c.svcCtx.Dao.ViewerOf(c.ctx, in.SelfId, in.ChannelId)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - viewer %d of channel %d: %v", in.SelfId, in.ChannelId, err)
		return nil, err
	}
	if !v.CanRead {
		return nil, mtproto.ErrChannelPrivate
	}
	timeout := wrapperspb.Int32(viewerTimeout)
	if v.Member {
		timeout = wrapperspb.Int32(memberTimeout)
	}
	limit := min(max(in.Limit, 1), differenceLimitMax)

	s, err := c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - pts of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	if in.Pts >= s {
		return mtproto.MakeTLUpdatesChannelDifferenceEmpty(&mtproto.Updates_ChannelDifference{
			Final:   true,
			Pts:     s,
			Timeout: timeout,
		}).To_Updates_ChannelDifference(), nil
	}

	// One row more than the limit tells whether this is the last page.
	rows, err := c.svcCtx.Dao.Journal(c.ctx, in.ChannelId, in.Pts, s, limit+1)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - journal of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	// The journal is contiguous (ADR 0034, K2): a first row that does not
	// start at pts means the rows after pts were trimmed.
	if len(rows) == 0 || rows[0].Pts-rows[0].PtsCount > in.Pts {
		return c.tooLong(in, v, timeout, limit)
	}

	end, final := s, len(rows) <= int(limit)
	if !final {
		rows = rows[:limit]
		end = rows[len(rows)-1].Pts
	}
	f := newChannelFold(in.SelfId, v.AvailableMinId, end)
	for _, r := range rows {
		u := new(mtproto.Update)
		if err = jsonx.UnmarshalFromString(r.UpdateData, u); err != nil {
			// A broken row would leave the client with a hole it cannot
			// fill; it fails the request so that it is seen and fixed.
			c.Logger.Errorf("channel.getChannelDifference - row %d of channel %d: %v", r.Pts, in.ChannelId, err)
			return nil, mtproto.ErrInternalServerError
		}
		f.add(u)
	}
	randomIds, err := c.svcCtx.Dao.OwnRandomIds(c.ctx, in.SelfId, in.ChannelId, f.newMessageIds())
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - random ids of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	f.addMessageIds(randomIds)

	d := &mtproto.Updates_ChannelDifference{
		Final:        final,
		Pts:          end,
		NewMessages:  f.messages(),
		OtherUpdates: f.otherUpdates(),
		Chats:        []*mtproto.Chat{},
		Users:        []*mtproto.User{},
	}
	if final {
		d.Timeout = timeout
	}
	// The journal holds a post as it was: its counters are read now.
	posts := slices.Clone(d.NewMessages)
	for _, u := range d.OtherUpdates {
		if m := u.GetMessage_MESSAGE(); m != nil {
			posts = append(posts, m)
		}
	}
	if err = c.svcCtx.Dao.WithPostCounters(c.ctx, in.ChannelId, posts); err != nil {
		c.Logger.Errorf("channel.getChannelDifference - counters of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	return mtproto.MakeTLUpdatesChannelDifference(d).To_Updates_ChannelDifference(), nil
}

// tooLong is channelDifferenceTooLong: the viewer's dialog, read first, and
// the last messages it sees, read after, so that they reach at least the
// dialog's pts. iOS takes pts, top_message, the read marks and the counts
// from the dialog (AccountStateManagementUtils.swift:3626-3727), Android
// overwrites the channel with it (MessagesStorage.java:11278-11352).
func (c *ChannelCore) tooLong(in *channel.TLChannelGetChannelDifference, v dao.Viewer, timeout *wrapperspb.Int32Value, limit int32) (*mtproto.Updates_ChannelDifference, error) {
	dialog, err := c.svcCtx.Dao.ViewerDialog(c.ctx, in.SelfId, in.ChannelId, v)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - dialog of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	messages, _, err := c.svcCtx.Dao.History(c.ctx, in.SelfId, in.ChannelId, 0, 0, 0, min(limit, tooLongMessages), 0, 0)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDifference - history of channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	return mtproto.MakeTLUpdatesChannelDifferenceTooLong(&mtproto.Updates_ChannelDifference{
		Final:    true,
		Timeout:  timeout,
		Dialog:   dialog,
		Messages: messages,
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}).To_Updates_ChannelDifference(), nil
}
