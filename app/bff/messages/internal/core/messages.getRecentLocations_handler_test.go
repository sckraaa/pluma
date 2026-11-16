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
	"math"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const locNow = int32(1_800_000_000)

func liveLocationMessage(id, date, period int32) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id:   id,
		Date: date,
		Media: mtproto.MakeTLMessageMediaGeoLive(&mtproto.MessageMedia{
			Geo:    mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 55.75, Long: 37.62}).To_GeoPoint(),
			Period: period,
		}).To_MessageMedia(),
	}).To_Message()
}

// pagedLocations serves the candidates (newest first) as the box and the
// channel do: below the id offset, recentLocationsPage at a time. It counts
// the pages read.
func pagedLocations(all []locationCandidate, pages *int) func(offset int32) ([]locationCandidate, error) {
	return func(offset int32) ([]locationCandidate, error) {
		*pages++
		var page []locationCandidate
		for _, c := range all {
			if c.message.GetId() < offset && len(page) < recentLocationsPage {
				page = append(page, c)
			}
		}
		return page, nil
	}
}

func locationIds(messages []*mtproto.Message) []int32 {
	out := make([]int32, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.GetId())
	}
	return out
}

// One message per sender, his newest live one, newest first; an ended
// location, a plain location and an older one of the same sender are left
// out; the indefinite period is live however old the message.
func TestPickRecentLocations(t *testing.T) {
	plain := mtproto.MakeTLMessage(&mtproto.Message{Id: 9, Date: locNow - 10, Media: mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{
		Geo: mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 1, Long: 2}).To_GeoPoint(),
	}).To_MessageMedia()}).To_Message()
	all := []locationCandidate{
		{sender: 1, message: liveLocationMessage(10, locNow-60, 900)},            // alice, newest
		{sender: 3, message: plain},                                              // carl, not live media
		{sender: 2, message: liveLocationMessage(8, locNow-1000, 900)},           // bob, ended
		{sender: 1, message: liveLocationMessage(7, locNow-120, 900)},            // alice again
		{sender: 2, message: liveLocationMessage(6, locNow-500, 3600)},           // bob, live
		{sender: 4, message: liveLocationMessage(5, locNow-3*86400, 0x7fffffff)}, // dan, indefinite
		{sender: -50, message: liveLocationMessage(4, locNow-10, 60)},            // a channel
		{sender: 5, message: liveLocationMessage(3, locNow-3*86400, 3*86400-10)}, // ended just now
	}
	var pages int
	got, err := pickRecentLocations(100, locNow, pagedLocations(all, &pages))
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{10, 6, 5, 4}; !slices.Equal(locationIds(got), want) {
		t.Errorf("picked %v, want %v", locationIds(got), want)
	}

	pages = 0
	if got, _ = pickRecentLocations(2, locNow, pagedLocations(all, &pages)); !slices.Equal(locationIds(got), []int32{10, 6}) {
		t.Errorf("limit 2: picked %v, want [10 6]", locationIds(got))
	}
}

// The scan reads every page of the last day; past it, it stops after
// recentLocationsScanMax messages, so an indefinite location further back
// is not found, and an error of a page is the answer's.
func TestPickRecentLocationsDepth(t *testing.T) {
	var recent, old []locationCandidate
	id := int32(5000)
	for i := 0; i < 3*recentLocationsPage; i++ {
		// One sender's moving updates of the last hour: all but one ended.
		recent = append(recent, locationCandidate{sender: 1, message: liveLocationMessage(id, locNow-3600, 60)})
		id--
	}
	for i := 0; i < 4*recentLocationsPage; i++ {
		old = append(old, locationCandidate{sender: int64(100 + i), message: liveLocationMessage(id, locNow-2*86400, 60)})
		id--
	}
	lost := locationCandidate{sender: 2, message: liveLocationMessage(id, locNow-2*86400, 0x7fffffff)}
	var pages int
	got, err := pickRecentLocations(100, locNow, pagedLocations(append(append(recent, old...), lost), &pages))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || pages != recentLocationsScanMax/recentLocationsPage {
		t.Errorf("picked %v in %d pages, want none in %d", locationIds(got), pages, recentLocationsScanMax/recentLocationsPage)
	}

	// Within the last day there is no bound: a live one 700 messages back
	// is found.
	var day []locationCandidate
	id = 5000
	for i := 0; i < 7*recentLocationsPage; i++ {
		day = append(day, locationCandidate{sender: 1, message: liveLocationMessage(id, locNow-3600, 60)})
		id--
	}
	day = append(day, locationCandidate{sender: 2, message: liveLocationMessage(id, locNow-3600, 7200)})
	pages = 0
	if got, _ = pickRecentLocations(100, locNow, pagedLocations(day, &pages)); !slices.Equal(locationIds(got), []int32{id}) {
		t.Errorf("a day back: picked %v, want [%d]", locationIds(got), id)
	}

	failed := errors.New("query failed")
	if _, err = pickRecentLocations(100, locNow, func(int32) ([]locationCandidate, error) { return nil, failed }); !errors.Is(err, failed) {
		t.Errorf("a failed page: %v, want %v", err, failed)
	}
	// A page that does not go back ends the scan.
	stuck := []locationCandidate{{sender: 1, message: liveLocationMessage(math.MaxInt32, locNow, 60)}}
	for len(stuck) < recentLocationsPage {
		stuck = append(stuck, stuck[0])
	}
	calls := 0
	if _, err = pickRecentLocations(100, locNow, func(int32) ([]locationCandidate, error) { calls++; return stuck, nil }); err != nil || calls != 1 {
		t.Errorf("a page that does not go back: %v after %d calls", err, calls)
	}
}

func TestLiveLocationNow(t *testing.T) {
	for _, c := range []struct {
		m    *mtproto.Message
		want bool
	}{
		{liveLocationMessage(1, locNow-899, 900), true},
		{liveLocationMessage(1, locNow-900, 900), false},
		{liveLocationMessage(1, locNow-10*365*86400, 0x7fffffff), true},
		{liveLocationMessage(1, locNow, 0), false},
		{mtproto.MakeTLMessageService(&mtproto.Message{Id: 1, Date: locNow}).To_Message(), false},
	} {
		if got := liveLocationNow(c.m, locNow); got != c.want {
			t.Errorf("date %d period %d: live %v, want %v", c.m.GetDate(), c.m.GetMedia().GetPeriod(), got, c.want)
		}
	}
}

// The hash changes with an edit of a message, which keeps its id, and with
// the order; it is that of the offsets algorithm over id and edit_date.
func TestRecentLocationsHash(t *testing.T) {
	a, b := liveLocationMessage(10, locNow, 900), liveLocationMessage(6, locNow, 900)
	h := recentLocationsHash([]*mtproto.Message{a, b})
	if h == 0 || h != recentLocationsHash([]*mtproto.Message{liveLocationMessage(10, locNow, 900), liveLocationMessage(6, locNow, 900)}) {
		t.Fatalf("hash %d is not stable", h)
	}
	if recentLocationsHash([]*mtproto.Message{b, a}) == h {
		t.Error("the hash does not change with the order")
	}
	a.EditDate = &wrapperspb.Int32Value{Value: locNow + 5}
	if recentLocationsHash([]*mtproto.Message{a, b}) == h {
		t.Error("the hash does not change with an edit")
	}
	// The algorithm by hand: id 10, edit_date 0.
	var want uint64
	for _, v := range []uint64{10, 0} {
		want ^= want >> 21
		want ^= want << 35
		want ^= want >> 4
		want += v
	}
	if got := recentLocationsHash([]*mtproto.Message{liveLocationMessage(10, locNow, 900)}); got != int64(want) {
		t.Errorf("hash %d, want %d", got, int64(want))
	}
	if recentLocationsHash(nil) != 0 {
		t.Error("the hash of no messages is not 0")
	}
}

func TestGetRecentLocationsBot(t *testing.T) {
	peer := mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 8}).To_InputPeer()
	if _, err := noiseCore(true).MessagesGetRecentLocations(&mtproto.TLMessagesGetRecentLocations{Peer: peer, Limit: 10}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("a bot: %v, want BOT_METHOD_INVALID", err)
	}
	if _, err := noiseCore(false).MessagesGetRecentLocations(&mtproto.TLMessagesGetRecentLocations{Peer: mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer()}); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("an empty peer: %v, want PEER_ID_INVALID", err)
	}
}
