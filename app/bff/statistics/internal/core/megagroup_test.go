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
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// fakeUsers answers user.getMutableUsersV2 as biz user does, with a row for
// each id asked for but those of gone (accounts no longer there).
type fakeUsers struct {
	gone map[int64]bool
}

func (f fakeUsers) UserGetMutableUsersV2(_ context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	out := &mtproto.MutableUsers{}
	for _, id := range in.Id {
		if f.gone[id] {
			continue
		}
		out.Users = append(out.Users, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id * 10, FirstName: "User", UserType: userpb.UserTypeRegular}).To_UserData(),
		}).To_ImmutableUser())
	}
	return out, nil
}

// The messages of a graph of kinds go to the series of their kind; a kind
// the code does not know is an error, never a count in the wrong series.
func TestByKind(t *testing.T) {
	d0 := statsgraph.Day.Bucket(day0)
	b := statsgraph.Span(statsgraph.Day, day0, day0+day)
	got, err := byKind(b, []dao.Count{{B: d0, K: messageText, N: 3}, {B: d0 + 1, K: messageRound, N: 1}, {B: d0 + 1, K: messageOther, N: 2}, {B: d0 + 5, K: messageText, N: 9}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != messageKinds || !reflect.DeepEqual(got[messageText], []int64{3, 0}) || !reflect.DeepEqual(got[messageRound], []int64{0, 1}) || !reflect.DeepEqual(got[messageOther], []int64{0, 2}) {
		t.Errorf("byKind %v", got)
	}
	if _, err := byKind(b, []dao.Count{{B: d0, K: messageKinds, N: 1}}); err == nil {
		t.Errorf("kind %d taken", messageKinds)
	}
}

// The messages of each day go to the day of the week of their week,
// Monday first.
func TestByWeekday(t *testing.T) {
	d0 := statsgraph.Day.Bucket(day0) // a Monday
	weeks := statsgraph.Span(statsgraph.Week, day0+2*day, day0+15*day)
	rows := []dao.Count{
		{B: d0, N: 1},      // Monday of the first week
		{B: d0 + 2, N: 2},  // its Wednesday
		{B: d0 + 6, N: 3},  // its Sunday
		{B: d0 + 7, N: 4},  // Monday of the second week
		{B: d0 + 15, N: 5}, // Tuesday of the third
		{B: d0 + 21, N: 9}, // a week out of the graph
		{B: d0 - 1, N: 9},  // and the Sunday before it
	}
	want := [][]int64{
		{1, 4, 0}, {0, 0, 5}, {2, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {3, 0, 0},
	}
	if got := byWeekday(weeks, rows); !reflect.DeepEqual(got, want) {
		t.Errorf("byWeekday %v, want %v", got, want)
	}
}

// The days of the readers and the admins' actions of a period end today
// and are as many as the period's.
func TestPeriodDayRange(t *testing.T) {
	now := day0 + 40*day + 10*hour
	first, last := periodDayRange(now-periodDays*day, now)
	if last != channelstats.Day(now) || int(last-first+1) != periodDays {
		t.Errorf("days [%d, %d] for today %d", first, last, channelstats.Day(now))
	}
	prevFirst, prevLast := periodDayRange(now-2*periodDays*day, now-periodDays*day)
	if prevLast != first-1 || int(prevLast-prevFirst+1) != periodDays {
		t.Errorf("previous days [%d, %d] before %d", prevFirst, prevLast, first)
	}
}

// The graphs of supergroups zoom from tokens for supergroups alone; the
// graph of actions has no hours to zoom into.
func TestGroupGraphs(t *testing.T) {
	for _, kind := range []string{"group_growth", "group_members", "group_new_members_by_source", "group_messages"} {
		spec, ok := graphs[kind]
		if !ok || spec.kind != megagroupOnly || spec.object {
			t.Errorf("%s: %+v", kind, spec)
		}
		if len(kind) > statsgraph.MaxTokenField {
			t.Errorf("%s: longer than a token holds", kind)
		}
	}
	if _, ok := graphs["group_actions"]; ok {
		t.Errorf("the graph of actions zooms")
	}
}

// The access to the statistics of a supergroup, before anything is read: a
// broadcast channel is MEGAGROUP_REQUIRED, an admin without the right to
// ban CHAT_ADMIN_REQUIRED, and without the key the method is off.
func TestMegagroupAccess(t *testing.T) {
	creator := participant(channelpb.RoleCreator, 0)
	poster := participant(channelpb.RoleAdmin, mtproto.ADMIN_POST_MESSAGES|mtproto.ADMIN_DELETE_MESSAGES)
	for _, c := range []struct {
		name   string
		mc     *channelpb.MutableChannel
		signer bool
		want   error
	}{
		{"broadcast channel", mutable(channelOpts{broadcast: true}, creator), true, mtproto.ErrMegagroupRequired},
		{"admin who does not ban", mutable(channelOpts{}, poster), true, errChatAdminRequired},
		{"stranger to a private group", mutable(channelOpts{}, nil), true, mtproto.ErrChannelPrivate},
		{"off", mutable(channelOpts{}, creator), false, mtproto.ErrEnterpriseIsBlocked},
	} {
		var signer *statsgraph.Signer
		if c.signer {
			signer = newSigner(t)
		}
		core := testCore(t, fakeChannels{mc: c.mc}, signer, day0+10*day)
		core.svcCtx.Users = fakeUsers{}
		if _, err := core.StatsGetMegagroupStats(&mtproto.TLStatsGetMegagroupStats{Channel: inputChannel(hash)}); !sameError(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	core := testCore(t, fakeChannels{err: fmt.Errorf("not to be read")}, nil, day0)
	if _, err := core.StatsGetMegagroupStats(&mtproto.TLStatsGetMegagroupStats{Channel: inputChannel(hash)}); !sameError(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Errorf("off: %v", err)
	}
}
