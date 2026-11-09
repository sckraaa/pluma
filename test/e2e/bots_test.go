//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

var botCreatedRe = regexp.MustCompile(`bot (\d+): (\S+)`)

// botLoggedIn creates a bot with pluma-admin and logs a new device in with
// its token. owner is the phone of the account that may edit it.
func botLoggedIn(ctx context.Context, t *testing.T, owner, username string) *user {
	t.Helper()
	out := admin(t, "bot", "create", username, "Test Bot", "-owner", owner)
	m := botCreatedRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no token in %q", out)
	}
	c, err := mtclient.Dial(ctx, config(t, "e2e bot "+username))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, username, c)

	if _, err := c.Invoke(ctx, &mtproto.TLAuthImportBotAuthorization{
		Constructor: c.Constructor(mtproto.Predicate_auth_importBotAuthorization),
		ApiId:       1, ApiHash: "e2e", BotAuthToken: "1:nope",
	}); !strings.Contains(fmt.Sprint(err), "ACCESS_TOKEN_INVALID") {
		t.Fatalf("a wrong token: %v", err)
	}
	o, err := c.Invoke(ctx, &mtproto.TLAuthImportBotAuthorization{
		Constructor: c.Constructor(mtproto.Predicate_auth_importBotAuthorization),
		ApiId:       1, ApiHash: "e2e", BotAuthToken: m[2],
	})
	if err != nil {
		t.Fatalf("auth.importBotAuthorization: %v", err)
	}
	self := o.(interface {
		To_Auth_Authorization() *mtproto.Auth_Authorization
	}).To_Auth_Authorization().GetUser()
	if !self.GetBot() || self.GetPhone() != nil {
		t.Fatalf("the bot's own user: bot %v, phone %v", self.GetBot(), self.GetPhone())
	}
	return &user{c: c, self: self}
}

// resolveBot is the input peer of a bot, found by its username.
func resolveBot(ctx context.Context, t *testing.T, u *user, username string) *mtproto.InputPeer {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLContactsResolveUsername{
		Constructor: u.c.Constructor(mtproto.Predicate_contacts_resolveUsername),
		Username:    username,
	})
	if err != nil {
		t.Fatalf("contacts.resolveUsername: %v", err)
	}
	for _, x := range o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	}).To_Contacts_ResolvedPeer().GetUsers() {
		if x.GetBot() {
			return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: x.GetId(), AccessHash: x.GetAccessHash().GetValue()}
		}
	}
	t.Fatalf("no bot in contacts.resolveUsername of %s", username)
	return nil
}

func inputUserOfPeer(c *mtclient.Client, p *mtproto.InputPeer) *mtproto.InputUser {
	return &mtproto.InputUser{Constructor: c.Constructor(mtproto.Predicate_inputUser), UserId: p.GetUserId(), AccessHash: p.GetAccessHash()}
}

// botWithCommands has the bot set its commands and returns them as a user
// sees them.
func botCommandsSeen(ctx context.Context, t *testing.T, alice, bot *user, toBot *mtproto.InputPeer) []string {
	t.Helper()
	scope := &mtproto.BotCommandScope{Constructor: bot.c.Constructor(mtproto.Predicate_botCommandScopeDefault)}
	if _, err := bot.c.Invoke(ctx, &mtproto.TLBotsSetBotCommands{
		Constructor: bot.c.Constructor(mtproto.Predicate_bots_setBotCommands),
		Scope:       scope,
		Commands:    []*mtproto.BotCommand{{Constructor: bot.c.Constructor(mtproto.Predicate_botCommand), Command: "start", Description: "Begin"}},
	}); err != nil {
		t.Fatalf("bots.setBotCommands: %v", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
		Constructor: alice.c.Constructor(mtproto.Predicate_users_getFullUser),
		Id:          inputUserOfPeer(alice.c, toBot),
	})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	full := o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull().GetFullUser()
	var out []string
	for _, c := range full.GetBotInfo().GetCommands() {
		out = append(out, c.GetCommand())
	}
	return out
}

// The core of a bot over MTProto (task 070): a token logs in, commands reach
// userFull.bot_info, /start with a parameter reaches the bot, a keyboard
// reaches the user, and a press and an inline query go through.
func TestBotAccount(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phone, username)
	stateBefore := getState(ctx, t, bot)
	toBot := resolveBot(ctx, t, alice, username)

	if got := botCommandsSeen(ctx, t, alice, bot, toBot); len(got) != 1 || got[0] != "start" {
		t.Fatalf("commands in userFull: %v", got)
	}
	if _, err := bot.c.Invoke(ctx, &mtproto.TLBotsSetBotInfo{
		Constructor: bot.c.Constructor(mtproto.Predicate_bots_setBotInfo),
		About:       wrapperspb.String("about it"),
	}); err != nil {
		t.Fatalf("bots.setBotInfo: %v", err)
	}

	// /start with a parameter.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesStartBot{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_startBot),
		Bot:         inputUserOfPeer(alice.c, toBot),
		Peer:        toBot,
		RandomId:    randomID(),
		StartParam:  "abc",
	}); err != nil {
		t.Fatalf("messages.startBot: %v", err)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "/start abc"
	})

	// getDifference gives a bot what it missed.
	o, err := bot.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bot.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         stateBefore.GetPts(), Date: stateBefore.GetDate(),
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	found := false
	for _, m := range o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference().GetNewMessages() {
		found = found || m.GetMessage() == "/start abc"
	}
	if !found {
		t.Fatal("the difference misses /start")
	}

	// A keyboard from the bot, and a press.
	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	markup := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
			Buttons: []*mtproto.KeyboardButton{mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte("go")}).To_KeyboardButton()},
		}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toAlice, Message: "pick", RandomId: randomID(), ReplyMarkup: markup,
	}); err != nil {
		t.Fatalf("messages.sendMessage from the bot: %v", err)
	}
	var msgId int32
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "pick" || m.GetReplyMarkup().GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
			return false
		}
		msgId = m.GetId()
		return true
	})

	press := make(chan error, 1)
	var answer *mtproto.Messages_BotCallbackAnswer
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetBotCallbackAnswer{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getBotCallbackAnswer),
			Peer:        toBot, MsgId: msgId, Data: []byte("go"),
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
		if string(u.GetData_FLAGBYTES()) != "go" || u.GetUserId() != alice.self.GetId() {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetBotCallbackAnswer{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setBotCallbackAnswer),
		QueryId:     queryId, Message: wrapperspb.String("done"),
	}); err != nil {
		t.Fatalf("messages.setBotCallbackAnswer: %v", err)
	}
	if err = <-press; err != nil {
		t.Fatalf("messages.getBotCallbackAnswer: %v", err)
	}
	if answer.GetMessage().GetValue() != "done" {
		t.Fatalf("the answer: %q", answer.GetMessage().GetValue())
	}

	// An inline query and its result.
	admin(t, "bot", "set", username, "-inline", "Search...")
	inline := make(chan error, 1)
	var results *mtproto.Messages_BotResults
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
			Bot:         inputUserOfPeer(alice.c, toBot), Peer: toBot, Query: "cats",
		})
		if err == nil {
			results = o.(interface {
				To_Messages_BotResults() *mtproto.Messages_BotResults
			}).To_Messages_BotResults()
		}
		inline <- err
	}()
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotInlineQuery, func(u *mtproto.Update) bool {
		if u.GetQuery() != "cats" {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetInlineBotResults{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setInlineBotResults),
		QueryId:     queryId,
		CacheTime:   0,
		Results: []*mtproto.InputBotInlineResult{mtproto.MakeTLInputBotInlineResult(&mtproto.InputBotInlineResult{
			Id: "1", Type: "article", Title: wrapperspb.String("Cat"),
			SendMessage: mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: "a cat"}).To_InputBotInlineMessage(),
		}).To_InputBotInlineResult()},
	}); err != nil {
		t.Fatalf("messages.setInlineBotResults: %v", err)
	}
	if err = <-inline; err != nil {
		t.Fatalf("messages.getInlineBotResults: %v", err)
	}
	if len(results.GetResults()) != 1 {
		t.Fatalf("%d results", len(results.GetResults()))
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendInlineBotResult{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendInlineBotResult),
		Peer:        toBot, RandomId: randomID(), QueryId: results.GetQueryId(), Id: "1",
	})
	if err != nil {
		t.Fatalf("messages.sendInlineBotResult: %v", err)
	}
	sent := false
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil && m.GetMessage() == "a cat" && m.GetViaBotId().GetValue() == bot.self.GetId() {
			sent = true
		}
	}
	if !sent {
		t.Fatal("no message via the bot in the answer")
	}
}

// A bot in privacy mode gets only what is meant for it in a group
// (core.telegram.org/bots/features#privacy-mode): the bot's getDifference
// holds the command for it and not the plain text.
func TestBotPrivacyInGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phones[0], username)

	chatId := createChat(ctx, t, alice, "bots", bob)
	toBot := resolveBot(ctx, t, alice, username)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesAddChatUserCBC6D107{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_addChatUserCBC6D107),
		ChatId:      chatId, UserId: inputUserOfPeer(alice.c, toBot), FwdLimit: 50,
	}); err != nil {
		t.Fatalf("messages.addChatUser: %v", err)
	}
	before := getState(ctx, t, bot)

	group := chatPeer(bob, chatId)
	sendTo(ctx, t, bob, group, "not for the bot")
	command := "/help@" + username + " x"
	sendTo(ctx, t, bob, group, command)
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == command
	})

	o, err := bot.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bot.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         before.GetPts(), Date: before.GetDate(),
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference().GetNewMessages() {
		seen[m.GetMessage()] = true
	}
	if !seen[command] || seen["not for the bot"] {
		t.Fatalf("the bot's messages: %v", seen)
	}
}
