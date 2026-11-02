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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Link previews (task 023) of links the server must not fetch: a link into
// a network gets no card, as Telegram answers http://192.168.0.1
// (messageMediaEmpty), and a message with one is sent without a card. A
// chosen card that cannot be made is WEBPAGE_NOT_FOUND unless optional
// (core.telegram.org/constructor/inputMediaWebPage). The cards of public
// pages are covered by the unit tests of pkg/webpage: the stand has no
// internet to fetch them from.
func TestWebPagePreviews(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	preview := func(message string) *mtproto.MessageMedia {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetWebPagePreview570D6F6F{
			Constructor: mtproto.CRC32_messages_getWebPagePreview570D6F6F,
			Message:     message,
		})
		if err != nil {
			t.Fatalf("messages.getWebPagePreview(%q): %v", message, err)
		}
		r, ok := o.(interface {
			To_Messages_WebPagePreview() *mtproto.Messages_WebPagePreview
		})
		if !ok {
			t.Fatalf("messages.getWebPagePreview returned %T", o)
		}
		return r.To_Messages_WebPagePreview().GetMedia()
	}
	for _, m := range []string{"no link here", "the router http://192.168.0.1/admin", "redis://10.0.0.5:6379", "http://localhost:8080/"} {
		if media := preview(m); media.GetPredicateName() != mtproto.Predicate_messageMediaEmpty {
			t.Errorf("preview of %q: %s", m, media.GetPredicateName())
		}
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetWebPage8D9692A3{
		Constructor: mtproto.CRC32_messages_getWebPage8D9692A3,
		Url:         "http://169.254.169.254/latest/meta-data/",
	})
	if err != nil {
		t.Fatalf("messages.getWebPage: %v", err)
	}
	page := o.(interface {
		To_Messages_WebPage() *mtproto.Messages_WebPage
	}).To_Messages_WebPage().GetWebpage()
	if page.GetPredicateName() != mtproto.Predicate_webPageEmpty {
		t.Errorf("messages.getWebPage of the metadata endpoint: %v", page)
	}

	// A message with such a link has no card.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Message:     "the router http://192.168.0.1/admin",
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	for _, m := range sentMessages(t, o) {
		if p := m.GetMedia().GetPredicateName(); p != "" && p != mtproto.Predicate_messageMediaEmpty {
			t.Errorf("a message with an internal link has %s", p)
		}
	}

	sendCard := func(optional bool, message string) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
			Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
			Media: mtproto.MakeTLInputMediaWebPage(&mtproto.InputMedia{
				Optional: optional,
				Url:      "http://10.1.2.3/",
			}).To_InputMedia(),
			Message:  message,
			RandomId: randomID(),
		})
	}
	if _, err := sendCard(false, "see http://10.1.2.3/"); !mtclient.IsRPCError(err, "WEBPAGE_NOT_FOUND") {
		t.Errorf("a chosen card that cannot be made: %v, want WEBPAGE_NOT_FOUND", err)
	}
	if _, err := sendCard(true, ""); !mtclient.IsRPCError(err, "MESSAGE_EMPTY") {
		t.Errorf("an optional card without text: %v, want MESSAGE_EMPTY", err)
	}
	o, err = sendCard(true, "see http://10.1.2.3/")
	if err != nil {
		t.Fatalf("an optional card: %v", err)
	}
	sent := sentMessages(t, o)
	if len(sent) != 1 || sent[0].GetMessage() != "see http://10.1.2.3/" {
		t.Fatalf("an optional card sent %d messages", len(sent))
	}
	if p := sent[0].GetMedia().GetPredicateName(); p != "" && p != mtproto.Predicate_messageMediaEmpty {
		t.Errorf("an optional card that cannot be made left %s", p)
	}
}

// sentMessages are the messages in the answer to a send.
func sentMessages(t *testing.T, o mtproto.TLObject) []*mtproto.Message {
	t.Helper()
	up, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("the send returned %T", o)
	}
	return newMessages(up.To_Updates())
}
