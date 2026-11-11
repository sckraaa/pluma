//go:build integration

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

package dao_test

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// sendChannelPoll sends a poll of from to the channel and returns its id.
func (w *world) sendChannelPoll(from, channelId int64) int32 {
	w.t.Helper()
	sent, err := w.sendOutgoing(from, channelId, dao.Outgoing{
		RandomId: randomId(), DialogMessageId: newDialogMessageId(), Message: pollMessage(w.t, from, mtproto.MakePeerChannel(channelId), nil),
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return sent[0].Message.GetId()
}

func (w *world) closeChannelPoll(user, channelId int64, id int32) {
	w.t.Helper()
	if _, err := w.d.RunBox(context.Background(), user, nil, func(b *userbox.Box) error {
		ref, err := w.d.ChannelPollRefOf(b, channelId, id, true)
		if err != nil {
			return err
		}
		_, err = w.d.ClosePoll(b, ref)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// stoppedPolls are the stopped polls the channel's recent actions keep, by
// whom, under the edits.
func (w *world) stoppedPolls(channelId int64) map[int32]int64 {
	w.t.Helper()
	rows, err := w.check.Query("SELECT user_id, action FROM channel_admin_log WHERE channel_id = ? AND (kind & ?) <> 0", channelId, adminlog.Edit)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	out := map[int32]int64{}
	for rows.Next() {
		var (
			userId int64
			data   []byte
		)
		if err = rows.Scan(&userId, &data); err != nil {
			w.t.Fatal(err)
		}
		a, err := adminlog.Decode(data)
		if err != nil {
			w.t.Fatal(err)
		}
		if a.GetPredicateName() == mtproto.Predicate_channelAdminLogEventActionStopPoll {
			out[a.GetMessage().GetId()] = userId
		}
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// TestStopPollKept: a poll of a post stopped is kept in the recent actions,
// as an edit of a post is; a member of a supergroup stopping its own poll
// is not, and a poll stopped again keeps nothing more.
func TestStopPollKept(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, member := newUser(), newUser()

	broadcast := w.newChannelRow(owner, true)
	post := w.sendChannelPoll(owner, broadcast)
	w.closeChannelPoll(owner, broadcast, post)
	w.closeChannelPoll(owner, broadcast, post)
	if got := w.stoppedPolls(broadcast); len(got) != 1 || got[post] != owner {
		t.Errorf("the post's poll: %v", got)
	}

	group := w.newChannelRow(owner, false)
	w.member(group, member, 0)
	own := w.sendChannelPoll(member, group)
	w.closeChannelPoll(member, group, own)
	if got := w.stoppedPolls(group); len(got) != 0 {
		t.Errorf("a member's own poll: %v", got)
	}
}
