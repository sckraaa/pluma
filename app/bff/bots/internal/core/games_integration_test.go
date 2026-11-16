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
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
)

// gameUsers knows the players and the bots of the test.
type gameUsers struct {
	user_client.UserClient
	bots map[int64]bool
}

func (u gameUsers) immutable(id int64) *mtproto.ImmutableUser {
	data := &mtproto.UserData{Id: id, AccessHash: 1, FirstName: "Player"}
	if u.bots[id] {
		data.FirstName = "Game bot"
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
}

func (u gameUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	if in.Id <= 0 {
		return nil, mtproto.ErrUserIdInvalid
	}
	return u.immutable(in.Id), nil
}

func (u gameUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, u.immutable(id))
	}
	return out, nil
}

// gameBoxKey is a message of a box.
type gameBoxKey struct {
	user int64
	id   int32
}

// gameBoxes are the boxes of the test: the copies of each message, by the
// dialog_message_id they share.
type gameBoxes struct {
	message_client.MessageClient
	boxes map[gameBoxKey]*mtproto.MessageBox
	// copies is the dialog message of each copy.
	copies map[gameBoxKey]int64
}

func (b *gameBoxes) put(user int64, id int32, dialogMessage int64, box *mtproto.MessageBox) {
	box.UserId, box.MessageId = user, id
	b.boxes[gameBoxKey{user, id}] = box
	b.copies[gameBoxKey{user, id}] = dialogMessage
}

func (b *gameBoxes) MessageGetUserMessage(_ context.Context, in *message.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	if box, ok := b.boxes[gameBoxKey{in.UserId, in.Id}]; ok {
		return box, nil
	}
	return nil, mtproto.ErrMsgIdInvalid
}

func (b *gameBoxes) MessageGetPeerUserMessage(_ context.Context, in *message.TLMessageGetPeerUserMessage) (*mtproto.MessageBox, error) {
	dm, ok := b.copies[gameBoxKey{in.UserId, in.MsgId}]
	if !ok {
		return nil, mtproto.ErrMsgIdInvalid
	}
	for k, box := range b.boxes {
		if k.user == in.PeerUserId && b.copies[k] == dm {
			return box, nil
		}
	}
	return nil, mtproto.ErrMsgIdInvalid
}

// TestGameScoresOnMySQL: a bot's game in a private chat and in a group. The
// bot sets the scores of players with the rules of setGameScore; the
// player's message of the score answers the player's copy of the game, once
// for a repeat; the table is of each chat, around the player, as the bot
// and as a user see it; another bot neither sets nor reads it.
func TestGameScoresOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	store := botstore.New(sqlx.NewMySQL(&conf))

	bot, _, err := store.Create(ctx, "jump_game_bot", "Jump", 0)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := store.Create(ctx, "other_game_bot", "Other", 0)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.AddGame(ctx, botstore.Game{BotId: bot, ShortName: "jump", Title: "Jump", PhotoId: 5})
	if err != nil {
		t.Fatal(err)
	}

	const ann, bob, cat, dan, group = int64(1000011), int64(1000012), int64(1000013), int64(1000014), int64(77)
	gameMedia := mtproto.MakeTLMessageMediaGame(&mtproto.MessageMedia{Game: mtproto.MakeTLGame(&mtproto.Game{Id: game.Id, AccessHash: game.AccessHash, ShortName: "jump", Title: "Jump"}).To_Game()}).To_MessageMedia()
	gameMessage := func(from int64, peer *mtproto.Peer) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{FromId: mtproto.MakePeerUser(from), PeerId: peer, Media: gameMedia}).To_Message()
	}
	boxes := &gameBoxes{boxes: map[gameBoxKey]*mtproto.MessageBox{}, copies: map[gameBoxKey]int64{}}
	// The game in the private chat of the bot and Ann: 3 in the bot's box,
	// 8 in Ann's.
	private := mtproto.MakeDialogId(bot, mtproto.PEER_USER, ann)
	boxes.put(bot, 3, 1, &mtproto.MessageBox{DialogId1: private.A, DialogId2: private.B, Message: gameMessage(bot, mtproto.MakePeerUser(ann))})
	boxes.put(ann, 8, 1, &mtproto.MessageBox{DialogId1: private.A, DialogId2: private.B, Message: gameMessage(bot, mtproto.MakePeerUser(ann))})
	// The game in the group: 4 in the bot's box, 9 in Ann's, 6 in Bob's,
	// none in Cat's and Dan's (they deleted it). Eve left the group.
	inGroup := mtproto.MakeDialogId(bot, mtproto.PEER_CHAT, group)
	for user, id := range map[int64]int32{bot: 4, ann: 9, bob: 6} {
		boxes.put(user, id, 2, &mtproto.MessageBox{DialogId1: inGroup.A, DialogId2: inGroup.B, Message: gameMessage(bot, mtproto.MakePeerChat(group))})
	}
	// A message without a game in the bot's box.
	boxes.put(bot, 5, 3, &mtproto.MessageBox{DialogId1: private.A, DialogId2: private.B, Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message()})

	const eve = int64(1000015)
	member := func(id int64, state int32) *mtproto.ImmutableChatParticipant {
		return mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{UserId: id, State: state}).To_ImmutableChatParticipant()
	}
	groupChat := &addingChats{chat: mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: group}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{
			member(bot, mtproto.ChatMemberStateNormal), member(ann, mtproto.ChatMemberStateNormal), member(bob, mtproto.ChatMemberStateNormal),
			member(cat, mtproto.ChatMemberStateNormal), member(dan, mtproto.ChatMemberStateNormal), member(eve, mtproto.ChatMemberStateLeft),
		},
	}).To_MutableChat(), present: true}

	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{Dao: &dao.Dao{
		UserClient:    gameUsers{bots: map[int64]bool{bot: true, other: true}},
		MessageClient: boxes,
		MsgClient:     sent,
		Chat:          groupChat,
		Bots:          store,
	}}
	as := func(id int64, isBot bool) *BotsCore {
		return &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: id, IsBot: isBot}}
	}
	input := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: 1}).To_InputUser()
	}
	toUser := func(id int64) *mtproto.InputPeer {
		return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: 1}).To_InputPeer()
	}
	toGroup := mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: group}).To_InputPeer()
	setScore := func(c *BotsCore, peer *mtproto.InputPeer, msgId int32, player int64, score int32, force, edit bool) error {
		_, err := c.MessagesSetGameScore(&mtproto.TLMessagesSetGameScore{Peer: peer, Id: msgId, UserId: input(player), Score: score, Force: force, EditMessage: edit})
		return err
	}

	b := as(bot, true)
	// The private chat.
	if err = setScore(b, toUser(ann), 3, ann, 10, false, true); err != nil {
		t.Fatalf("the first score: %v", err)
	}
	if len(sent.sent) != 1 {
		t.Fatalf("%d messages of the score", len(sent.sent))
	}
	s := sent.sent[0]
	m := s.GetMessage()[0].GetMessage()
	if s.UserId != ann || s.PeerType != mtproto.PEER_USER || s.PeerId != bot || m.GetFromId().GetUserId() != ann ||
		m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionGameScore || m.GetAction().GetGameId() != game.Id ||
		m.GetAction().GetScore() != 10 || m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != 8 {
		t.Fatalf("the message of the score: %v", s)
	}
	if err = setScore(b, toUser(ann), 3, ann, 10, false, true); !errors.Is(err, mtproto.ErrBotScoreNotModified) {
		t.Errorf("the same score again: %v", err)
	}
	if err = setScore(b, toUser(ann), 3, ann, 7, false, false); !errors.Is(err, mtproto.ErrBotScoreNotModified) {
		t.Errorf("a lesser score: %v", err)
	}
	if err = setScore(b, toUser(ann), 3, ann, 7, true, true); err != nil {
		t.Errorf("a lesser score with force: %v", err)
	}
	if err = setScore(b, toUser(ann), 3, ann, 7, true, true); err != nil || len(sent.sent) != 2 {
		t.Errorf("a repeat of the forced score: %v, %d messages", err, len(sent.sent))
	}
	for name, err := range map[string]error{
		"negative":                setScore(b, toUser(ann), 3, ann, -1, true, false),
		"the player of no chat":   setScore(b, toUser(ann), 3, bob, 1, false, false),
		"a bot as the player":     setScore(b, toUser(ann), 3, other, 1, false, false),
		"a message without games": setScore(b, toUser(ann), 5, ann, 1, false, false),
		"a message of no box":     setScore(b, toUser(ann), 99, ann, 1, false, false),
		"another bot's game":      setScore(as(other, true), toUser(ann), 3, ann, 1, false, false),
		"a user":                  setScore(as(ann, false), toUser(bot), 8, ann, 1, false, false),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	// The group: its own table.
	for _, p := range []struct {
		user  int64
		score int32
	}{{ann, 30}, {bob, 50}, {cat, 40}, {dan, 20}} {
		if err = setScore(b, toGroup, 4, p.user, p.score, false, true); err != nil {
			t.Fatalf("the score of %d in the group: %v", p.user, err)
		}
	}
	// Who is not a member has no score in the group, and no message of one.
	posted := len(sent.sent)
	for _, player := range []int64{eve, 1000016} {
		if err = setScore(b, toGroup, 4, player, 60, false, true); !errors.Is(err, mtproto.ErrUserIdInvalid) {
			t.Errorf("the score of %d, not a member: %v", player, err)
		}
	}
	if len(sent.sent) != posted {
		t.Errorf("%d messages of scores of non-members", len(sent.sent)-posted)
	}
	last := sent.sent[len(sent.sent)-1]
	if last.UserId != dan || last.PeerType != mtproto.PEER_CHAT || last.PeerId != group || last.GetMessage()[0].GetMessage().GetReplyTo() != nil {
		t.Errorf("the score of a player without the game message: %v", last)
	}
	for _, s := range sent.sent {
		if s.UserId == bob && s.GetMessage()[0].GetMessage().GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != 6 {
			t.Errorf("Bob's score answers %v", s.GetMessage()[0].GetMessage().GetReplyTo())
		}
	}

	table := func(c *BotsCore, peer *mtproto.InputPeer, msgId int32, player int64) ([]*mtproto.HighScore, error) {
		r, err := c.MessagesGetGameHighScores(&mtproto.TLMessagesGetGameHighScores{Peer: peer, Id: msgId, UserId: input(player)})
		if err != nil {
			return nil, err
		}
		if len(r.GetUsers()) != len(r.GetScores()) {
			t.Errorf("%d users for %d scores", len(r.GetUsers()), len(r.GetScores()))
		}
		return r.GetScores(), nil
	}
	got, err := table(b, toGroup, 4, dan)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		pos   int32
		user  int64
		score int32
	}{{1, bob, 50}, {2, cat, 40}, {3, ann, 30}, {4, dan, 20}}
	if len(got) != len(want) {
		t.Fatalf("the group's table: %v", got)
	}
	for i, w := range want {
		if got[i].GetPos() != w.pos || got[i].GetUserId() != w.user || got[i].GetScore() != w.score {
			t.Errorf("line %d: %v, want %+v", i, got[i], w)
		}
	}
	// Ann sees the table of the private chat in her own box.
	if got, err = table(as(ann, false), toUser(bot), 8, ann); err != nil || len(got) != 1 || got[0].GetScore() != 7 {
		t.Errorf("Ann's table of the private chat: %v %v", got, err)
	}
	if _, err = table(as(other, true), toUser(ann), 3, ann); !errors.Is(err, mtproto.ErrMessageIdInvalid) {
		t.Errorf("another bot's table: %v", err)
	}

	// The press of the game's button names the game; a message without the
	// button or of another bot's game is no game to press.
	play := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
		mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			mtproto.MakeTLKeyboardButtonGame(&mtproto.KeyboardButton{Text: "Play Jump"}).To_KeyboardButton(),
		}}).To_KeyboardButtonRow(),
	}}).To_ReplyMarkup()
	withButton := gameMessage(bot, mtproto.MakePeerUser(ann))
	withButton.ReplyMarkup = play
	p := &pressed{message: withButton, peer: mtproto.MakeUserPeerUtil(bot), botId: bot}
	if name, err := as(ann, false).pressedGame(p); err != nil || name != "jump" || p.inline {
		t.Errorf("the press of the game: %q %v, inline %v", name, err, p.inline)
	}
	if _, err = as(ann, false).pressedGame(&pressed{message: gameMessage(bot, mtproto.MakePeerUser(ann)), peer: mtproto.MakeUserPeerUtil(bot), botId: bot}); !errors.Is(err, mtproto.ErrDataInvalid) {
		t.Errorf("a game without its button: %v", err)
	}
	if _, err = as(ann, false).pressedGame(&pressed{message: withButton, peer: mtproto.MakeUserPeerUtil(other), botId: other}); !errors.Is(err, mtproto.ErrDataInvalid) {
		t.Errorf("the game of another bot: %v", err)
	}
	// A forward keeps no button, and iOS presses the card.
	forwarded := gameMessage(bob, mtproto.MakePeerChat(group))
	forwarded.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerUser(bot)}).To_MessageFwdHeader()
	p = &pressed{message: forwarded, peer: mtproto.MakeChatPeerUtil(group), botId: bob}
	if name, err := as(ann, false).pressedGame(p); err != nil || name != "jump" || !p.inline || p.botId != bot {
		t.Errorf("a forwarded game: %q %v, inline %v, bot %d", name, err, p.inline, p.botId)
	}
}

// TestInlineGameResultOnMySQL: a bot answers an inline query with its game;
// the result shows the game and sends it, with the button the user's
// language names.
func TestInlineGameResultOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	store := botstore.New(sqlx.NewMySQL(&conf))
	bot, _, err := store.Create(ctx, "inline_game_bot", "Games", 0)
	if err != nil {
		t.Fatal(err)
	}
	game, err := store.AddGame(ctx, botstore.Game{BotId: bot, ShortName: "jump", Title: "Jump", Description: "Up and up", PhotoId: 5, DocumentId: 6})
	if err != nil {
		t.Fatal(err)
	}
	svcCtx := &svc.ServiceContext{Dao: &dao.Dao{
		UserClient:  gameUsers{bots: map[int64]bool{bot: true}},
		MediaClient: fakeFiles{},
		Bots:        store,
	}}
	c := &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: bot, IsBot: true}}
	result := func(name string, send *mtproto.InputBotInlineMessage) *mtproto.InputBotInlineResult {
		return mtproto.MakeTLInputBotInlineResultGame(&mtproto.InputBotInlineResult{Id: "g", ShortName: name, SendMessage: send}).To_InputBotInlineResult()
	}
	gameSend := mtproto.MakeTLInputBotInlineMessageGame(&mtproto.InputBotInlineMessage{}).To_InputBotInlineMessage()

	res, ref, err := c.inlineGameResult(result("jump", gameSend))
	if err != nil {
		t.Fatal(err)
	}
	if res.GetPredicateName() != mtproto.Predicate_botInlineMediaResult || res.GetType() != "game" || res.GetTitle().GetValue() != "Jump" ||
		res.GetDescription().GetValue() != "Up and up" || res.GetPhoto().GetId() != 5 || res.GetDocument().GetId() != 6 ||
		res.GetSendMessage().GetPredicateName() != mtproto.Predicate_botInlineMessageMediaAuto {
		t.Fatalf("the result: %v", res)
	}
	if ref.Id != game.Id || ref.AccessHash != game.AccessHash {
		t.Fatalf("the game of the result: %+v", ref)
	}
	if _, _, err = c.inlineGameResult(result("fly", gameSend)); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("an unknown game: %v", err)
	}
	text := mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: "x"}).To_InputBotInlineMessage()
	if _, _, err = c.inlineGameResult(result("jump", text)); !errors.Is(err, mtproto.ErrSendMessageTypeInvalid) {
		t.Errorf("a game sending a text: %v", err)
	}

	user := &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 1000021, LangCode: "de"}}
	tl, err := user.inlineGame(ref)
	if err != nil {
		t.Fatal(err)
	}
	out, err := outOfResult(res, tl)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || out.GetMedia().GetGame().GetId() != game.Id {
		t.Fatalf("the message of the result: %v", out)
	}
	if err = store.DeleteGame(ctx, bot, "jump"); err != nil {
		t.Fatal(err)
	}
	if _, err = user.inlineGame(ref); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("a game removed since the answer: %v", err)
	}
}
