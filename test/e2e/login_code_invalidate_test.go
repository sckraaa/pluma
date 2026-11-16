//go:build e2e

package e2e

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// appLoginCodeRe finds the code in the message of the service notifications
// account (app/bff/authorization/servicetext, loginCode).
var appLoginCodeRe = regexp.MustCompile(`^Login code: (\d+)\.`)

// appCodeRequest is a login of a new device whose code went to the
// account's other session, as a message of the service notifications
// account.
type appCodeRequest struct {
	c       *mtclient.Client
	phone   string
	hash    string
	code    string
	message *mtproto.Message // in the chat of owner with the service account
}

// requestAppCode connects a new device, asks for a login code for owner's
// phone and waits for it in owner's chat with the service account.
func requestAppCode(ctx context.Context, t *testing.T, owner *user, device string) *appCodeRequest {
	t.Helper()
	c, err := mtclient.Dial(ctx, config(t, device))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, device, c)
	// The code comes about 2 s after the request (pushSignInMessage); the
	// chat may hold the codes of earlier requests, all older than before.
	var before int32
	if old := serviceMessages(ctx, t, owner); len(old) > 0 {
		before = old[0].GetId()
	}
	sendCode := &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: owner.phone,
		ApiId:       1,
		ApiHash:     "e2e",
		Settings:    &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	}
	o, err := c.Invoke(ctx, sendCode)
	if e := (*mtclient.RPCError)(nil); errors.As(err, &e) && strings.HasPrefix(e.Message, "FLOOD_WAIT_") {
		resetLoginsPerAddress(t)
		o, err = c.Invoke(ctx, sendCode)
	}
	sent := sentCodeOf(t, o, err, "auth.sendCode")
	if sent.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeApp {
		t.Fatalf("auth.sendCode: %s, want the code in the other session", sent.GetType().GetPredicateName())
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		for _, m := range serviceMessages(ctx, t, owner) { // newest first
			if m.GetId() <= before {
				break
			}
			if found := appLoginCodeRe.FindStringSubmatch(m.GetMessage()); found != nil {
				return &appCodeRequest{c: c, phone: owner.phone, hash: sent.GetPhoneCodeHash(), code: found[1], message: m}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no login code in the chat with %d after message %d", serviceUserId, before)
		}
	}
}

// signIn tries the code on the new device.
func (r *appCodeRequest) signIn(ctx context.Context) error {
	_, err := r.c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor:          r.c.Constructor(mtproto.Predicate_auth_signIn),
		PhoneNumber:          r.phone,
		PhoneCodeHash:        r.hash,
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(r.code),
	})
	return err
}

// TestInvalidateSignInCodes (task 098): the codes a client names in
// account.invalidateSignInCodes, as iOS does on a screenshot of the chat
// with the service account, no longer log in; a call that names no live
// code is answered all the same and leaves the code working.
func TestInvalidateSignInCodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	getState(ctx, t, alice)

	invalidate := func(codes ...string) {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountInvalidateSignInCodes{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_invalidateSignInCodes),
			Codes:       codes,
		})
		if _, ok := o.(*mtproto.TLBoolTrue); err != nil || !ok {
			t.Fatalf("account.invalidateSignInCodes: %T, %v; want boolTrue", o, err)
		}
	}

	shown := requestAppCode(ctx, t, alice, "e2e screenshot")
	miss := "000000"
	if shown.code == miss {
		miss = "111111"
	}
	invalidate(miss)
	invalidate(shown.code)
	wantRPCError(t, shown.signIn(ctx), "PHONE_CODE_EXPIRED")

	// A code nobody showed still logs in after a call without it.
	kept := requestAppCode(ctx, t, alice, "e2e kept")
	miss = "000000"
	if kept.code == miss {
		miss = "111111"
	}
	invalidate(miss)
	if err := kept.signIn(ctx); err != nil {
		t.Fatalf("auth.signIn with a code that was not shown: %v", err)
	}
}

// TestLoginCodeForwardedIsInvalidated (task 098): a code the account
// forwards from the chat with the service account, or pastes as text, is
// invalidated by the server, and the message still reaches the other chat.
func TestLoginCodeForwardedIsInvalidated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)

	t.Run("forwarded", func(t *testing.T) {
		r := requestAppCode(ctx, t, alice, "e2e forwarded")
		service := serviceChat(ctx, t, alice)
		if service == nil {
			t.Fatal("no chat with the service account")
		}
		if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_forwardMessages),
			FromPeer:    service,
			Id:          []int32{r.message.GetId()},
			RandomId:    []int64{randomID()},
			ToPeer:      toBob,
		}); err != nil {
			t.Fatalf("messages.forwardMessages: %v", err)
		}
		wantRPCError(t, r.signIn(ctx), "PHONE_CODE_EXPIRED")
		historyMessage(ctx, t, bob, toAlice, r.message.GetMessage())
	})

	t.Run("pasted", func(t *testing.T) {
		r := requestAppCode(ctx, t, alice, "e2e pasted")
		// As a person types it, with dashes.
		text := "my code is " + r.code[:2] + "-" + r.code[2:4] + "-" + r.code[4:]
		sendMessageID(ctx, t, alice, toBob, text, randomID())
		wantRPCError(t, r.signIn(ctx), "PHONE_CODE_EXPIRED")
		historyMessage(ctx, t, bob, toAlice, text)
	})
}
