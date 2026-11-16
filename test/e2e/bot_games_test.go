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

//go:build e2e

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
	if m.GetReplyMarkup().GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		return false
	}
	// A client of layer 229 and later gets the buttons with a type
	// (pkg/layercompat); the bots of these tests are of layer 228.
	if rows := m.GetReplyMarkup().GetRows_VECTORKEYBOARDINLINEBUTTONROW(); len(rows) > 0 {
		return len(rows[0].GetButtons()) > 0 && rows[0].GetButtons()[0].GetType().GetPredicateName() == mtproto.Predicate_inlineButtonTypeGame
	}
	rows := m.GetReplyMarkup().GetRows()
	return len(rows) > 0 && len(rows[0].GetButtons()) > 0 && rows[0].GetButtons()[0].GetPredicateName() == mtproto.Predicate_keyboardButtonGame
}

// buttonURLs are the addresses of the first buttons of the rows of m's
// inline keyboard, "" for a button with none, in either shape (playButton).
func buttonURLs(m *mtproto.Message) []string {
	var out []string
	if rows := m.GetReplyMarkup().GetRows_VECTORKEYBOARDINLINEBUTTONROW(); len(rows) > 0 {
		for _, r := range rows {
			if len(r.GetButtons()) > 0 {
				out = append(out, r.GetButtons()[0].GetType().GetUrl())
			}
		}
		return out
	}
	for _, r := range m.GetReplyMarkup().GetRows() {
		if len(r.GetButtons()) > 0 {
			out = append(out, r.GetButtons()[0].GetUrl())
		}
	}
	return out
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
	inlineGame := sentMessage(t, o)
	if inlineGame.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || inlineGame.GetViaBotId().GetValue() != botId || !playButton(inlineGame) {
		t.Fatalf("the game sent inline: %v", inlineGame)
	}

	// The press of the inline game names it by its inline message id, by
	// which the bot sets and reads the score and edits the message.
	go func() {
		_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetBotCallbackAnswer{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getBotCallbackAnswer),
			Game:        true, Peer: toBot, MsgId: inlineGame.GetId(),
		})
		press <- err
	}()
	var inlineId *mtproto.InputBotInlineMessageID
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateInlineBotCallbackQuery, func(u *mtproto.Update) bool {
		if u.GetGameShortName().GetValue() != "jump" {
			return false
		}
		queryId, inlineId = u.GetQueryId_INT64(), u.GetMsgId_FLAGINPUTBOTINLINEMESSAGEID()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetBotCallbackAnswer{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setBotCallbackAnswer),
		QueryId:     queryId, Url: wrapperspb.String("https://games.example/jump"),
	}); err != nil {
		t.Fatalf("messages.setBotCallbackAnswer of the inline game: %v", err)
	}
	if err = <-press; err != nil {
		t.Fatalf("the press of the inline game: %v", err)
	}
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetInlineGameScore{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setInlineGameScore),
		EditMessage: true, Id: inlineId, UserId: inputUserOfPeer(bot.c, toAlice), Score: 200,
	}); err != nil {
		t.Fatalf("messages.setInlineGameScore: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		a := m.GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionGameScore && a.GetScore() == 200 &&
			m.GetFromId().GetUserId() == alice.self.GetId() && m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() == inlineGame.GetId()
	})
	o, err = bot.c.Invoke(ctx, &mtproto.TLMessagesGetInlineGameHighScores{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_getInlineGameHighScores),
		Id:          inlineId, UserId: inputUserOfPeer(bot.c, toAlice),
	})
	if err != nil {
		t.Fatalf("messages.getInlineGameHighScores: %v", err)
	}
	table = o.(interface {
		To_Messages_HighScores() *mtproto.Messages_HighScores
	}).To_Messages_HighScores()
	// The private chat of Alice and the bot holds both games: one table.
	if s := table.GetScores(); len(s) != 1 || s[0].GetUserId() != alice.self.GetId() || s[0].GetScore() != 200 {
		t.Fatalf("the inline game's table: %v", table)
	}

	// The bot adds a row to the keyboard of the inline game; the game's
	// button stays first.
	rules := &mtproto.ReplyMarkup{
		Constructor: bot.c.Constructor(mtproto.Predicate_replyInlineMarkup),
		Rows: []*mtproto.KeyboardButtonRow{
			{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonRow), Buttons: []*mtproto.KeyboardButton{
				{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonGame), Text: "Play"},
			}},
			{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonRow), Buttons: []*mtproto.KeyboardButton{
				{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonUrl), Text: "Rules", Url: "https://games.example/rules"},
			}},
		},
	}
	editInline := func() error {
		_, err := bot.c.Invoke(ctx, &mtproto.TLMessagesEditInlineBotMessage{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_editInlineBotMessage),
			Id:          inlineId, ReplyMarkup: rules,
		})
		return err
	}
	if err = editInline(); err != nil {
		t.Fatalf("messages.editInlineBotMessage: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateEditMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		urls := buttonURLs(m)
		return m.GetId() == inlineGame.GetId() && m.GetEditDate() != nil && playButton(m) && len(urls) == 2 &&
			urls[1] == "https://games.example/rules"
	})
	if err = editInline(); !strings.Contains(fmt.Sprint(err), "MESSAGE_NOT_MODIFIED") {
		t.Fatalf("the same edit again: %v", err)
	}
}

// TestGamesInGroup: a game in a basic group (task 106, ADR 0068 p. 7;
// core.telegram.org/bots/api#setgamescore). Its keyboard keeps the game's
// button first, and a game button anywhere else or without a game is
// refused. Each member's score is posted from that member in answer to the
// member's own copy of the game; the table is of the group, ordered by
// score, and any member reads it. force lowers a score without a message;
// a negative score, a player outside the group and a score set by a user
// are refused.
func TestGamesInGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phones[0], username)
	getState(ctx, t, bot)

	picture := filepath.Join(t.TempDir(), "race.png")
	if err := os.WriteFile(picture, gamePicture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := fmt.Sprintf("/tmp/game-%d.png", uint64(randomID()))
	stack(t, "cp", picture, "bff:"+dest)
	defer stack(t, "exec", "-T", "bff", "rm", "-f", dest)
	admin(t, "bot", "game", "add", username, "race", "-title", "Race", "-photo", dest)

	chatId := createChat(ctx, t, alice, "racers", bob)
	toBot := resolveBot(ctx, t, alice, username)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesAddChatUserCBC6D107{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_addChatUserCBC6D107),
		ChatId:      chatId, UserId: inputUserOfPeer(alice.c, toBot), FwdLimit: 50,
	}); err != nil {
		t.Fatalf("messages.addChatUser of the bot: %v", err)
	}
	group := chatPeer(bot, chatId)
	row := func(buttons ...*mtproto.KeyboardButton) *mtproto.KeyboardButtonRow {
		return &mtproto.KeyboardButtonRow{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonRow), Buttons: buttons}
	}
	playBtn := &mtproto.KeyboardButton{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonGame), Text: "Race!"}
	rulesBtn := &mtproto.KeyboardButton{Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButtonUrl), Text: "Rules", Url: "https://games.example/rules"}
	markup := func(rows ...*mtproto.KeyboardButtonRow) *mtproto.ReplyMarkup {
		return &mtproto.ReplyMarkup{Constructor: bot.c.Constructor(mtproto.Predicate_replyInlineMarkup), Rows: rows}
	}
	game := &mtproto.InputMedia{
		Constructor: bot.c.Constructor(mtproto.Predicate_inputMediaGame),
		Id_INPUTGAME: &mtproto.InputGame{
			Constructor: bot.c.Constructor(mtproto.Predicate_inputGameShortName),
			BotId:       &mtproto.InputUser{Constructor: bot.c.Constructor(mtproto.Predicate_inputUserSelf)},
			ShortName:   "race",
		},
	}
	sendGame := func(m *mtproto.ReplyMarkup) (mtproto.TLObject, error) {
		return bot.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMedia),
			Peer:        group, Media: game, RandomId: randomID(), ReplyMarkup: m,
		})
	}

	// The game's button: first in the first row of a game, nowhere else.
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        group, Message: "no game here", RandomId: randomID(), ReplyMarkup: markup(row(playBtn)),
	}); !strings.Contains(fmt.Sprint(err), "BUTTON_TYPE_INVALID") {
		t.Fatalf("a game button without a game: %v", err)
	}
	if _, err := sendGame(markup(row(rulesBtn), row(playBtn))); !strings.Contains(fmt.Sprint(err), "BUTTON_TYPE_INVALID") {
		t.Fatalf("a game button in the second row: %v", err)
	}
	o, err := sendGame(markup(row(playBtn), row(rulesBtn)))
	if err != nil {
		t.Fatalf("messages.sendMedia of the game to the group: %v", err)
	}
	botMsgId := sentMessage(t, o).GetId()
	gameCopy := func(u *user) int32 {
		var id int32
		waitUpdate(ctx, t, u, mtproto.Predicate_updateNewMessage, func(up *mtproto.Update) bool {
			m := up.GetMessage_MESSAGE()
			if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || m.GetPeerId().GetChatId() != chatId {
				return false
			}
			urls := buttonURLs(m)
			if !playButton(m) || len(urls) != 2 || urls[1] != "https://games.example/rules" {
				t.Fatalf("the game in the group: %v", m)
			}
			id = m.GetId()
			return true
		})
		return id
	}
	aliceMsgId, bobMsgId := gameCopy(alice), gameCopy(bob)

	setScore := func(c *user, player *user, score int32, force, edit bool) error {
		_, err := c.c.Invoke(ctx, &mtproto.TLMessagesSetGameScore{
			Constructor: c.c.Constructor(mtproto.Predicate_messages_setGameScore),
			EditMessage: edit, Force: force, Peer: group, Id: botMsgId,
			UserId: &mtproto.InputUser{Constructor: c.c.Constructor(mtproto.Predicate_inputUser), UserId: player.self.GetId(), AccessHash: player.self.GetAccessHash().GetValue()},
			Score:  score,
		})
		return err
	}
	scored := func(u *user, player *user, score int32, replyTo int32) {
		t.Helper()
		waitUpdate(ctx, t, u, mtproto.Predicate_updateNewMessage, func(up *mtproto.Update) bool {
			m := up.GetMessage_MESSAGE()
			a := m.GetAction()
			return a.GetPredicateName() == mtproto.Predicate_messageActionGameScore && a.GetScore() == score &&
				m.GetFromId().GetUserId() == player.self.GetId() && m.GetPeerId().GetChatId() == chatId &&
				m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() == replyTo
		})
	}
	if err = setScore(bot, alice, 30, false, true); err != nil {
		t.Fatalf("messages.setGameScore of Alice: %v", err)
	}
	scored(bob, alice, 30, bobMsgId)
	if err = setScore(bot, bob, 20, false, true); err != nil {
		t.Fatalf("messages.setGameScore of Bob: %v", err)
	}
	scored(alice, bob, 20, aliceMsgId)

	table := func(u *user, msgId int32) []*mtproto.HighScore {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetGameHighScores{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getGameHighScores),
			Peer:        chatPeer(u, chatId), Id: msgId,
			UserId: &mtproto.InputUser{Constructor: u.c.Constructor(mtproto.Predicate_inputUserSelf)},
		})
		if err != nil {
			t.Fatalf("messages.getGameHighScores: %v", err)
		}
		return o.(interface {
			To_Messages_HighScores() *mtproto.Messages_HighScores
		}).To_Messages_HighScores().GetScores()
	}
	// A member reads the table of the group by the member's own copy
	// (Web K appGamesManager.ts:72-89).
	if s := table(bob, bobMsgId); len(s) != 2 || s[0].GetUserId() != alice.self.GetId() || s[0].GetPos() != 1 || s[0].GetScore() != 30 ||
		s[1].GetUserId() != bob.self.GetId() || s[1].GetPos() != 2 || s[1].GetScore() != 20 {
		t.Fatalf("the group's table: %v", s)
	}

	// force sets a lower score, without a message.
	if err = setScore(bot, alice, 10, true, false); err != nil {
		t.Fatalf("a forced lower score: %v", err)
	}
	if s := table(alice, aliceMsgId); len(s) != 2 || s[0].GetUserId() != bob.self.GetId() || s[1].GetUserId() != alice.self.GetId() || s[1].GetScore() != 10 {
		t.Fatalf("the table after the forced score: %v", s)
	}

	if err = setScore(bot, alice, -1, true, false); !strings.Contains(fmt.Sprint(err), "SCORE_INVALID") {
		t.Errorf("a negative score: %v", err)
	}
	if err = setScore(bot, carol, 5, false, false); !strings.Contains(fmt.Sprint(err), "USER_ID_INVALID") {
		t.Errorf("a player outside the group: %v", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSetGameScore{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_setGameScore),
		Peer:        chatPeer(alice, chatId), Id: aliceMsgId,
		UserId: &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)}, Score: 99,
	}); !strings.Contains(fmt.Sprint(err), "USER_BOT_REQUIRED") {
		t.Errorf("a score set by a user: %v", err)
	}
}
