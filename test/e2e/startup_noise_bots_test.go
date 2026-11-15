//go:build e2e

package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The attachment menu and the dice game, which clients ask for on start
// (task 098): iOS TelegramCore/Sources/State/AccountTaskManager.swift:110,
// 137, Web appAttachMenuBotsManager.ts:70. The menu is empty with hash 0,
// the game unavailable; the methods of these services the server does not
// serve answer 400, which clients do not repeat, never a 500, which tgnet
// does.
func TestStartupNoiseBotMenu(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	for _, hash := range []int64{0, 12345} {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetAttachMenuBots{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getAttachMenuBots),
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("messages.getAttachMenuBots(%d): %v", hash, err)
		}
		r := o.(interface {
			To_AttachMenuBots() *mtproto.AttachMenuBots
		}).To_AttachMenuBots()
		if r.GetPredicateName() != mtproto.Predicate_attachMenuBots || r.GetHash() != 0 || len(r.GetBots()) != 0 || len(r.GetUsers()) != 0 {
			t.Errorf("messages.getAttachMenuBots(%d): %v, want attachMenuBots with nothing", hash, r)
		}
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetEmojiGameInfo{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getEmojiGameInfo),
	})
	if err != nil {
		t.Fatalf("messages.getEmojiGameInfo: %v", err)
	}
	if r := o.(interface {
		To_Messages_EmojiGameInfo() *mtproto.Messages_EmojiGameInfo
	}).To_Messages_EmojiGameInfo(); r.GetPredicateName() != mtproto.Predicate_messages_emojiGameUnavailable {
		t.Errorf("messages.getEmojiGameInfo: %s, want messages.emojiGameUnavailable", r.GetPredicateName())
	}

	for name, req := range map[string]mtproto.TLObject{
		"messages.getAttachMenuBot": &mtproto.TLMessagesGetAttachMenuBot{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getAttachMenuBot),
			Bot:         &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)},
		},
		"messages.getGameHighScores": &mtproto.TLMessagesGetGameHighScores{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getGameHighScores),
			Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
			Id:          1,
			UserId:      &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)},
		},
	} {
		_, err := alice.c.Invoke(ctx, req)
		var e *mtclient.RPCError
		if !errors.As(err, &e) || e.Code != 400 || e.Message != "ERR_ENTERPRISE_IS_BLOCKED" {
			t.Errorf("%s: %v, want 400 ERR_ENTERPRISE_IS_BLOCKED", name, err)
		}
	}
}
