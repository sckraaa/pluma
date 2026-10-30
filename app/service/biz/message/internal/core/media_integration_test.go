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
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/message/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var (
	itDB    *testinfra.MySQL
	itRedis *testinfra.Redis
	itSvc   *svc.ServiceContext
	itRaw   *sql.DB
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var (
		wg   sync.WaitGroup
		errs = make([]error, 2)
	)
	wg.Add(2)
	go func() { defer wg.Done(); itDB, errs[0] = testinfra.StartMySQL(ctx) }()
	go func() { defer wg.Done(); itRedis, errs[1] = testinfra.StartRedis(ctx) }()
	wg.Wait()

	code := 1
	if err := errors.Join(errs...); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else if itRaw, err = sql.Open("mysql", itDB.DSN()); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		itSvc = svc.NewServiceContext(config.Config{Mysql: itDB.Config(), Cache: itRedis.CacheConf()}, nil)
		code = m.Run()
		_ = itRaw.Close()
	}
	if itDB != nil {
		_ = itDB.Terminate(ctx)
	}
	if itRedis != nil {
		_ = itRedis.Terminate(ctx)
	}
	os.Exit(code)
}

// TestPhotoVideo: the "Photos and videos" filter lists and counts the
// photos and videos of the dialog that are not deleted, newest first; the
// photo and the video filters count their own type.
func TestPhotoVideo(t *testing.T) {
	const user, peer = int64(2001), int64(2002)
	did := mtproto.MakeDialogId(user, mtproto.PEER_USER, peer)
	types := []int32{
		mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_EMPTY, mtproto.MEDIA_VIDEOS_ONLY, mtproto.MEDIA_FILE,
		mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_VIDEOS_ONLY,
	}
	for i, mt := range types {
		if _, err := itRaw.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_filter_type, message_data, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?)",
			user, i+1, did.A, did.B, int64(user*100+int64(i)), peer, mtproto.PEER_USER, peer, mt, i == 5); err != nil {
			t.Fatal(err)
		}
	}

	c := New(context.Background(), itSvc)
	var ids []int32
	for offset := int32(math.MaxInt32); ; {
		r, err := c.MessageSearchByMediaType(&message.TLMessageSearchByMediaType{
			UserId: user, PeerType: mtproto.PEER_USER, PeerId: peer, MediaType: mtproto.MEDIA_PHOTOVIDEO, Offset: offset, Limit: 2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.GetBoxList()) == 0 || len(ids) > len(types) {
			break
		}
		for _, b := range r.GetBoxList() {
			ids = append(ids, b.GetMessageId())
		}
		offset = ids[len(ids)-1]
	}
	if want := []int32{7, 5, 3, 1}; !slices.Equal(ids, want) {
		t.Errorf("photos and videos %v, want %v", ids, want)
	}

	for mt, want := range map[int32]int32{mtproto.MEDIA_PHOTOVIDEO: 4, mtproto.MEDIA_PHOTOS_ONLY: 2, mtproto.MEDIA_VIDEOS_ONLY: 2} {
		n, err := c.MessageGetSearchCounter(&message.TLMessageGetSearchCounter{UserId: user, PeerType: mtproto.PEER_USER, PeerId: peer, MediaType: mt})
		if err != nil {
			t.Fatal(err)
		}
		if n.GetV() != want {
			t.Errorf("counter of media type %d: %d, want %d", mt, n.GetV(), want)
		}
	}
}
