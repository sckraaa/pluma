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

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// gamePicture is a PNG for the picture of a game.
func gamePicture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// sentMessage is the message of a send in its Updates.
func sentMessage(t *testing.T, o mtproto.TLObject) *mtproto.Message {
	t.Helper()
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage, mtproto.Predicate_updateNewChannelMessage:
			return u.GetMessage_MESSAGE()
		}
	}
	t.Fatalf("no message in %v", o)
	return nil
}

// playButton tells whether the message's keyboard starts with the game's
// button.
func playButton(m *mtproto.Message) bool {
	rows := m.GetReplyMarkup().GetRows()
	return m.GetReplyMarkup().GetPredicateName() == mtproto.Predicate_replyInlineMarkup && len(rows) > 0 &&
		len(rows[0].GetButtons()) > 0 && rows[0].GetButtons()[0].GetPredicateName() == mtproto.Predicate_keyboardButtonGame
}

// TestGames: the administrator gives a bot a game (task 106, ADR 0068
// p. 7). The bot sends it with the button that starts it; the user's press
// reaches the bot with the game's short name and the bot's address comes
// back; the bot's setGameScore posts "scored" from the player and keeps the
// best score; the table has the player. The user shares the game by its
// link and sends it as an inline result: both go via the bot, with the
// button.
func TestGames(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phone, username)
	getState(ctx, t, bot)
	botId := bot.self.GetId()

	picture := filepath.Join(t.TempDir(), "jump.png")
	if err := os.WriteFile(picture, gamePicture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := fmt.Sprintf("/tmp/game-%d.png", uint64(randomID()))
	stack(t, "cp", picture, "bff:"+dest)
	defer stack(t, "exec", "-T", "bff", "rm", "-f", dest)
	admin(t, "bot", "game", "add", username, "jump", "-title", "Jump", "-description", "Up and up", "-photo", dest)

	toBot := resolveBot(ctx, t, alice, username)
	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	byName := func(c *user, bot *mtproto.InputUser, name string) *mtproto.InputMedia {
		return &mtproto.InputMedia{
			Constructor: c.c.Constructor(mtproto.Predicate_inputMediaGame),
			Id_INPUTGAME: &mtproto.InputGame{
				Constructor: c.c.Constructor(mtproto.Predicate_inputGameShortName),
				BotId:       bot,
				ShortName:   name,
			},
		}
	}
	sendMedia := func(c *user, peer *mtproto.InputPeer, media *mtproto.InputMedia) (mtproto.TLObject, error) {
		return c.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
			Constructor: c.c.Constructor(mtproto.Predicate_messages_sendMedia),
			Peer:        peer, Media: media, RandomId: randomID(),
		})
	}
	botSelf := &mtproto.InputUser{Constructor: bot.c.Constructor(mtproto.Predicate_inputUserSelf)}

	// The bot sends its game; an unknown one is no media.
	if _, err := sendMedia(bot, toAlice, byName(bot, botSelf, "fly")); !strings.Contains(fmt.Sprint(err), "MEDIA_INVALID") {
		t.Fatalf("an unknown game: %v", err)
	}
	o, err := sendMedia(bot, toAlice, byName(bot, botSelf, "jump"))
	if err != nil {
		t.Fatalf("messages.sendMedia of the game: %v", err)
	}
	botMsgId := sentMessage(t, o).GetId()
	var msgId int32
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame {
			return false
		}
		g := m.GetMedia().GetGame()
		if g.GetShortName() != "jump" || g.GetTitle() != "Jump" || g.GetDescription() != "Up and up" || g.GetPhoto().GetPredicateName() != mtproto.Predicate_photo || !playButton(m) {
			t.Fatalf("the game message: %v", m)
		}
		msgId = m.GetId()
		return true
	})

	// The press of the game's button.
	press := make(chan error, 1)
	var answer *mtproto.Messages_BotCallbackAnswer
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetBotCallbackAnswer{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getBotCallbackAnswer),
			Game:        true, Peer: toBot, MsgId: msgId,
		})
		if err == nil {
			answer = o.(interface {
				To_Messages_BotCallbackAnswer() *mtproto.Messages_BotCallbackAnswer
			}).To_Messages_BotCallbackAnswer()
		}
		press <- err
	}()
	var queryId int64
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotCallbackQuery, func(u *mtproto.Update) bool {
		if u.GetGameShortName().GetValue() != "jump" || u.GetData_FLAGBYTES() != nil || u.GetMsgId_INT32() != botMsgId {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetBotCallbackAnswer{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setBotCallbackAnswer),
		QueryId:     queryId, Url: wrapperspb.String("https://games.example/jump"),
	}); err != nil {
		t.Fatalf("messages.setBotCallbackAnswer: %v", err)
	}
	if err = <-press; err != nil {
		t.Fatalf("messages.getBotCallbackAnswer of the game: %v", err)
	}
	if answer.GetUrl().GetValue() != "https://games.example/jump" {
		t.Fatalf("the game's address: %v", answer)
	}

	// The score.
	setScore := func(score int32) error {
		_, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSetGameScore{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_setGameScore),
			EditMessage: true, Peer: toAlice, Id: botMsgId,
			UserId: inputUserOfPeer(bot.c, toAlice), Score: score,
		})
		return err
	}
	if err = setScore(100); err != nil {
		t.Fatalf("messages.setGameScore: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		a := m.GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionGameScore && a.GetScore() == 100 &&
			m.GetFromId().GetUserId() == alice.self.GetId() && m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() == msgId
	})
	if err = setScore(50); !strings.Contains(fmt.Sprint(err), "BOT_SCORE_NOT_MODIFIED") {
		t.Fatalf("a lesser score: %v", err)
	}
	o, err = bot.c.Invoke(ctx, &mtproto.TLMessagesGetGameHighScores{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_getGameHighScores),
		Peer:        toAlice, Id: botMsgId, UserId: inputUserOfPeer(bot.c, toAlice),
	})
	if err != nil {
		t.Fatalf("messages.getGameHighScores: %v", err)
	}
	table := o.(interface {
		To_Messages_HighScores() *mtproto.Messages_HighScores
	}).To_Messages_HighScores()
	if s := table.GetScores(); len(s) != 1 || s[0].GetPos() != 1 || s[0].GetUserId() != alice.self.GetId() || s[0].GetScore() != 100 ||
		len(table.GetUsers()) != 1 {
		t.Fatalf("the table: %v", table)
	}

	// Alice shares the game by its link: it goes via the bot, with the
	// button.
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	if o, err = sendMedia(alice, self, byName(alice, inputUserOfPeer(alice.c, toBot), "jump")); err != nil {
		t.Fatalf("the game shared by its link: %v", err)
	}
	if m := sentMessage(t, o); m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || m.GetViaBotId().GetValue() != botId || !playButton(m) {
		t.Fatalf("the shared game: %v", m)
	}

	// The game as an inline result.
	admin(t, "bot", "set", username, "-inline", "Games...")
	inline := make(chan error, 1)
	var results *mtproto.Messages_BotResults
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
			Bot:         inputUserOfPeer(alice.c, toBot), Peer: toBot, Query: "jump",
		})
		if err == nil {
			results = o.(interface {
				To_Messages_BotResults() *mtproto.Messages_BotResults
			}).To_Messages_BotResults()
		}
		inline <- err
	}()
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotInlineQuery, func(u *mtproto.Update) bool {
		if u.GetQuery() != "jump" {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetInlineBotResults{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setInlineBotResults),
		QueryId:     queryId,
		Results: []*mtproto.InputBotInlineResult{{
			Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineResultGame),
			Id:          "g", ShortName: "jump",
			SendMessage: &mtproto.InputBotInlineMessage{Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineMessageGame)},
		}},
	}); err != nil {
		t.Fatalf("messages.setInlineBotResults with a game: %v", err)
	}
	if err = <-inline; err != nil {
		t.Fatalf("messages.getInlineBotResults: %v", err)
	}
	if r := results.GetResults(); len(r) != 1 || r[0].GetType() != "game" || r[0].GetPhoto().GetPredicateName() != mtproto.Predicate_photo || r[0].GetTitle().GetValue() != "Jump" {
		t.Fatalf("the game result: %v", results)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendInlineBotResult{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendInlineBotResult),
		Peer:        toBot, RandomId: randomID(), QueryId: results.GetQueryId(), Id: "g",
	})
	if err != nil {
		t.Fatalf("messages.sendInlineBotResult of the game: %v", err)
	}
	if m := sentMessage(t, o); m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || m.GetViaBotId().GetValue() != botId || !playButton(m) {
		t.Fatalf("the game sent inline: %v", m)
	}
}
