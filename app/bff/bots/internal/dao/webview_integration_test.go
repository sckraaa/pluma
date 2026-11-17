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

package dao

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"

	_ "github.com/go-sql-driver/mysql"
)

// RecentBotMarkups reads the markups of the bot's latest messages in the
// user's box, newest first, within the limit: the user's own messages, the
// deleted ones and other chats do not count.
func TestRecentBotMarkupsOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	d := &Dao{db: sqlx.NewMySQL(&conf)}

	const user, bot, other = int64(1000001), int64(2000001), int64(2000002)
	keyboard := func(text string) *mtproto.ReplyMarkup {
		return mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
				mtproto.MakeTLKeyboardButtonSimpleWebView(&mtproto.KeyboardButton{Text: text, Url: "https://shop.example/"}).To_KeyboardButton(),
			}}).To_KeyboardButtonRow(),
		}}).To_ReplyMarkup()
	}
	boxId := int32(0)
	put := func(sender, peer int64, markup *mtproto.ReplyMarkup, deleted bool) {
		t.Helper()
		boxId++
		data, err := jsonx.Marshal(mtproto.MakeTLMessage(&mtproto.Message{Id: boxId, Message: "m", ReplyMarkup: markup}).To_Message())
		if err != nil {
			t.Fatal(err)
		}
		did := mtproto.MakeDialogId(user, mtproto.PEER_USER, peer)
		if _, err = d.db.Exec(ctx,
			"INSERT INTO messages (id, user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_data, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			int64(boxId), user, boxId, did.A, did.B, int64(boxId), sender, mtproto.PEER_USER, peer, string(data), deleted); err != nil {
			t.Fatal(err)
		}
	}
	put(bot, bot, keyboard("Oldest"), false)
	put(bot, bot, nil, false)
	put(user, bot, keyboard("Mine"), false)
	put(other, other, keyboard("Other bot"), false)
	put(bot, bot, keyboard("Deleted"), true)
	put(bot, bot, keyboard("Newest"), false)

	texts := func(limit int) []string {
		t.Helper()
		markups, err := d.RecentBotMarkups(ctx, user, bot, limit)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range markups {
			out = append(out, m.GetRows()[0].GetButtons()[0].GetText())
		}
		return out
	}
	if got := texts(50); len(got) != 2 || got[0] != "Newest" || got[1] != "Oldest" {
		t.Errorf("markups %v, want Newest, Oldest", got)
	}
	// The limit counts messages, the one without markup too.
	if got := texts(2); len(got) != 1 || got[0] != "Newest" {
		t.Errorf("markups of the latest two %v, want Newest", got)
	}
}
