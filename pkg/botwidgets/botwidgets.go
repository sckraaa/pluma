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

// Package botwidgets makes the widgets of bot messages (messageMediaWidget,
// task 106, ADR 0068 p. 8) out of what a bot sends: inputMediaWidget and
// inputMediaWidgetId to messages.sendMedia, inputBotInlineMessageWidget and
// inputBotInlineMessageWidgetId in inline results.
//
// Telegram publishes nothing on widgets; the official clients carry the
// framework that runs them with the server's rules
// (Android TMessagesProj/jni/twidget/widget-sandbox/docs/tgw-format.md):
// the bot sends the HTML, the server checks it, compresses it with zlib
// and puts the bytes in the media; the HTML never changes after the send,
// only the widget's state does, which the bot alone writes: chat_data, of
// the message, and public_data, of the message or, for a shared widget, of
// the object all its messages refer to by widget_id (pkg/botstore
// bot_widgets). Where a widget may send its bot a request the server says
// in the flags of the media (Flags).
package botwidgets

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The limits of Telegram's server (tgw-format.md: "at most 4 KB compressed
// and 32 KB uncompressed", the state "a text of up to 1024 bytes", a
// request's data "1–64 opaque bytes", the answer "at most 1024 bytes").
// The clients keep none of their own (tdesktop tgw/tgw_media.cpp: "No
// client cap: the server enforces payload limits").
const (
	MaxHTML      = 32 << 10
	MaxPayload   = 4 << 10
	MaxData      = 1024
	MaxQueryData = 64
	MaxResult    = 1024
)

// Store keeps the shared widgets (botstore.Store).
type Store interface {
	CreateWidget(ctx context.Context, w botstore.Widget, now int32) (botstore.Widget, error)
	BotWidget(ctx context.Context, botId, id int64) (*botstore.Widget, error)
}

// Input is a widget as a bot sends it: by its HTML (Html, or Payload when
// it was compressed already, as an inline result's is), or by the id of a
// shared widget of the bot (WidgetId).
type Input struct {
	Html    string
	Payload []byte
	Width   int32
	Height  int32
	// AllowQueries lets the widget send its bot requests.
	AllowQueries bool
	// Shared makes the widget an object of its own (shared_widget).
	Shared     bool
	PublicData *wrapperspb.StringValue
	ChatData   *wrapperspb.StringValue
	WidgetId   int64
}

// FromInputMedia is the widget of inputMediaWidget or inputMediaWidgetId;
// false for other media.
func FromInputMedia(in *mtproto.InputMedia) (Input, bool) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputMediaWidget:
		return Input{
			Html: in.GetHtml(), Width: in.GetWidth(), Height: in.GetHeight(),
			AllowQueries: in.GetAllowQueries(), Shared: in.GetSharedWidget(),
			PublicData: in.GetPublicData(), ChatData: in.GetChatData(),
		}, true
	case mtproto.Predicate_inputMediaWidgetId:
		return Input{WidgetId: in.GetWidgetId(), ChatData: in.GetChatData()}, true
	}
	return Input{}, false
}

// FromInlineMessage is the widget of inputBotInlineMessageWidget or
// inputBotInlineMessageWidgetId; false for other messages.
func FromInlineMessage(in *mtproto.InputBotInlineMessage) (Input, bool) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputBotInlineMessageWidget:
		return Input{
			Html: in.GetHtml(), Width: in.GetWidth(), Height: in.GetHeight(),
			AllowQueries: in.GetAllowQueries(), Shared: in.GetSharedWidget(),
			PublicData: in.GetPublicData(), ChatData: in.GetChatData(),
		}, true
	case mtproto.Predicate_inputBotInlineMessageWidgetId:
		return Input{WidgetId: in.GetWidgetId(), ChatData: in.GetChatData()}, true
	}
	return Input{}, false
}

// Pack checks the HTML of a widget and compresses it as the server does:
// zlib (RFC 1950) at level 9, which the clients inflate (tgw-format.md,
// "Inflating the payload"). Text that is not UTF-8 or has NUL bytes is
// INPUT_MEDIA_WIDGET_HTML_INVALID, HTML over MaxHTML or a payload over
// MaxPayload INPUT_MEDIA_WIDGET_TOO_LARGE.
func Pack(html string) ([]byte, error) {
	switch {
	case html == "" || !text(html):
		return nil, mtproto.ErrInputMediaWidgetHtmlInvalid
	case len(html) > MaxHTML:
		return nil, mtproto.ErrInputMediaWidgetTooLarge
	}
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("botwidgets: zlib: %w", err)
	}
	if _, err = w.Write([]byte(html)); err != nil {
		return nil, fmt.Errorf("botwidgets: zlib: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, fmt.Errorf("botwidgets: zlib: %w", err)
	}
	if buf.Len() > MaxPayload {
		return nil, mtproto.ErrInputMediaWidgetTooLarge
	}
	return buf.Bytes(), nil
}

// text tells whether s is UTF-8 without NUL bytes.
func text(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// CheckData checks a state of a widget, or the answer of its bot (MaxResult
// is MaxData): nil, or text of at most MaxData bytes; tooLarge or invalid
// otherwise.
func CheckData(s *wrapperspb.StringValue, tooLarge, invalid error) error {
	switch {
	case s == nil:
		return nil
	case len(s.GetValue()) > MaxData:
		return tooLarge
	case !text(s.GetValue()):
		return invalid
	}
	return nil
}

// Preview is what an inline result shows of a widget before it is sent
// (botInlineMessageWidget): the HTML compressed, the size, the state, and
// the shared widget's id for a result by reference. A widget with Shared
// becomes an object only when it is sent (Media), as Telegram makes "a new
// object on every send" (tgw-format.md, "Who may send").
func Preview(ctx context.Context, store Store, botId int64, in Input) (*mtproto.BotInlineMessage, error) {
	m, err := checked(ctx, store, botId, in)
	if err != nil {
		return nil, err
	}
	out := &mtproto.BotInlineMessage{
		Width: m.width, Height: m.height, Payload: m.payload,
		ChatData: in.ChatData,
	}
	if m.publicData != nil {
		out.PublicData, out.PublicDataHash = m.publicData, wrapperspb.Int64(botstore.WidgetDataHash(m.publicData.GetValue()))
	}
	if in.WidgetId != 0 {
		out.WidgetId = wrapperspb.Int64(in.WidgetId)
	}
	return out, nil
}

// FromPreview is the widget of an inline result that Preview made, to be
// sent: its payload, size and state, with what the result does not show,
// which the caller kept: whether it may ask its bot, whether it becomes an
// object, and the object it refers to.
func FromPreview(p *mtproto.BotInlineMessage, allowQueries, shared bool, widgetId int64) Input {
	return Input{
		Payload: p.GetPayload(), Width: p.GetWidth(), Height: p.GetHeight(),
		AllowQueries: allowQueries, Shared: shared,
		PublicData: p.GetPublicData(), ChatData: p.GetChatData(), WidgetId: widgetId,
	}
}

// widget is a widget checked: of an object, or of the HTML of the send.
type widget struct {
	payload       []byte
	width, height int32
	publicData    *wrapperspb.StringValue
	allowQueries  bool
	object        *botstore.Widget
}

// checked checks in: an object of the bot, WIDGET_ID_INVALID for another's
// or none; the HTML or the payload, the size and the state otherwise.
func checked(ctx context.Context, store Store, botId int64, in Input) (*widget, error) {
	if err := CheckData(in.ChatData, mtproto.ErrInputMediaWidgetDataTooLarge, mtproto.ErrInputMediaWidgetDataInvalid); err != nil {
		return nil, err
	}
	if in.WidgetId != 0 {
		w, err := store.BotWidget(ctx, botId, in.WidgetId)
		if errors.Is(err, botstore.ErrNoWidget) {
			return nil, mtproto.ErrWidgetIdInvalid
		}
		if err != nil {
			return nil, err
		}
		return &widget{
			payload: w.Payload, width: w.Width, height: w.Height,
			publicData: wrapperspb.String(w.PublicData), allowQueries: w.AllowQueries, object: w,
		}, nil
	}
	if in.Width < 0 || in.Width > botstore.MaxWidgetSide || in.Height < 0 || in.Height > botstore.MaxWidgetSide {
		return nil, mtproto.ErrMediaInvalid
	}
	if err := CheckData(in.PublicData, mtproto.ErrInputMediaWidgetDataTooLarge, mtproto.ErrInputMediaWidgetDataInvalid); err != nil {
		return nil, err
	}
	payload := in.Payload
	if payload == nil {
		var err error
		if payload, err = Pack(in.Html); err != nil {
			return nil, err
		}
	} else if len(payload) > MaxPayload {
		// A payload packed before (an inline result's) is the server's own.
		return nil, mtproto.ErrInputMediaWidgetTooLarge
	}
	return &widget{payload: payload, width: in.Width, height: in.Height, publicData: in.PublicData, allowQueries: in.AllowQueries}, nil
}

// Where is where a widget is sent, which decides what requests its
// message may send (Flags).
type Where int

const (
	// PrivateWithBot is the private chat of a user with the bot.
	PrivateWithBot Where = iota
	// Group is a basic group or a supergroup the bot sent it to.
	Group
	// ViaBot is a message a user sent via the bot, in any chat.
	ViaBot
)

// Flags are can_query and background_query of an original message: "the
// server computes the flags; the client passes them on". A widget sent
// without allow_queries never has them; one with it may ask on a tap
// everywhere, and also in the background in the private chat with its bot
// (tgw-format.md, "Who may request").
func Flags(allowQueries bool, where Where) (canQuery, backgroundQuery bool) {
	if !allowQueries {
		return false, false
	}
	return true, where == PrivateWithBot
}

// Media makes the media of a widget botId sends where: of an object of the
// bot by its id, or of the HTML, which becomes an object first with
// Shared. now is the time of the send.
func Media(ctx context.Context, store Store, botId int64, in Input, where Where, now int32) (*mtproto.MessageMedia, error) {
	m, err := checked(ctx, store, botId, in)
	if err != nil {
		return nil, err
	}
	if m.object == nil && in.Shared {
		pd := ""
		if m.publicData != nil {
			pd = m.publicData.GetValue()
		}
		o, err := store.CreateWidget(ctx, botstore.Widget{
			BotId: botId, Payload: m.payload, Width: m.width, Height: m.height,
			AllowQueries: m.allowQueries, PublicData: pd,
		}, now)
		if err != nil {
			return nil, err
		}
		m.object = &o
		m.publicData = wrapperspb.String(o.PublicData)
	}
	out := &mtproto.MessageMedia{
		Width: m.width, Height: m.height, Payload: m.payload,
		ChatData: in.ChatData,
	}
	out.CanQuery, out.BackgroundQuery = Flags(m.allowQueries, where)
	if m.object != nil {
		out.WidgetId = wrapperspb.Int64(m.object.Id)
		out.PublicData, out.PublicDataHash = wrapperspb.String(m.object.PublicData), wrapperspb.Int64(m.object.PublicHash)
	} else if m.publicData != nil {
		out.PublicData, out.PublicDataHash = m.publicData, wrapperspb.Int64(botstore.WidgetDataHash(m.publicData.GetValue()))
	}
	return mtproto.MakeTLMessageMediaWidget(out).To_MessageMedia(), nil
}

// Is tells whether m is a widget.
func Is(m *mtproto.MessageMedia) bool {
	return m.GetPredicateName() == mtproto.Predicate_messageMediaWidget
}

// ForForward is the media of a forward of m: a widget forwarded is a copy
// (is_copy) that runs the same HTML with its shared state and without the
// chat state, which is "never copied by a forward", and asks its bot
// nothing: only a widget sent with fwd_allow_queries, which this layer's
// schema does not have, lets its copies ask (tgw-format.md, "Who may
// send" and "Who may request"). Other media are m.
func ForForward(m *mtproto.MessageMedia) *mtproto.MessageMedia {
	if !Is(m) {
		return m
	}
	out := proto.Clone(m).(*mtproto.MessageMedia)
	out.IsCopy, out.CanQuery, out.BackgroundQuery, out.ChatData = true, false, false, nil
	return out
}

// WithData is the media of a widget m with the state an edit gives: a
// field the edit names replaces the message's, an empty string clears it
// ("in an edit a field not passed is unchanged and an empty string clears
// it", tgw-format.md). The hash of the public state is the one
// botstore.WidgetDataHash gives, the same as a shared widget's object has.
func WithData(m *mtproto.MessageMedia, publicData, chatData *wrapperspb.StringValue) *mtproto.MessageMedia {
	out := proto.Clone(m).(*mtproto.MessageMedia)
	if publicData != nil {
		out.PublicData, out.PublicDataHash = wrapperspb.String(publicData.GetValue()), wrapperspb.Int64(botstore.WidgetDataHash(publicData.GetValue()))
	}
	if chatData != nil {
		out.ChatData = wrapperspb.String(chatData.GetValue())
	}
	return out
}
