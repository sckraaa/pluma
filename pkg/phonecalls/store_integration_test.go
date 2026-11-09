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

package phonecalls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
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

func newCall(admin, participant int64) *Call {
	n := itSeq.Add(1)
	return &Call{
		Id:             time.Now().UnixNano()%1_000_000_000*1000 + n,
		AccessHash:     n,
		AdminId:        admin,
		ParticipantId:  participant,
		AdminAuthKeyId: 1000 + admin,
		RandomId:       int32(n),
		GAHash:         make([]byte, 32),
		AdminProtocol:  Protocol{UdpP2P: true, UdpReflector: true, MinLayer: 65, MaxLayer: 92, LibraryVersions: []string{"9.0.0"}},
	}
}

func itUserIDs() (int64, int64) {
	base := time.Now().UnixMilli()%1_000_000_000*1000 + itSeq.Add(10)
	return base, base + 1
}

func TestCreateRepeats(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	call := newCall(a, b)
	first, created, err := s.Create(ctx, call)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if first.Deadline == 0 || first.State != StateRequested {
		t.Fatalf("new call: %+v", first)
	}
	again := *call
	again.Id = call.Id + 1
	dup, created, err := s.Create(ctx, &again)
	if err != nil || created || dup.Id != call.Id {
		t.Fatalf("same request again: created=%v id=%d err=%v", created, dup.Id, err)
	}
	other := *call
	other.Id, other.ParticipantId = call.Id+2, b+5
	if _, _, err = s.Create(ctx, &other); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("another call with the same random_id: %v", err)
	}
}

func TestFlowAndDuration(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if r, changed, err := s.Received(ctx, created.Id); err != nil || !changed || r.ReceiveDate == 0 {
		t.Fatalf("received: %v %v", changed, err)
	}
	if _, changed, err := s.Received(ctx, created.Id); err != nil || changed {
		t.Fatalf("second received changed: %v", err)
	}
	if _, won, err := s.Accept(ctx, created.Id, 42, []byte("gb"), created.AdminProtocol); err != nil || !won {
		t.Fatalf("accept: %v", err)
	}
	if c, won, err := s.Confirm(ctx, created.Id, []byte("ga"), 9, created.AdminProtocol); err != nil || !won || c.State != StateActive {
		t.Fatalf("confirm: %v %v", won, err)
	}
	if c, won, err := s.Discard(ctx, created.Id, ReasonHangup, false); err != nil || !won || c.Duration < 0 || c.EndDate == 0 {
		t.Fatalf("discard: %v %v", won, err)
	}
	if _, won, err := s.Discard(ctx, created.Id, ReasonMissed, false); err != nil || won {
		t.Fatalf("second discard won: %v", err)
	}
}

// Of the devices that accept at the same moment, one wins (the race of
// core.telegram.org/api/end-to-end/voice-calls).
func TestAcceptRace(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := int64(0); i < 8; i++ {
		wg.Add(1)
		go func(key int64) {
			defer wg.Done()
			if _, won, err := s.Accept(ctx, created.Id, key, []byte("gb"), created.AdminProtocol); err != nil {
				t.Error(err)
			} else if won {
				wins.Add(1)
			}
		}(i + 1)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d devices won", wins.Load())
	}
}

func TestDueAndUnposted(t *testing.T) {
	s, ctx := itStore(), context.Background()
	s.now = func() time.Time { return time.Now().Add(-time.Minute) }
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	s.now = time.Now
	due, err := s.Due(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range due {
		found = found || c.Id == created.Id
	}
	if !found {
		t.Fatal("expired call not due")
	}
	if _, _, err := s.Discard(ctx, created.Id, ReasonMissed, false); err != nil {
		t.Fatal(err)
	}
	list, err := s.Unposted(ctx, time.Now().Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, c := range list {
		found = found || c.Id == created.Id
	}
	if !found {
		t.Fatal("not in unposted")
	}
	if err := s.MarkPosted(ctx, created.Id); err != nil {
		t.Fatal(err)
	}
	list, err = s.Unposted(ctx, time.Now().Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Id == created.Id {
			t.Fatal("posted call still unposted")
		}
	}
}

// A device that heard the call already never gets it again; another device
// that was offline gets it once from the difference.
func TestClaimRinging(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOffered(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimRinging(ctx, b, 5); err != nil || len(got) != 0 {
		t.Fatalf("online device: %v %v", got, err)
	}
	got, err := s.ClaimRinging(ctx, b, 6)
	if err != nil || len(got) != 1 || got[0].Id != created.Id {
		t.Fatalf("offline device: %v %v", got, err)
	}
	if got, err := s.ClaimRinging(ctx, b, 6); err != nil || len(got) != 0 {
		t.Fatalf("offline device again: %v %v", got, err)
	}
	// Neither offer was confirmed: both come again later, the answer or the
	// live update may have been lost. A device that confirmed does not.
	if err := s.MarkReceived(ctx, created.Id, 6); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(ReofferAfter + time.Second) }
	if got, err := s.ClaimRinging(ctx, b, 5); err != nil || len(got) != 1 {
		t.Fatalf("unconfirmed device: %v %v", got, err)
	}
	if got, err := s.ClaimRinging(ctx, b, 6); err != nil || len(got) != 0 {
		t.Fatalf("confirmed device: %v %v", got, err)
	}
	s.now = time.Now
	if _, _, err := s.Discard(ctx, created.Id, ReasonHangup, false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimRinging(ctx, b, 7); err != nil || len(got) != 0 {
		t.Fatalf("ended call: %v %v", got, err)
	}
}

func TestPurge(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOffered(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Discard(ctx, created.Id, ReasonMissed, false); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPosted(ctx, created.Id); err != nil {
		t.Fatal(err)
	}
	n, err := s.Purge(ctx, time.Now().Add(time.Minute), 1000)
	if err != nil || n == 0 {
		t.Fatalf("purge: %d %v", n, err)
	}
	if _, err := s.Get(ctx, created.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after purge: %v", err)
	}
}

func TestDeviceThatDeclinesLosesToTheOneThatAccepted(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if _, won, err := s.Accept(ctx, created.Id, 21, []byte{1}, created.AdminProtocol); err != nil || !won {
		t.Fatalf("accept: %v %v", won, err)
	}
	got, won, err := s.DiscardAsParticipant(ctx, created.Id, 22, ReasonHangup, false)
	if err != nil || won || got.State != StateAccepted {
		t.Fatalf("the other device: %+v %v %v", got, won, err)
	}
	if got, won, err = s.DiscardAsParticipant(ctx, created.Id, 21, ReasonHangup, false); err != nil || !won || got.State != StateDiscarded {
		t.Fatalf("the device that accepted: %+v %v %v", got, won, err)
	}
}

func TestDiscardKeepsVideo(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if got, won, err := s.Discard(ctx, created.Id, ReasonHangup, true); err != nil || !won || !got.Video {
		t.Fatalf("voice call that ended with a camera on: %+v %v %v", got, won, err)
	}
}

func TestExpireRechecks(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	// The sweep read the call in Requested; it moved on and has a new deadline.
	if _, won, err := s.Accept(ctx, created.Id, 21, []byte{1}, created.AdminProtocol); err != nil || !won {
		t.Fatal(won, err)
	}
	later := time.Now().Add(ReceiveTimeout + Grace + time.Second)
	s.now = func() time.Time { return later }
	if got, won, err := s.Expire(ctx, created.Id, StateRequested, ReasonMissed); err != nil || won || got.State != StateAccepted {
		t.Fatalf("a call that moved on: %+v %v %v", got, won, err)
	}
	s.now = time.Now
	if got, won, err := s.Expire(ctx, created.Id, StateAccepted, ReasonDisconnect); err != nil || won || got.State != StateAccepted {
		t.Fatalf("a deadline not reached: %+v %v %v", got, won, err)
	}
	s.now = func() time.Time { return time.Now().Add(ConnectTimeout + Grace + time.Second) }
	if got, won, err := s.Expire(ctx, created.Id, StateAccepted, ReasonDisconnect); err != nil || !won || got.State != StateDiscarded {
		t.Fatalf("expired: %+v %v %v", got, won, err)
	}
}

func TestDeliveryRepeated(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if d, err := s.DeliveryOf(ctx, created.Id, 5); err != nil || d != nil || d.Repeated() {
		t.Fatalf("never offered: %+v %v", d, err)
	}
	if err := s.MarkOffered(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	if d, err := s.DeliveryOf(ctx, created.Id, 5); err != nil || d.Repeated() {
		t.Fatalf("one live offer: %+v %v", d, err)
	}
	if err := s.MarkReceived(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	if d, err := s.DeliveryOf(ctx, created.Id, 5); err != nil || !d.Repeated() {
		t.Fatalf("confirmed: %+v %v", d, err)
	}
	// Offered by a difference: a live update may have crossed it.
	if got, err := s.ClaimRinging(ctx, b, 6); err != nil || len(got) != 1 {
		t.Fatalf("difference: %v %v", got, err)
	}
	if d, err := s.DeliveryOf(ctx, created.Id, 6); err != nil || !d.Repeated() {
		t.Fatalf("offer of a difference: %+v %v", d, err)
	}
}

func TestUnconfirmedOffers(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOffered(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	has := func(list []Device) bool {
		for _, d := range list {
			if d.CallId == created.Id && d.AuthKeyId == 5 {
				return true
			}
		}
		return false
	}
	if got, err := s.Unconfirmed(ctx, 1000); err != nil || has(got) {
		t.Fatalf("too early: %v %v", got, err)
	}
	dev := Device{CallId: created.Id, AuthKeyId: 5}
	base := time.Now()
	for i := 1; i < maxSweepOffers; i++ {
		step := time.Duration(i) * (ReofferAfter + time.Second)
		s.now = func() time.Time { return base.Add(step) }
		if got, err := s.Unconfirmed(ctx, 1000); err != nil || !has(got) {
			t.Fatalf("offer %d: %v %v", i, got, err)
		}
		if ok, err := s.ClaimUnconfirmed(ctx, dev); err != nil || !ok {
			t.Fatalf("claim %d: %v %v", i, ok, err)
		}
		if ok, err := s.ClaimUnconfirmed(ctx, dev); err != nil || ok {
			t.Fatalf("second claim %d: %v %v", i, ok, err)
		}
	}
	step := time.Duration(maxSweepOffers) * (ReofferAfter + time.Second)
	s.now = func() time.Time { return base.Add(step) }
	if got, err := s.Unconfirmed(ctx, 1000); err != nil || has(got) {
		t.Fatalf("over the limit: %v %v", got, err)
	}
	s.now = time.Now
	if rung, err := s.Rung(ctx, created.Id); err != nil || len(rung) != 1 || rung[0] != 5 {
		t.Fatalf("rung: %v %v", rung, err)
	}
}

func TestMissed(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	created, _, err := s.Create(ctx, newCall(a, b))
	if err != nil {
		t.Fatal(err)
	}
	dev := Device{CallId: created.Id, AuthKeyId: created.AdminAuthKeyId}
	has := func() bool {
		got, err := s.Missed(ctx, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range got {
			if d == dev {
				return true
			}
		}
		return false
	}
	if err := s.MarkMissed(ctx, created.Id, dev.AuthKeyId); err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Fatal("not listed")
	}
	if ok, err := s.ClaimMissed(ctx, dev); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, err := s.ClaimMissed(ctx, dev); err != nil || ok {
		t.Fatalf("second claim: %v %v", ok, err)
	}
	// The record of the caller's device does not make it a ringing one.
	if rung, err := s.Rung(ctx, created.Id); err != nil || len(rung) != 0 {
		t.Fatalf("rung: %v %v", rung, err)
	}
	if err := s.MarkMissed(ctx, created.Id, dev.AuthKeyId); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Discard(ctx, created.Id, ReasonHangup, false); err != nil {
		t.Fatal(err)
	}
	if err := s.DropMissed(ctx, time.Now().Add(-MissedKeep)); err != nil {
		t.Fatal(err)
	}
	if !has() {
		t.Fatal("dropped too early")
	}
	if err := s.DropMissed(ctx, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Fatal("not dropped")
	}
}

func TestRecentCalls(t *testing.T) {
	s, ctx := itStore(), context.Background()
	a, b := itUserIDs()
	since := time.Now().Add(-time.Minute)
	if total, peer, err := s.RecentCalls(ctx, a, b, since); err != nil || total != 0 || peer != 0 {
		t.Fatalf("none: %d %d %v", total, peer, err)
	}
	for _, to := range []int64{b, b, b + 1} {
		if _, _, err := s.Create(ctx, newCall(a, to)); err != nil {
			t.Fatal(err)
		}
	}
	if total, peer, err := s.RecentCalls(ctx, a, b, since); err != nil || total != 3 || peer != 2 {
		t.Fatalf("three: %d %d %v", total, peer, err)
	}
	if total, _, err := s.RecentCalls(ctx, a, b, time.Now().Add(time.Hour)); err != nil || total != 0 {
		t.Fatalf("outside the window: %d %v", total, err)
	}
}
