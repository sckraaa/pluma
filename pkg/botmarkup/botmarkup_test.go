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

package botmarkup

import (
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func inlineOf(b *mtproto.KeyboardButton) *mtproto.ReplyMarkup {
	return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{b}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
}

func none(*mtproto.InputUser) int64 { return 0 }

func TestNormalizeCallback(t *testing.T) {
	ok := mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte("x"), RequiresPassword: true}).To_KeyboardButton()
	got, err := Normalize(inlineOf(ok), none)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetRows()[0].GetButtons()[0].GetRequiresPassword() {
		t.Error("requires_password stayed")
	}

	big := mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte(strings.Repeat("x", maxCallbackData+1))}).To_KeyboardButton()
	if _, err := Normalize(inlineOf(big), none); !errors.Is(err, mtproto.ErrButtonDataInvalid) {
		t.Errorf("long data: %v", err)
	}
	empty := mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go"}).To_KeyboardButton()
	if _, err := Normalize(inlineOf(empty), none); !errors.Is(err, mtproto.ErrButtonDataInvalid) {
		t.Errorf("no data: %v", err)
	}
	blank := mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "  ", Data: []byte("x")}).To_KeyboardButton()
	if _, err := Normalize(inlineOf(blank), none); !errors.Is(err, mtproto.ErrButtonTextInvalid) {
		t.Errorf("no text: %v", err)
	}
}

func TestNormalizeInputButtons(t *testing.T) {
	profile := mtproto.MakeTLInputKeyboardButtonUserProfile(&mtproto.KeyboardButton{
		Text: "Who", UserId_INPUTUSER: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 7}).To_InputUser(),
	}).To_KeyboardButton()
	got, err := Normalize(inlineOf(profile), func(u *mtproto.InputUser) int64 { return u.GetUserId() })
	if err != nil {
		t.Fatal(err)
	}
	b := got.GetRows()[0].GetButtons()[0]
	if b.GetPredicateName() != mtproto.Predicate_keyboardButtonUserProfile || b.GetUserId_INT64() != 7 {
		t.Errorf("got %s %d", b.GetPredicateName(), b.GetUserId_INT64())
	}
	if _, err := Normalize(inlineOf(profile), none); !errors.Is(err, mtproto.ErrUserIdInvalid) {
		t.Errorf("unknown user: %v", err)
	}

	peer := mtproto.MakeTLInputKeyboardButtonRequestPeer(&mtproto.KeyboardButton{Text: "Pick", ButtonId: 3, MaxQuantity: 1}).To_KeyboardButton()
	if got, err = Normalize(inlineOf(peer), none); err != nil || got.GetRows()[0].GetButtons()[0].GetPredicateName() != mtproto.Predicate_keyboardButtonRequestPeer {
		t.Errorf("request peer: %v", err)
	}

	login := mtproto.MakeTLInputKeyboardButtonUrlAuth(&mtproto.KeyboardButton{Text: "Log in", Url: "https://a.b"}).To_KeyboardButton()
	if _, err := Normalize(inlineOf(login), none); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("login: %v", err)
	}
}

func TestNormalizeKinds(t *testing.T) {
	if got, err := Normalize(nil, none); got != nil || err != nil {
		t.Errorf("nil: %v %v", got, err)
	}
	hide := mtproto.MakeTLReplyKeyboardHide(&mtproto.ReplyMarkup{}).To_ReplyMarkup()
	if _, err := Normalize(hide, none); err != nil {
		t.Error(err)
	}
	if _, err := Normalize(&mtproto.ReplyMarkup{PredicateName: "nothing"}, none); !errors.Is(err, mtproto.ErrReplyMarkupInvalid) {
		t.Errorf("unknown kind: %v", err)
	}
	// A callback button belongs to an inline keyboard only.
	cb := mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte("x")}).To_KeyboardButton()
	reply := mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{cb}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
	if _, err := Normalize(reply, none); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("callback on a reply keyboard: %v", err)
	}
}
