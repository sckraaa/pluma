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

package plugin

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// cards gives every link the card with its address as the title.
type cards struct{ asked []string }

func (c *cards) UsernameResolveUsername(context.Context, *user.TLUserResolveUsername) (*mtproto.Peer, error) {
	return nil, nil
}

func (c *cards) GetWebpagePreview(_ context.Context, url string) (*mtproto.WebPage, error) {
	c.asked = append(c.asked, url)
	return mtproto.MakeTLWebPage(&mtproto.WebPage{Id: 1, Url_STRING: url, Title: mtproto.MakeFlagsString(url)}).To_WebPage(), nil
}

// The card of a message (task 023): of its first link, hidden ones
// included; a message with media of its own or with the card the client
// chose keeps it; a card made of an earlier text is made anew.
func TestRemakeMessageCard(t *testing.T) {
	photo := mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{}).To_MessageMedia()
	card := func(url string, manual bool) *mtproto.MessageMedia {
		return mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
			Manual:  manual,
			Webpage: mtproto.MakeTLWebPage(&mtproto.WebPage{Id: 2, Url_STRING: url}).To_WebPage(),
		}).To_MessageMedia()
	}
	hidden := mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: 0, Length: 4, Url: "https://hidden.example/"}).To_MessageEntity()
	for _, tc := range []struct {
		name      string
		text      string
		entities  []*mtproto.MessageEntity
		media     *mtproto.MessageMedia
		noWebpage bool
		want      string // the card's address; "" for none
		keep      *mtproto.MessageMedia
	}{
		{name: "plain text", text: "no links"},
		{name: "a link", text: "see https://a.example/x and b.example", want: "https://a.example/x"},
		{name: "a hidden link first", text: "here https://a.example/", entities: []*mtproto.MessageEntity{hidden}, want: "https://hidden.example/"},
		{name: "no_webpage", text: "see https://a.example/", noWebpage: true},
		{name: "empty media", text: "see https://a.example/", media: mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia(), want: "https://a.example/"},
		{name: "a photo", text: "see https://a.example/", media: photo, keep: photo},
		{name: "a chosen card", text: "see https://a.example/", media: card("https://chosen.example/", true), want: "https://chosen.example/"},
		{name: "an edited link", text: "now https://new.example/", media: card("https://old.example/", false), want: "https://new.example/"},
		{name: "a dropped link", text: "no link now", media: card("https://old.example/", false)},
		{name: "a dropped link, no_webpage", text: "https://new.example/", media: card("https://old.example/", false), noWebpage: true},
	} {
		m := &mtproto.Message{Message: tc.text, Entities: tc.entities, Media: tc.media}
		m = RemakeMessage(context.Background(), &cards{}, m, 1, tc.noWebpage, func() bool { return false })
		switch {
		case tc.keep != nil:
			if m.Media != tc.keep {
				t.Errorf("%s: media %v, want kept", tc.name, m.Media)
			}
		case tc.want == "":
			if p := m.GetMedia().GetPredicateName(); p != "" && p != mtproto.Predicate_messageMediaEmpty {
				t.Errorf("%s: media %v, want none", tc.name, m.Media)
			}
		default:
			if got := m.GetMedia().GetWebpage().GetUrl_STRING(); got != tc.want {
				t.Errorf("%s: card of %q, want %q", tc.name, got, tc.want)
			}
		}
	}
}
