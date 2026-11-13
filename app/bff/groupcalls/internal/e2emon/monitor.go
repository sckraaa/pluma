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

// Package e2emon watches the end-to-end encryption of group calls for the
// operator (ADR 0060 §5): who polls the chain behind its height, who
// encrypts with an old epoch, whose emoji verification is stuck, who
// cannot decrypt whom, whose key failed, who is in the chain but not in
// the call and who joins again and again. It sees heights, prefixes of
// hashes and counters, never a key or a frame. The state is in memory, per
// bff instance; the metrics are per call, without user labels, and the
// details per participant go to group_call_e2e_diag for pluma-admin calls
// show.
package e2emon

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"

	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/zeromicro/go-zero/core/logx"
)

// The alarms of ADR 0060 §5 and how long each condition must hold.
const (
	// AlarmPollBehind: the participant polls sub-chain 0 from below the
	// height, or has not polled, for pollBehindFor.
	AlarmPollBehind = "poll_behind"
	// AlarmStaleEpoch: the participant's sound is not encrypted with the
	// epoch of the current height for staleEpochFor after the block:
	// tde2e forgets an epoch 10 s after the next (Call.h:98).
	AlarmStaleEpoch = "stale_send_epoch"
	// AlarmVerification: the participant has not sent its commit or
	// reveal of the current height verificationFor after the block.
	AlarmVerification = "verification_stuck"
	// AlarmUnknownEpoch: the last report of a receiver counts frames of a
	// sender in an unknown epoch and none decrypted; one per pair.
	AlarmUnknownEpoch = "unknown_epoch"
	// AlarmTdE2EError: the last report of the participant names a tde2e
	// error its key stopped at.
	AlarmTdE2EError = "tde2e_error"
	// AlarmNotInCall: in the group state of the chain, not in the call,
	// for notInCallFor while another member of the chain is in the call:
	// no client removed it. With nobody of the chain in the call nobody
	// can: the writer of a removal must be in the group state
	// (tde2e.Chain.Remove), and the server never writes a block (ADR 0060
	// §2).
	AlarmNotInCall = "not_in_call"
	// AlarmRejoinLoop: more than rejoinLoopJoins joins again, of one who
	// was in the group state already, in rejoinLoopWindow: the
	// self-healing in a loop (ADR 0060 §5: more than 3 rejoins in 5
	// minutes). The first join of a membership is not one.
	AlarmRejoinLoop = "rejoin_loop"
	// AlarmUndecrypted: the reports that name the participant as a sender
	// decrypted none of its frames for undecryptedFor.
	AlarmUndecrypted = "undecrypted_sender"
)

var alarms = []string{AlarmPollBehind, AlarmStaleEpoch, AlarmVerification, AlarmUnknownEpoch, AlarmTdE2EError, AlarmNotInCall, AlarmRejoinLoop, AlarmUndecrypted}

const (
	pollBehindFor    = 15 * time.Second
	staleEpochFor    = 12 * time.Second
	verificationFor  = 15 * time.Second
	notInCallFor     = 30 * time.Second
	undecryptedFor   = 10 * time.Second
	rejoinLoopWindow = 5 * time.Minute
	rejoinLoopJoins  = 3
	// fresh: a report or the epochs of the SFU count while they are this
	// old at most (reports come every 5 s).
	fresh = 12 * time.Second
	// idleCall: a call nobody polled or reported for this long is dropped
	// from the watch.
	idleCall = 2 * time.Minute
	// flushEvery: the diagnostics are written this often.
	flushEvery = 5 * time.Second
	// TickEvery is how often Tick should run: the alarms hold for 10 s and
	// more.
	TickEvery = time.Second
)

var (
	metricParticipants = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "groupcalls",
		Name:      "e2e_participants",
		Help:      "Participants of a call in an alarm of the end-to-end encryption (ADR 0060 §5), by call and alarm; unknown_epoch counts receiver and sender pairs.",
	}, []string{"call", "alarm"})
	metricHeight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "groupcalls",
		Name:      "e2e_chain_height",
		Help:      "Height of sub-chain 0 of the encryption of a call.",
	}, []string{"call"})
	metricReports = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "groupcalls",
		Name:      "e2e_reports_total",
		Help:      "Status reports of the encryption the SFU passed on (accepted), those missing by their seq (lost), and those bff refused over the limits of the schema (refused).",
	}, []string{"result"})
	metricRejoins = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "groupcalls",
		Name:      "e2e_rejoins_total",
		Help:      "Joins of a user who was in the group state of the chain already: a new connection or a new key after a failure.",
	})
)

// Store is what the monitor reads and writes of pkg/groupcalls.
type Store interface {
	Head(ctx context.Context, callId int64) (*groupcalls.ChainHead, error)
	Chain(ctx context.Context, callId int64) (*tde2e.Chain, error)
	Verification(ctx context.Context, callId int64) (*tde2e.Chain, *tde2e.Verification, error)
	Users(ctx context.Context, callId int64) ([]int64, error)
	SaveDiag(ctx context.Context, callId, userId int64, at time.Time, d *groupcalls.E2EDiag) error
}

// Monitor is the watch of all calls of one bff instance. Its methods are
// safe for concurrent use; the hooks never block on the database.
type Monitor struct {
	store Store
	now   func() time.Time

	mu        sync.Mutex
	calls     map[int64]*callState
	lastFlush time.Time
}

type callState struct {
	active time.Time
	// height, hash and heightAt are the chain as last read, and when this
	// instance first saw that height.
	height   int32
	hash     [32]byte
	heightAt time.Time
	members  map[int64]bool
	inCall   map[int64]bool
	// verifiedHeight is the height the verification was read at;
	// committed and revealed are who sent theirs there; verified is set
	// once everyone did.
	verifiedHeight int32
	verified       bool
	committed      map[int64]bool
	revealed       map[int64]bool
	peers          map[int64]*peerState
	// labeled is whether the call has series of the metrics.
	labeled bool
}

type peerState struct {
	poll        [2]groupcalls.E2EPoll
	behindSince time.Time
	sendEpochs  []string
	sendAt      time.Time
	report      *tgcallsjson.E2EStatus
	reportAt    time.Time
	reportsLost int64
	// rejoins are the joins of the user while in the group state already.
	rejoins  []time.Time
	outSince time.Time
	// undecryptedSince is since when no receiver decrypted this sender.
	undecryptedSince time.Time
	alarms           []string
	dirty            bool
}

// New returns a monitor over store.
func New(store Store) *Monitor {
	return &Monitor{store: store, now: time.Now, calls: map[int64]*callState{}}
}

func (m *Monitor) call(callId int64, now time.Time) *callState {
	c := m.calls[callId]
	if c == nil {
		c = &callState{height: -2, verifiedHeight: -2, members: map[int64]bool{}, inCall: map[int64]bool{}, peers: map[int64]*peerState{}}
		m.calls[callId] = c
	}
	c.active = now
	return c
}

func (c *callState) peer(userId int64) *peerState {
	p := c.peers[userId]
	if p == nil {
		p = &peerState{}
		c.peers[userId] = p
	}
	p.dirty = true
	return p
}

// prefix is how the reports and the SFU name a hash: its first 8 bytes in
// hex.
func prefix(hash [32]byte) string {
	return hex.EncodeToString(hash[:8])
}

// Poll records a phone.getGroupCallChainBlocks of userId.
func (m *Monitor) Poll(callId, userId int64, subChain int32, offset int32) {
	if subChain != 0 && subChain != 1 {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.call(callId, now).peer(userId)
	p.poll[subChain] = groupcalls.E2EPoll{Offset: offset, At: now.UnixMilli()}
}

// Joined records a join of userId; wasMember is whether the user was in the
// group state before its block: a join again, of a new connection or of a
// new key after a failure.
func (m *Monitor) Joined(callId, userId int64, wasMember bool) {
	now := m.now()
	if wasMember {
		metricRejoins.Inc()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.call(callId, now).peer(userId)
	if wasMember {
		p.rejoins = append(p.rejoins, now)
	}
	// The client polls and reports anew from its new state.
	p.poll = [2]groupcalls.E2EPoll{}
	p.behindSince, p.report, p.reportAt = time.Time{}, nil, time.Time{}
}

// Left forgets what the polls of userId told: a participant who left polls
// no more until it joins again.
func (m *Monitor) Left(callId, userId int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.calls[callId]
	if c == nil {
		return
	}
	if p := c.peers[userId]; p != nil {
		p.poll, p.behindSince, p.dirty = [2]groupcalls.E2EPoll{}, time.Time{}, true
	}
}

// Chain records a block appended by this instance: the call's new height,
// hash and group state.
func (m *Monitor) Chain(callId int64, height int32, hash [32]byte, members []int64) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.call(callId, now).setChain(height, hash, members, now)
}

func (c *callState) setChain(height int32, hash [32]byte, members []int64, now time.Time) {
	if height == c.height && hash == c.hash {
		return
	}
	c.height, c.hash, c.heightAt = height, hash, now
	c.members = make(map[int64]bool, len(members))
	for _, id := range members {
		c.members[id] = true
	}
	c.verified, c.committed, c.revealed = false, nil, nil
}

// SendEpochs records the epochs the SFU saw in the sound of userId. A list
// longer than tde2e keeps, or of other than prefixes of hashes, is not an
// SFU's and is dropped.
func (m *Monitor) SendEpochs(callId, userId int64, epochs []string) {
	if !tgcallsjson.ValidE2EEpochs(epochs, tgcallsjson.E2EStatusMaxEpochs) {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.call(callId, now).peer(userId)
	p.sendEpochs, p.sendAt = slices.Clone(epochs), now
}

// Report records a status report of userId's client. The SFU checked it
// against the schema; a report over its limits did not come from there and
// is refused.
func (m *Monitor) Report(callId, userId int64, r *pb.E2EStatus) {
	if r == nil {
		return
	}
	report := reportOf(r)
	if report.Check() != nil {
		metricReports.WithLabelValues("refused").Inc()
		return
	}
	now := m.now()
	metricReports.WithLabelValues("accepted").Inc()
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.call(callId, now).peer(userId)
	if p.report != nil && report.Seq > p.report.Seq+1 {
		lost := report.Seq - p.report.Seq - 1
		p.reportsLost += lost
		metricReports.WithLabelValues("lost").Add(float64(lost))
	}
	p.report, p.reportAt = report, now
}

// reportOf is a report of the control API in the form of the diagnostics.
func reportOf(r *pb.E2EStatus) *tgcallsjson.E2EStatus {
	out := &tgcallsjson.E2EStatus{
		Seq: r.GetSeq(), Height: r.GetHeight(), LastBlock: r.GetLastBlock(), Offset0: r.GetOffset0(), Offset1: r.GetOffset1(),
		Error: r.GetError(), SendEpochs: r.GetSendEpochs(), DecryptEpochs: r.GetDecryptEpochs(), UnknownEpochs: r.GetUnknownEpochs(),
		Encrypted: r.GetEncrypted(), EncryptFailed: r.GetEncryptFailed(), Senders: []tgcallsjson.E2ESender{},
	}
	for _, s := range r.GetSenders() {
		out.Senders = append(out.Senders, tgcallsjson.E2ESender{
			User: s.GetUser(), OK: s.GetOk(), UnknownEpoch: s.GetUnknownEpoch(), TooOld: s.GetTooOld(),
			Duplicate: s.GetDuplicate(), Failed: s.GetFailed(), LastOkMs: s.GetLastOkMs(),
		})
	}
	return out
}

// Tick reads the chain and the participants of the watched calls, sets the
// alarms and the metrics and, every flushEvery, writes the diagnostics.
// bff runs it every TickEvery.
func (m *Monitor) Tick(ctx context.Context) {
	now := m.now()
	m.mu.Lock()
	ids := make([]int64, 0, len(m.calls))
	for id, c := range m.calls {
		if now.Sub(c.active) > idleCall {
			m.forget(id, c)
			continue
		}
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.refresh(ctx, id, now)
	}
	m.mu.Lock()
	for _, id := range ids {
		if c := m.calls[id]; c != nil {
			c.evaluate(id, now)
		}
	}
	flush := now.Sub(m.lastFlush) >= flushEvery
	if flush {
		m.lastFlush = now
	}
	type pending struct {
		call, user int64
		d          *groupcalls.E2EDiag
	}
	var writes []pending
	if flush {
		for _, id := range ids {
			c := m.calls[id]
			if c == nil {
				continue
			}
			for uid, p := range c.peers {
				if p.dirty {
					p.dirty = false
					writes = append(writes, pending{id, uid, p.diag(now)})
				}
			}
		}
	}
	m.mu.Unlock()
	for _, w := range writes {
		if err := m.store.SaveDiag(ctx, w.call, w.user, now, w.d); err != nil {
			logx.WithContext(ctx).Errorf("groupcalls: diagnostics of %d in call %d: %v", w.user, w.call, err)
		}
	}
}

// forget drops a call from the watch and its series; m.mu is held.
func (m *Monitor) forget(id int64, c *callState) {
	delete(m.calls, id)
	if c.labeled {
		label := strconv.FormatInt(id, 10)
		metricHeight.DeleteLabelValues(label)
		for _, a := range alarms {
			metricParticipants.DeleteLabelValues(label, a)
		}
	}
}

// refresh reads what a call's alarms need from the database: the chain
// when its height moved, who is in the call, and the verification of the
// height once it is old enough to be stuck.
func (m *Monitor) refresh(ctx context.Context, id int64, now time.Time) {
	head, err := m.store.Head(ctx, id)
	if err != nil && !errors.Is(err, groupcalls.ErrNotFound) {
		logx.WithContext(ctx).Errorf("groupcalls: the chain of call %d: %v", id, err)
		return
	}
	if err != nil || head.Discarded {
		m.mu.Lock()
		if c := m.calls[id]; c != nil {
			m.forget(id, c)
		}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	c := m.calls[id]
	stale := c != nil && (c.height != head.Height || c.hash != head.Hash)
	m.mu.Unlock()
	if c == nil {
		return
	}
	if stale && head.Height >= 0 {
		ch, err := m.store.Chain(ctx, id)
		if err != nil {
			logx.WithContext(ctx).Errorf("groupcalls: the chain of call %d: %v", id, err)
			return
		}
		var members []int64
		for _, p := range ch.GroupState().Participants {
			members = append(members, p.UserID)
		}
		m.mu.Lock()
		c.setChain(ch.Height(), ch.LastHash(), members, now)
		m.mu.Unlock()
	}
	users, err := m.store.Users(ctx, id)
	if err != nil {
		logx.WithContext(ctx).Errorf("groupcalls: who is in call %d: %v", id, err)
		return
	}
	m.mu.Lock()
	c.inCall = make(map[int64]bool, len(users))
	for _, u := range users {
		c.inCall[u] = true
	}
	check := c.height >= 0 && !c.verified && now.Sub(c.heightAt) >= verificationFor
	m.mu.Unlock()
	if !check {
		return
	}
	ch, v, err := m.store.Verification(ctx, id)
	if err != nil {
		logx.WithContext(ctx).Errorf("groupcalls: the verification of call %d: %v", id, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch.Height() != c.height {
		return
	}
	c.verifiedHeight = ch.Height()
	c.verified = v.Phase() == tde2e.PhaseEnd
	c.committed, c.revealed = map[int64]bool{}, map[int64]bool{}
	for id := range c.members {
		c.committed[id] = v.Committed(id)
		c.revealed[id] = v.Revealed(id)
	}
}

// evaluate sets the alarms of every participant and the metrics of the
// call; m.mu is held.
func (c *callState) evaluate(id int64, now time.Time) {
	current := prefix(c.hash)
	counts := map[string]int{}
	for uid := range c.members {
		if c.peers[uid] == nil {
			c.peer(uid)
		}
	}
	// Who decrypted whom, by the fresh reports.
	decrypted := map[int64]int64{}
	named := map[int64]bool{}
	// A member of the chain who left can be removed only by another one
	// in the call.
	writer := false
	for uid := range c.members {
		writer = writer || c.inCall[uid]
	}
	for rid, r := range c.peers {
		if r.report == nil || now.Sub(r.reportAt) > fresh {
			continue
		}
		for _, s := range r.report.Senders {
			if s.User == rid {
				continue
			}
			named[s.User] = true
			decrypted[s.User] += s.OK
			if s.UnknownEpoch > 0 && s.OK == 0 {
				counts[AlarmUnknownEpoch]++
			}
		}
	}
	for uid, p := range c.peers {
		var on []string
		member, in := c.members[uid], c.inCall[uid]
		// Polls of sub-chain 0: from the next height the client expects.
		if member && in && c.height >= 0 {
			behind := p.poll[0].At == 0 || p.poll[0].Offset < c.height+1
			switch {
			case !behind:
				p.behindSince = time.Time{}
			case p.behindSince.IsZero():
				p.behindSince = now
			}
			if !p.behindSince.IsZero() && now.Sub(p.behindSince) >= pollBehindFor {
				on = append(on, AlarmPollBehind)
			}
		} else {
			p.behindSince = time.Time{}
		}
		if member && in && c.height >= 0 && now.Sub(p.sendAt) <= fresh && now.Sub(c.heightAt) >= staleEpochFor && !slices.Contains(p.sendEpochs, current) {
			on = append(on, AlarmStaleEpoch)
		}
		if member && c.verifiedHeight == c.height && !c.verified && now.Sub(c.heightAt) >= verificationFor && (!c.committed[uid] || !c.revealed[uid]) {
			on = append(on, AlarmVerification)
		}
		if p.report != nil && now.Sub(p.reportAt) <= fresh && p.report.Error != 0 {
			on = append(on, AlarmTdE2EError)
		}
		switch {
		case !member || in || !writer:
			p.outSince = time.Time{}
		case p.outSince.IsZero():
			p.outSince = now
		}
		if !p.outSince.IsZero() && now.Sub(p.outSince) >= notInCallFor {
			on = append(on, AlarmNotInCall)
		}
		p.rejoins = slices.DeleteFunc(p.rejoins, func(t time.Time) bool { return now.Sub(t) > rejoinLoopWindow })
		if len(p.rejoins) > rejoinLoopJoins {
			on = append(on, AlarmRejoinLoop)
		}
		switch {
		case !named[uid] || decrypted[uid] > 0:
			p.undecryptedSince = time.Time{}
		case p.undecryptedSince.IsZero():
			p.undecryptedSince = now
		}
		if !p.undecryptedSince.IsZero() && now.Sub(p.undecryptedSince) >= undecryptedFor {
			on = append(on, AlarmUndecrypted)
		}
		if !slices.Equal(on, p.alarms) {
			p.alarms, p.dirty = on, true
		}
		for _, a := range on {
			if a != AlarmUnknownEpoch {
				counts[a]++
			}
		}
		if !member && !in && p.poll[0].At == 0 && p.report == nil {
			// Gone and quiet: nothing to watch.
			delete(c.peers, uid)
		}
	}
	label := strconv.FormatInt(id, 10)
	c.labeled = true
	metricHeight.WithLabelValues(label).Set(float64(c.height))
	for _, a := range alarms {
		metricParticipants.WithLabelValues(label, a).Set(float64(counts[a]))
	}
}

// diag is the participant's diagnostics to store.
func (p *peerState) diag(now time.Time) *groupcalls.E2EDiag {
	d := &groupcalls.E2EDiag{
		Poll: p.poll, SendEpochs: p.sendEpochs, Report: p.report, ReportsLost: p.reportsLost,
		Rejoins: len(p.rejoins), Alarms: p.alarms,
	}
	if !p.sendAt.IsZero() {
		d.SendAt = p.sendAt.UnixMilli()
	}
	if !p.reportAt.IsZero() {
		d.ReportAt = p.reportAt.UnixMilli()
	}
	return d
}
