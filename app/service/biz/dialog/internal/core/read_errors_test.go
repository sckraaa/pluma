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
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dao/mysql_dao"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/svc"
)

// A failed read of the database fails the request: the handlers answer an
// error, not an empty or partial list of chats.

// downCore is a DialogCore whose database refuses every connection (nothing
// listens on port 1).
func downCore() *DialogCore {
	db := sqlx.NewMySQL(&sqlx.Config{DSN: "pluma:pluma@tcp(127.0.0.1:1)/pluma?timeout=1s"})
	return New(context.Background(), &svc.ServiceContext{
		Dao: &dao.Dao{Mysql: &dao.Mysql{DialogsDAO: mysql_dao.NewDialogsDAO(db)}},
	})
}

// getDialogs calls DialogGetDialogs and fails the test on a panic: a
// handler that goes on past a failed read reaches the cache, which a
// DialogCore of downCore does not have.
func getDialogs(t *testing.T, c *DialogCore, in *dialog.TLDialogGetDialogs) (r *dialog.Vector_DialogExt, err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("folder %d, exclude pinned %v: went on past the failed read: %v",
				in.GetFolderId(), mtproto.FromBool(in.GetExcludePinned()), p)
		}
	}()
	return c.DialogGetDialogs(in)
}

func TestGetDialogsFailsOnReadError(t *testing.T) {
	for _, tc := range []struct {
		folderId      int32
		excludePinned bool
	}{
		{0, true}, {0, false}, {1, true}, {1, false},
	} {
		r, err := getDialogs(t, downCore(), &dialog.TLDialogGetDialogs{
			UserId:        7,
			ExcludePinned: mtproto.ToBool(tc.excludePinned),
			FolderId:      tc.folderId,
		})
		if err == nil {
			t.Fatalf("folder %d, exclude pinned %v: got %v and no error", tc.folderId, tc.excludePinned, r.GetDatas())
		}
		if r != nil {
			t.Fatalf("folder %d, exclude pinned %v: got a list with the error", tc.folderId, tc.excludePinned)
		}
	}
}

func TestGetChannelMessageReadParticipantsFailsOnReadError(t *testing.T) {
	r, err := downCore().DialogGetChannelMessageReadParticipants(&dialog.TLDialogGetChannelMessageReadParticipants{
		UserId:    7,
		ChannelId: 9,
		MsgId:     1,
	})
	if err == nil {
		t.Fatalf("got %v and no error", r.GetDatas())
	}
}

func TestGetDialogFolderFailsOnReadError(t *testing.T) {
	r, err := downCore().DialogGetDialogFolder(&dialog.TLDialogGetDialogFolder{UserId: 7, FolderId: 1})
	if err == nil {
		t.Fatalf("got %v and no error", r.GetDatas())
	}
}
