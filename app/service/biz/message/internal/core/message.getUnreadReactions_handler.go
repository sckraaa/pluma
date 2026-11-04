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
	"math"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// maxUnreadReactions is the most messages of one page, as of the history.
const maxUnreadReactions = 100

// messageColumns are the columns of messages the DAOs read into MessagesDO.
const messageColumns = "user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id, message_filter_type, message_data, message, mentioned, media_unread, pinned, has_reaction, reaction, reaction_date, reaction_unread, saved_peer_type, saved_peer_id, date2, ttl_period"

// MessageGetUnreadReactions
// message.getUnreadReactions user_id:long peer_type:int peer_id:long offset_id:int add_offset:int limit:int min_id:int max_id:int = Vector<MessageBox>;
//
// The user's messages of a private chat or a basic group with reactions it
// has not seen (reaction_unread of its copies, task 053), newest first, paged
// as the history: add_offset messages newer than offset_id are skipped
// (negative: taken), then up to limit; min_id and max_id bound the ids
// (0 for none).
func (c *MessageCore) MessageGetUnreadReactions(in *message.TLMessageGetUnreadReactions) (*message.Vector_MessageBox, error) {
	out := &message.Vector_MessageBox{Datas: []*mtproto.MessageBox{}}
	if in.PeerType != mtproto.PEER_USER && in.PeerType != mtproto.PEER_CHAT {
		return out, nil
	}
	limit := min(max(in.Limit, 0), maxUnreadReactions)
	// A page never holds more than limit messages: a negative add_offset
	// takes at most limit newer ones.
	addOffset := max(in.AddOffset, -limit)
	offsetId := in.OffsetId
	if offsetId <= 0 {
		offsetId = math.MaxInt32
	}
	did := mtproto.MakeDialogId(in.UserId, in.PeerType, in.PeerId)
	where := "user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND reaction_unread = 1 AND deleted = 0 AND user_message_box_id > ? AND user_message_box_id < ?"
	lower, upper := in.MinId, int32(math.MaxInt32)
	if in.MaxId > 0 {
		upper = in.MaxId + 1
	}
	table := c.svcCtx.Dao.MessagesDAO.CalcTableName(in.UserId)

	var rows []dataobject.MessagesDO
	if addOffset < 0 {
		// The newer ones first, from offset_id up, oldest first.
		var newer []dataobject.MessagesDO
		if err := c.svcCtx.Dao.Mysql.QueryRowsPartial(c.ctx, &newer,
			"SELECT "+messageColumns+" FROM "+table+" WHERE "+where+" AND user_message_box_id >= ? ORDER BY user_message_box_id LIMIT ?",
			in.UserId, did.A, did.B, lower, upper, offsetId, -addOffset); err != nil {
			return nil, err
		}
		slices.Reverse(newer)
		rows = newer
	}
	rest := limit - int32(len(rows))
	skip := max(addOffset, 0)
	if rest > 0 {
		var older []dataobject.MessagesDO
		if err := c.svcCtx.Dao.Mysql.QueryRowsPartial(c.ctx, &older,
			"SELECT "+messageColumns+" FROM "+table+" WHERE "+where+" AND user_message_box_id < ? ORDER BY user_message_box_id DESC LIMIT ? OFFSET ?",
			in.UserId, did.A, did.B, lower, upper, offsetId, rest, skip); err != nil {
			return nil, err
		}
		rows = append(rows, older...)
	}
	for i := range rows {
		out.Datas = append(out.Datas, c.svcCtx.Dao.MakeMessageBox(c.ctx, in.UserId, &rows[i]))
	}
	return out, nil
}
