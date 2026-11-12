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

package stories

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

func setBlocked(t *testing.T, db *sqlx.DB, owner, peer int64, blocked bool, at int32) bool {
	t.Helper()
	var changed bool
	if err := db.Transact(context.Background(), func(tx *sqlx.Tx) error {
		var err error
		changed, err = SetBlockedTx(tx, owner, peer, blocked, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return changed
}

// The two blocklists of a user keep their users out of its stories
// whatever the rules of a story say, and the blocklist of stories is a list
// of its own.
func TestBlocklists(t *testing.T) {
	s, ctx, db := NewStore(itDB()), context.Background(), itDB()
	owner := UserPeer(itId())
	mainBlocked, storiesBlocked, other := itId(), itId(), itId()
	st := itStory(owner, owner.Id, itNow)
	st.Privacy = []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll}}
	st = mustCreate(t, s, st)
	if _, err := db.Exec(ctx, "INSERT INTO user_peer_blocks (id, user_id, peer_type, peer_id, `date`) VALUES (?, ?, ?, ?, ?)",
		itId(), owner.Id, mtproto.PEER_USER, mainBlocked, itNow); err != nil {
		t.Fatal(err)
	}
	if !setBlocked(t, db, owner.Id, storiesBlocked, true, itNow) {
		t.Fatalf("a new user on the list is no change")
	}
	if setBlocked(t, db, owner.Id, storiesBlocked, true, itNow+1) {
		t.Errorf("the same user twice is a change")
	}

	at := time.Unix(int64(itNow), 0)
	audiences, err := Audiences(ctx, db, owner.Id, at)
	if err != nil {
		t.Fatal(err)
	}
	u := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: &mtproto.UserData{Id: owner.Id}, KeysPrivacyRules: audiences}).To_ImmutableUser()
	for id, want := range map[int64]int32{mainBlocked: 0, storiesBlocked: 0, other: st.Id, owner.Id: st.Id} {
		if got := u.StoriesMaxIdFor(id, false, at); got != want {
			t.Errorf("user %d sees story %d, want %d", id, got, want)
		}
	}

	if blocked, err := BlockedOf(ctx, db, owner.Id, []int64{mainBlocked, storiesBlocked, other}); err != nil || len(blocked) != 1 || !blocked[storiesBlocked] {
		t.Errorf("the blocklist of stories: %v, %v", blocked, err)
	}
	page, total, err := s.BlockedPage(ctx, owner.Id, 0, 10)
	if err != nil || total != 1 || len(page) != 1 || page[0].PeerId != storiesBlocked || page[0].Date != itNow {
		t.Errorf("the page of the list: %+v of %d, %v", page, total, err)
	}
	if err = db.Transact(ctx, func(tx *sqlx.Tx) error {
		main, err := MainBlockedTx(tx, owner.Id, mainBlocked)
		if err != nil || !main {
			t.Errorf("the main blocklist: %v, %v", main, err)
		}
		hides, err := IsBlockedTx(tx, owner.Id, mainBlocked)
		if err != nil || hides {
			t.Errorf("the main blocklist read as that of stories: %v, %v", hides, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if !setBlocked(t, db, owner.Id, storiesBlocked, false, itNow+2) {
		t.Errorf("taking a user off is no change")
	}
	if setBlocked(t, db, owner.Id, storiesBlocked, false, itNow+3) {
		t.Errorf("taking off a user not on the list is a change")
	}
	audiences, _ = Audiences(ctx, db, owner.Id, at)
	u = mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: &mtproto.UserData{Id: owner.Id}, KeysPrivacyRules: audiences}).To_ImmutableUser()
	if got := u.StoriesMaxIdFor(storiesBlocked, false, at); got != st.Id {
		t.Errorf("off the list, the user sees %d, want %d", got, st.Id)
	}
	if got := u.StoriesMaxIdFor(mainBlocked, false, at); got != 0 {
		t.Errorf("the main blocklist no longer counts: %d", got)
	}
}

// A supergroup a story names counts its members, as a basic group does.
func TestChatMembersOfSupergroups(t *testing.T) {
	ctx, db := context.Background(), itDB()
	group, broadcast := itId(), itId()
	member, left, stranger := itId(), itId(), itId()
	if _, err := db.Exec(ctx, "INSERT INTO channels (id, access_hash, creator_user_id, title, megagroup) VALUES (?, 1, ?, 'g', 1), (?, 1, ?, 'c', 0)",
		group, member, broadcast, member); err != nil {
		t.Fatal(err)
	}
	for _, row := range [][3]int64{{group, member, 0}, {group, left, 1}, {broadcast, stranger, 0}} {
		if _, err := db.Exec(ctx, "INSERT INTO channel_participants (channel_id, user_id, state) VALUES (?, ?, ?)", row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	rules := []*mtproto.PrivacyRule{
		{PredicateName: mtproto.Predicate_privacyValueAllowAll},
		{PredicateName: mtproto.Predicate_privacyValueDisallowChatParticipants, Chats: []int64{group, broadcast}},
	}
	resolved, err := ResolveChats(ctx, db, rules, []int64{member, left, stranger})
	if err != nil {
		t.Fatal(err)
	}
	// A broadcast channel is no chat of the rules: its subscribers are not
	// its members here.
	for id, want := range map[int64]bool{member: false, left: true, stranger: true} {
		if got := mtproto.EvaluateStoryPrivacy(resolved, mtproto.StoryViewer{PrivacyViewer: mtproto.PrivacyViewer{Id: id}}); got != want {
			t.Errorf("user %d sees a story for all but the supergroup: %v, want %v", id, got, want)
		}
	}
}

// A share is counted once per message, and goes with its story.
func TestSharesCountedOnce(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	st := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	sender := itId()
	for _, random := range []int64{1, 1, 2} {
		if err := s.AddShare(ctx, peer, st.Id, sender, random, itNow); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.One(ctx, peer, st.Id); got.ForwardsCount != 2 {
		t.Errorf("forwards: %d, want 2", got.ForwardsCount)
	}
	if _, err := s.Delete(ctx, peer, []int32{st.Id}, itNow+1); err != nil {
		t.Fatal(err)
	}
	if n, _ := count(ctx, s.db, "SELECT COUNT(*) FROM story_shares WHERE peer_type = ? AND peer_id = ?", peer.Type, peer.Id); n != 0 {
		t.Errorf("the shares of a deleted story stay: %d", n)
	}
}

// Two edits at once each change what they set and keep what the other
// set.
func TestConcurrentEdits(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	st := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	var wg sync.WaitGroup
	edits := []func(*Story){
		func(e *Story) { e.Caption = "edited" },
		func(e *Story) {
			e.Privacy = []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll}}
		},
	}
	errs := make([]error, len(edits))
	for i, edit := range edits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = s.Update(ctx, peer, st.Id, itNow+1, edit)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.One(ctx, peer, st.Id)
	if got.Caption != "edited" || len(got.Privacy) != 1 || got.Privacy[0].GetPredicateName() != mtproto.Predicate_privacyValueAllowAll {
		t.Errorf("after two edits: caption %q, privacy %v", got.Caption, got.Privacy)
	}
}
