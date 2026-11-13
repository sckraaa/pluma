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

package e2emon

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const callId = 7

type fakeStore struct {
	mu    sync.Mutex
	chain *tde2e.Chain
	users []int64
	diags map[int64]*groupcalls.E2EDiag
}

func (f *fakeStore) Head(_ context.Context, id int64) (*groupcalls.ChainHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != callId {
		return nil, groupcalls.ErrNotFound
	}
	return &groupcalls.ChainHead{Id: callId, Height: f.chain.Height(), Hash: f.chain.LastHash()}, nil
}

func (f *fakeStore) Chain(context.Context, int64) (*tde2e.Chain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chain, nil
}

func (f *fakeStore) Verification(context.Context, int64) (*tde2e.Chain, *tde2e.Verification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := tde2e.NewVerification()
	v.Reset(f.chain)
	return f.chain, v, nil
}

func (f *fakeStore) Users(context.Context, int64) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.users), nil
}

func (f *fakeStore) SaveDiag(_ context.Context, _, userId int64, _ time.Time, d *groupcalls.E2EDiag) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.diags[userId] = d
	return nil
}

// chainOf is a chain the users entered one by one.
func chainOf(t *testing.T, users ...int64) *tde2e.Chain {
	t.Helper()
	ch := tde2e.NewChain()
	for _, u := range users {
		if _, err := ch.Apply(tde2etest.JoinBlock(ch, u, tde2etest.Key(byte(u)))); err != nil {
			t.Fatal(err)
		}
	}
	return ch
}

func gauge(alarm string) float64 {
	return testutil.ToFloat64(metricParticipants.WithLabelValues("7", alarm))
}

// A call of three in its chain: 1 well, 2 behind with an old key that
// failed and that 1 cannot decrypt, 3 gone from the call without being
// removed, 1 joining again and again. Each alarm comes once its condition
// held long enough, is in the diagnostics, and goes when it ends.
func TestAlarms(t *testing.T) {
	st := &fakeStore{chain: chainOf(t, 1, 2, 3), users: []int64{1, 2}, diags: map[int64]*groupcalls.E2EDiag{}}
	m := New(st)
	t0 := time.Now()
	now := t0
	m.now = func() time.Time { return now }
	ctx := context.Background()
	h := st.chain.Height()
	current := prefix(st.chain.LastHash())
	for range 4 {
		m.Joined(callId, 1, true)
	}
	feed := func() {
		m.Poll(callId, 1, 0, h+1)
		m.Poll(callId, 2, 0, h)
		m.SendEpochs(callId, 1, []string{current})
		m.SendEpochs(callId, 2, []string{"0000000000000000"})
		m.Report(callId, 1, &pb.E2EStatus{Seq: 1, Height: h, LastBlock: current, Offset0: h + 1,
			Senders: []*pb.E2ESender{{User: 2, UnknownEpoch: 5, LastOkMs: -1}}})
		m.Report(callId, 2, &pb.E2EStatus{Seq: 1, Height: -1, Error: 300, Offset0: h})
	}
	feed()
	m.Tick(ctx)
	if gauge(AlarmTdE2EError) != 1 || gauge(AlarmUnknownEpoch) != 1 || gauge(AlarmRejoinLoop) != 1 || gauge(AlarmPollBehind) != 0 {
		t.Fatalf("at once: key %v, unknown %v, loop %v, behind %v", gauge(AlarmTdE2EError), gauge(AlarmUnknownEpoch), gauge(AlarmRejoinLoop), gauge(AlarmPollBehind))
	}
	now = t0.Add(31 * time.Second)
	feed()
	m.Tick(ctx)
	for alarm, want := range map[string]float64{
		AlarmPollBehind: 1, AlarmStaleEpoch: 1, AlarmVerification: 3, AlarmUnknownEpoch: 1, AlarmTdE2EError: 1,
		AlarmNotInCall: 1, AlarmRejoinLoop: 1, AlarmUndecrypted: 1,
	} {
		if got := gauge(alarm); got != want {
			t.Errorf("%s: %v, want %v", alarm, got, want)
		}
	}
	if d := st.diags[2]; d == nil || !slices.Contains(d.Alarms, AlarmPollBehind) || d.Report == nil || d.Report.Error != 300 || d.Poll[0].Offset != h {
		t.Fatalf("diagnostics of 2: %+v", d)
	}
	if d := st.diags[3]; d == nil || !slices.Equal(d.Alarms, []string{AlarmVerification, AlarmNotInCall}) {
		t.Fatalf("diagnostics of 3: %+v", d)
	}
	if testutil.ToFloat64(metricHeight.WithLabelValues("7")) != float64(h) {
		t.Fatal("the height of the call")
	}

	// 2 joins again with a new key, catches up and is decrypted; 3 is
	// removed; the joins of 1 age out of the window.
	st.mu.Lock()
	removed := tde2e.NewChain()
	for _, u := range []int64{1, 2} {
		if _, err := removed.Apply(tde2etest.JoinBlock(removed, u, tde2etest.Key(byte(u+10)))); err != nil {
			t.Fatal(err)
		}
	}
	st.chain = removed
	st.mu.Unlock()
	now = t0.Add(6 * time.Minute)
	m.Joined(callId, 2, true)
	h, current = removed.Height(), prefix(removed.LastHash())
	m.Tick(ctx)
	for _, u := range []int64{1, 2} {
		m.Poll(callId, u, 0, h+1)
		m.SendEpochs(callId, u, []string{current})
	}
	m.Report(callId, 1, &pb.E2EStatus{Seq: 2, Height: h, LastBlock: current, Senders: []*pb.E2ESender{{User: 2, Ok: 50, LastOkMs: 10}}})
	m.Report(callId, 2, &pb.E2EStatus{Seq: 1, Height: h, LastBlock: current, Senders: []*pb.E2ESender{{User: 1, Ok: 50, LastOkMs: 10}}})
	m.Tick(ctx)
	for _, alarm := range []string{AlarmPollBehind, AlarmStaleEpoch, AlarmUnknownEpoch, AlarmTdE2EError, AlarmNotInCall, AlarmRejoinLoop, AlarmUndecrypted} {
		if got := gauge(alarm); got != 0 {
			t.Errorf("%s after the repair: %v", alarm, got)
		}
	}

	// A report missing by its seq is counted lost.
	before := testutil.ToFloat64(metricReports.WithLabelValues("lost"))
	m.Report(callId, 1, &pb.E2EStatus{Seq: 5, Height: h, LastBlock: current})
	if got := testutil.ToFloat64(metricReports.WithLabelValues("lost")) - before; got != 2 {
		t.Fatalf("lost reports %v", got)
	}

	// A call nobody touches is dropped with its series.
	now = now.Add(idleCall + time.Second)
	m.Tick(ctx)
	if n := testutil.CollectAndCount(metricHeight); n != 0 {
		t.Fatalf("%d series of the height after the call went idle", n)
	}
}

// A call that empties keeps its last members in the chain: nobody is left
// to remove them, so they are no alarm; once a member of the chain is in
// the call again, one who left and stays is.
func TestNotInCallNeedsAWriter(t *testing.T) {
	st := &fakeStore{chain: chainOf(t, 1, 2), diags: map[int64]*groupcalls.E2EDiag{}}
	m := New(st)
	t0 := time.Now()
	now := t0
	m.now = func() time.Time { return now }
	ctx := context.Background()
	m.Joined(callId, 1, false)
	m.Joined(callId, 2, false)
	m.Left(callId, 1)
	m.Left(callId, 2)
	m.Tick(ctx)
	now = t0.Add(notInCallFor + time.Second)
	m.Tick(ctx)
	if got := gauge(AlarmNotInCall); got != 0 {
		t.Fatalf("an empty call: %v in the alarm", got)
	}
	st.mu.Lock()
	st.users = []int64{2}
	st.mu.Unlock()
	m.Poll(callId, 2, 0, st.chain.Height()+1)
	m.Tick(ctx)
	now = now.Add(notInCallFor + time.Second)
	m.Poll(callId, 2, 0, st.chain.Height()+1)
	m.Tick(ctx)
	if got := gauge(AlarmNotInCall); got != 1 {
		t.Fatalf("one who left while another is in the call: %v in the alarm", got)
	}
}

// The client may join again with a new key 3 times in 5 minutes (ADR 0060
// §5): its first join and those 3 are no loop, a fourth join again is.
func TestRejoinLoopCountsRejoins(t *testing.T) {
	st := &fakeStore{chain: chainOf(t, 1), users: []int64{1}, diags: map[int64]*groupcalls.E2EDiag{}}
	m := New(st)
	now := time.Now()
	m.now = func() time.Time { return now }
	ctx := context.Background()
	m.Joined(callId, 1, false)
	for range rejoinLoopJoins {
		now = now.Add(time.Second)
		m.Joined(callId, 1, true)
	}
	m.Tick(ctx)
	if got := gauge(AlarmRejoinLoop); got != 0 {
		t.Fatalf("a join and %d rejoins: %v in the alarm", rejoinLoopJoins, got)
	}
	if d := st.diags[1]; d == nil || d.Rejoins != rejoinLoopJoins {
		t.Fatalf("diagnostics: %+v", d)
	}
	m.Joined(callId, 1, true)
	m.Tick(ctx)
	if got := gauge(AlarmRejoinLoop); got != 1 {
		t.Fatalf("%d rejoins: %v in the alarm", rejoinLoopJoins+1, got)
	}
}

// A report or epochs over the limits of the schema are not an SFU's and
// change nothing; a call the database does not have is dropped from the
// watch with its series.
func TestRefusesWhatNoSFUSends(t *testing.T) {
	st := &fakeStore{chain: chainOf(t, 1), users: []int64{1}, diags: map[int64]*groupcalls.E2EDiag{}}
	m := New(st)
	ctx := context.Background()
	before := testutil.ToFloat64(metricReports.WithLabelValues("refused"))
	many := make([]*pb.E2ESender, 101)
	for i := range many {
		many[i] = &pb.E2ESender{User: int64(i + 1), LastOkMs: -1}
	}
	m.Report(callId, 1, &pb.E2EStatus{Seq: 1, Height: -1, Offset0: -1, Offset1: -1, Senders: many})
	m.SendEpochs(callId, 1, slices.Repeat([]string{"0000000000000000"}, 16))
	if got := testutil.ToFloat64(metricReports.WithLabelValues("refused")) - before; got != 1 {
		t.Fatalf("refused reports %v", got)
	}
	m.mu.Lock()
	watched := len(m.calls)
	m.mu.Unlock()
	if watched != 0 {
		t.Fatalf("%d calls watched after what was refused", watched)
	}

	const unknown = callId + 1
	m.Report(unknown, 1, &pb.E2EStatus{Seq: 1, Height: -1, Offset0: -1, Offset1: -1})
	m.Tick(ctx)
	m.mu.Lock()
	_, kept := m.calls[unknown]
	m.mu.Unlock()
	if kept {
		t.Fatal("a call the database does not have stays watched")
	}
	// DeleteLabelValues tells whether the series was there.
	series := metricHeight.DeleteLabelValues("8")
	for _, a := range alarms {
		series = metricParticipants.DeleteLabelValues("8", a) || series
	}
	if series {
		t.Fatal("series of a call the database does not have")
	}
}
