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
	"encoding/base64"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Every right goes to its own field of businessBotRights and back; the
// flag left out is no right, and the bits are the flags of the schema,
// as iOS sets them (QuickReplyMessages.swift:1222-1264).
func TestRights(t *testing.T) {
	if r := RightsFromTL(nil); r != 0 {
		t.Errorf("no rights: %b", r)
	}
	for _, b := range rightBits {
		tl := b.right.ToTL()
		if got := RightsFromTL(tl); got != b.right {
			t.Errorf("right %b: back %b", b.right, got)
		}
		if !b.get(tl) {
			t.Errorf("right %b: its field is not set", b.right)
		}
	}
	if len(rightBits) != 14 || RightsFromTL(rightsAll.ToTL()) != rightsAll {
		t.Errorf("%d rights, all %b", len(rightBits), RightsFromTL(rightsAll.ToTL()))
	}
	r := RightsFromTL(mtproto.MakeTLBusinessBotRights(&mtproto.BusinessBotRights{Reply: true, EditBio: true, ManageStories: true}).To_BusinessBotRights())
	if r != 1<<0|1<<5|1<<13 || !r.Has(RightReply) || r.Has(RightReadMessages) || !r.Has(RightReply|RightEditBio) || r.Has(RightReply|RightEditName) {
		t.Errorf("reply, bio, stories: %b", r)
	}
}

// A connection id is 24 random bytes in base64url, a new one each time.
func TestNewConnectionID(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := NewConnectionID()
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.RawURLEncoding.DecodeString(id)
		if err != nil || len(b) != connectionIDBytes || len(id) != 32 {
			t.Fatalf("%q: %d bytes, %v", id, len(b), err)
		}
		if seen[id] {
			t.Fatalf("%q twice", id)
		}
		seen[id] = true
	}
}

// disablePeerConnectedBot takes the chat away in both modes of the
// recipients, whatever took it in; the chat is never served after it, and
// the other chats are served as before.
func TestWithoutPeer(t *testing.T) {
	const p = 7
	for _, c := range []struct {
		name string
		r    Recipients
		peer Peer
		want Recipients
	}{
		{"listed", Recipients{Users: []int64{5, p}}, Peer{UserID: p}, Recipients{Users: []int64{5}, ExcludeUsers: []int64{p}}},
		{"listed and a contact", Recipients{Contacts: true, Users: []int64{p}}, Peer{UserID: p, Contact: true},
			Recipients{Contacts: true, ExcludeUsers: []int64{p}}},
		{"by category", Recipients{NewChats: true, ExcludeUsers: []int64{3}}, Peer{UserID: p, New: true},
			Recipients{NewChats: true, ExcludeUsers: []int64{3, p}}},
		{"all but some", Recipients{ExcludeSelected: true, Contacts: true, Users: []int64{9}}, Peer{UserID: p},
			Recipients{ExcludeSelected: true, Contacts: true, Users: []int64{p, 9}}},
		{"all but some, excluded users dropped", Recipients{ExcludeSelected: true, ExcludeUsers: []int64{p}}, Peer{UserID: p},
			Recipients{ExcludeSelected: true, Users: []int64{p}}},
	} {
		got := c.r.WithoutPeer(c.peer)
		if !(&Connection{Recipients: got}).Same(&Connection{Recipients: c.want}) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		if got.Match(c.peer) {
			t.Errorf("%s: the chat is still served", c.name)
		}
		other := Peer{UserID: 5, New: c.peer.New, Contact: c.peer.Contact}
		if c.r.Match(other) != got.Match(other) {
			t.Errorf("%s: another chat changed", c.name)
		}
	}
	// A chat taken away stays away when a category takes it in later.
	later := Recipients{Contacts: true, Users: []int64{p}}.WithoutPeer(Peer{UserID: p})
	if later.Match(Peer{UserID: p, Contact: true}) {
		t.Error("the chat is served again once it is a contact")
	}
	r := Recipients{Users: []int64{1, 2}}
	_ = r.WithoutPeer(Peer{UserID: 1})
	if !slices.Equal(r.Users, []int64{1, 2}) {
		t.Errorf("the recipients changed in place: %v", r.Users)
	}
}

func TestConnectionSame(t *testing.T) {
	a := &Connection{UserID: 1, BotID: 2, ConnectionID: "x", Rights: RightReply, Recipients: Recipients{Contacts: true, Users: []int64{3}}}
	b := *a
	b.Recipients.Users = []int64{3}
	if !a.Same(&b) || (*Connection)(nil).Same(a) || a.Same(nil) || !(*Connection)(nil).Same(nil) {
		t.Error("same connections differ")
	}
	for _, change := range []func(c *Connection){
		func(c *Connection) { c.BotID = 4 },
		func(c *Connection) { c.ConnectionID = "y" },
		func(c *Connection) { c.Rights = 0 },
		func(c *Connection) { c.Recipients.NonContacts = true },
		func(c *Connection) { c.Recipients.ExcludeUsers = []int64{3} },
	} {
		c := *a
		change(&c)
		if a.Same(&c) {
			t.Errorf("a changed connection is the same: %+v", c)
		}
	}
}

// The bar sets the four fields together (business_bot_id and the manage
// link are one flag), a nil bar clears them, and the settings given stay as
// they were.
func TestPeerBarApply(t *testing.T) {
	base := mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{ReportSpam: true, BusinessBotId: wrapperspb.Int64(9)}).To_PeerSettings()
	got := (&PeerBar{BotID: 42, ManageURL: "https://pluma.chat/shopbot", CanReply: true}).Apply(base)
	if !got.ReportSpam || got.GetBusinessBotId().GetValue() != 42 || got.GetBusinessBotManageUrl().GetValue() != "https://pluma.chat/shopbot" ||
		!got.BusinessBotCanReply || got.BusinessBotPaused || got.GetPredicateName() != mtproto.Predicate_peerSettings {
		t.Errorf("bar: %v", got)
	}
	if base.GetBusinessBotId().GetValue() != 9 || base.BusinessBotCanReply {
		t.Errorf("the settings given changed: %v", base)
	}
	cleared := (*PeerBar)(nil).Apply(got)
	if !cleared.ReportSpam || cleared.BusinessBotId != nil || cleared.BusinessBotManageUrl != nil || cleared.BusinessBotCanReply {
		t.Errorf("no bar: %v", cleared)
	}
	if empty := (&PeerBar{BotID: 1, Paused: true}).Apply(nil); empty.GetBusinessBotId().GetValue() != 1 || empty.BusinessBotManageUrl == nil || !empty.BusinessBotPaused {
		t.Errorf("bar on no settings: %v", empty)
	}
	if u := ManageURL("pluma.chat", "shopbot"); u != "https://pluma.chat/shopbot" {
		t.Errorf("manage url %q", u)
	}
}

// A bot serves the chats with people only.
func TestServedUser(t *testing.T) {
	user := func(id int64, f func(u *mtproto.UserData)) *mtproto.ImmutableUser {
		u := &mtproto.UserData{Id: id}
		if f != nil {
			f(u)
		}
		return &mtproto.ImmutableUser{User: u}
	}
	for _, c := range []struct {
		name string
		u    *mtproto.ImmutableUser
		want bool
	}{
		{"a person", user(5, nil), true},
		{"the account itself", user(1, nil), false},
		{"a bot", user(5, func(u *mtproto.UserData) { u.Bot = &mtproto.BotData{} }), false},
		{"deleted", user(5, func(u *mtproto.UserData) { u.Deleted = true }), false},
		{"support", user(5, func(u *mtproto.UserData) { u.Support = true }), false},
		{"the support ghost", user(5, func(u *mtproto.UserData) { u.Ghost = true }), false},
		{"service notifications", user(777000, nil), false},
		{"nobody", nil, false},
	} {
		if got := ServedUser(1, c.u); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
