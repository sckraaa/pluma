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
	if c, won, err := s.Discard(ctx, created.Id, ReasonHangup); err != nil || !won || c.Duration < 0 || c.EndDate == 0 {
		t.Fatalf("discard: %v %v", won, err)
	}
	if _, won, err := s.Discard(ctx, created.Id, ReasonMissed); err != nil || won {
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
	if _, _, err := s.Discard(ctx, created.Id, ReasonMissed); err != nil {
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
	if err := s.MarkDelivered(ctx, created.Id, 5); err != nil {
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
	if _, _, err := s.Discard(ctx, created.Id, ReasonHangup); err != nil {
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
	if err := s.MarkDelivered(ctx, created.Id, 5); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Discard(ctx, created.Id, ReasonMissed); err != nil {
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
