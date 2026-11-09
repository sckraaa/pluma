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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/moderation"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestScheduleDateOf(t *testing.T) {
	box := func(date int32) *msg.OutboxMessage {
		if date == 0 {
			return &msg.OutboxMessage{}
		}
		return &msg.OutboxMessage{ScheduleDate: wrapperspb.Int32(date)}
	}
	if d, err := scheduleDateOf([]*msg.OutboxMessage{box(0), box(0)}); err != nil || d != 0 {
		t.Errorf("no date: %d, %v", d, err)
	}
	if d, err := scheduleDateOf([]*msg.OutboxMessage{box(5), box(5)}); err != nil || d != 5 {
		t.Errorf("one date: %d, %v", d, err)
	}
	if _, err := scheduleDateOf([]*msg.OutboxMessage{box(5), box(6)}); err == nil {
		t.Error("two dates in one send: no error")
	}
}

func TestDeliveryUnits(t *testing.T) {
	rows := []*dao.ScheduledRow{
		{Id: 1, SenderUserId: 1},
		{Id: 2, SenderUserId: 1, GroupedId: 9},
		{Id: 3, SenderUserId: 2, GroupedId: 9}, // another sender's album
		{Id: 4, SenderUserId: 1, GroupedId: 9},
		{Id: 5, SenderUserId: 1},
	}
	got := deliveryUnits(rows)
	var ids [][]int32
	for _, u := range got {
		ids = append(ids, dao.ScheduledIds(u))
	}
	want := [][]int32{{1}, {2, 4}, {3}, {5}}
	if len(ids) != len(want) {
		t.Fatalf("units %v, want %v", ids, want)
	}
	for i := range want {
		if len(ids[i]) != len(want[i]) {
			t.Fatalf("units %v, want %v", ids, want)
		}
		for j := range want[i] {
			if ids[i][j] != want[i][j] {
				t.Fatalf("units %v, want %v", ids, want)
			}
		}
	}
}

func TestPermanentSendError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{mtproto.ErrChatWriteForbidden, true},
		{mtproto.ErrChannelPrivate, true},
		{mtproto.ErrPeerIdInvalid, true},
		{moderation.ErrPeerFlood, true},
		{mtproto.ErrUserBannedInChannel, true},
		{status.Error(codes.Unavailable, "msg is down"), false},
		{mtproto.ErrInternalServerError, false},
		{errors.New("deadlock"), false},
		{dao.ErrScheduledGone, false},
	} {
		if got := permanentSendError(tc.err); got != tc.want {
			t.Errorf("permanentSendError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestRescheduled(t *testing.T) {
	before := []*dao.ScheduledRow{{Id: 1, ScheduleDate: 100}, {Id: 2, ScheduleDate: 100}}
	if rescheduled(before, []*dao.ScheduledRow{{Id: 2, ScheduleDate: 100}}) {
		t.Error("a row gone is not a new date")
	}
	if !rescheduled(before, []*dao.ScheduledRow{{Id: 1, ScheduleDate: 100}, {Id: 2, ScheduleDate: 500}}) {
		t.Error("a new date of one row of an album is not seen")
	}
}

func TestViewScheduled(t *testing.T) {
	m := &mtproto.Message{Out: true, Id: 3}
	if !viewScheduled(m, 7, 7).Out {
		t.Error("the sender sees out")
	}
	if viewScheduled(m, 7, 8).Out {
		t.Error("another admin sees out")
	}
	if !m.Out {
		t.Error("the stored message changed")
	}
}

func TestQueueUpdatesOf(t *testing.T) {
	deleted := dao.DeleteScheduledUpdate(mtproto.MakePeerChannel(7), []int32{1}, []int32{2})
	r := dao.UpdatesOf([]*mtproto.Update{
		mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: &mtproto.Message{Id: 2, Out: true}}).To_Update(),
		deleted,
	}, []*mtproto.User{{Id: 1}}, []*mtproto.Chat{{Id: 7, Creator: true}}, 100)
	got := queueUpdatesOf(r)
	if len(got.GetUpdates()) != 1 || got.GetUpdates()[0] != deleted {
		t.Errorf("updates: %v", got.GetUpdates())
	}
	if len(got.GetUsers()) != 0 || len(got.GetChats()) != 0 {
		t.Errorf("the author's view is left: users %v, chats %v", got.GetUsers(), got.GetChats())
	}
}

func TestRepeatPeriodOf(t *testing.T) {
	one := func(period int32) []*msg.OutboxMessage {
		return []*msg.OutboxMessage{{Message: &mtproto.Message{ScheduleRepeatPeriod: wrapperspb.Int32(period)}}}
	}
	if p, err := repeatPeriodOf(one(7*86400), 100); err != nil || p != 7*86400 {
		t.Errorf("weekly: %d, %v", p, err)
	}
	if p, err := repeatPeriodOf([]*msg.OutboxMessage{{Message: &mtproto.Message{}}}, 100); err != nil || p != 0 {
		t.Errorf("no period: %d, %v", p, err)
	}
	if _, err := repeatPeriodOf(one(1000), 100); err == nil {
		t.Error("a period that is not offered")
	}
	if _, err := repeatPeriodOf(one(86400), dao.ScheduleWhenOnline); err == nil {
		t.Error("a repeat of a message waiting for the peer")
	}
	if _, err := repeatPeriodOf(append(one(86400), one(86400)...), 100); err == nil {
		t.Error("a repeat of several messages")
	}
}
