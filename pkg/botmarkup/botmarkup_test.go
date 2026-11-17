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
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/layercompat"
)

func inlineOf(b *mtproto.KeyboardButton) *mtproto.ReplyMarkup {
	return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{b}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
}

// none resolves no user and knows no bot.
var none = Env{ResolveUser: func(*mtproto.InputUser) int64 { return 0 }}

// byId resolves inputUser by its id.
func byId(u *mtproto.InputUser) int64 { return u.GetUserId() }

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
	got, err := Normalize(inlineOf(profile), Env{ResolveUser: byId})
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

	// The received form of a login button is not taken from a bot: it
	// names no login bot.
	received := mtproto.MakeTLKeyboardButtonUrlAuth(&mtproto.KeyboardButton{Text: "Log in", Url: "https://a.example", ButtonId: 1}).To_KeyboardButton()
	if _, err := Normalize(inlineOf(received), none); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("received login button: %v", err)
	}
}

// loginEnv is the bot 10 sending, with the bots 10 (domain a.example) and
// 11 (domain b.example), 12 a bot without a domain and 13 a user.
func loginEnv() Env {
	return Env{
		Self:        10,
		ResolveUser: byId,
		LoginDomain: func(id int64) (string, bool, error) {
			switch id {
			case 10:
				return "a.example", true, nil
			case 11:
				return "b.example", true, nil
			case 12:
				return "", true, nil
			}
			return "", false, nil
		},
	}
}

func loginInput(url string, bot *mtproto.InputUser, write bool) *mtproto.KeyboardButton {
	return mtproto.MakeTLInputKeyboardButtonUrlAuth(&mtproto.KeyboardButton{
		Text: "Log in", FwdText: mtproto.MakeFlagsString("Log in to the site"), Url: url, Bot: bot, RequestWriteAccess: write,
	}).To_KeyboardButton()
}

func inputUser(id int64) *mtproto.InputUser {
	return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id}).To_InputUser()
}

func TestNormalizeLoginButton(t *testing.T) {
	plain := mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Site", Url: "https://a.example/about"}).To_KeyboardButton()
	markup := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{plain}}).To_KeyboardButtonRow(),
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
				loginInput("https://A.example:8443/login?next=1", mtproto.MakeTLInputUserSelf(nil).To_InputUser(), true),
				loginInput("https://b.example/login", inputUser(11), false),
			}}).To_KeyboardButtonRow(),
		},
	}).To_ReplyMarkup()
	env := loginEnv()
	env.ResolveUser = func(u *mtproto.InputUser) int64 {
		if u.GetPredicateName() == mtproto.Predicate_inputUserSelf {
			return env.Self
		}
		return u.GetUserId()
	}
	got, err := Normalize(markup, env)
	if err != nil {
		t.Fatal(err)
	}
	own, other := got.GetRows()[1].GetButtons()[0], got.GetRows()[1].GetButtons()[1]
	if own.GetPredicateName() != mtproto.Predicate_keyboardButtonUrlAuth || own.GetButtonId() != 2 || own.GetUserId_INT64() != 10 ||
		!own.GetRequestWriteAccess() || own.GetFwdText().GetValue() != "Log in to the site" || own.GetUrl() != "https://A.example:8443/login?next=1" {
		t.Errorf("own login button: %s id %d bot %d write %v", own.GetPredicateName(), own.GetButtonId(), own.GetUserId_INT64(), own.GetRequestWriteAccess())
	}
	if other.GetButtonId() != 3 || other.GetUserId_INT64() != 11 || other.GetRequestWriteAccess() || other.GetBot() != nil {
		t.Errorf("other bot's login button: id %d bot %d write %v", other.GetButtonId(), other.GetUserId_INT64(), other.GetRequestWriteAccess())
	}

	// A button of layer 229 and later may leave out its bot: the bot of the
	// message.
	if got, err := Normalize(inlineOf(loginInput("https://a.example", nil, false)), loginEnv()); err != nil || got.GetRows()[0].GetButtons()[0].GetUserId_INT64() != 10 {
		t.Errorf("no bot: %v", err)
	}

	for name, c := range map[string]struct {
		button *mtproto.KeyboardButton
		want   error
	}{
		"other domain":       {loginInput("https://b.example", nil, false), mtproto.ErrBotDomainInvalid},
		"subdomain":          {loginInput("https://www.a.example", nil, false), mtproto.ErrBotDomainInvalid},
		"bot without domain": {loginInput("https://a.example", inputUser(12), false), mtproto.ErrBotDomainInvalid},
		"not a bot":          {loginInput("https://a.example", inputUser(13), false), mtproto.ErrBotInvalid},
		"nobody":             {loginInput("https://a.example", inputUser(0), false), mtproto.ErrBotInvalid},
		"http":               {loginInput("http://a.example", nil, false), mtproto.ErrButtonUrlInvalid},
		"no host":            {loginInput("https:///login", nil, false), mtproto.ErrButtonUrlInvalid},
		"user info":          {loginInput("https://me@a.example", nil, false), mtproto.ErrButtonUrlInvalid},
	} {
		if _, err := Normalize(inlineOf(c.button), loginEnv()); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}

	// A login button belongs to an inline keyboard only.
	reply := mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			loginInput("https://a.example", nil, false),
		}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
	if _, err := Normalize(reply, loginEnv()); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("login button on a reply keyboard: %v", err)
	}
}

// The login bot and request_write_access kept in a stored login button never
// reach a client: a message with the button encodes the same as with the
// button without them, in the layer of the server (where layercompat turns
// it into inlineButtonTypeUrlAuth) and in layer 228.
func TestLoginButtonEncodingHidesBot(t *testing.T) {
	got, err := Normalize(inlineOf(loginInput("https://a.example", nil, true)), loginEnv())
	if err != nil {
		t.Fatal(err)
	}
	stored := got.GetRows()[0].GetButtons()[0]
	bare := mtproto.MakeTLKeyboardButtonUrlAuth(&mtproto.KeyboardButton{
		Text: stored.GetText(), FwdText: stored.GetFwdText(), Url: stored.GetUrl(), ButtonId: stored.GetButtonId(),
	}).To_KeyboardButton()
	message := func(b *mtproto.KeyboardButton) mtproto.TLObject {
		return mtproto.MakeTLMessage(&mtproto.Message{
			Id: 5, FromId: mtproto.MakePeerUser(10), PeerId: mtproto.MakePeerUser(20), Date: 1_700_000_000, Message: "log in",
			ReplyMarkup: inlineOf(b),
		}).To_Message()
	}
	for _, layer := range []int32{228, 233} {
		a, err := layercompat.EncodeFor(message(stored), layer)
		if err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		b, err := layercompat.EncodeFor(message(bare), layer)
		if err != nil {
			t.Fatalf("layer %d: %v", layer, err)
		}
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Errorf("layer %d: the stored button encodes as % x, the bare one as % x", layer, a, b)
		}
	}
	if stored.GetUserId_INT64() != 10 || !stored.GetRequestWriteAccess() {
		t.Errorf("the encoding changed the stored button: bot %d, write %v", stored.GetUserId_INT64(), stored.GetRequestWriteAccess())
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

// The game's button is the first button of the first row of an inline
// keyboard of a message with a game, and nowhere else.
func TestNormalizeGameButton(t *testing.T) {
	game := func() *mtproto.KeyboardButton {
		return mtproto.MakeTLKeyboardButtonGame(&mtproto.KeyboardButton{Text: "Play"}).To_KeyboardButton()
	}
	cb := func() *mtproto.KeyboardButton {
		return mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte("x")}).To_KeyboardButton()
	}
	rows := func(maker func(...*mtproto.KeyboardButtonRow) *mtproto.ReplyMarkup, rows ...[]*mtproto.KeyboardButton) *mtproto.ReplyMarkup {
		out := make([]*mtproto.KeyboardButtonRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: r}).To_KeyboardButtonRow())
		}
		return maker(out...)
	}
	inline := func(r ...*mtproto.KeyboardButtonRow) *mtproto.ReplyMarkup {
		return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: r}).To_ReplyMarkup()
	}
	reply := func(r ...*mtproto.KeyboardButtonRow) *mtproto.ReplyMarkup {
		return mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{Rows: r}).To_ReplyMarkup()
	}
	withGame := none
	withGame.Game = true

	if _, err := Normalize(rows(inline, []*mtproto.KeyboardButton{game(), cb()}, []*mtproto.KeyboardButton{cb()}), withGame); err != nil {
		t.Errorf("the game's button first: %v", err)
	}
	for name, tc := range map[string]struct {
		markup *mtproto.ReplyMarkup
		env    Env
	}{
		"a message without a game": {rows(inline, []*mtproto.KeyboardButton{game()}), none},
		"second in the first row":  {rows(inline, []*mtproto.KeyboardButton{cb(), game()}), withGame},
		"first of the second row":  {rows(inline, []*mtproto.KeyboardButton{cb()}, []*mtproto.KeyboardButton{game()}), withGame},
		"after an empty first row": {rows(inline, []*mtproto.KeyboardButton{}, []*mtproto.KeyboardButton{game()}), withGame},
		"twice":                    {rows(inline, []*mtproto.KeyboardButton{game(), game()}), withGame},
		"a reply keyboard":         {rows(reply, []*mtproto.KeyboardButton{game()}), withGame},
	} {
		if _, err := Normalize(tc.markup, tc.env); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
			t.Errorf("%s: %v, want BUTTON_TYPE_INVALID", name, err)
		}
	}
}

// The requested parts of a request-peer button stay on the stored button for
// messages.sendBotRequestedPeer, and its TL form does not carry them.
func TestRequestPeerKeepsRequestedParts(t *testing.T) {
	in := mtproto.MakeTLInputKeyboardButtonRequestPeer(&mtproto.KeyboardButton{
		Text: "Pick", ButtonId: 3, MaxQuantity: 2, NameRequested: true, PhotoRequested: true,
		PeerType: mtproto.MakeTLRequestPeerTypeUser(&mtproto.RequestPeerType{}).To_RequestPeerType(),
	}).To_KeyboardButton()
	got, err := Normalize(mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{in}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup(), none)
	if err != nil {
		t.Fatal(err)
	}
	b := got.GetRows()[0].GetButtons()[0]
	if b.GetPredicateName() != mtproto.Predicate_keyboardButtonRequestPeer || !b.GetNameRequested() || b.GetUsernameRequested() || !b.GetPhotoRequested() {
		t.Fatalf("stored: %v", b)
	}
	x := mtproto.NewEncodeBuf(64)
	if err = b.Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	d := mtproto.NewDecodeBuf(x.GetBuf())
	back := &mtproto.KeyboardButton{}
	if err = back.Decode(d); err != nil || back.GetNameRequested() || back.GetPhotoRequested() || back.GetButtonId() != 3 {
		t.Fatalf("decoded: %v %v", back, err)
	}
}

func TestPreparedPeerButton(t *testing.T) {
	user := mtproto.MakeTLRequestPeerTypeUser(&mtproto.RequestPeerType{}).To_RequestPeerType()
	b, err := PreparedPeerButton(mtproto.MakeTLInputKeyboardButtonRequestPeer(&mtproto.KeyboardButton{
		Text: "Pick", ButtonId: 9, MaxQuantity: 1, UsernameRequested: true, PeerType: user,
	}).To_KeyboardButton())
	if err != nil || b.GetPredicateName() != mtproto.Predicate_keyboardButtonRequestPeer || b.GetButtonId() != 9 || !b.GetUsernameRequested() {
		t.Fatalf("input form: %v %v", b, err)
	}
	// The typed form of layer 229 and later.
	typed := mtproto.MakeTLKeyboardButton(&mtproto.KeyboardButton{
		Text: "Pick",
		Type: mtproto.MakeTLInputButtonTypeRequestPeer(&mtproto.ButtonType{ButtonId: 2, MaxQuantity: 1, NameRequested: true, PeerType: user}).To_ButtonType(),
	}).To_KeyboardButton()
	if b, err = PreparedPeerButton(typed); err != nil || b.GetButtonId() != 2 || !b.GetNameRequested() {
		t.Fatalf("typed form: %v %v", b, err)
	}
	for name, bad := range map[string]*mtproto.KeyboardButton{
		"callback":     mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "x", Data: []byte("d")}).To_KeyboardButton(),
		"no text":      mtproto.MakeTLInputKeyboardButtonRequestPeer(&mtproto.KeyboardButton{Text: " ", PeerType: user}).To_KeyboardButton(),
		"no peer type": mtproto.MakeTLInputKeyboardButtonRequestPeer(&mtproto.KeyboardButton{Text: "Pick"}).To_KeyboardButton(),
		"none":         nil,
	} {
		if _, err := PreparedPeerButton(bad); !errors.Is(err, mtproto.ErrButtonInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
