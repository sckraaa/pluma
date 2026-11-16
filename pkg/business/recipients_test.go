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

package business

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestRecipientsMatch(t *testing.T) {
	const p = 42
	newContact := Peer{UserID: p, New: true, Contact: true}
	oldStranger := Peer{UserID: p}
	for _, c := range []struct {
		name string
		r    Recipients
		peer Peer
		want bool
	}{
		{"new chats, a new chat", Recipients{NewChats: true}, newContact, true},
		{"new chats, an existing chat", Recipients{NewChats: true}, oldStranger, false},
		{"existing chats, an existing chat", Recipients{ExistingChats: true}, oldStranger, true},
		{"contacts, a contact", Recipients{Contacts: true}, newContact, true},
		{"contacts, a stranger", Recipients{Contacts: true}, oldStranger, false},
		{"non-contacts, a stranger", Recipients{NonContacts: true}, oldStranger, true},
		{"the user listed", Recipients{Users: []int64{7, p}}, oldStranger, true},
		{"another user listed", Recipients{Users: []int64{7}}, oldStranger, false},
		{"nothing chosen", Recipients{}, newContact, false},

		{"all but nothing", Recipients{ExcludeSelected: true}, oldStranger, true},
		{"all but new chats, a new chat", Recipients{ExcludeSelected: true, NewChats: true}, newContact, false},
		{"all but new chats, an existing chat", Recipients{ExcludeSelected: true, NewChats: true}, oldStranger, true},
		{"all but contacts, a stranger", Recipients{ExcludeSelected: true, Contacts: true}, oldStranger, true},
		{"all but the user", Recipients{ExcludeSelected: true, Users: []int64{p}}, newContact, false},
		{"all but another user", Recipients{ExcludeSelected: true, Users: []int64{7}}, newContact, true},

		// The bot's recipients: chats by category but some users.
		{"contacts but the user", Recipients{Contacts: true, ExcludeUsers: []int64{p}}, newContact, false},
		{"contacts but another user", Recipients{Contacts: true, ExcludeUsers: []int64{7}}, newContact, true},
		{"the user listed and left out", Recipients{Users: []int64{p}, ExcludeUsers: []int64{p}}, newContact, false},
	} {
		if got := c.r.Match(c.peer); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// exclude_selected "inverts the selection": for every choice of categories
// and users and every peer, the chat is in the inverted choice exactly when
// it is not in the plain one.
func TestRecipientsInverted(t *testing.T) {
	const p = 42
	for bits := range 1 << 5 {
		r := Recipients{
			ExistingChats: bits&1 != 0,
			NewChats:      bits&2 != 0,
			Contacts:      bits&4 != 0,
			NonContacts:   bits&8 != 0,
		}
		if bits&16 != 0 {
			r.Users = []int64{p}
		}
		inverted := r
		inverted.ExcludeSelected = true
		for _, peer := range []Peer{{UserID: p}, {UserID: p, New: true}, {UserID: p, Contact: true}, {UserID: p, New: true, Contact: true}} {
			if inverted.Match(peer) == r.Match(peer) {
				t.Errorf("%+v and %+v agree on %+v", r, inverted, peer)
			}
		}
	}
}

func TestRecipientsEmpty(t *testing.T) {
	for _, c := range []struct {
		r    Recipients
		want bool
	}{
		{Recipients{}, true},
		{Recipients{ExcludeUsers: []int64{1}}, true},
		{Recipients{ExcludeSelected: true}, false},
		{Recipients{Users: []int64{1}}, false},
		{Recipients{NonContacts: true}, false},
	} {
		if got := c.r.Empty(); got != c.want {
			t.Errorf("%+v: empty %v, want %v", c.r, got, c.want)
		}
	}
}

func TestRecipientsKept(t *testing.T) {
	in := mtproto.MakeTLInputBusinessBotRecipients(&mtproto.InputBusinessBotRecipients{Contacts: true, NewChats: true}).To_InputBusinessBotRecipients()
	r := RecipientsFromBotInput(in, []int64{9, 3, 9}, []int64{5, 5})
	if !r.Contacts || !r.NewChats || r.ExistingChats || !slices.Equal(r.Users, []int64{3, 9}) || !slices.Equal(r.ExcludeUsers, []int64{5}) {
		t.Fatalf("from the input: %+v", r)
	}
	data, err := MarshalRecipients(r)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalRecipients(data)
	if err != nil || !slices.Equal(back.Users, r.Users) || !slices.Equal(back.ExcludeUsers, r.ExcludeUsers) || back.Contacts != r.Contacts || back.NewChats != r.NewChats {
		t.Errorf("kept %s, read %+v %v", data, back, err)
	}
	bot := back.ToBotTL()
	if bot.GetPredicateName() != mtproto.Predicate_businessBotRecipients || !bot.GetContacts() || !slices.Equal(bot.GetExcludeUsers(), []int64{5}) {
		t.Errorf("businessBotRecipients %v", bot)
	}
	if !slices.Equal(back.UserIDs(), []int64{3, 5, 9}) {
		t.Errorf("the users %v", back.UserIDs())
	}

	plain := RecipientsFromInput(mtproto.MakeTLInputBusinessRecipients(&mtproto.InputBusinessRecipients{ExcludeSelected: true}).To_InputBusinessRecipients(), []int64{4})
	tl := plain.ToTL()
	if tl.GetPredicateName() != mtproto.Predicate_businessRecipients || !tl.GetExcludeSelected() || !slices.Equal(tl.GetUsers(), []int64{4}) {
		t.Errorf("businessRecipients %v", tl)
	}
}
