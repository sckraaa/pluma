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
	"github.com/teamgram/proto/mtproto"
)

// Keyboards are stored and handled in the shape of layer 228: the buttons of
// keyboardButton* in ReplyMarkup.rows. Layer 229 split them into a button with
// a ButtonType or an InlineButtonType (api.tl of tdesktop v7.1.0 and later) and
// removed the 17 keyboardButton* constructors. Stored messages keep the old
// shape, and all the code that reads a markup is written for it, so the new
// shape exists only at the two edges: Canonical on the way in, For on the way
// out.

// Canonical returns a markup received from a client in the stored shape. A
// markup that already has it is returned as it is.
func Canonical(m *mtproto.ReplyMarkup) *mtproto.ReplyMarkup {
	if m == nil {
		return nil
	}
	if m.GetPredicateName() == mtproto.Predicate_replyInlineMarkup && len(m.GetRows_VECTORKEYBOARDINLINEBUTTONROW()) != 0 {
		m.Rows = inlineRowsToCanonical(m.GetRows_VECTORKEYBOARDINLINEBUTTONROW())
		m.Rows_VECTORKEYBOARDINLINEBUTTONROW = nil
	}
	if m.GetPredicateName() == mtproto.Predicate_replyKeyboardMarkup {
		for _, row := range m.GetRows() {
			for i, b := range row.GetButtons() {
				row.Buttons[i] = canonicalButton(b)
			}
		}
	}
	return m
}

func markupNeedsChange(m *mtproto.ReplyMarkup, layer int32) bool {
	switch m.GetPredicateName() {
	case mtproto.Predicate_replyInlineMarkup:
		if layer >= layerInlineButtons {
			return len(m.GetRows()) != 0
		}
		return len(m.GetRows_VECTORKEYBOARDINLINEBUTTONROW()) != 0
	case mtproto.Predicate_replyKeyboardMarkup:
		for _, row := range m.GetRows() {
			for _, b := range row.GetButtons() {
				if layer >= layerInlineButtons {
					if b.GetPredicateName() != mtproto.Predicate_keyboardButton || b.GetType() == nil {
						return true
					}
				} else if b.GetType() != nil {
					return true
				}
			}
		}
	}
	return false
}

// convertMarkup puts a markup into the shape of the layer.
func convertMarkup(m *mtproto.ReplyMarkup, layer int32) {
	switch m.GetPredicateName() {
	case mtproto.Predicate_replyInlineMarkup:
		if layer >= layerInlineButtons {
			if len(m.GetRows()) != 0 {
				m.Rows_VECTORKEYBOARDINLINEBUTTONROW = canonicalRowsToInline(m.GetRows())
				m.Rows = nil
			}
		} else if len(m.GetRows_VECTORKEYBOARDINLINEBUTTONROW()) != 0 {
			m.Rows = inlineRowsToCanonical(m.GetRows_VECTORKEYBOARDINLINEBUTTONROW())
			m.Rows_VECTORKEYBOARDINLINEBUTTONROW = nil
		}
	case mtproto.Predicate_replyKeyboardMarkup:
		for _, row := range m.GetRows() {
			for i, b := range row.GetButtons() {
				if layer >= layerInlineButtons {
					row.Buttons[i] = canonicalToTypedButton(b)
				} else if b.GetType() != nil {
					row.Buttons[i] = canonicalButton(b)
				}
			}
		}
	}
}

// canonicalRowsToInline: replyInlineMarkup rows of a stored message for a client
// of layer 229 and later.
func canonicalRowsToInline(rows []*mtproto.KeyboardButtonRow) []*mtproto.KeyboardInlineButtonRow {
	out := make([]*mtproto.KeyboardInlineButtonRow, 0, len(rows))
	for _, row := range rows {
		buttons := make([]*mtproto.KeyboardInlineButton, 0, len(row.GetButtons()))
		for _, b := range row.GetButtons() {
			buttons = append(buttons, mtproto.MakeTLKeyboardInlineButton(&mtproto.KeyboardInlineButton{
				Style: b.GetStyle(),
				Text:  b.GetText(),
				Type:  canonicalInlineType(b),
			}).To_KeyboardInlineButton())
		}
		out = append(out, mtproto.MakeTLKeyboardInlineButtonRow(&mtproto.KeyboardInlineButtonRow{Buttons: buttons}).To_KeyboardInlineButtonRow())
	}
	return out
}

func canonicalInlineType(b *mtproto.KeyboardButton) *mtproto.InlineButtonType {
	t := &mtproto.InlineButtonType{}
	switch b.GetPredicateName() {
	case mtproto.Predicate_keyboardButtonUrl:
		t.Url = b.GetUrl()
		return mtproto.MakeTLInlineButtonTypeUrl(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonCallback:
		t.RequiresPassword, t.Data = b.GetRequiresPassword(), b.GetData()
		return mtproto.MakeTLInlineButtonTypeCallback(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonUrlAuth:
		t.FwdText, t.Url, t.ButtonId = b.GetFwdText(), b.GetUrl(), b.GetButtonId()
		return mtproto.MakeTLInlineButtonTypeUrlAuth(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonWebView:
		t.Url = b.GetUrl()
		return mtproto.MakeTLInlineButtonTypeWebView(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonGame:
		return mtproto.MakeTLInlineButtonTypeGame(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonBuy:
		return mtproto.MakeTLInlineButtonTypeBuy(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonSwitchInline:
		t.SamePeer, t.Query, t.PeerTypes = b.GetSamePeer(), b.GetQuery(), b.GetPeerTypes()
		return mtproto.MakeTLInlineButtonTypeSwitchInline(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonUserProfile:
		t.UserId_INT64 = b.GetUserId_INT64()
		return mtproto.MakeTLInlineButtonTypeUserProfile(t).To_InlineButtonType()
	case mtproto.Predicate_keyboardButtonCopy:
		t.CopyText = b.GetCopyText()
		return mtproto.MakeTLInlineButtonTypeCopy(t).To_InlineButtonType()
	}
	return mtproto.MakeTLInlineButtonTypeDisabled(t).To_InlineButtonType()
}

// canonicalToTypedButton: a button of replyKeyboardMarkup for layer 229 and later.
func canonicalToTypedButton(b *mtproto.KeyboardButton) *mtproto.KeyboardButton {
	if b.GetPredicateName() == mtproto.Predicate_keyboardButton && b.GetType() != nil {
		return b
	}
	t := &mtproto.ButtonType{}
	var typ *mtproto.ButtonType
	switch b.GetPredicateName() {
	case mtproto.Predicate_keyboardButtonRequestPhone:
		typ = mtproto.MakeTLButtonTypeRequestPhone(t).To_ButtonType()
	case mtproto.Predicate_keyboardButtonRequestGeoLocation:
		typ = mtproto.MakeTLButtonTypeRequestGeoLocation(t).To_ButtonType()
	case mtproto.Predicate_keyboardButtonRequestPoll:
		t.Quiz = b.GetQuiz()
		typ = mtproto.MakeTLButtonTypeRequestPoll(t).To_ButtonType()
	case mtproto.Predicate_keyboardButtonRequestPeer:
		t.ButtonId, t.PeerType, t.MaxQuantity = b.GetButtonId(), b.GetPeerType(), b.GetMaxQuantity()
		typ = mtproto.MakeTLButtonTypeRequestPeer(t).To_ButtonType()
	case mtproto.Predicate_keyboardButtonSimpleWebView:
		t.Url = b.GetUrl()
		typ = mtproto.MakeTLButtonTypeSimpleWebView(t).To_ButtonType()
	default:
		typ = mtproto.MakeTLButtonTypeDefault(t).To_ButtonType()
	}
	return mtproto.MakeTLKeyboardButton(&mtproto.KeyboardButton{
		Style: b.GetStyle(),
		Text:  b.GetText(),
		Type:  typ,
	}).To_KeyboardButton()
}

// inlineRowsToCanonical: rows of the shape of layer 229 back to the stored one.
func inlineRowsToCanonical(rows []*mtproto.KeyboardInlineButtonRow) []*mtproto.KeyboardButtonRow {
	out := make([]*mtproto.KeyboardButtonRow, 0, len(rows))
	for _, row := range rows {
		buttons := make([]*mtproto.KeyboardButton, 0, len(row.GetButtons()))
		for _, b := range row.GetButtons() {
			buttons = append(buttons, canonicalInlineButton(b))
		}
		out = append(out, mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: buttons}).To_KeyboardButtonRow())
	}
	return out
}

func canonicalInlineButton(b *mtproto.KeyboardInlineButton) *mtproto.KeyboardButton {
	t := b.GetType()
	k := &mtproto.KeyboardButton{Style: b.GetStyle(), Text: b.GetText()}
	switch t.GetPredicateName() {
	case mtproto.Predicate_inlineButtonTypeUrl:
		k.Url = t.GetUrl()
		return mtproto.MakeTLKeyboardButtonUrl(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeCallback:
		k.RequiresPassword, k.Data = t.GetRequiresPassword(), t.GetData()
		return mtproto.MakeTLKeyboardButtonCallback(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeUrlAuth:
		k.FwdText, k.Url, k.ButtonId = t.GetFwdText(), t.GetUrl(), t.GetButtonId()
		return mtproto.MakeTLKeyboardButtonUrlAuth(k).To_KeyboardButton()
	case mtproto.Predicate_inputInlineButtonTypeUrlAuth:
		k.RequestWriteAccess, k.FwdText, k.Url, k.Bot = t.GetRequestWriteAccess(), t.GetFwdText(), t.GetUrl(), t.GetBot()
		return mtproto.MakeTLInputKeyboardButtonUrlAuth(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeWebView:
		k.Url = t.GetUrl()
		return mtproto.MakeTLKeyboardButtonWebView(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeGame:
		return mtproto.MakeTLKeyboardButtonGame(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeBuy:
		return mtproto.MakeTLKeyboardButtonBuy(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeSwitchInline:
		k.SamePeer, k.Query, k.PeerTypes = t.GetSamePeer(), t.GetQuery(), t.GetPeerTypes()
		return mtproto.MakeTLKeyboardButtonSwitchInline(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeUserProfile:
		k.UserId_INT64 = t.GetUserId_INT64()
		return mtproto.MakeTLKeyboardButtonUserProfile(k).To_KeyboardButton()
	case mtproto.Predicate_inputInlineButtonTypeUserProfile:
		k.UserId_INPUTUSER = t.GetUserId_INPUTUSER()
		return mtproto.MakeTLInputKeyboardButtonUserProfile(k).To_KeyboardButton()
	case mtproto.Predicate_inlineButtonTypeCopy:
		k.CopyText = t.GetCopyText()
		return mtproto.MakeTLKeyboardButtonCopy(k).To_KeyboardButton()
	}
	// inlineButtonTypeDisabled and unknown types: a plain button, it does
	// nothing inline.
	return mtproto.MakeTLKeyboardButton(k).To_KeyboardButton()
}

// canonicalButton: a typed button of replyKeyboardMarkup to its stored form.
func canonicalButton(b *mtproto.KeyboardButton) *mtproto.KeyboardButton {
	if b.GetPredicateName() != mtproto.Predicate_keyboardButton || b.GetType() == nil {
		return b
	}
	t := b.GetType()
	k := &mtproto.KeyboardButton{Style: b.GetStyle(), Text: b.GetText()}
	switch t.GetPredicateName() {
	case mtproto.Predicate_buttonTypeRequestPhone:
		return mtproto.MakeTLKeyboardButtonRequestPhone(k).To_KeyboardButton()
	case mtproto.Predicate_buttonTypeRequestGeoLocation:
		return mtproto.MakeTLKeyboardButtonRequestGeoLocation(k).To_KeyboardButton()
	case mtproto.Predicate_buttonTypeRequestPoll:
		k.Quiz = t.GetQuiz()
		return mtproto.MakeTLKeyboardButtonRequestPoll(k).To_KeyboardButton()
	case mtproto.Predicate_buttonTypeRequestPeer:
		k.ButtonId, k.PeerType, k.MaxQuantity = t.GetButtonId(), t.GetPeerType(), t.GetMaxQuantity()
		return mtproto.MakeTLKeyboardButtonRequestPeer(k).To_KeyboardButton()
	case mtproto.Predicate_inputButtonTypeRequestPeer:
		k.NameRequested, k.UsernameRequested, k.PhotoRequested = t.GetNameRequested(), t.GetUsernameRequested(), t.GetPhotoRequested()
		k.ButtonId, k.PeerType, k.MaxQuantity = t.GetButtonId(), t.GetPeerType(), t.GetMaxQuantity()
		return mtproto.MakeTLInputKeyboardButtonRequestPeer(k).To_KeyboardButton()
	case mtproto.Predicate_buttonTypeSimpleWebView:
		k.Url = t.GetUrl()
		return mtproto.MakeTLKeyboardButtonSimpleWebView(k).To_KeyboardButton()
	}
	return mtproto.MakeTLKeyboardButton(k).To_KeyboardButton()
}
