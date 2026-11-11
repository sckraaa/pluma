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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

var fakeCodesAddr = os.Getenv("PLUMA_E2E_FAKECODES")

// senderPhone is phone as bff gives it to the sender: digits only.
func senderPhone(phone string) string { return strings.TrimPrefix(phone, "+") }

// codesAnswer makes test/e2e/fakecodes answer result for phone.
func codesAnswer(t *testing.T, phone, result string) {
	t.Helper()
	resp, err := http.Post("http://"+fakeCodesAddr+"/e2e/result?phone="+senderPhone(phone)+"&result="+result, "", nil)
	if err != nil {
		t.Fatalf("fakecodes: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fakecodes: %s", resp.Status)
	}
}

// codesWritten are the codes fakecodes "wrote" to phone's Telegram.
func codesWritten(t *testing.T, phone string) []string {
	t.Helper()
	resp, err := http.Get("http://" + fakeCodesAddr + "/e2e/codes?phone=" + url.QueryEscape(senderPhone(phone)))
	if err != nil {
		t.Fatalf("fakecodes: %v", err)
	}
	defer resp.Body.Close()
	var codes []string
	if err := json.NewDecoder(resp.Body).Decode(&codes); err != nil {
		t.Fatalf("fakecodes: %v", err)
	}
	return codes
}

func sentCodeOf(t *testing.T, o mtproto.TLObject, err error, step string) *mtproto.TLAuthSentCode {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
	sent, ok := o.(*mtproto.TLAuthSentCode)
	if !ok {
		t.Fatalf("%s returned %T", step, o)
	}
	return sent
}

// The login code screens of task 076: a code in the person's Telegram is
// sentCodeTypeFragmentSms with the chat of the account; "Didn't get the
// code?" leads to the screen of a code that did not reach Telegram
// (sentCodeTypeSms, "Send again" after 30 s); "Send again" tries Telegram
// anew; the administrator's code works throughout.
func TestLoginCodeThroughTelegram(t *testing.T) {
	if fakeCodesAddr == "" {
		t.Skip("PLUMA_E2E_FAKECODES is not set: the stand has no code sender")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	admin(t, "allowlist", "add", phone, "e2e test")
	codesAnswer(t, phone, "sent")

	c, err := mtclient.Dial(ctx, config(t, "e2e codes"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, "codes", c)

	sendCode := &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone,
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
	typ := sent.GetType()
	if typ.GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeFragmentSms || typ.GetUrl() != "tg://resolve?domain=pluma_codes" {
		t.Fatalf("a code in Telegram: %s %q", typ.GetPredicateName(), typ.GetUrl())
	}
	if sent.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || sent.GetTimeout() != nil {
		t.Fatalf("next %v, timeout %v", sent.GetNextType(), sent.GetTimeout())
	}
	hash := sent.GetPhoneCodeHash()
	if n := len(codesWritten(t, phone)); n != 1 {
		t.Fatalf("the account wrote %d codes", n)
	}

	resend := func(step string) *mtproto.TLAuthSentCode {
		o, err := c.Invoke(ctx, &mtproto.TLAuthResendCode{
			Constructor:   c.Constructor(mtproto.Predicate_auth_resendCode),
			PhoneNumber:   phone,
			PhoneCodeHash: hash,
		})
		return sentCodeOf(t, o, err, step)
	}

	// "Didn't get the code?": the fallback, nothing written again.
	fallback := resend("didn't get the code")
	if fallback.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms ||
		fallback.GetNextType().GetPredicateName() != mtproto.Predicate_auth_codeTypeSms || fallback.GetTimeout().GetValue() != 30 {
		t.Fatalf("fallback: %v", fallback)
	}
	if n := len(codesWritten(t, phone)); n != 1 {
		t.Fatalf("the account wrote again: %d codes", n)
	}

	// "Send again" while Telegram still cannot find the person.
	codesAnswer(t, phone, "no_telegram")
	if again := resend("send again, not found"); again.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeSms {
		t.Fatalf("send again, not found: %v", again.GetType())
	}

	// The administrator's code, then "Send again" after the person allowed
	// Telegram to find them: a new code in Telegram, both valid.
	m := loginCodeRe.FindStringSubmatch(admin(t, "login-code", phone))
	if m == nil {
		t.Fatal("pluma-admin login-code printed no code")
	}
	codesAnswer(t, phone, "sent")
	if again := resend("send again, found"); again.GetType().GetPredicateName() != mtproto.Predicate_auth_sentCodeTypeFragmentSms {
		t.Fatalf("send again, found: %v", again.GetType())
	}
	if n := len(codesWritten(t, phone)); n != 2 {
		t.Fatalf("the account wrote %d codes", n)
	}

	o, err = c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor:          c.Constructor(mtproto.Predicate_auth_signIn),
		PhoneNumber:          phone,
		PhoneCodeHash:        hash,
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(m[1]),
	})
	if err != nil {
		t.Fatalf("auth.signIn with the administrator's code: %v", err)
	}
	if _, ok := o.(*mtproto.TLAuthAuthorizationSignUpRequired); !ok {
		t.Fatalf("auth.signIn returned %T, want a sign-up for a new phone", o)
	}
}
