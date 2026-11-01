//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

const serviceUserId = 777000

// serviceChat finds the chat with the service notifications account among
// u's dialogs, as a client opens it, and returns its peer; nil while there
// is none.
func serviceChat(ctx context.Context, t *testing.T, u *user) *mtproto.InputPeer {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getDialogs: %v", err)
	}
	d := o.(interface {
		To_Messages_Dialogs() *mtproto.Messages_Dialogs
	}).To_Messages_Dialogs()
	found := false
	for _, dl := range d.GetDialogs() {
		found = found || dl.GetPeer().GetUserId() == serviceUserId
	}
	for _, x := range d.GetUsers() {
		if found && x.GetId() == serviceUserId {
			return &mtproto.InputPeer{
				Constructor: u.c.Constructor(mtproto.Predicate_inputPeerUser),
				UserId:      serviceUserId,
				AccessHash:  x.GetAccessHash().GetValue(),
			}
		}
	}
	return nil
}

// serviceMessages returns u's chat with the service account, newest first;
// none while there is no chat.
func serviceMessages(ctx context.Context, t *testing.T, u *user) []*mtproto.Message {
	t.Helper()
	peer := serviceChat(ctx, t, u)
	if peer == nil {
		return nil
	}
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	return o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
}

// serviceTexts waits until u's chat with the service account has a message
// starting with each of prefixes and returns the chat's texts.
func serviceTexts(ctx context.Context, t *testing.T, u *user, prefixes ...string) []string {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		var texts []string
		for _, m := range serviceMessages(ctx, t, u) {
			texts = append(texts, m.GetMessage())
		}
		missing := ""
		for _, p := range prefixes {
			if countPrefix(texts, p) == 0 {
				missing = p
				break
			}
		}
		if missing == "" {
			return texts
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message %q in the chat with %d: %q", missing, serviceUserId, texts)
		}
	}
}

func countPrefix(texts []string, prefix string) int {
	n := 0
	for _, s := range texts {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// TestServiceChatHistory (task 022, ADR 0026): the messages of the service
// notifications account are messages of the account's history, as at
// Telegram. A device that logs in later finds the earlier ones, the login
// code and New login among them; a change of the cloud password leaves its
// message there too. Sessions online get them as new messages, not as
// updateServiceNotification. The test phones are reused, so the chat may
// hold messages of earlier runs: they are counted from what is there.
func TestServiceChatHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]

	phoneApp := loginIn(ctx, t, phone, "Vera", "en")
	getState(ctx, t, phoneApp)
	const (
		code     = "Login code:"
		newLogin = "New login. Dear "
		password = "Two-Step Verification settings changed. "
	)
	// The phone's account may have sessions of earlier runs: then the first
	// login has a code and a New login of its own, which come
	// asynchronously, the code 2 s after the request (pushSignInMessage).
	// Messages are counted from since, after they are in.
	time.Sleep(3 * time.Second)
	since := int32(time.Now().Unix())
	// waitFor waits until u's chat has want messages of each prefix dated
	// since or later, and returns their texts.
	waitFor := func(u *user, want map[string]int) []string {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
			var texts []string
			for _, m := range serviceMessages(ctx, t, u) {
				if m.GetDate() >= since {
					texts = append(texts, m.GetMessage())
				}
			}
			ok := true
			for p, n := range want {
				ok = ok && countPrefix(texts, p) == n
			}
			if ok {
				return texts
			}
			if time.Now().After(deadline) {
				t.Fatalf("want %v messages since %d; they are %q", want, since, texts)
			}
		}
	}

	// A second device: its code goes to the first one's chat, and the
	// account gets the New login message.
	second := login(ctx, t, phone, "Vera")
	getState(ctx, t, second)
	waitFor(phoneApp, map[string]int{code: 1, newLogin: 1})
	noPushOf(ctx, t, phoneApp, "the first session", time.Second, mtproto.Predicate_updateServiceNotification, func(*mtproto.Update) bool { return true })

	// A cloud password set from the first device.
	resetPasswordAtEnd(t, phoneApp)
	if err := updatePassword(ctx, t, phoneApp.c, "", "service chat", "", ""); err != nil {
		t.Fatalf("account.updatePasswordSettings: %v", err)
	}
	for _, s := range waitFor(phoneApp, map[string]int{code: 1, newLogin: 1, password: 1}) {
		if strings.HasPrefix(s, password) && (!strings.Contains(s, ", your Two-Step Verification password and/or email were changed on ") ||
			!strings.Contains(s, "\n\nDevice: ") || !strings.Contains(s, "\nLocation: ")) {
			t.Fatalf("the message on the password: %q", s)
		}
	}

	// A device that comes later finds all of them in the chat, with its own
	// code and New login.
	admin(t, "password", "reset", phone)
	third := login(ctx, t, phone, "Vera")
	waitFor(third, map[string]int{code: 2, newLogin: 2, password: 1})
}
