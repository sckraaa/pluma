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

package core

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

var channelSeq int64

// channelRow writes a channel with pts last changed at date2 and the
// user's participant row in state.
func channelRow(t *testing.T, userId int64, pts int32, date2 int64, state int) int64 {
	t.Helper()
	channelSeq++
	id := 1_500_000_000 + userId%1_000_000*100 + channelSeq
	if _, err := itRaw.Exec("INSERT INTO channels (id, access_hash, creator_user_id, title, megagroup, pts, date, date2) VALUES (?, 1, ?, 't', 1, ?, 0, ?)",
		id, userId, pts, date2); err != nil {
		t.Fatal(err)
	}
	if _, err := itRaw.Exec("INSERT INTO channel_participants (channel_id, user_id, state) VALUES (?, ?, ?)", id, userId, state); err != nil {
		t.Fatal(err)
	}
	return id
}

// tooLongOf lists the updateChannelTooLong of d as id:pts.
func tooLongOf(d *updates.Difference) string {
	var out []string
	for _, u := range d.GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateChannelTooLong {
			out = append(out, fmt.Sprintf("%d:%d", u.GetChannelId(), u.GetPts_FLAGINT32().GetValue()))
		}
	}
	return strings.Join(out, " ")
}

// TestDifferenceChannels: the last page marks the channels the account is
// in that changed since the earlier of the request's date and its
// cursor's date, less MaxRunTime, each with its pts (ADR 0034, p. 6).
func TestDifferenceChannels(t *testing.T) {
	margin := int64(userbox.MaxRunTime / time.Second)
	t.Run("by the date", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 0)
		changed := channelRow(t, u, 7, 1000-margin, 0)
		channelRow(t, u, 9, 1000-margin-1, 0) // before the margin
		channelRow(t, u, 4, 2000, 1)          // left
		d := differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: 1000})
		if d.GetPredicateName() != updates.Predicate_difference {
			t.Fatalf("%s: channels alone still make a difference", d.GetPredicateName())
		}
		if got, want := tooLongOf(d), fmt.Sprintf("%d:7", changed); got != want {
			t.Errorf("channels %q, want %q", got, want)
		}
		if d = differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: 5000}); d.GetPredicateName() != updates.Predicate_differenceEmpty {
			t.Errorf("after the change: %s %q", d.GetPredicateName(), tooLongOf(d))
		}
		if d = differenceWith(t, &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: math.MaxInt32}); d.GetPredicateName() != updates.Predicate_differenceEmpty {
			t.Errorf("pts only: %s %q", d.GetPredicateName(), tooLongOf(d))
		}
	})
	t.Run("the cursor's date finds what a moved date misses", func(t *testing.T) {
		u := newUser()
		seqState(t, u, userbox.BasePts, 0)
		changed := channelRow(t, u, 3, 1000, 0)
		if err := itSvc.Dao.SetSeqCursor(context.Background(), u, key, 0, 900); err != nil {
			t.Fatal(err)
		}
		in := &updates.TLUpdatesGetDifferenceV2{AuthKeyId: key, UserId: u, Pts: userbox.BasePts, Date: 5000}
		d := differenceWith(t, in)
		if got, want := tooLongOf(d), fmt.Sprintf("%d:3", changed); got != want {
			t.Fatalf("channels %q, want %q", got, want)
		}
		// The difference gave the state now: the cursor's date moved, and
		// the same request finds nothing more.
		cursor, _, err := itSvc.Dao.SeqCursor(context.Background(), u, key)
		if err != nil {
			t.Fatal(err)
		}
		if cursor.Date < time.Now().Unix()-60 {
			t.Errorf("cursor date %d not refreshed", cursor.Date)
		}
		if d = differenceWith(t, in); d.GetPredicateName() != updates.Predicate_differenceEmpty {
			t.Errorf("again: %s %q", d.GetPredicateName(), tooLongOf(d))
		}
	})
	t.Run("slices: on the last page", func(t *testing.T) {
		u := newUser()
		fixture(t, u, 0, 2, 3)
		seqState(t, u, 3, 0)
		changed := channelRow(t, u, 5, 1000, 0)
		defer func(n int32) { sliceRows = n }(sliceRows)
		sliceRows = 1
		in := &updates.TLUpdatesGetDifferenceV2{UserId: u, Pts: userbox.BasePts, Date: 1000}
		if d := differenceWith(t, in); d.GetPredicateName() != updates.Predicate_differenceSlice || tooLongOf(d) != "" {
			t.Fatalf("first page: %s %q", d.GetPredicateName(), tooLongOf(d))
		}
		in.Pts = 2
		if d := differenceWith(t, in); tooLongOf(d) != fmt.Sprintf("%d:5", changed) {
			t.Errorf("last page: %q", tooLongOf(d))
		}
	})
}
