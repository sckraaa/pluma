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
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestInlineGameScoresOnMySQL: Ann sent the bot's game via the bot to her
// private chat with Bob (4 in her box, 7 in his); Bob pressed it, so the bot
// names the game by Bob's copy. Both play: the table is of their chat, each
// score message answers the player's own copy of the game, and the bot
// reads the table by the same id. Cat is not in the chat; another bot's
// calls find no game of theirs.
func TestInlineGameScoresOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	store := botstore.New(sqlx.NewMySQL(&conf))

	bot, _, err := store.Create(ctx, "jump_inline_bot", "Jump", 0)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := store.Create(ctx, "other_inline_bot", "Other", 0)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.AddGame(ctx, botstore.Game{BotId: bot, ShortName: "jump", Title: "Jump", PhotoId: 5})
	if err != nil {
		t.Fatal(err)
	}

	const ann, bob, cat = int64(1000021), int64(1000022), int64(1000023)
	media := mtproto.MakeTLMessageMediaGame(&mtproto.MessageMedia{Game: mtproto.MakeTLGame(&mtproto.Game{Id: game.Id, AccessHash: game.AccessHash, ShortName: "jump", Title: "Jump"}).To_Game()}).To_MessageMedia()
	private := mtproto.MakeDialogId(ann, mtproto.PEER_USER, bob)
	boxes := &gameBoxes{boxes: map[gameBoxKey]*mtproto.MessageBox{}, copies: map[gameBoxKey]int64{}}
	for user, id := range map[int64]int32{ann: 4, bob: 7} {
		boxes.put(user, id, 1, &mtproto.MessageBox{SenderUserId: ann, DialogId1: private.A, DialogId2: private.B, DialogMessageId: 1,
			Message: mtproto.MakeTLMessage(&mtproto.Message{FromId: mtproto.MakePeerUser(ann), PeerId: mtproto.MakePeerUser(bob), ViaBotId: wrapperspb.Int64(bot), Media: media}).To_Message()})
	}
	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{Dao: &dao.Dao{
		UserClient:    gameUsers{bots: map[int64]bool{bot: true, other: true}},
		MessageClient: boxes,
		MsgClient:     sent,
		Bots:          store,
	}}
	as := func(id int64) *BotsCore {
		return &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: id, IsBot: true}}
	}
	byBob := inlineMessageId(mtproto.MakeUserPeerUtil(ann), bob, 7)
	input := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: 1}).To_InputUser()
	}
	setScore := func(c *BotsCore, player int64, score int32, edit bool) error {
		_, err := c.MessagesSetInlineGameScore(&mtproto.TLMessagesSetInlineGameScore{Id: byBob, UserId: input(player), Score: score, EditMessage: edit})
		return err
	}

	b := as(bot)
	if err = setScore(b, bob, 10, true); err != nil {
		t.Fatalf("Bob's score: %v", err)
	}
	if err = setScore(b, ann, 30, true); err != nil {
		t.Fatalf("Ann's score: %v", err)
	}
	if len(sent.sent) != 2 {
		t.Fatalf("%d messages of the scores", len(sent.sent))
	}
	for _, s := range sent.sent {
		m := s.GetMessage()[0].GetMessage()
		want := map[int64]struct {
			peer  int64
			reply int32
		}{bob: {ann, 7}, ann: {bob, 4}}[s.UserId]
		if s.PeerType != mtproto.PEER_USER || s.PeerId != want.peer || m.GetAction().GetGameId() != game.Id ||
			m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != want.reply {
			t.Errorf("the score message of %d: %v", s.UserId, s)
		}
	}
	if err = setScore(b, bob, 5, false); !errors.Is(err, mtproto.ErrBotScoreNotModified) {
		t.Errorf("a lesser score: %v", err)
	}
	for name, err := range map[string]error{
		"a player of no chat": setScore(b, cat, 1, false),
		"another bot":         setScore(as(other), bob, 1, false),
		"a bot as the player": setScore(b, other, 1, false),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	r, err := b.MessagesGetInlineGameHighScores(&mtproto.TLMessagesGetInlineGameHighScores{Id: byBob, UserId: input(bob)})
	if err != nil {
		t.Fatal(err)
	}
	if s := r.GetScores(); len(s) != 2 || s[0].GetUserId() != ann || s[0].GetScore() != 30 || s[1].GetUserId() != bob || s[1].GetPos() != 2 {
		t.Errorf("the table: %v", s)
	}
	// The sender's id of the game names the same table.
	byAnn := inlineMessageId(mtproto.MakeUserPeerUtil(bob), ann, 4)
	if r, err = b.MessagesGetInlineGameHighScores(&mtproto.TLMessagesGetInlineGameHighScores{Id: byAnn, UserId: input(ann)}); err != nil || len(r.GetScores()) != 2 {
		t.Errorf("by Ann's copy: %v %v", r, err)
	}
	if _, err = as(other).MessagesGetInlineGameHighScores(&mtproto.TLMessagesGetInlineGameHighScores{Id: byBob, UserId: input(bob)}); !errors.Is(err, mtproto.ErrMessageIdInvalid) {
		t.Errorf("another bot's table: %v", err)
	}
}
