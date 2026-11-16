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
	"encoding/binary"
	"hash/fnv"
	"net/url"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The colors of theme_params as the clients send them come to the page as
// "#rrggbb": tdesktop's strings, Android's ARGB and iOS's RGB integers.
func TestThemeParams(t *testing.T) {
	got, err := themeParamsOf(&mtproto.DataJSON{Data: `{"bg_color":"#FFFFFF","text_color":-16777216,"hint_color":7368816,` +
		`"link_color":"#abc","button_color":"rgb(1, 2, 255)","weird":"blue","Bad Key":"#000000","header_bg_color":{"x":1}}`})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"bg_color":"#ffffff","button_color":"#0102ff","hint_color":"#707070","link_color":"#aabbcc","text_color":"#000000"}`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	for _, bad := range []string{`[1,2]`, `"#ffffff"`, `{`, `{"a":"` + strings.Repeat("x", maxThemeParams) + `"}`} {
		if _, err := themeParamsOf(&mtproto.DataJSON{Data: bad}); err != mtproto.ErrThemeParamsInvalid {
			t.Errorf("%.40s: %v, want THEME_PARAMS_INVALID", bad, err)
		}
	}
	if got, err := themeParamsOf(nil); got != "" || err != nil {
		t.Errorf("no theme: %q, %v", got, err)
	}
}

func TestWebViewParams(t *testing.T) {
	p, err := webViewParamsOf("", nil, wrapperspb.String("ref_42-x"), true)
	if err != nil {
		t.Fatal(err)
	}
	if p.platform != "unknown" || p.startParam != "ref_42-x" || !p.fullscreen {
		t.Errorf("params %+v", p)
	}
	for _, bad := range []string{"and roid", "ios;", strings.Repeat("a", maxWebViewPlatform+1)} {
		if _, err := webViewParamsOf(bad, nil, nil, false); err != mtproto.ErrPlatformInvalid {
			t.Errorf("platform %q: %v", bad, err)
		}
	}
	for _, bad := range []string{"a b", "a=b", strings.Repeat("a", maxWebAppStartParam+1)} {
		if _, err := webViewParamsOf("tdesktop", nil, wrapperspb.String(bad), false); err != mtproto.ErrStartParamInvalid {
			t.Errorf("start_param %q: %v", bad, err)
		}
	}
	if p, err := webViewParamsOf("android", nil, wrapperspb.String(""), false); err != nil || p.startParam != "" {
		t.Errorf("empty start_param: %+v, %v", p, err)
	}
}

func TestCheckWebAppURL(t *testing.T) {
	for _, ok := range []string{"https://app.example/", "https://app.example/x?y=1#/route"} {
		if err := checkWebAppURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://app.example/", "https://", "https://u:p@app.example/", "javascript:alert(1)",
		"https://app.example/" + strings.Repeat("a", maxWebAppURL)} {
		if err := checkWebAppURL(bad); err != mtproto.ErrUrlInvalid {
			t.Errorf("%.40s: %v", bad, err)
		}
	}
}

// The fragment is read by telegram-web-app.js (urlParseHashParams): the
// launch parameters after the app's own path fragment, the data encoded
// once more inside, and the version literally as tweb rewrites it.
func TestWebAppURL(t *testing.T) {
	f := webAppFragment{data: "query_id=1&user=%7B%7D&hash=ab", version: "8.0", platform: "android",
		theme: `{"bg_color":"#ffffff"}`, startParam: "ref", fullscreen: true, botInline: true, showSettings: true}
	link := webAppURL("https://app.example/start?x=1", f)
	base, frag, _ := strings.Cut(link, "#")
	if base != "https://app.example/start?x=1" {
		t.Errorf("base %q", base)
	}
	if !strings.HasPrefix(frag, "tgWebAppData=") || !strings.Contains(frag, "&tgWebAppVersion=8.0&tgWebAppPlatform=android&") {
		t.Errorf("fragment %q", frag)
	}
	q, err := url.ParseQuery(frag)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("tgWebAppData") != f.data || q.Get("tgWebAppThemeParams") != f.theme || q.Get("tgWebAppStartParam") != "ref" ||
		q.Get("tgWebAppFullscreen") != "1" || q.Get("tgWebAppBotInline") != "1" || q.Get("tgWebAppShowSettings") != "1" {
		t.Errorf("parameters %v", q)
	}

	plain := webAppURL("https://app.example/", webAppFragment{data: "a=1", version: "8.0", platform: "tdesktop"})
	if strings.Contains(plain, "tgWebAppThemeParams") || strings.Contains(plain, "tgWebAppFullscreen") || strings.Contains(plain, "tgWebAppStartParam") {
		t.Errorf("optional parameters given: %s", plain)
	}
	if got := webAppURL("https://app.example/#/shop", webAppFragment{data: "a", version: "8.0", platform: "ios"}); !strings.HasPrefix(got, "https://app.example/#/shop?tgWebAppData=a&") {
		t.Errorf("a path fragment: %s", got)
	}
	if got := webAppURL("https://app.example/#/shop?tab=2", webAppFragment{data: "a", version: "8.0", platform: "ios"}); !strings.HasPrefix(got, "https://app.example/#/shop?tab=2&tgWebAppData=a&") {
		t.Errorf("a fragment with parameters: %s", got)
	}
	if got := webAppURL("https://app.example/#", webAppFragment{data: "a", version: "8.0", platform: "ios"}); !strings.HasPrefix(got, "https://app.example/#tgWebAppData=a&") {
		t.Errorf("an empty fragment: %s", got)
	}
}

func TestWebViewChatKinds(t *testing.T) {
	const bot = 500
	user := func(id int64, isBot bool) *webViewChat {
		data := &mtproto.UserData{Id: id}
		if isBot {
			data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
		}
		u := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
		return &webViewChat{peer: &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: id}, user: u}
	}
	for _, c := range []struct {
		ch   *webViewChat
		kind string
		bit  int32
	}{
		{user(bot, true), "sender", 1},
		{user(501, true), "private", 2},
		{user(7, false), "private", 4},
		{&webViewChat{peer: &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: 9}, chat: &mtproto.MutableChat{}}, "group", 8},
	} {
		if got := c.ch.chatType(bot); got != c.kind {
			t.Errorf("%v: chat_type %q, want %q", c.ch.peer, got, c.kind)
		}
		if got := c.ch.attachPeerBit(bot); got != c.bit {
			t.Errorf("%v: peer type %d, want %d", c.ch.peer, got, c.bit)
		}
	}
	if id := botAPIChatId(&mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: 1234567890}); id != -1001234567890 {
		t.Errorf("channel id %d", id)
	}
	if id := botAPIChatId(&mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: 42}); id != -42 {
		t.Errorf("group id %d", id)
	}
}

// chat_instance uniquely names the chat a mini app or a press comes from
// (core.telegram.org/bots/webapps#webappinitdata): two private chats of one
// user differ, a chat of two users is the same from either side, and the
// chat with the bot keeps the value its presses always had.
func TestChatInstancePrivateChats(t *testing.T) {
	const bot, ann, bob, eve = 500, 7, 8, 9
	in := func(userId, peerId int64) int64 {
		return chatInstance(bot, &pressed{peer: &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: peerId}}, userId)
	}
	if in(ann, bob) == in(ann, eve) {
		t.Error("two private chats of one user share chat_instance")
	}
	if in(ann, bob) != in(bob, ann) {
		t.Error("a chat of two users differs by who opens it")
	}
	if in(ann, bot) == in(bob, bot) || in(ann, bot) == in(ann, bob) || in(ann, bot) == in(ann, ann) {
		t.Error("the chat with the bot is not a chat of its own")
	}
	h := fnv.New64a()
	var b [17]byte
	binary.BigEndian.PutUint64(b[:8], bot)
	b[8] = byte(mtproto.PEER_USER)
	binary.BigEndian.PutUint64(b[9:], ann)
	_, _ = h.Write(b[:])
	if got, want := in(ann, bot), int64(h.Sum64()>>1); got != want {
		t.Errorf("chat with the bot: %d, want %d as before", got, want)
	}
}
