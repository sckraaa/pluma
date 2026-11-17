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
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// muteEvents are userId's rows of channel_stats_mute_events of the channel,
// their mute_until in order.
func muteEvents(t *testing.T, channelId, userId int64) []int32 {
	t.Helper()
	rows, err := itRaw.Query("SELECT mute_until FROM channel_stats_mute_events WHERE channel_id = ? AND user_id = ? ORDER BY id", channelId, userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var m int32
		if err = rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// newChannel adds a channel, a broadcast one or a supergroup, of creator.
func newChannel(t *testing.T, creator int64, broadcast bool) int64 {
	t.Helper()
	id := 1<<41 + itSeq.Add(1)
	if _, err := itRaw.Exec("INSERT INTO channels (id, access_hash, creator_user_id, title, broadcast, megagroup) VALUES (?, ?, ?, 't', ?, ?)",
		id, id*7, creator, broadcast, !broadcast); err != nil {
		t.Fatal(err)
	}
	return id
}

// muteSettings are settings with mute_until set, 0 included.
func muteSettings(muteUntil int32) *mtproto.PeerNotifySettings {
	return mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: wrapperspb.Int32(muteUntil)}).To_PeerNotifySettings()
}

// TestMuteEvents: a change of the muting in effect of a channel is recorded
// for its statistics (task 103) with what it came to; a change that keeps it
// is not. A channel without its own mute_until follows the settings of all
// channels, a supergroup those of all groups; a change of those records
// nothing. The reset unmutes the channels muted.
func TestMuteEvents(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Mia")
	ch := newChannel(t, u, true)
	group := newChannel(t, u, false)
	set := func(peerType int32, peerId int64, s *mtproto.PeerNotifySettings) {
		t.Helper()
		if _, err := c.UserSetNotifySettings(&user.TLUserSetNotifySettings{UserId: u, PeerType: peerType, PeerId: peerId, Settings: s}); err != nil {
			t.Fatal(err)
		}
	}
	noMute := mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{ShowPreviews: mtproto.BoolTrue}).To_PeerNotifySettings()
	now := int32(time.Now().Unix())
	hour, twoHours := now+3600, now+7200

	set(mtproto.PEER_CHANNEL, ch, muteSettings(math.MaxInt32))
	set(mtproto.PEER_CHANNEL, ch, muteSettings(math.MaxInt32)) // the same again
	set(mtproto.PEER_CHANNEL, ch, muteSettings(0))
	set(mtproto.PEER_CHANNEL, ch, muteSettings(hour))
	set(mtproto.PEER_CHANNEL, ch, muteSettings(twoHours))
	set(mtproto.PEER_CHANNEL, ch, muteSettings(now-10)) // a time that passed: unmuted
	set(mtproto.PEER_CHANNEL, ch, muteSettings(0))      // unmuted already
	want := fmt.Sprint([]int32{math.MaxInt32, 0, hour, twoHours, 0})
	if got := fmt.Sprint(muteEvents(t, ch, u)); got != want {
		t.Fatalf("events %s, want %s", got, want)
	}

	// The settings of all channels mute the channel without its own, and
	// record nothing themselves.
	set(mtproto.PEER_BROADCASTS, 0, muteSettings(math.MaxInt32))
	if got := len(muteEvents(t, ch, u)); got != 5 {
		t.Fatalf("the settings of all channels: %d events", got)
	}
	set(mtproto.PEER_CHANNEL, ch, noMute)
	set(mtproto.PEER_CHANNEL, group, noMute)
	want = fmt.Sprint([]int32{math.MaxInt32, 0, hour, twoHours, 0, math.MaxInt32})
	if got := fmt.Sprint(muteEvents(t, ch, u)); got != want {
		t.Fatalf("events %s, want %s", got, want)
	}
	if got := muteEvents(t, group, u); len(got) != 0 {
		t.Fatalf("a supergroup follows the settings of the groups: %v", got)
	}
	set(mtproto.PEER_CHANNEL, group, muteSettings(hour))

	// Another peer records nothing.
	set(mtproto.PEER_USER, u, muteSettings(math.MaxInt32))

	if _, err := c.UserResetNotifySettings(&user.TLUserResetNotifySettings{UserId: u}); err != nil {
		t.Fatal(err)
	}
	want = fmt.Sprint([]int32{math.MaxInt32, 0, hour, twoHours, 0, math.MaxInt32, 0})
	if got := fmt.Sprint(muteEvents(t, ch, u)); got != want {
		t.Fatalf("after the reset: events %s, want %s", got, want)
	}
	if got, want := fmt.Sprint(muteEvents(t, group, u)), fmt.Sprint([]int32{hour, 0}); got != want {
		t.Fatalf("the supergroup after the reset: events %s, want %s", got, want)
	}
	if n := count(t, "SELECT COUNT(*) FROM channel_stats_mute_events WHERE user_id = ? AND channel_id NOT IN (?, ?)", u, ch, group); n != 0 {
		t.Fatalf("%d events of other peers", n)
	}
}
