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
	"strconv"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// botsAmong are users of whom the ids of bots are bots.
type botsAmong struct {
	user_client.UserClient
	bots map[int64]bool
}

func (u botsAmong) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	data := &mtproto.UserData{Id: in.Id, AccessHash: 1}
	if u.bots[in.Id] {
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser(), nil
}

// failingMsg records the sends given to msg and fails the first fail of
// them.
type failingMsg struct {
	msg_client.MsgClient
	fail int
	sent []*msgpb.TLMsgSendMessageV2
}

func (m *failingMsg) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	if m.fail > 0 {
		m.fail--
		return nil, errors.New("msg is down")
	}
	m.sent = append(m.sent, in)
	return mtproto.MakeEmptyUpdates(), nil
}

// seqPushes records the updates with seq pushed to accounts.
type seqPushes struct {
	to      []int64
	updates []*mtproto.Update
}

func (p *seqPushes) PushSeq(_ context.Context, userId int64, u *mtproto.Update, _ []*mtproto.User, _ []*mtproto.Chat) error {
	p.to = append(p.to, userId)
	p.updates = append(p.updates, u)
	return nil
}

func articleResult(text string) *mtproto.InputBotInlineResult {
	return mtproto.MakeTLInputBotInlineResult(&mtproto.InputBotInlineResult{
		Id: "1", Type: "article", Title: wrapperspb.String("Order"),
		SendMessage: mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: text}).To_InputBotInlineMessage(),
	}).To_InputBotInlineResult()
}

// The bot's answer of a web view goes from the user who opened it to the
// chat it was opened in, via the bot, once; then the user's devices are
// told to close the web view. Another bot, a bad result and a failed send
// leave the query to answer.
func TestSendWebViewResultMessage(t *testing.T) {
	const user, bot, otherBot, queryId = int64(10), int64(20), int64(21), int64(777)
	msgs := &failingMsg{fail: 1}
	pushes := &seqPushes{}
	d := &dao.Dao{
		UserClient: botsAmong{bots: map[int64]bool{bot: true, otherBot: true}},
		MsgClient:  msgs,
		KV:         testKV(t),
		SendAs:     sendAsChannels{},
		UserSeq:    pushes,
	}
	ctx := context.Background()
	if err := d.PutWebView(ctx, queryId, &dao.WebView{User: user, Bot: bot, PeerType: mtproto.PEER_USER, PeerId: bot, Silent: true}); err != nil {
		t.Fatal(err)
	}
	answer := func(caller int64, q string, r *mtproto.InputBotInlineResult) error {
		_, err := testCore(d, caller).MessagesSendWebViewResultMessage(&mtproto.TLMessagesSendWebViewResultMessage{BotQueryId: q, Result: r})
		return err
	}
	q := strconv.FormatInt(queryId, 10)

	for _, tc := range []struct {
		name   string
		caller int64
		query  string
		result *mtproto.InputBotInlineResult
		want   error
	}{
		{"a user", user, q, articleResult("Paid"), mtproto.ErrUserBotRequired},
		{"not a number", bot, "abc", articleResult("Paid"), mtproto.ErrQueryIdInvalid},
		{"another bot", otherBot, q, articleResult("Paid"), mtproto.ErrQueryIdInvalid},
		{"an empty text", bot, q, articleResult(""), mtproto.ErrMessageEmpty},
		{"an unknown query", bot, "778", articleResult("Paid"), mtproto.ErrQueryIdInvalid},
	} {
		if err := answer(tc.caller, tc.query, tc.result); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := answer(bot, q, articleResult("Paid")); err == nil || !strings.Contains(err.Error(), "msg is down") {
		t.Fatalf("a failed send: %v", err)
	}
	if w, err := d.GetWebView(ctx, queryId); err != nil || w == nil {
		t.Fatalf("the query after a failed send: %v, %v", w, err)
	}
	if len(pushes.updates) != 0 {
		t.Fatalf("pushed %v before the answer was sent", pushes.updates)
	}

	if err := answer(bot, q, articleResult("Paid")); err != nil {
		t.Fatal(err)
	}
	if len(msgs.sent) != 1 {
		t.Fatalf("%d sends", len(msgs.sent))
	}
	in := msgs.sent[0]
	m := in.GetMessage()[0]
	if in.UserId != user || in.AuthKeyId != 0 || in.PeerType != mtproto.PEER_USER || in.PeerId != bot {
		t.Errorf("sent as %d (key %d) to %d/%d", in.UserId, in.AuthKeyId, in.PeerType, in.PeerId)
	}
	if m.GetRandomId() != webViewResultRandomId(queryId) || m.GetRandomId() == 0 {
		t.Errorf("random_id %d", m.GetRandomId())
	}
	got := m.GetMessage()
	if got.GetMessage() != "Paid" || got.GetViaBotId().GetValue() != bot || !got.GetSilent() || got.GetFromId().GetUserId() != user {
		t.Errorf("message %v", got)
	}
	if len(pushes.updates) != 1 || pushes.to[0] != user ||
		pushes.updates[0].GetPredicateName() != mtproto.Predicate_updateWebViewResultSent || pushes.updates[0].GetQueryId_INT64() != queryId {
		t.Errorf("pushed %v to %v", pushes.updates, pushes.to)
	}
	if w, err := d.GetWebView(ctx, queryId); err != nil || w != nil {
		t.Errorf("the answered query is still there: %v, %v", w, err)
	}
	if err := answer(bot, q, articleResult("Paid")); !errors.Is(err, mtproto.ErrQueryIdInvalid) {
		t.Errorf("a second answer: %v", err)
	}
}

// One message per query, never random_id 0.
func TestWebViewResultRandomId(t *testing.T) {
	a, b := webViewResultRandomId(1), webViewResultRandomId(2)
	if a == b || a <= 0 || b <= 0 || a != webViewResultRandomId(1) {
		t.Errorf("random ids %d, %d", a, b)
	}
}

// The keyboard button of a mini app is one of a reply keyboard, matched by
// its text; inline keyboards and other buttons do not count.
func TestHasWebViewButton(t *testing.T) {
	button := func(predicate, text string) *mtproto.KeyboardButton {
		return &mtproto.KeyboardButton{PredicateName: predicate, Text: text, Url: "https://shop.example/"}
	}
	keyboard := func(predicate string, buttons ...*mtproto.KeyboardButton) *mtproto.ReplyMarkup {
		return &mtproto.ReplyMarkup{PredicateName: predicate, Rows: []*mtproto.KeyboardButtonRow{{Buttons: buttons}}}
	}
	markups := []*mtproto.ReplyMarkup{
		keyboard(mtproto.Predicate_replyInlineMarkup, button(mtproto.Predicate_keyboardButtonSimpleWebView, "Inline")),
		keyboard(mtproto.Predicate_replyKeyboardMarkup, button(mtproto.Predicate_keyboardButton, "Plain"), button(mtproto.Predicate_keyboardButtonSimpleWebView, "Order")),
	}
	for text, want := range map[string]bool{"Order": true, "order": false, "Plain": false, "Inline": false, "": false} {
		if got := hasWebViewButton(markups, text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

// The data of a mini app is checked before anything is looked up: at most
// 4096 bytes, not empty, UTF-8; a bot cannot send it.
func TestSendWebViewDataChecks(t *testing.T) {
	const user, bot = int64(10), int64(20)
	d := &dao.Dao{UserClient: botsAmong{bots: map[int64]bool{bot: true}}}
	send := func(caller int64, isBot bool, target int64, data string) error {
		c := testCore(d, caller)
		c.MD.IsBot = isBot
		_, err := c.MessagesSendWebViewData(&mtproto.TLMessagesSendWebViewData{
			Bot:        mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: target, AccessHash: 1}).To_InputUser(),
			RandomId:   1,
			ButtonText: "Order",
			Data:       data,
		})
		return err
	}
	for _, tc := range []struct {
		name   string
		caller int64
		isBot  bool
		target int64
		data   string
		want   error
	}{
		{"from a bot", bot, true, bot, "x", mtproto.ErrBotMethodInvalid},
		{"to a user", user, false, 30, "x", mtproto.ErrBotInvalid},
		{"4097 bytes", user, false, bot, strings.Repeat("x", maxWebViewData+1), mtproto.ErrDataTooLong},
		{"empty", user, false, bot, "", mtproto.ErrDataInvalid},
		{"not UTF-8", user, false, bot, "\xff\xfe", mtproto.ErrDataInvalid},
	} {
		if err := send(tc.caller, tc.isBot, tc.target, tc.data); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
