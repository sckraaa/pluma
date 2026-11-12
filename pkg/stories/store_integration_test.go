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
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

var itSeq atomic.Int64

func itDB() *sqlx.DB {
	c := itMySQL.Config()
	return sqlx.NewMySQL(&c)
}

// itId is an id no other test of the run uses.
func itId() int64 {
	return time.Now().UnixMilli()%1_000_000_000*1000 + itSeq.Add(1)
}

const itNow = int32(1_800_000_000)

func itStory(peer Peer, sender int64, date int32) *Story {
	return &Story{
		Peer: peer, RandomId: itId(), SenderId: sender, Date: date, ExpireDate: date + DefaultPeriod,
		Privacy: []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowContacts}},
		Caption: "caption",
		Media:   mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 77, AccessHash: 9}).To_Photo()}).To_MessageMedia(),
		MediaAreas: []*mtproto.MediaArea{mtproto.MakeTLMediaAreaUrl(&mtproto.MediaArea{
			Coordinates: mtproto.MakeTLMediaAreaCoordinates(&mtproto.MediaAreaCoordinates{X: 1, Y: 2, W: 3, H: 4}).To_MediaAreaCoordinates(),
			Url:         "https://pluma.chat",
		}).To_MediaArea()},
		Entities: []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity()},
	}
}

func mustCreate(t *testing.T, s *Store, st *Story) *Story {
	t.Helper()
	stored, created, err := s.Create(context.Background(), st)
	if err != nil || !created {
		t.Fatalf("create: %v, created %v", err, created)
	}
	return stored
}

// A story is stored under the next id of its peer and read back whole; a
// repeat of the request stores nothing and answers the same story.
func TestCreateAndRepeat(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	st := itStory(peer, peer.Id, itNow)
	st.Mentions = []int64{5, 6}
	first := mustCreate(t, s, st)
	if first.Id != 1 {
		t.Fatalf("the first id: %d", first.Id)
	}
	again, created, err := s.Create(ctx, st)
	if err != nil || created || again == nil || again.Id != 1 {
		t.Fatalf("a repeat: %+v, created %v, %v", again, created, err)
	}
	second := mustCreate(t, s, itStory(peer, peer.Id, itNow+1))
	if second.Id != 2 {
		t.Fatalf("the second id: %d", second.Id)
	}
	got, err := s.One(ctx, peer, 1)
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.Caption != "caption" || got.Media.GetPhoto_FLAGPHOTO().GetId() != 77 || len(got.MediaAreas) != 1 || got.MediaAreas[0].GetUrl() != "https://pluma.chat" ||
		len(got.Entities) != 1 || len(got.Privacy) != 1 || got.ExpireDate != itNow+DefaultPeriod || !slices.Equal(got.Mentions, []int64{5, 6}) || got.Edited || got.Pinned {
		t.Errorf("read back: %+v", got)
	}
	pending, err := s.PendingMentions(ctx, itNow+10, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(pending, func(p *Story) bool { return p.Peer == peer && p.Id == 1 }) {
		t.Errorf("the story with mentions is not pending")
	}
	if err = s.SetMentionsPending(ctx, peer, 1, false); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.PendingMentions(ctx, itNow+10, 1000); slices.ContainsFunc(pending, func(p *Story) bool { return p.Peer == peer }) {
		t.Errorf("mentions still pending after they were told")
	}
	// The id of a deleted story is not used again.
	if _, err = s.Delete(ctx, peer, []int32{2}, itNow+2); err != nil {
		t.Fatal(err)
	}
	if third := mustCreate(t, s, itStory(peer, peer.Id, itNow+3)); third.Id != 3 {
		t.Errorf("the id after a deletion: %d, want 3", third.Id)
	}
}

// Requests at once get different ids, and the same request at once stores
// one story.
func TestCreateConcurrently(t *testing.T) {
	s := NewStore(itDB())
	peer := UserPeer(itId())
	same := itStory(peer, peer.Id, itNow)
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     []int32
		created int
	)
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st := same
			if i%2 == 0 {
				st = itStory(peer, peer.Id, itNow)
			}
			stored, made, err := s.Create(context.Background(), st)
			if err != nil {
				t.Errorf("create: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if made {
				created++
				ids = append(ids, stored.Id)
			}
		}()
	}
	wg.Wait()
	slices.Sort(ids)
	if created != 7 || !slices.Equal(ids, []int32{1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("created %d stories with ids %v, want 7 with 1..7", created, ids)
	}
}

// The limits of a peer: the stories in the feeds at once, and those of a
// week.
func TestLimits(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	// A week's worth, each already out of the feeds.
	for i := range MaxWeekly {
		st := itStory(peer, peer.Id, itNow+int32(i))
		st.ExpireDate = st.Date + 1
		mustCreate(t, s, st)
	}
	at := itNow + MaxWeekly + 10
	_, _, err := s.Create(ctx, itStory(peer, peer.Id, at))
	var flood *FloodError
	if !errors.As(err, &flood) || flood.Monthly || flood.Until != itNow+7*86400 {
		t.Fatalf("the weekly limit: %v", err)
	}
	if _, err = s.Remains(ctx, peer, at); !errors.As(err, &flood) {
		t.Errorf("Remains at the weekly limit: %v", err)
	}
	// A week later the first of them has left the window.
	if remains, err := s.Remains(ctx, peer, itNow+7*86400+1); err != nil || remains != MaxActive {
		t.Errorf("a week later: %d, %v", remains, err)
	}

	crowded := ChannelPeer(itId())
	// Posted eight days apart, oldest first, each still in the feeds.
	for i := MaxActive - 1; i >= 0; i-- {
		st := itStory(crowded, 1, itNow-int32(i)*86400*8)
		st.ExpireDate = itNow + 86400
		mustCreate(t, s, st)
	}
	if _, _, err = s.Create(ctx, itStory(crowded, 1, itNow)); !errors.Is(err, ErrTooMany) {
		t.Errorf("the limit of the feeds: %v", err)
	}
}

// The feeds, the profile and the archive of a peer.
func TestListsAndPins(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	old := itStory(peer, peer.Id, itNow-3*86400)
	mustCreate(t, s, old) // 1: out of the feeds
	for i := range 4 {
		mustCreate(t, s, itStory(peer, peer.Id, itNow-int32(4-i))) // 2..5
	}
	active, err := s.Active(ctx, peer, itNow)
	if err != nil || !slices.Equal(idsOfStories(active), []int32{2, 3, 4, 5}) {
		t.Fatalf("the feeds: %v, %v", idsOfStories(active), err)
	}
	if id, _ := s.MaxActiveId(ctx, peer, itNow); id != 5 {
		t.Errorf("the newest in the feeds: %d", id)
	}
	if peers, _ := s.ActivePeers(ctx, mtproto.PEER_USER, itNow); !slices.Contains(peers, peer.Id) {
		t.Errorf("the peer is not among those with stories")
	}
	byPeer, err := s.ActiveOf(ctx, mtproto.PEER_USER, []int64{peer.Id, itId()}, itNow)
	if err != nil || len(byPeer) != 1 || len(byPeer[peer.Id]) != 4 {
		t.Errorf("the feeds of several peers: %v, %v", byPeer, err)
	}

	found, err := s.SetPinned(ctx, peer, []int32{1, 3, 5, 99}, true)
	if err != nil || !slices.Equal(found, []int32{1, 3, 5}) {
		t.Fatalf("pin: %v, %v", found, err)
	}
	if has, _ := s.HasPinned(ctx, peer); !has {
		t.Errorf("the profile has no stories")
	}
	page, count, err := s.Pinned(ctx, peer, 0, 2)
	if err != nil || count != 3 || !slices.Equal(idsOfStories(page), []int32{5, 3}) {
		t.Errorf("the profile, the first page: %v of %d, %v", idsOfStories(page), count, err)
	}
	if page, _, _ = s.Pinned(ctx, peer, 3, 2); !slices.Equal(idsOfStories(page), []int32{1}) {
		t.Errorf("the profile, the next page: %v", idsOfStories(page))
	}
	if err = s.SetPinnedToTop(ctx, peer, []int32{1, 5}); err != nil {
		t.Fatal(err)
	}
	if top, _ := s.PinnedToTop(ctx, peer); !slices.Equal(top, []int32{1, 5}) {
		t.Errorf("the top of the profile: %v", top)
	}
	if err = s.SetPinnedToTop(ctx, peer, []int32{2}); !errors.Is(err, ErrNotPinned) {
		t.Errorf("a story that is not in the profile at its top: %v", err)
	}
	if top, _ := s.PinnedToTop(ctx, peer); !slices.Equal(top, []int32{1, 5}) {
		t.Errorf("the top after a refused change: %v", top)
	}
	// Taken out of the profile, a story leaves its top too.
	if _, err = s.SetPinned(ctx, peer, []int32{1}, false); err != nil {
		t.Fatal(err)
	}
	if top, _ := s.PinnedToTop(ctx, peer); !slices.Equal(top, []int32{5}) {
		t.Errorf("the top after an unpin: %v", top)
	}
	all, count, err := s.Archive(ctx, peer, 0, 100)
	if err != nil || count != 5 || !slices.Equal(idsOfStories(all), []int32{5, 4, 3, 2, 1}) {
		t.Errorf("the archive: %v of %d, %v", idsOfStories(all), count, err)
	}
}

func idsOfStories(list []*Story) []int32 {
	ids := make([]int32, 0, len(list))
	for _, st := range list {
		ids = append(ids, st.Id)
	}
	return ids
}

// An edit, a deletion, and the mark they leave for the services that show
// the peer.
func TestEditDeleteAndDirty(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	st := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	if at, _ := s.DirtyAt(ctx, peer); at != itNow {
		t.Fatalf("a new story marks its peer at %d, want %d", at, itNow)
	}
	// A change after the telling began stays marked.
	if err := s.ClearDirty(ctx, peer, itNow-1); err != nil {
		t.Fatal(err)
	}
	if at, _ := s.DirtyAt(ctx, peer); at != itNow {
		t.Errorf("an older telling cleared the mark")
	}
	if err := s.ClearDirty(ctx, peer, itNow); err != nil {
		t.Fatal(err)
	}
	if at, _ := s.DirtyAt(ctx, peer); at != 0 {
		t.Errorf("the mark stays after the telling: %d", at)
	}

	before, after, err := s.Update(ctx, peer, st.Id, itNow+5, func(e *Story) {
		e.Caption = "new"
		e.Privacy = []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll}}
		e.MediaAreas = nil
	})
	if err != nil || after == nil || before == nil || before.Caption != st.Caption || !after.Edited {
		t.Fatalf("edit: %+v %+v %v", before, after, err)
	}
	got, _ := s.One(ctx, peer, st.Id)
	if got.Caption != "new" || !got.Edited || len(got.MediaAreas) != 0 || got.Privacy[0].GetPredicateName() != mtproto.Predicate_privacyValueAllowAll || got.Date != itNow {
		t.Errorf("after the edit: %+v", got)
	}
	dirty, err := s.Dirty(ctx, itNow+100, 1000)
	if err != nil || dirty[peer] != itNow+5 {
		t.Errorf("the edit marks the peer: %v, %v", dirty[peer], err)
	}
	if before, after, err := s.Update(ctx, peer, 99, itNow+6, func(*Story) {}); err != nil || before != nil || after != nil {
		t.Errorf("an edit of a story that is gone: %v %v %v", before, after, err)
	}

	if _, err = s.AddViews(ctx, peer, []int32{st.Id}, 500, itNow); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.Delete(ctx, peer, []int32{st.Id, 42}, itNow+7)
	if err != nil || len(deleted) != 1 || deleted[0].Id != st.Id {
		t.Fatalf("delete: %v, %v", deleted, err)
	}
	if got, _ = s.One(ctx, peer, st.Id); got != nil {
		t.Errorf("the story is still there")
	}
	if views, _ := s.Views(ctx, peer, st.Id); len(views) != 0 {
		t.Errorf("the views of a deleted story stay: %v", views)
	}
	if at, _ := s.DirtyAt(ctx, peer); at != itNow+7 {
		t.Errorf("the deletion marks the peer at %d", at)
	}
	if deleted, err = s.Delete(ctx, peer, []int32{st.Id}, itNow+8); err != nil || len(deleted) != 0 {
		t.Errorf("a second deletion: %v, %v", deleted, err)
	}
}

// Views, reactions and what is read.
func TestViewsReactionsReads(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	a, b, c := itId(), itId(), itId()
	st1 := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	st2 := mustCreate(t, s, itStory(peer, peer.Id, itNow+1))
	ids := []int32{st1.Id, st2.Id}

	if added, err := s.AddViews(ctx, peer, ids, a, itNow+10); err != nil || !slices.Equal(added, ids) {
		t.Fatalf("the first views: %v, %v", added, err)
	}
	if added, err := s.AddViews(ctx, peer, ids, a, itNow+11); err != nil || len(added) != 0 {
		t.Fatalf("the same views again: %v, %v", added, err)
	}
	if _, err := s.AddViews(ctx, peer, []int32{st1.Id}, b, itNow+12); err != nil {
		t.Fatal(err)
	}
	// A reaction makes a viewer of one who was not, and replaces the one
	// before it.
	if err := s.SetReaction(ctx, peer, st1.Id, c, "e❤", itNow+13); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReaction(ctx, peer, st1.Id, a, "e\U0001F525", itNow+14); err != nil {
		t.Fatal(err)
	}
	if err := s.SetReaction(ctx, peer, st1.Id, a, "e❤", itNow+15); err != nil {
		t.Fatal(err)
	}
	stats, err := s.StatsOf(ctx, peer, ids, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := stats[st1.Id]; got.Views != 3 || got.Reactions != 2 || len(got.ByKind) != 1 || got.ByKind[0].Count != 2 || !slices.Equal(got.Recent, []int64{c, b, a}) {
		t.Errorf("the counters of the first story: %+v", got)
	}
	if got := stats[st2.Id]; got.Views != 1 || got.Reactions != 0 {
		t.Errorf("the counters of the second story: %+v", got)
	}
	views, err := s.Views(ctx, peer, st1.Id)
	if err != nil || len(views) != 3 || views[0].ViewerId != c || views[2].ViewerId != a || views[2].Reaction != "e❤" || views[2].Date != itNow+10 || views[2].ReactionDate != itNow+15 {
		t.Errorf("the viewers: %+v, %v", views, err)
	}
	if sent, _ := s.SentReactions(ctx, peer, ids, a); len(sent) != 1 || sent[st1.Id] != "e❤" {
		t.Errorf("the reactions a viewer left: %v", sent)
	}
	// Taking a reaction back keeps the view.
	if err = s.SetReaction(ctx, peer, st1.Id, c, "", itNow+16); err != nil {
		t.Fatal(err)
	}
	if stats, _ = s.StatsOf(ctx, peer, ids, 0); stats[st1.Id].Views != 3 || stats[st1.Id].Reactions != 1 {
		t.Errorf("after a reaction is taken back: %+v", stats[st1.Id])
	}

	if changed, err := s.MarkRead(ctx, a, peer, 1); err != nil || !changed {
		t.Fatalf("the first read: %v %v", changed, err)
	}
	if changed, _ := s.MarkRead(ctx, a, peer, 1); changed {
		t.Errorf("the same read again is news")
	}
	if changed, _ := s.MarkRead(ctx, a, peer, 2); !changed {
		t.Errorf("a later read is not news")
	}
	if changed, _ := s.MarkRead(ctx, a, peer, 1); changed {
		t.Errorf("an earlier read moved the mark back")
	}
	if id, _ := s.MaxRead(ctx, a, peer); id != 2 {
		t.Errorf("read up to %d, want 2", id)
	}
	if reads, _ := s.Reads(ctx, a); len(reads) != 1 || reads[peer] != 2 {
		t.Errorf("everything read: %v", reads)
	}
	if id, _ := s.MaxRead(ctx, b, peer); id != 0 {
		t.Errorf("a user who read nothing: %d", id)
	}
}

// The stealth mode: its dates, its cooldown and the views it erases.
func TestStealth(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	viewer := itId()
	user, channel := UserPeer(itId()), ChannelPeer(itId())
	mustCreate(t, s, itStory(user, user.Id, itNow))
	mustCreate(t, s, itStory(user, user.Id, itNow))
	mustCreate(t, s, itStory(user, user.Id, itNow))
	mustCreate(t, s, itStory(channel, 1, itNow))
	// An old view, a fresh one, a fresh one with a reaction, a fresh one of
	// a channel's story.
	for id, at := range map[int32]int32{1: itNow - StealthPast - 5, 2: itNow - 10} {
		if _, err := s.AddViews(ctx, user, []int32{id}, viewer, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetReaction(ctx, user, 3, viewer, "e❤", itNow-10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddViews(ctx, channel, []int32{1}, viewer, itNow-10); err != nil {
		t.Fatal(err)
	}

	if st, _ := s.Stealth(ctx, viewer); st.Active(itNow) || st.CooldownUntil != 0 {
		t.Fatalf("the mode of one who never used it: %+v", st)
	}
	st, err := s.ActivateStealth(ctx, viewer, true, true, itNow)
	if err != nil || st.ActiveUntil != itNow+StealthFuture || st.CooldownUntil != itNow+StealthCooldown || !st.Active(itNow+1) || st.Active(itNow+StealthFuture) {
		t.Fatalf("turned on: %+v, %v", st, err)
	}
	if got, _ := s.Stealth(ctx, viewer); got != st {
		t.Errorf("read back: %+v", got)
	}
	left := map[int32]int{}
	for id := int32(1); id <= 3; id++ {
		views, _ := s.Views(ctx, user, id)
		left[id] = len(views)
	}
	if left[1] != 1 || left[2] != 0 || left[3] != 1 {
		t.Errorf("the views left: %v, want the old one and the one with a reaction", left)
	}
	if views, _ := s.Views(ctx, channel, 1); len(views) != 1 {
		t.Errorf("the view of a channel's story was erased")
	}
	if _, err = s.ActivateStealth(ctx, viewer, true, true, itNow+StealthCooldown-1); !errors.Is(err, ErrCooldown) {
		t.Errorf("turned on again while cooling down: %v", err)
	}
	if st, err = s.ActivateStealth(ctx, viewer, false, false, itNow+StealthCooldown); err != nil || st.ActiveUntil != 0 {
		t.Errorf("after the cooldown, neither past nor future: %+v, %v", st, err)
	}
}

// What the services of users and of channels read: the stories of a user
// with their rules, the newest story of each channel, and whose archive
// row a channel is in.
func TestWhatOtherServicesRead(t *testing.T) {
	s, ctx, db := NewStore(itDB()), context.Background(), itDB()
	user := UserPeer(itId())
	gone := itStory(user, user.Id, itNow-2*86400)
	mustCreate(t, s, gone)
	st := itStory(user, user.Id, itNow)
	st.Privacy = []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowUsers, Users: []int64{42}}}
	mustCreate(t, s, st)
	audiences, err := Audiences(ctx, db, user.Id, time.Unix(int64(itNow), 0))
	if err != nil || len(audiences) != 1 || !mtproto.IsStoryAudience(audiences[0]) {
		t.Fatalf("the stories of the user: %v, %v", audiences, err)
	}
	owner := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: &mtproto.UserData{Id: user.Id}, KeysPrivacyRules: audiences}).To_ImmutableUser()
	at := time.Unix(int64(itNow), 0)
	if id := owner.StoriesMaxIdFor(42, false, at); id != 2 {
		t.Errorf("the one the story names sees %d, want 2", id)
	}
	if id := owner.StoriesMaxIdFor(43, false, at); id != 0 {
		t.Errorf("another user sees %d, want none", id)
	}

	ch1, ch2 := ChannelPeer(itId()), ChannelPeer(itId())
	mustCreate(t, s, itStory(ch1, 1, itNow))
	mustCreate(t, s, itStory(ch1, 1, itNow))
	mustCreate(t, s, itStory(ch2, 1, itNow-2*86400))
	maxIds, err := ActiveMaxIds(ctx, db, mtproto.PEER_CHANNEL, []int64{ch1.Id, ch2.Id}, at)
	if err != nil || maxIds[ch1.Id] != 2 || maxIds[ch2.Id] != 0 {
		t.Errorf("the newest stories of the channels: %v, %v", maxIds, err)
	}

	viewer, other := itId(), itId()
	if err = s.SetHidden(ctx, viewer, ch1, true); err != nil {
		t.Fatal(err)
	}
	if err = s.SetHidden(ctx, viewer, ch1, true); err != nil {
		t.Fatalf("hidden twice: %v", err)
	}
	if hidden, _ := HiddenChannels(ctx, db, viewer, []int64{ch1.Id, ch2.Id}); !hidden[ch1.Id] || hidden[ch2.Id] {
		t.Errorf("the channels in the archive row: %v", hidden)
	}
	if hiding, _ := HidingUsers(ctx, db, ch1.Id, []int64{viewer, other}); !hiding[viewer] || hiding[other] {
		t.Errorf("the users who hid the channel: %v", hiding)
	}
	if err = s.SetHidden(ctx, viewer, ch1, false); err != nil {
		t.Fatal(err)
	}
	if hidden, _ := HiddenChannels(ctx, db, viewer, []int64{ch1.Id}); hidden[ch1.Id] {
		t.Errorf("the channel is still in the archive row")
	}
}

// A story leaves the feeds when its period ends; the worker is told once.
func TestExpiry(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	short := itStory(peer, peer.Id, itNow)
	short.ExpireDate = itNow + 6*3600
	mustCreate(t, s, short)
	mustCreate(t, s, itStory(peer, peer.Id, itNow))

	has := func(at int32) bool {
		peers, err := s.Expired(ctx, at, 100000)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(peers, peer)
	}
	if has(itNow + 6*3600 - 1) {
		t.Fatalf("a story expired before its period ended")
	}
	at := itNow + 6*3600
	if !has(at) {
		t.Fatalf("the story whose period ended is not found")
	}
	if id, _ := s.MaxActiveId(ctx, peer, at); id != 2 {
		t.Errorf("the newest in the feeds after the first expired: %d", id)
	}
	if err := s.MarkExpired(ctx, peer, at); err != nil {
		t.Fatal(err)
	}
	if has(at) {
		t.Errorf("the expiry is found again after it was recorded")
	}
	// The row stays: the archive of its owner.
	if st, _ := s.One(ctx, peer, 1); st == nil || st.Active(at) {
		t.Errorf("the expired story: %+v", st)
	}
	if !has(itNow + DefaultPeriod) {
		t.Errorf("the second story does not expire")
	}
}

// The chats of a story's rules, resolved for the viewers, and the channels
// a user may post stories to.
func TestChatsAndPostingChannels(t *testing.T) {
	ctx, db := context.Background(), itDB()
	chat, member, stranger, creator := itId(), itId(), itId(), itId()
	if _, err := db.Exec(ctx, "INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title) VALUES (?, ?, 1, ?, 2, 'g')", chat, creator, itId()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{creator, member} {
		if _, err := db.Exec(ctx, "INSERT INTO chat_participants (id, chat_id, user_id) VALUES (?, ?, ?)", itId(), chat, id); err != nil {
			t.Fatal(err)
		}
	}
	rules := []*mtproto.PrivacyRule{
		{PredicateName: mtproto.Predicate_privacyValueAllowChatParticipants, Chats: []int64{chat}},
	}
	resolved, err := ResolveChats(ctx, db, rules, []int64{member, stranger})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]bool{member: true, stranger: false} {
		if got := mtproto.EvaluateStoryPrivacy(resolved, mtproto.StoryViewer{PrivacyViewer: mtproto.PrivacyViewer{Id: id}}); got != want {
			t.Errorf("user %d sees a story for the chat: %v, want %v", id, got, want)
		}
	}
	if same, _ := ResolveChats(ctx, db, rules[:0], []int64{member}); len(same) != 0 {
		t.Errorf("rules without chats: %v", same)
	}

	owned, adminOf, plain, noRight := itId(), itId(), itId(), itId()
	for _, id := range []int64{owned, adminOf, plain, noRight} {
		if _, err := db.Exec(ctx, "INSERT INTO channels (id, access_hash, creator_user_id, title) VALUES (?, 1, ?, 'c')", id, creator); err != nil {
			t.Fatal(err)
		}
	}
	for id, row := range map[int64][2]int32{
		owned:   {2, 0},
		adminOf: {1, mtproto.ADMIN_POST_STORIES | mtproto.ADMIN_POST_MESSAGES},
		plain:   {0, 0},
		noRight: {1, mtproto.ADMIN_POST_MESSAGES},
	} {
		if _, err := db.Exec(ctx, "INSERT INTO channel_participants (channel_id, user_id, role, admin_rights) VALUES (?, ?, ?, ?)", id, creator, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	got, err := PostingChannels(ctx, db, creator)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{owned, adminOf}
	slices.Sort(want)
	slices.Reverse(want)
	if !slices.Equal(got, want) {
		t.Errorf("the channels to post to: %v, want %v", got, want)
	}
}
