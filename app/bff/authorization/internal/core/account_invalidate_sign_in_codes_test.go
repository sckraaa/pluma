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
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"
)

// account.invalidateSignInCodes drops the pending request of the caller's
// phone whose code it names, and answers boolTrue also when nothing matched.
func TestAccountInvalidateSignInCodes(t *testing.T) {
	e := newPasswordEnv(t)
	code, err := e.codes.Issue(e.ctx, pwPhone, "h-shown", "203.0.113.9", true)
	if err != nil {
		t.Fatal(err)
	}
	// A request of another account with the same code stays.
	if _, err := e.codes.Issue(e.ctx, freePhone, "h-other", "203.0.113.9", false); err != nil {
		t.Fatal(err)
	}
	other, err := e.codes.Reissue(e.ctx, freePhone, "")
	if err != nil {
		t.Fatal(err)
	}

	invalidate := func(codes ...string) (*mtproto.Bool, error) {
		return e.core(pwKey, pwUserId).AccountInvalidateSignInCodes(&mtproto.TLAccountInvalidateSignInCodes{Codes: codes})
	}
	miss := "000000"
	if code == miss || other.Code == miss {
		miss = "111111"
	}
	if r, err := invalidate(miss, "not a code"); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("no code matched: %v, %v; want boolTrue", r, err)
	}
	if r, err := invalidate(code[:3]+"-"+code[3:], other.Code); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("the shown code: %v, %v; want boolTrue", r, err)
	}
	if got := rpcErrorMessage(e.codes.Verify(e.ctx, pwPhone, "h-shown", code, "203.0.113.9")); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("auth.signIn with the shown code: %s, want PHONE_CODE_EXPIRED", got)
	}
	if err := e.codes.Verify(e.ctx, freePhone, "h-other", other.Code, "203.0.113.9"); err != nil {
		t.Fatalf("the code of another account: %v", err)
	}

	tooMany := make([]string, logincode.MaxInvalidateCodes+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%06d", i)
	}
	if _, err := invalidate(tooMany...); rpcErrorMessage(err) != "LIMIT_INVALID" {
		t.Fatalf("%d codes: %v, want LIMIT_INVALID", len(tooMany), err)
	}
	bot := e.core(pwKey, pwUserId)
	bot.MD.IsBot = true
	if _, err := bot.AccountInvalidateSignInCodes(&mtproto.TLAccountInvalidateSignInCodes{Codes: []string{code}}); rpcErrorMessage(err) != "BOT_METHOD_INVALID" {
		t.Fatalf("a bot: %v, want BOT_METHOD_INVALID", err)
	}
}
