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

//go:build integration

package groupcalls

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

func TestConferenceMigrationTwice(t *testing.T) {
	path, err := testinfra.SchemaFile("migrate-20261126-conference-calls.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = itMySQL.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
}

func newConference(t *testing.T, s *Store, creator int64, join *JoinParams) (*Call, *JoinResult) {
	t.Helper()
	slug, err := NewSlug()
	if err != nil {
		t.Fatal(err)
	}
	c, created, joined, err := s.CreateConference(context.Background(), ConferenceParams{
		Id: itId(), AccessHash: itId(), CreatorId: creator, RandomId: int32(itSeq.Add(1)), Slug: slug, Join: join,
	})
	if err != nil || !created {
		t.Fatalf("create a conference: %v %v", created, err)
	}
	return c, joined
}

// A conference is a call without a chat: its own id as the peer, the slug
// finds it, a repeat of the request gets it again, and a creator has
// ConferencesPerCreator that are not over.
func TestConferenceCreateRepeatLimit(t *testing.T) {
	s, ctx := itStore(), context.Background()
	creator := itId()
	c, _ := newConference(t, s, creator, nil)
	if !c.Conference || c.PeerType != 0 || c.PeerId != c.Id || c.ChainHeight != -1 || c.Version != 1 || c.Scheduled() {
		t.Fatalf("the conference: %+v", c)
	}
	if len(c.Slug) != 22 {
		t.Fatalf("slug of %d characters", len(c.Slug))
	}
	again, created, _, err := s.CreateConference(ctx, ConferenceParams{
		Id: itId(), AccessHash: itId(), CreatorId: creator, RandomId: c.RandomId, Slug: "other",
	})
	if err != nil || created || again.Id != c.Id || again.Slug != c.Slug {
		t.Fatalf("a repeat: %v %v %+v", created, err, again)
	}
	found, err := s.BySlug(ctx, c.Slug)
	if err != nil || found.Id != c.Id {
		t.Fatalf("by slug: %v %v", found, err)
	}
	for i := 1; i < ConferencesPerCreator; i++ {
		newConference(t, s, creator, nil)
	}
	slug, _ := NewSlug()
	if _, _, _, err = s.CreateConference(ctx, ConferenceParams{Id: itId(), AccessHash: itId(), CreatorId: creator, RandomId: int32(itSeq.Add(1)), Slug: slug}); !errors.Is(err, ErrConferenceLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, _, _, err = s.Discard(ctx, c.Id); err != nil {
		t.Fatal(err)
	}
	newConference(t, s, creator, nil)
	if got, _ := s.Unposted(ctx, time.Now().Add(-time.Hour), 1000); func() bool {
		for _, u := range got {
			if u.Conference {
				return true
			}
		}
		return false
	}() {
		t.Fatal("a conference has service messages to post")
	}
}

// Created with the join: the call, the creator's zero block and his row
// commit together; a bad block leaves no conference behind.
func TestConferenceCreateWithJoin(t *testing.T) {
	s, ctx := itStore(), context.Background()
	creator := itId()
	key := userKey(creator)
	state := func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, true, false, false) }
	slug, _ := NewSlug()
	random := int32(itSeq.Add(1))
	if _, _, _, err := s.CreateConference(ctx, ConferenceParams{
		Id: itId(), AccessHash: itId(), CreatorId: creator, RandomId: random, Slug: slug,
		Join: &JoinParams{UserId: creator, Source: 7, State: state, Chain: &ChainJoin{PublicKey: tde2etest.Pub(key), Block: []byte("not a block")}},
	}); err == nil {
		t.Fatal("a bad zero block was taken")
	}
	if _, err := s.BySlug(ctx, slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the refused conference is there: %v", err)
	}
	c, joined := newConference(t, s, creator, &JoinParams{
		UserId: creator, Source: 7, State: state,
		Chain: &ChainJoin{PublicKey: tde2etest.Pub(key), Block: tde2etest.JoinBlock(tde2e.NewChain(), creator, key)},
	})
	if joined == nil || c.ChainHeight != 0 || c.ParticipantsCount != 1 || c.Version != 2 || joined.Chain.Next != 1 {
		t.Fatalf("created with the join: %+v %+v", c, joined)
	}
}

// The creator's kick keeps one out; his invitation lets him in again.
func TestConferenceKicked(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itId(), itId()
	c, _ := newConference(t, s, a, nil)
	for i, uid := range []int64{a, b} {
		if _, err := join(s, c.Id, uid, uint32(30+i)); err != nil {
			t.Fatal(err)
		}
	}
	ch, _ := s.Chain(ctx, c.Id)
	if _, err := s.RemoveFromChain(ctx, RemoveParams{
		CallId: c.Id, CallerId: a, Ids: []int64{b}, Block: tde2etest.RemoveBlock(ch, userKey(a), b), Kick: true, MarkKicked: true,
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.ParticipantOf(ctx, c.Id, b)
	if err != nil || !p.Kicked || p.InCall {
		t.Fatalf("the kicked: %+v %v", p, err)
	}
	if err = s.ClearKicked(ctx, c.Id, b); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.ParticipantOf(ctx, c.Id, b); p.Kicked {
		t.Fatal("still kicked after the creator's invitation")
	}
}

// The slug is reset only while the conference lasts; the old one finds
// nothing.
func TestConferenceResetSlug(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c, _ := newConference(t, s, itId(), nil)
	slug, _ := NewSlug()
	after, err := s.ResetSlug(ctx, c.Id, slug)
	if err != nil || after.Slug != slug {
		t.Fatalf("reset: %v %v", after, err)
	}
	if _, err = s.BySlug(ctx, c.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old slug: %v", err)
	}
	if _, _, _, err = s.Discard(ctx, c.Id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResetSlug(ctx, c.Id, "x"); !errors.Is(err, ErrDiscarded) {
		t.Fatalf("reset of an ended conference: %v", err)
	}
	if over, err := s.BySlug(ctx, slug); err != nil || !over.Discarded {
		t.Fatalf("the slug of an ended conference: %v %v", over, err)
	}
}

// An empty conference ends ConferenceEmptyEnd after the last one left, an
// unused one ConferenceUnusedEnd after it was made, and never while someone
// is in it.
func TestConferenceEmptyEnd(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a := itId()
	c, _ := newConference(t, s, a, nil)
	find := func(id int64) *LiveConference {
		t.Helper()
		for after := int64(0); ; {
			list, err := s.LiveConferences(ctx, after, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, lc := range list {
				if lc.Call.Id == id {
					return lc
				}
				after = lc.Call.Id
			}
			if len(list) < 100 {
				return nil
			}
		}
	}
	lc := find(c.Id)
	if lc == nil || lc.Used || lc.EmptySince == 0 {
		t.Fatalf("a new conference: %+v", lc)
	}
	now := time.Now()
	if EmptyEnds(lc, now.Add(time.Hour)) || !EmptyEnds(lc, now.Add(ConferenceUnusedEnd+time.Minute)) {
		t.Fatal("an unused conference ends at the wrong time")
	}
	if _, err := join(s, c.Id, a, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkJoined(ctx, c.Id); err != nil {
		t.Fatal(err)
	}
	lc = find(c.Id)
	if !lc.Used || lc.EmptySince != 0 || EmptyEnds(lc, now.Add(48*time.Hour)) {
		t.Fatalf("a conference with someone in it: %+v", lc)
	}
	if _, won, err := s.DiscardEmpty(ctx, c.Id); err != nil || won {
		t.Fatalf("an empty discard of a busy conference: %v %v", won, err)
	}
	if _, _, err := s.Leave(ctx, c.Id, a, LeaveCond{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkEmpty(ctx, c.Id, now); err != nil {
		t.Fatal(err)
	}
	lc = find(c.Id)
	if EmptyEnds(lc, now.Add(time.Minute)) || !EmptyEnds(lc, now.Add(ConferenceEmptyEnd+time.Second)) {
		t.Fatalf("an emptied conference ends at the wrong time: %+v", lc)
	}
	over, won, err := s.DiscardEmpty(ctx, c.Id)
	if err != nil || !won || !over.Discarded {
		t.Fatalf("the end: %v %v", won, err)
	}
}

// An invitation moves one way; a move another step won is not repeated;
// the collector finds the unanswered and the unshown.
func TestConferenceInvites(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itId(), itId()
	c, _ := newConference(t, s, a, nil)
	inv := &Invite{CallId: c.Id, InviterId: a, InviteeId: b, InviterMsgId: 11, DialogMessageId: itId()}
	if err := s.AddInvite(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Ringing(ctx, c.Id, b); err != nil || got.Id != inv.Id {
		t.Fatalf("ringing: %v %v", got, err)
	}
	for _, uid := range []int64{a, b} {
		if got, err := s.InviteOf(ctx, inv.DialogMessageId, uid); err != nil || got.Id != inv.Id {
			t.Fatalf("the invitation of the message for %d: %v %v", uid, got, err)
		}
	}
	if _, err := s.InviteOf(ctx, inv.DialogMessageId, itId()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the invitation of another's message: %v", err)
	}
	if ok, _ := s.Invited(ctx, c.Id, b); !ok {
		t.Fatal("not invited")
	}
	if n, _ := s.InvitesSent(ctx, a); n != 1 {
		t.Fatalf("%d invitations sent", n)
	}
	got, moved, err := s.MoveInvite(ctx, inv.Id, InviteRinging, InviteActive, 0)
	if err != nil || !moved || got.State != InviteActive || got.Shown != InviteRinging {
		t.Fatalf("join: %v %v %+v", moved, err, got)
	}
	if _, moved, _ = s.MoveInvite(ctx, inv.Id, InviteRinging, InviteMissed, 0); moved {
		t.Fatal("a decline after the join moved the invitation")
	}
	if list, _ := s.Unshown(ctx, time.Now().Add(-time.Hour), 100); !hasInvite(list, inv.Id) {
		t.Fatal("the unshown invitation is not found")
	}
	if err = s.MarkShown(ctx, inv.Id, InviteActive); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Unshown(ctx, time.Now().Add(-time.Hour), 100); hasInvite(list, inv.Id) {
		t.Fatal("a shown invitation is unshown")
	}
	if got, _, _ = s.MoveInvite(ctx, inv.Id, InviteActive, InviteEnded, 42); got.Duration != 42 {
		t.Fatalf("the end: %+v", got)
	}

	late := &Invite{CallId: c.Id, InviterId: a, InviteeId: itId(), InviterMsgId: 12, DialogMessageId: itId()}
	if err = s.AddInvite(ctx, late); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(InviteRingTimeout + time.Second) }
	defer func() { s.now = time.Now }()
	if list, _ := s.Unanswered(ctx, 1000); !hasInvite(list, late.Id) {
		t.Fatal("the unanswered invitation is not found")
	}
	if _, err = s.Ringing(ctx, c.Id, late.InviteeId); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an invitation that rang out rings: %v", err)
	}
}

func hasInvite(list []*Invite, id int64) bool {
	for _, inv := range list {
		if inv.Id == id {
			return true
		}
	}
	return false
}
