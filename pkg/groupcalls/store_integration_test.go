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
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"
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

func itStore() *Store {
	c := itMySQL.Config()
	return NewStore(sqlx.NewMySQL(&c))
}

func itId() int64 {
	return time.Now().UnixNano()%1_000_000_000*1000 + itSeq.Add(1)
}

// newCall creates a started call of a new supergroup.
func newCall(t *testing.T, s *Store) *Call {
	t.Helper()
	c, created, err := s.Create(context.Background(), &Call{
		Id: itId(), AccessHash: itId(), PeerType: mtproto.PEER_CHANNEL, PeerId: itId(), CreatorId: itId(), RandomId: int32(itSeq.Add(1)),
	})
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	return c
}

func join(s *Store, callId, userId int64, source uint32) (*JoinResult, error) {
	return joinWith(s, JoinParams{
		CallId: callId, UserId: userId, AuthKeyId: userId + 1, Source: source,
		State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
	})
}

// userKey is the key of a user in the tests: one per user, so that his
// joins again are with the same key.
func userKey(userId int64) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte(strconv.FormatInt(userId, 10)))
	return ed25519.NewKeyFromSeed(seed[:])
}

// joinWith joins with the block a client writes: on top of the last block,
// and again on the new last block when another join took the height.
func joinWith(s *Store, p JoinParams) (*JoinResult, error) {
	for attempt := 0; ; attempt++ {
		ch, err := s.Chain(context.Background(), p.CallId)
		if err != nil {
			return nil, err
		}
		key := userKey(p.UserId)
		p.Chain = &ChainJoin{PublicKey: tde2etest.Pub(key), Block: tde2etest.JoinBlock(ch, p.UserId, key)}
		r, err := s.Join(context.Background(), p)
		var e *tde2e.Error
		if errors.As(err, &e) && e.Kind == tde2e.KindStale && attempt < 100 {
			continue
		}
		return r, err
	}
}

func TestMigrationTwice(t *testing.T) {
	for _, name := range []string{"migrate-20261117-group-calls.sql", "migrate-20261118-group-call-chain.sql"} {
		path, err := testinfra.SchemaFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err = itMySQL.ApplySQLFile(context.Background(), path); err != nil {
				t.Fatalf("%s, run %d: %v", name, i+1, err)
			}
		}
	}
}

func TestCreateRepeatsAndOneLiveCall(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	if c.Version != 1 || c.StartDate == 0 || c.Discarded || c.ParticipantsCount != 0 {
		t.Fatalf("new call: %+v", c)
	}
	again := *c
	again.Id = itId()
	first, created, err := s.Create(ctx, &again)
	if err != nil || created || first.Id != c.Id {
		t.Fatalf("the same request again: %v %v %v", created, first, err)
	}
	other := *c
	other.Id, other.RandomId = itId(), c.RandomId+1
	if _, _, err = s.Create(ctx, &other); !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("a second call of the peer: %v", err)
	}
	if _, _, won, err := s.Discard(ctx, c.Id); err != nil || !won {
		t.Fatalf("discard: %v %v", won, err)
	}
	if _, created, err = s.Create(ctx, &other); err != nil || !created {
		t.Fatalf("a call after the end of the first: %v %v", created, err)
	}
}

// Ten accounts join at once: the versions are 2..11, no gaps and no
// repeats, and the count follows.
func TestParallelJoinsVersions(t *testing.T) {
	s := itStore()
	c := newCall(t, s)
	base := itId() * 100
	var (
		mu       sync.Mutex
		versions []int
		wg       sync.WaitGroup
	)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := join(s, c.Id, base+int64(i), uint32(1000+i))
			if err != nil {
				t.Errorf("join %d: %v", i, err)
				return
			}
			if len(r.Changes) != 1 || !r.Changes[0].JustJoined || !r.Changes[0].Versioned {
				t.Errorf("join %d changes: %+v", i, r.Changes)
			}
			mu.Lock()
			versions = append(versions, int(r.Call.Version))
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	sort.Ints(versions)
	for i, v := range versions {
		if v != i+2 {
			t.Fatalf("versions %v", versions)
		}
	}
	got, err := s.Get(context.Background(), c.Id)
	if err != nil || got.ParticipantsCount != 10 || got.Version != 11 {
		t.Fatalf("call after joins: %+v %v", got, err)
	}
}

func TestDuplicateSourceAndRejoin(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a, b := itId(), itId()
	if _, err := join(s, c.Id, a, 77); err != nil {
		t.Fatal(err)
	}
	if _, err := join(s, c.Id, b, 77); !errors.Is(err, ErrSourceInUse) {
		t.Fatalf("a source of another: %v", err)
	}
	// A new connection of a: a new source, versioned, not a new arrival.
	r, err := join(s, c.Id, a, 78)
	if err != nil || r.Changes[0].JustJoined || !r.Changes[0].Versioned || r.Call.ParticipantsCount != 1 {
		t.Fatalf("rejoin: %+v %v", r, err)
	}
	// The old source is free again.
	if _, err = join(s, c.Id, b, 77); err != nil {
		t.Fatalf("a freed source: %v", err)
	}
	// A leave of the replaced connection does not end the new one.
	if _, ch, err := s.Leave(ctx, c.Id, a, LeaveCond{Source: 77}); err != nil || ch != nil {
		t.Fatalf("stale leave: %v %v", ch, err)
	}
	call, ch, err := s.Leave(ctx, c.Id, a, LeaveCond{Source: 78})
	if err != nil || ch == nil || ch.Participant.InCall || !ch.Versioned || call.ParticipantsCount != 1 {
		t.Fatalf("leave: %+v %v", ch, err)
	}
	if call.Version != r.Call.Version+2 {
		t.Fatalf("version after join and leave: %d, the rejoin had %d", call.Version, r.Call.Version)
	}
}

func TestAdminMuteSurvivesLeave(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a := itId()
	if _, err := join(s, c.Id, a, 5); err != nil {
		t.Fatal(err)
	}
	_, p, changed, err := s.Edit(ctx, c.Id, PeerKey{mtproto.PEER_USER, a}, func(p *Participant) (bool, error) {
		r, err := ApplyEdit(p, &Pref{}, Edit{Muted: ptr(true)}, Actor{UserId: a + 1, Manage: true}, false, time.Now())
		return r.Row, err
	})
	if err != nil || !changed || !p.AdminMuted() {
		t.Fatalf("admin mute: %v %v", changed, err)
	}
	if _, _, err = s.Leave(ctx, c.Id, a, LeaveCond{}); err != nil {
		t.Fatal(err)
	}
	r, err := join(s, c.Id, a, 6)
	if err != nil || !r.Self.AdminMuted() || !r.Changes[0].JustJoined {
		t.Fatalf("back after leaving: %+v %v", r, err)
	}
}

// A word an admin gave in a join_muted call stays across a join again of
// one still in the call (a new connection after an SFU restart).
func TestGivenWordSurvivesRejoin(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	if _, _, err := s.SetJoinMuted(ctx, c.Id, true); err != nil {
		t.Fatal(err)
	}
	a := itId()
	r, err := join(s, c.Id, a, 31)
	if err != nil || r.Self.CanSelfUnmute {
		t.Fatalf("join of a listener: %+v %v", r, err)
	}
	if _, _, _, err = s.Edit(ctx, c.Id, PeerKey{mtproto.PEER_USER, a}, func(p *Participant) (bool, error) {
		r, err := ApplyEdit(p, &Pref{}, Edit{Muted: ptr(false)}, Actor{UserId: a + 1, Manage: true}, false, time.Now())
		return r.Row, err
	}); err != nil {
		t.Fatal(err)
	}
	if r, err = join(s, c.Id, a, 32); err != nil || !r.Self.CanSelfUnmute || r.Changes[0].JustJoined {
		t.Fatalf("join again with the word: %+v %v", r.Self, err)
	}
}

// No screen and no camera without the word.
func TestAdminMuteNoScreen(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a := itId()
	if _, err := join(s, c.Id, a, 15); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SetPresentation(ctx, c.Id, a, 16, &Media{Endpoint: "s"}); err != nil {
		t.Fatalf("screen with the word: %v", err)
	}
	_, p, _, err := s.Edit(ctx, c.Id, PeerKey{mtproto.PEER_USER, a}, func(p *Participant) (bool, error) {
		r, err := ApplyEdit(p, &Pref{}, Edit{Muted: ptr(true)}, Actor{UserId: a + 1, Manage: true}, false, time.Now())
		return r.Row, err
	})
	if err != nil || p.Presentation != nil || p.PresentationSource != 0 || !p.VideoStopped {
		t.Fatalf("admin mute keeps the screen: %v %+v", err, p)
	}
	if got, _ := s.ParticipantOf(ctx, c.Id, a); got.Presentation != nil || got.PresentationSource != 0 {
		t.Fatalf("stored screen after the mute: %+v", got)
	}
	if _, _, _, err = s.SetPresentation(ctx, c.Id, a, 16, &Media{Endpoint: "s"}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("screen without the word: %v", err)
	}
	r, err := joinWith(s, JoinParams{
		CallId: c.Id, UserId: a, Source: 17, Video: &Media{Endpoint: "v"}, VideoStopped: false,
		State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
	})
	if err != nil || !r.Self.VideoStopped {
		t.Fatalf("camera on a join without the word: %+v %v", r, err)
	}
}

// A join that opened a room the collector closed as empty places the call
// on its node again; the collector does not forget the node of a call
// someone is in; a join on a node the call left is refused.
func TestJoinNodeAndForgetEmpty(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	if _, _, err := s.SetNode(ctx, c.Id, "", "n1"); err != nil {
		t.Fatal(err)
	}
	if done, err := s.ForgetEmpty(ctx, c.Id, "n1"); err != nil || !done {
		t.Fatalf("forget an empty room: %v %v", done, err)
	}
	a := itId()
	r, err := joinWith(s, JoinParams{
		CallId: c.Id, UserId: a, Source: 21, Node: "n1",
		State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
	})
	if err != nil || r.Call.SfuNode != "n1" {
		t.Fatalf("join after the room was forgotten: %+v %v", r, err)
	}
	if done, err := s.ForgetEmpty(ctx, c.Id, "n1"); err != nil || done {
		t.Fatalf("forget the room of a call someone is in: %v %v", done, err)
	}
	if got, _ := s.Get(ctx, c.Id); got.SfuNode != "n1" {
		t.Fatalf("node %q", got.SfuNode)
	}
	_, err = joinWith(s, JoinParams{
		CallId: c.Id, UserId: itId(), Source: 22, Node: "n2",
		State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
	})
	if !errors.Is(err, ErrMoved) {
		t.Fatalf("join on another node: %v", err)
	}
}

// Two nodes race to place a lost room: one wins.
func TestSetNodeRace(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, won, err := s.SetNode(ctx, c.Id, "", fmt.Sprintf("node%d", i))
			if err != nil {
				t.Error(err)
			}
			if won {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	got, _ := s.Get(ctx, c.Id)
	if wins.Load() != 1 || got.SfuNode == "" {
		t.Fatalf("wins %d node %q", wins.Load(), got.SfuNode)
	}
}

// The collector's leave: only a participant not seen since the limit, with
// the version.
func TestCollectorLeave(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a, b := itId(), itId()
	for i, u := range []int64{a, b} {
		if _, err := join(s, c.Id, u, uint32(20+i)); err != nil {
			t.Fatal(err)
		}
	}
	later := time.Now().Add(time.Minute)
	if err := s.MarkSeen(ctx, c.Id, []int64{a}, later); err != nil {
		t.Fatal(err)
	}
	in, err := s.InCall(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	seen, gone := Sweep(in, map[int64]bool{a: true}, later.Add(time.Second), 30*time.Second)
	if len(seen) != 1 || len(gone) != 1 || gone[0].UserId != b {
		t.Fatalf("sweep: %v %v", seen, gone)
	}
	limit := later.Add(-29 * time.Second).UnixMilli()
	if _, ch, err := s.Leave(ctx, c.Id, a, LeaveCond{SeenBefore: limit}); err != nil || ch != nil {
		t.Fatalf("a seen participant left: %v %v", ch, err)
	}
	call, ch, err := s.Leave(ctx, c.Id, b, LeaveCond{SeenBefore: limit})
	if err != nil || ch == nil || call.Version != 4 || call.ParticipantsCount != 1 {
		t.Fatalf("collector leave: %+v %+v %v", call, ch, err)
	}
}

func TestPagesAndLookup(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	base := itId() * 10
	for i := 0; i < 5; i++ {
		if _, err := join(s, c.Id, base+int64(i), uint32(100+i)); err != nil {
			t.Fatal(err)
		}
	}
	var got []int64
	offset := ""
	for pages := 0; ; pages++ {
		page, err := s.List(ctx, c.Id, offset, 2)
		if err != nil || pages > 5 {
			t.Fatalf("page: %v", err)
		}
		for _, p := range page.Participants {
			got = append(got, p.UserId)
		}
		if page.Next == "" {
			break
		}
		offset = page.Next
	}
	if len(got) != 5 {
		t.Fatalf("pages gave %v", got)
	}
	found, err := s.Lookup(ctx, c.Id, []PeerKey{{mtproto.PEER_USER, base}}, []uint32{103})
	if err != nil || len(found) != 2 {
		t.Fatalf("lookup: %v %v", found, err)
	}
}

func TestDiscardAndPrefs(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a := itId()
	if _, err := join(s, c.Id, a, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPref(ctx, c.Id, a+5, PeerKey{mtproto.PEER_USER, a}, Pref{Muted: true, Volume: 400}); err != nil {
		t.Fatal(err)
	}
	prefs, err := s.Prefs(ctx, c.Id, a+5)
	if err != nil || prefs[PeerKey{mtproto.PEER_USER, a}] != (Pref{Muted: true, Volume: 400}) {
		t.Fatalf("prefs: %v %v", prefs, err)
	}
	call, left, won, err := s.Discard(ctx, c.Id)
	if err != nil || !won || !call.Discarded || len(left) != 1 || call.ParticipantsCount != 0 {
		t.Fatalf("discard: %+v %v %v", call, left, err)
	}
	if _, _, won, err = s.Discard(ctx, c.Id); err != nil || won {
		t.Fatalf("second discard: %v %v", won, err)
	}
	if _, err = join(s, c.Id, a, 2); !errors.Is(err, ErrDiscarded) {
		t.Fatalf("join after the end: %v", err)
	}
	st, err := s.ChatStates(ctx, mtproto.PEER_CHANNEL, []int64{c.PeerId})
	if err != nil || len(st) != 0 {
		t.Fatalf("state after the end: %v %v", st, err)
	}
}

func TestScheduledStart(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c, _, err := s.Create(ctx, &Call{Id: itId(), AccessHash: 1, PeerType: mtproto.PEER_CHAT, PeerId: itId(), CreatorId: 1, RandomId: 1, ScheduleDate: int32(time.Now().Unix() + 3600)})
	if err != nil || !c.Scheduled() {
		t.Fatalf("scheduled: %+v %v", c, err)
	}
	if _, err = join(s, c.Id, 5, 5); !errors.Is(err, ErrScheduled) {
		t.Fatalf("join before the start: %v", err)
	}
	if err = s.Subscribe(ctx, c.Id, 5, true); err != nil {
		t.Fatal(err)
	}
	if on, err := s.Subscribed(ctx, c.Id, 5); err != nil || !on {
		t.Fatalf("subscribed: %v %v", on, err)
	}
	st, err := s.ChatStates(ctx, mtproto.PEER_CHAT, []int64{c.PeerId})
	if err != nil || !st[c.PeerId].Active || st[c.PeerId].NotEmpty {
		t.Fatalf("state of a scheduled call: %v %v", st, err)
	}
	started, won, err := s.Start(ctx, c.Id)
	if err != nil || !won || started.Scheduled() {
		t.Fatalf("start: %v %v", won, err)
	}
	if _, won, _ = s.Start(ctx, c.Id); won {
		t.Fatal("started twice")
	}
	if _, err = join(s, c.Id, 5, 5); err != nil {
		t.Fatalf("join after the start: %v", err)
	}
}
