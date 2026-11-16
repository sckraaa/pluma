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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/polls"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The polls (pkg/polls): a row of polls for each poll sent, with where it
// was sent (the channel and its post, or the dialog of a private chat or a
// basic group), and a row of poll_votes for each option a voter chose, at
// the time it was chosen. A vote taken back leaves no row: the votes counted
// are the ones standing, by when they were cast.

// pollIdOf is the id of the poll of the message stored as data (jsonx of
// mtproto.Message, as msg stores it), 0 when it has none.
func pollIdOf(data string) (int64, error) {
	var m *mtproto.Message
	if err := jsonx.UnmarshalFromString(data, &m); err != nil {
		return 0, err
	}
	if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPoll {
		return 0, nil
	}
	return m.GetMedia().GetPoll().GetId(), nil
}

// ChannelPollId is the poll of the message msgId of the channel, 0 when
// there is no such message, it is deleted or it has no poll.
func (d *Dao) ChannelPollId(ctx context.Context, channelId int64, msgId int32) (int64, error) {
	var data []string
	if err := d.db.QueryRowsPartial(ctx, &data,
		"SELECT m.message_data FROM channel_messages m WHERE m.channel_id = ? AND m.id = ? AND "+isPost, channelId, msgId); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	return pollIdOf(data[0])
}

// BoxPollId is the poll of the message msgId of userId's box in the dialog
// (dialogId1, dialogId2) of a private chat or a basic group, 0 when the box
// has no such message there, it is deleted or it has no poll.
func (d *Dao) BoxPollId(ctx context.Context, userId int64, msgId int32, dialogId1, dialogId2 int64) (int64, error) {
	var data []string
	if err := d.db.QueryRowsPartial(ctx, &data,
		"SELECT message_data FROM messages WHERE user_id = ? AND user_message_box_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0",
		userId, msgId, dialogId1, dialogId2); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	return pollIdOf(data[0])
}

// Poll is the poll id, nil when there is none.
func (d *Dao) Poll(ctx context.Context, id int64) (*polls.Record, error) {
	if id == 0 {
		return nil, nil
	}
	var rows []polls.Row
	if err := d.db.QueryRowsPartial(ctx, &rows, "SELECT "+polls.Columns+" FROM polls WHERE id = ?", id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].Decode()
}

// VoteCounts are the standing votes of the poll cast in r by bucket and
// option (K: the option's bytes), over the index by_date.
func (d *Dao) VoteCounts(ctx context.Context, pollId int64, r Range) ([]KeyCount, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	var rows []KeyCount
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT FLOOR(v.`date` / ?) AS b, v.opt AS k, COUNT(*) AS n FROM poll_votes v"+
			" WHERE v.poll_id = ? AND v.`date` >= ? AND v.`date` < ? AND "+notGhost("v.user_id")+" GROUP BY b, k",
		r.Bucket, pollId, r.From, r.To)
	return rows, err
}
