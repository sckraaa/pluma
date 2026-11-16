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

//go:build integration

package core

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
)

const geoLiveMigration = "migrate-20261119-geo-live-filter.sql"

// geoMessageData is message_data as msg stores a message (jsonx).
func geoMessageData(t *testing.T, id int32, media *mtproto.MessageMedia) string {
	t.Helper()
	m := mtproto.MakeTLMessage(&mtproto.Message{Id: id, Date: 1_800_000_000, Media: media}).To_Message()
	data, err := jsonx.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestGeoLiveFilter: the migration gives the stored live locations of the
// boxes and of the channels message_filter_type 14 and nothing else, and
// running it again changes nothing; message.searchByMediaType(14) finds
// them; an edit writes the media type of the new media.
func TestGeoLiveFilter(t *testing.T) {
	ctx := context.Background()
	const user, peer, channelId = int64(3001), int64(3002), int64(3003)
	did := mtproto.MakeDialogId(user, mtproto.PEER_USER, peer)
	geo := mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 55.75, Long: 37.62}).To_GeoPoint()
	live := mtproto.MakeTLMessageMediaGeoLive(&mtproto.MessageMedia{Geo: geo, Period: 900}).To_MessageMedia()
	plain := mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{Geo: geo}).To_MessageMedia()

	// Rows written before the type existed: -1 (MEDIA_EMPTY) for both.
	for i, media := range []*mtproto.MessageMedia{live, plain, live} {
		id := int32(i + 1)
		if _, err := itRaw.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_filter_type, message_data, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)",
			user, id, did.A, did.B, int64(user*100)+int64(id), peer, mtproto.PEER_USER, peer, mtproto.MEDIA_EMPTY, geoMessageData(t, id, media)); err != nil {
			t.Fatal(err)
		}
	}
	for i, media := range []*mtproto.MessageMedia{live, plain} {
		if _, err := itRaw.Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_filter_type, message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 0)",
			channelId, i+1, peer, i+1, mtproto.MEDIA_EMPTY, geoMessageData(t, int32(i+1), media)); err != nil {
			t.Fatal(err)
		}
	}

	path, err := testinfra.SchemaFile(geoLiveMigration)
	if err != nil {
		t.Fatal(err)
	}
	boxTypes := func() []int32 {
		var out []int32
		rows, err := itRaw.Query("SELECT message_filter_type FROM messages WHERE user_id = ? ORDER BY user_message_box_id", user)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var v int32
			if err = rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			out = append(out, v)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for run := range 2 {
		if err = itDB.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got, want := boxTypes(), []int32{mtproto.MEDIA_GEO_LIVE, mtproto.MEDIA_EMPTY, mtproto.MEDIA_GEO_LIVE}; !slices.Equal(got, want) {
			t.Errorf("run %d: box media types %v, want %v", run, got, want)
		}
		var liveType, plainType int32
		if err = itRaw.QueryRow("SELECT (SELECT message_filter_type FROM channel_messages WHERE channel_id = ? AND id = 1), (SELECT message_filter_type FROM channel_messages WHERE channel_id = ? AND id = 2)", channelId, channelId).Scan(&liveType, &plainType); err != nil {
			t.Fatal(err)
		}
		if liveType != mtproto.MEDIA_GEO_LIVE || plainType != mtproto.MEDIA_EMPTY {
			t.Errorf("run %d: channel media types %d and %d, want %d and %d", run, liveType, plainType, mtproto.MEDIA_GEO_LIVE, mtproto.MEDIA_EMPTY)
		}
		var keys int
		if err = itRaw.QueryRow("SELECT COUNT(DISTINCT TABLE_NAME, INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND ((TABLE_NAME = 'messages' AND INDEX_NAME = 'user_dialog_filter') OR (TABLE_NAME = 'channel_messages' AND INDEX_NAME = 'filter'))").Scan(&keys); err != nil {
			t.Fatal(err)
		}
		if keys != 2 {
			t.Errorf("run %d: %d of the 2 keys", run, keys)
		}
	}

	c := New(ctx, itSvc)
	r, err := c.MessageSearchByMediaType(&message.TLMessageSearchByMediaType{
		UserId: user, PeerType: mtproto.PEER_USER, PeerId: peer, MediaType: mtproto.MEDIA_GEO_LIVE, Offset: math.MaxInt32, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	var found []int32
	for _, b := range r.GetBoxList() {
		found = append(found, b.GetMessageId())
		if b.GetMessage().GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGeoLive || b.GetMessage().GetMedia().GetPeriod() != 900 {
			t.Errorf("box %d: media %v", b.GetMessageId(), b.GetMessage().GetMedia())
		}
	}
	if want := []int32{3, 1}; !slices.Equal(found, want) {
		t.Errorf("live locations %v, want %v", found, want)
	}

	// An edit that replaces the live location writes the new type.
	photo := mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()
	edited := mtproto.MakeTLMessage(&mtproto.Message{Id: 3, Media: photo}).To_Message()
	data, err := jsonx.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = itSvc.Dao.MessagesDAO.UpdateEditMessage(ctx, string(data), "", mtproto.GetMediaType(edited), user, 3); err != nil {
		t.Fatal(err)
	}
	if got, want := boxTypes(), []int32{mtproto.MEDIA_GEO_LIVE, mtproto.MEDIA_EMPTY, mtproto.MEDIA_PHOTOS_ONLY}; !slices.Equal(got, want) {
		t.Errorf("after the edit: box media types %v, want %v", got, want)
	}
}
