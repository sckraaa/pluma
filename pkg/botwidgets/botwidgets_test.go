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

package botwidgets

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func inflate(t *testing.T, b []byte) string {
	t.Helper()
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not zlib: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("inflate: %v", err)
	}
	return string(out)
}

func TestPack(t *testing.T) {
	html := "<div>" + strings.Repeat("widget ", 1000) + "</div>"
	b, err := Pack(html)
	if err != nil {
		t.Fatal(err)
	}
	// RFC 1950, as the clients read it, not gzip (tgw-format.md).
	if b[0] != 0x78 || inflate(t, b) != html {
		t.Fatalf("payload % x... does not inflate to the HTML", b[:2])
	}

	random := make([]byte, 5000)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		html string
		want error
	}{
		"empty":                 {"", mtproto.ErrInputMediaWidgetHtmlInvalid},
		"not UTF-8":             {"<b>\xff</b>", mtproto.ErrInputMediaWidgetHtmlInvalid},
		"NUL":                   {"<b>\x00</b>", mtproto.ErrInputMediaWidgetHtmlInvalid},
		"over 32 KB":            {strings.Repeat("a", MaxHTML+1), mtproto.ErrInputMediaWidgetTooLarge},
		"over 4 KB compressed":  {hex.EncodeToString(random), mtproto.ErrInputMediaWidgetTooLarge},
		"32 KB that compresses": {strings.Repeat("a", MaxHTML), nil},
	} {
		if _, err := Pack(c.html); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

func TestCheckData(t *testing.T) {
	big, bad := errors.New("big"), errors.New("bad")
	for _, c := range []struct {
		in   *wrapperspb.StringValue
		want error
	}{
		{nil, nil},
		{wrapperspb.String(""), nil},
		{wrapperspb.String(strings.Repeat("я", MaxData/2)), nil},
		{wrapperspb.String(strings.Repeat("a", MaxData+1)), big},
		{wrapperspb.String("a\x00"), bad},
		{wrapperspb.String("\xc3"), bad},
	} {
		if err := CheckData(c.in, big, bad); err != c.want {
			t.Errorf("%q: %v, want %v", c.in.GetValue(), err, c.want)
		}
	}
}

func TestFlags(t *testing.T) {
	for _, c := range []struct {
		allow       bool
		where       Where
		can, ground bool
	}{
		{false, PrivateWithBot, false, false},
		{true, PrivateWithBot, true, true},
		{true, Group, true, false},
		{true, ViaBot, true, false},
		{false, ViaBot, false, false},
	} {
		if can, ground := Flags(c.allow, c.where); can != c.can || ground != c.ground {
			t.Errorf("Flags(%v, %d) = %v, %v; want %v, %v", c.allow, c.where, can, ground, c.can, c.ground)
		}
	}
}

// memStore keeps the shared widgets in memory.
type memStore struct {
	widgets map[int64]botstore.Widget
	next    int64
}

func (s *memStore) CreateWidget(_ context.Context, w botstore.Widget, now int32) (botstore.Widget, error) {
	s.next++
	w.Id, w.UpdatedAt, w.Period, w.PublicHash = s.next, now, botstore.DefaultWidgetPeriod, botstore.WidgetDataHash(w.PublicData)
	if s.widgets == nil {
		s.widgets = map[int64]botstore.Widget{}
	}
	s.widgets[w.Id] = w
	return w, nil
}

func (s *memStore) BotWidget(_ context.Context, botId, id int64) (*botstore.Widget, error) {
	w, ok := s.widgets[id]
	if !ok || w.BotId != botId {
		return nil, botstore.ErrNoWidget
	}
	return &w, nil
}

func TestMedia(t *testing.T) {
	const bot = 100
	ctx, s := context.Background(), &memStore{}

	static, err := Media(ctx, s, bot, Input{Html: "<p>hi</p>", Width: 300, PublicData: wrapperspb.String("p"), ChatData: wrapperspb.String("c"), AllowQueries: true}, Group, 10)
	if err != nil {
		t.Fatal(err)
	}
	if static.GetPredicateName() != mtproto.Predicate_messageMediaWidget || inflate(t, static.GetPayload()) != "<p>hi</p>" ||
		static.GetWidth() != 300 || static.GetHeight() != 0 || static.GetWidgetId() != nil ||
		static.GetPublicData().GetValue() != "p" || static.GetPublicDataHash().GetValue() != botstore.WidgetDataHash("p") ||
		static.GetChatData().GetValue() != "c" || !static.GetCanQuery() || static.GetBackgroundQuery() {
		t.Fatalf("static widget: %v", static)
	}
	if len(s.widgets) != 0 {
		t.Fatal("a static widget made an object")
	}

	shared, err := Media(ctx, s, bot, Input{Html: "<p>x</p>", Shared: true, PublicData: wrapperspb.String("p1")}, PrivateWithBot, 10)
	if err != nil {
		t.Fatal(err)
	}
	id := shared.GetWidgetId().GetValue()
	if o, ok := s.widgets[id]; !ok || o.PublicData != "p1" || o.AllowQueries || shared.GetCanQuery() {
		t.Fatalf("shared widget: %v, object %+v", shared, o)
	}

	byId, err := Media(ctx, s, bot, Input{WidgetId: id, ChatData: wrapperspb.String("mine")}, ViaBot, 11)
	if err != nil || byId.GetWidgetId().GetValue() != id || !bytes.Equal(byId.GetPayload(), shared.GetPayload()) ||
		byId.GetPublicData().GetValue() != "p1" || byId.GetChatData().GetValue() != "mine" {
		t.Fatalf("by id: %v, %v", byId, err)
	}
	if len(s.widgets) != 1 {
		t.Fatal("a send by id made an object")
	}
	if _, err = Media(ctx, s, bot+1, Input{WidgetId: id}, ViaBot, 11); !errors.Is(err, mtproto.ErrWidgetIdInvalid) {
		t.Errorf("another bot's widget: %v", err)
	}
	if _, err = Media(ctx, s, bot, Input{Html: "x", Width: -1}, Group, 11); !errors.Is(err, mtproto.ErrMediaInvalid) {
		t.Errorf("negative width: %v", err)
	}
	if _, err = Media(ctx, s, bot, Input{Html: "x", ChatData: wrapperspb.String(strings.Repeat("a", MaxData+1))}, Group, 11); !errors.Is(err, mtproto.ErrInputMediaWidgetDataTooLarge) {
		t.Errorf("long chat data: %v", err)
	}

	// An inline result: previewed when the bot answers, an object at the
	// send.
	p, err := Preview(ctx, s, bot, Input{Html: "<i>r</i>", Shared: true, PublicData: wrapperspb.String("q")})
	if err != nil || p.GetWidgetId() != nil || inflate(t, p.GetPayload()) != "<i>r</i>" || p.GetPublicDataHash().GetValue() != botstore.WidgetDataHash("q") {
		t.Fatalf("preview: %v, %v", p, err)
	}
	sent, err := Media(ctx, s, bot, FromPreview(p, true, true, 0), ViaBot, 12)
	if err != nil || sent.GetWidgetId() == nil || !sent.GetCanQuery() || sent.GetBackgroundQuery() || s.widgets[sent.GetWidgetId().GetValue()].PublicData != "q" {
		t.Fatalf("sent preview: %v, %v", sent, err)
	}
}

func TestForForwardAndWithData(t *testing.T) {
	m := mtproto.MakeTLMessageMediaWidget(&mtproto.MessageMedia{
		Payload: []byte{1}, CanQuery: true, BackgroundQuery: true,
		PublicData: wrapperspb.String("p"), PublicDataHash: wrapperspb.Int64(botstore.WidgetDataHash("p")), ChatData: wrapperspb.String("c"),
	}).To_MessageMedia()
	f := ForForward(m)
	if !f.GetIsCopy() || f.GetCanQuery() || f.GetBackgroundQuery() || f.GetChatData() != nil || f.GetPublicData().GetValue() != "p" {
		t.Fatalf("forward: %v", f)
	}
	if !m.GetCanQuery() || m.GetChatData() == nil {
		t.Fatal("ForForward changed the original")
	}
	photo := mtproto.MakeTLMessageMediaPhoto(nil).To_MessageMedia()
	if ForForward(photo) != photo {
		t.Fatal("a photo changed")
	}

	e := WithData(m, nil, wrapperspb.String(""))
	if e.GetChatData().GetValue() != "" || e.GetChatData() == nil || e.GetPublicData().GetValue() != "p" {
		t.Fatalf("chat state cleared: %v", e)
	}
	e = WithData(m, wrapperspb.String("p2"), nil)
	if e.GetPublicData().GetValue() != "p2" || e.GetPublicDataHash().GetValue() != botstore.WidgetDataHash("p2") || e.GetChatData().GetValue() != "c" {
		t.Fatalf("public state: %v", e)
	}
}
