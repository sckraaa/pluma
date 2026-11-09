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

package layercompat

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

func inlineMarkup() *mtproto.ReplyMarkup {
	return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
				Buttons: []*mtproto.KeyboardButton{
					mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Press", Data: []byte("x")}).To_KeyboardButton(),
					mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Site", Url: "https://example.org"}).To_KeyboardButton(),
				},
			}).To_KeyboardButtonRow(),
		},
	}).To_ReplyMarkup()
}

func replyKeyboard() *mtproto.ReplyMarkup {
	return mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{
			mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
				Buttons: []*mtproto.KeyboardButton{
					mtproto.MakeTLKeyboardButtonRequestPhone(&mtproto.KeyboardButton{Text: "Phone"}).To_KeyboardButton(),
					mtproto.MakeTLKeyboardButton(&mtproto.KeyboardButton{Text: "Plain"}).To_KeyboardButton(),
				},
			}).To_KeyboardButtonRow(),
		},
	}).To_ReplyMarkup()
}

func message(markup *mtproto.ReplyMarkup, media *mtproto.MessageMedia) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id:          7,
		PeerId:      mtproto.MakePeerUser(1),
		Message:     "text",
		Media:       media,
		ReplyMarkup: markup,
	}).To_Message()
}

func decodeMessage(t *testing.T, b []byte) *mtproto.Message {
	t.Helper()
	x := mtproto.NewDecodeBuf(b)
	m := &mtproto.Message{}
	if err := m.Decode(x); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if x.GetError() != nil || x.GetOffset() != len(b) {
		t.Fatalf("decode: err %v, read %d of %d", x.GetError(), x.GetOffset(), len(b))
	}
	return m
}

func TestStoredKeyboardForNewLayer(t *testing.T) {
	stored := message(inlineMarkup(), nil)
	b, err := EncodeFor(stored, 233)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.GetReplyMarkup().GetRows()) == 0 {
		t.Fatal("the stored markup was changed")
	}
	got := decodeMessage(t, b).GetReplyMarkup()
	rows := got.GetRows_VECTORKEYBOARDINLINEBUTTONROW()
	if len(rows) != 1 || len(rows[0].GetButtons()) != 2 {
		t.Fatalf("rows: %v", got)
	}
	cb := rows[0].GetButtons()[0]
	if cb.GetText() != "Press" || cb.GetType().GetPredicateName() != mtproto.Predicate_inlineButtonTypeCallback || string(cb.GetType().GetData()) != "x" {
		t.Fatalf("callback button: %v", cb)
	}
	if u := rows[0].GetButtons()[1]; u.GetType().GetPredicateName() != mtproto.Predicate_inlineButtonTypeUrl || u.GetType().GetUrl() != "https://example.org" {
		t.Fatalf("url button: %v", u)
	}
}

func TestStoredKeyboardStaysForOldLayer(t *testing.T) {
	stored := message(inlineMarkup(), nil)
	o, err := For(stored, 228)
	if err != nil {
		t.Fatal(err)
	}
	if o != mtproto.TLObject(stored) {
		t.Fatal("a form the layer has must not be copied")
	}
	b, err := EncodeFor(stored, 228)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeMessage(t, b).GetReplyMarkup()
	if got.GetPredicateName() != mtproto.Predicate_replyInlineMarkup || len(got.GetRows()) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestReplyKeyboardTypedForNewLayer(t *testing.T) {
	b, err := EncodeFor(message(replyKeyboard(), nil), 233)
	if err != nil {
		t.Fatal(err)
	}
	buttons := decodeMessage(t, b).GetReplyMarkup().GetRows()[0].GetButtons()
	if buttons[0].GetType().GetPredicateName() != mtproto.Predicate_buttonTypeRequestPhone {
		t.Fatalf("got %v", buttons[0])
	}
	if buttons[1].GetType().GetPredicateName() != mtproto.Predicate_buttonTypeDefault {
		t.Fatalf("got %v", buttons[1])
	}
}

func TestCanonicalBackAndForth(t *testing.T) {
	sent := message(inlineMarkup(), nil)
	o, err := For(sent, 233)
	if err != nil {
		t.Fatal(err)
	}
	onWire := o.(*mtproto.Message).GetReplyMarkup()
	back := Canonical(proto.Clone(onWire).(*mtproto.ReplyMarkup))
	if !proto.Equal(back, inlineMarkup()) {
		t.Fatalf("back: %v\nwant: %v", back, inlineMarkup())
	}

	typed := mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{Rows: []*mtproto.KeyboardButtonRow{
		mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			mtproto.MakeTLKeyboardButton(&mtproto.KeyboardButton{Text: "Phone", Type: mtproto.MakeTLButtonTypeRequestPhone(nil).To_ButtonType()}).To_KeyboardButton(),
		}}).To_KeyboardButtonRow(),
	}}).To_ReplyMarkup()
	Canonical(typed)
	if p := typed.GetRows()[0].GetButtons()[0].GetPredicateName(); p != mtproto.Predicate_keyboardButtonRequestPhone {
		t.Fatalf("got %s", p)
	}
}

func TestUnknownMediaBecomesUnsupported(t *testing.T) {
	widget := mtproto.MakeTLMessageMediaWidget(&mtproto.MessageMedia{Width: 1, Height: 2, Payload: []byte("p")}).To_MessageMedia()
	b, err := EncodeFor(message(nil, widget), 228)
	if err != nil {
		t.Fatal(err)
	}
	if p := decodeMessage(t, b).GetMedia().GetPredicateName(); p != mtproto.Predicate_messageMediaUnsupported {
		t.Fatalf("media: %s", p)
	}
	if widget.GetPredicateName() != mtproto.Predicate_messageMediaWidget {
		t.Fatal("the original was changed")
	}
	// the newer layer keeps it
	b, err = EncodeFor(message(nil, widget), 233)
	if err != nil {
		t.Fatal(err)
	}
	if p := decodeMessage(t, b).GetMedia().GetPredicateName(); p != mtproto.Predicate_messageMediaWidget {
		t.Fatalf("media: %s", p)
	}
}

func TestUnknownUpdateIsLeftOut(t *testing.T) {
	updates := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{
			mtproto.MakeTLUpdateEditForumTopic(&mtproto.Update{Peer_PEER: mtproto.MakePeerChannel(5), TopicId_INT32: 2}).To_Update(),
			mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: 5}).To_Update(),
		},
		Users: []*mtproto.User{},
		Chats: []*mtproto.Chat{},
	}).To_Updates()
	o, err := For(updates, 228)
	if err != nil {
		t.Fatal(err)
	}
	got := o.(*mtproto.Updates)
	if len(got.GetUpdates()) != 1 || got.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Fatalf("got %v", got.GetUpdates())
	}
	if len(updates.GetUpdates()) != 2 {
		t.Fatal("the shared push was changed")
	}
}

func TestRequiredFieldMakesParentGoAway(t *testing.T) {
	// userFull's settings is required: an unknown constructor in it has
	// no fallback, and the object must not be encoded with a hole.
	full := mtproto.MakeTLUserFull(&mtproto.UserFull{Id: 1,
		Settings: &mtproto.PeerSettings{PredicateName: "peerSettingsFromTheFuture"},
	}).To_UserFull()
	if _, err := EncodeFor(full, 233); !errors.Is(err, ErrNotRepresentable) {
		t.Fatalf("got %v", err)
	}
}

func TestChannelFormsByLayer(t *testing.T) {
	ch := mtproto.MakeTLChannel(&mtproto.Chat{Id: 9, Title: "c", ForumSidebar: true, Photo: mtproto.MakeTLChatPhotoEmpty(nil).To_ChatPhoto()}).To_Chat()
	new_, err := EncodeFor(ch, 233)
	if err != nil {
		t.Fatal(err)
	}
	old, err := EncodeFor(ch, 228)
	if err != nil {
		t.Fatal(err)
	}
	if id := int32FromBytes(new_); uint32(id) != 0x7ad15240 {
		t.Fatalf("233: %x", uint32(id))
	}
	if id := int32FromBytes(old); uint32(id) != 0xd49f34c6 {
		t.Fatalf("228: %x", uint32(id))
	}
	if !decodeForumSidebar(t, new_) {
		t.Fatal("forum_sidebar lost at 233")
	}
}

func int32FromBytes(b []byte) int32 {
	return int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16 | int32(b[3])<<24
}

func decodeForumSidebar(t *testing.T, b []byte) bool {
	t.Helper()
	x := mtproto.NewDecodeBuf(b)
	m := &mtproto.Chat{}
	if err := m.Decode(x); err != nil {
		t.Fatal(err)
	}
	return m.GetForumSidebar()
}

func TestInputReplyQuoteRichDecoded(t *testing.T) {
	in := mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 3,
		QuoteRich: []*mtproto.RichText{mtproto.MakeTLTextPlain(&mtproto.RichText{Text_STRING: "hi"}).To_RichText()},
	}).To_InputReplyTo()
	x := mtproto.NewEncodeBuf(64)
	if err := in.Encode(x, 233); err != nil {
		t.Fatal(err)
	}
	d := mtproto.NewDecodeBuf(x.GetBuf())
	got := &mtproto.InputReplyTo{}
	if err := got.Decode(d); err != nil || d.GetError() != nil {
		t.Fatal(err, d.GetError())
	}
	if got.GetReplyToMsgId() != 3 || len(got.GetQuoteRich()) != 1 || got.GetQuoteRich()[0].GetText_STRING() != "hi" {
		t.Fatalf("got %v", got)
	}
}

func TestQuoteRichForOldLayer(t *testing.T) {
	h := mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		QuoteRich: []*mtproto.RichText{
			mtproto.MakeTLTextConcat(&mtproto.RichText{Texts: []*mtproto.RichText{
				mtproto.MakeTLTextPlain(&mtproto.RichText{Text_STRING: "hello "}).To_RichText(),
				mtproto.MakeTLTextBold(&mtproto.RichText{Text_RICHTEXT: mtproto.MakeTLTextPlain(&mtproto.RichText{Text_STRING: "world"}).To_RichText()}).To_RichText(),
			}}).To_RichText(),
		},
	}).To_MessageReplyHeader()
	m := message(nil, nil)
	m.ReplyTo = h
	o, err := For(m, 228)
	if err != nil {
		t.Fatal(err)
	}
	q := o.(*mtproto.Message).GetReplyTo().GetQuoteText().GetValue()
	if q != "hello world" {
		t.Fatalf("quote %q", q)
	}
	if h.GetQuoteText() != nil {
		t.Fatal("the stored header was changed")
	}
	if o2, _ := For(m, 233); o2.(*mtproto.Message).GetReplyTo().GetQuoteText() != nil {
		t.Fatal("layer 232 and later derive the text themselves")
	}
}

func TestEncodeNeverPartial(t *testing.T) {
	// the codec returns the failure of a nested object
	bad := mtproto.MakeTLUser(&mtproto.User{Id: 1, Photo: &mtproto.UserProfilePhoto{PredicateName: "nothing"}}).To_User()
	if err := bad.Encode(mtproto.NewEncodeBuf(32), 228); err == nil {
		t.Fatal("no error for a nested object without a constructor")
	}
}
