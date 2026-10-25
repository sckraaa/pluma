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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/pushcrypt"
)

// Push notifications (task 009): the push service sends to
// test/e2e/fakepush, which keeps what came; the tests decrypt it with the
// device secret as a client would.

var fakePushAddr = os.Getenv("PLUMA_E2E_FAKEPUSH")

type pushRequest struct {
	Provider string            `json:"provider"`
	Token    string            `json:"token"`
	Headers  map[string]string `json:"headers"`
	Body     json.RawMessage   `json:"body"`
}

// device is a registered push device and its secret.
type device struct {
	tokenType int32
	token     string
	secret    []byte
}

func registerDevice(ctx context.Context, t *testing.T, c *mtclient.Client, tokenType int32) *device {
	t.Helper()
	d := &device{tokenType: tokenType, secret: make([]byte, pushcrypt.SecretLength)}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	_, _ = rand.Read(d.secret)
	d.token = hex.EncodeToString(raw) // an APNs token is hex; FCM takes any
	if _, err := c.Invoke(ctx, &mtproto.TLAccountRegisterDevice{
		Constructor: c.Constructor(mtproto.Predicate_account_registerDevice),
		TokenType:   tokenType,
		Token:       d.token,
		AppSandbox:  mtproto.BoolFalse,
		Secret:      d.secret,
		OtherUids:   []int64{},
	}); err != nil {
		t.Fatalf("account.registerDevice: %v", err)
	}
	return d
}

func pushRequests(t *testing.T, token string) []pushRequest {
	t.Helper()
	resp, err := http.Get("http://" + fakePushAddr + "/requests?token=" + url.QueryEscape(token))
	if err != nil {
		t.Fatalf("fakepush: %v", err)
	}
	defer resp.Body.Close()
	var rs []pushRequest
	if err = json.NewDecoder(resp.Body).Decode(&rs); err != nil {
		t.Fatalf("fakepush: %v", err)
	}
	return rs
}

// payloads returns the decrypted payloads of the pushes to d, and the
// requests.
func payloads(t *testing.T, d *device) ([]map[string]any, []pushRequest) {
	t.Helper()
	var out []map[string]any
	rs := pushRequests(t, d.token)
	for _, r := range rs {
		var body struct {
			P       string `json:"p"`
			Message struct {
				Data map[string]string `json:"data"`
			} `json:"message"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatalf("push body: %v", err)
		}
		p := body.P
		if r.Provider == "fcm" {
			p = body.Message.Data["p"]
		}
		plain, err := pushcrypt.DecodeP(d.secret, p)
		if err != nil {
			t.Fatalf("a push to %s does not decrypt with the device secret: %v", d.token, err)
		}
		var m map[string]any
		if err = json.Unmarshal(plain, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out, rs
}

// waitPush waits for a push to d that match accepts.
func waitPush(ctx context.Context, t *testing.T, d *device, what string, match func(map[string]any) bool) (map[string]any, pushRequest) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ps, rs := payloads(t, d)
		for i, p := range ps {
			if match(p) {
				return p, rs[i]
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", what, ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatalf("%s: no push to %s", what, d.token)
	return nil, pushRequest{}
}

// noPush checks that d got no push that match accepts.
func noPush(t *testing.T, d *device, what string, match func(map[string]any) bool) {
	t.Helper()
	ps, _ := payloads(t, d)
	for _, p := range ps {
		if match(p) {
			t.Fatalf("%s: a push came: %v", what, p)
		}
	}
}

// androidText matches an Android push of a message with text.
func androidText(text string) func(map[string]any) bool {
	return func(p map[string]any) bool {
		args, _ := p["loc_args"].([]any)
		return p["loc_key"] == "MESSAGE_TEXT" && len(args) == 2 && args[1] == text
	}
}

func iosText(text string) func(map[string]any) bool {
	return func(p map[string]any) bool {
		aps, _ := p["aps"].(map[string]any)
		alert, _ := aps["alert"].(map[string]any)
		return alert["body"] == text
	}
}

// signedIn logs Anna in on one more device that syncs as a client does
// (updates.getState): while it is connected its authorization is online.
func signedIn(ctx context.Context, t *testing.T, phone, name string) *user {
	t.Helper()
	u := login(ctx, t, phone, name)
	getState(ctx, t, u)
	return u
}

// offline closes a client; the push service then counts its authorization
// as offline. The status service learns it when the gateway drops the
// connection, a moment later.
func offline(t *testing.T, c *mtclient.Client) {
	t.Helper()
	_ = c.Close()
	time.Sleep(2 * time.Second)
}

func TestPush(t *testing.T) {
	if fakePushAddr == "" {
		t.Skip("PLUMA_E2E_FAKEPUSH is not set: the stand pushes to the real providers")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)

	// Anna's phone (FCM) and iPhone (APNs), both offline later; Bob writes.
	phone := signedIn(ctx, t, phones[0], "Anna")
	androidDev := registerDevice(ctx, t, phone.c, 2)
	iphone := signedIn(ctx, t, phones[0], "Anna")
	iosDev := registerDevice(ctx, t, iphone.c, 1)
	bob := login(ctx, t, phones[1], "Bob")
	toAnna := peerOf(ctx, t, bob, phone)
	offline(t, phone.c)
	offline(t, iphone.c)

	text := fmt.Sprintf("push %d", randomID())
	sendTo(ctx, t, bob, toAnna, text)

	t.Run("android", func(t *testing.T) {
		p, r := waitPush(ctx, t, androidDev, "the message on Android", androidText(text))
		custom, _ := p["custom"].(map[string]any)
		args, _ := p["loc_args"].([]any)
		if p["user_id"] != float64(phone.self.GetId()) || custom["from_id"] != float64(bob.self.GetId()) || custom["msg_id"] == nil ||
			args[0] != "Bob E2E" {
			t.Fatalf("android payload: %v", p)
		}
		if string(r.Body) == "" || r.Provider != "fcm" {
			t.Fatalf("request: %+v", r)
		}
	})

	t.Run("ios", func(t *testing.T) {
		p, r := waitPush(ctx, t, iosDev, "the message on iOS", iosText(text))
		alert := p["aps"].(map[string]any)["alert"].(map[string]any)
		if alert["title"] != "Bob E2E" || p["from_id"] != strconv.FormatInt(bob.self.GetId(), 10) || p["loc-key"] != nil {
			t.Fatalf("ios payload: %v", p)
		}
		var outer map[string]any
		_ = json.Unmarshal(r.Body, &outer)
		aps, _ := outer["aps"].(map[string]any)
		// The text is only in the encrypted payload.
		if r.Headers["apns-push-type"] != "alert" || r.Headers["apns-topic"] != "chat.pluma.app" || aps["mutable-content"] != float64(1) ||
			bytes.Contains(r.Body, []byte(text)) {
			t.Fatalf("APNs request: %+v %s", r.Headers, r.Body)
		}
	})

	t.Run("online and logged out", func(t *testing.T) {
		web := signedIn(ctx, t, phones[0], "Anna")
		webDev := registerDevice(ctx, t, web.c, 2)
		online := fmt.Sprintf("while online %d", randomID())
		sendTo(ctx, t, bob, toAnna, online)
		waitPush(ctx, t, androidDev, "the second message on Android", androidText(online))
		noPush(t, webDev, "the authorization that is online", androidText(online))

		if _, err := web.c.Invoke(ctx, &mtproto.TLAuthLogOut{Constructor: web.c.Constructor(mtproto.Predicate_auth_logOut)}); err != nil {
			t.Fatalf("auth.logOut: %v", err)
		}
		offline(t, web.c)
		after := fmt.Sprintf("after logout %d", randomID())
		sendTo(ctx, t, bob, toAnna, after)
		waitPush(ctx, t, androidDev, "the third message on Android", androidText(after))
		noPush(t, webDev, "the authorization that logged out", androidText(after))
	})

	t.Run("read on another device", func(t *testing.T) {
		reader := signedIn(ctx, t, phones[0], "Anna")
		toBob := peerOf(ctx, t, reader, bob)
		if _, err := reader.c.Invoke(ctx, &mtproto.TLMessagesReadHistory{
			Constructor: reader.c.Constructor(mtproto.Predicate_messages_readHistory),
			Peer:        toBob,
			MaxId:       1 << 30,
		}); err != nil {
			t.Fatalf("messages.readHistory: %v", err)
		}
		p, _ := waitPush(ctx, t, androidDev, "READ_HISTORY on Android", func(p map[string]any) bool { return p["loc_key"] == "READ_HISTORY" })
		if custom, _ := p["custom"].(map[string]any); custom["from_id"] != float64(bob.self.GetId()) || custom["max_id"] == nil {
			t.Fatalf("READ_HISTORY: %v", p)
		}
		waitPush(ctx, t, iosDev, "READ_HISTORY on iOS", func(p map[string]any) bool { return p["loc-key"] == "READ_HISTORY" })
	})

	t.Run("invalid token", func(t *testing.T) {
		gone := signedIn(ctx, t, phones[0], "Anna")
		goneDev := &device{tokenType: 2, secret: androidDev.secret, token: fmt.Sprintf("unregistered-%d", randomID())}
		if _, err := gone.c.Invoke(ctx, &mtproto.TLAccountRegisterDevice{
			Constructor: gone.c.Constructor(mtproto.Predicate_account_registerDevice),
			TokenType:   2, Token: goneDev.token, AppSandbox: mtproto.BoolFalse, Secret: goneDev.secret, OtherUids: []int64{},
		}); err != nil {
			t.Fatalf("account.registerDevice: %v", err)
		}
		offline(t, gone.c)
		// The first push to the token, whatever it says, makes FCM refuse it.
		first := fmt.Sprintf("to a gone token %d", randomID())
		sendTo(ctx, t, bob, toAnna, first)
		waitPush(ctx, t, goneDev, "a push to a gone token", func(map[string]any) bool { return true })
		waitPush(ctx, t, androidDev, "the message on Android", androidText(first))
		before := len(pushRequests(t, goneDev.token))
		second := fmt.Sprintf("again %d", randomID())
		sendTo(ctx, t, bob, toAnna, second)
		waitPush(ctx, t, androidDev, "the next message on Android", androidText(second))
		if n := len(pushRequests(t, goneDev.token)); n != before {
			t.Fatalf("%d more pushes to the token the provider refused", n-before)
		}
	})
}
