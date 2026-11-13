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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/peer"
)

// A participant of a group call as a patched client is (ADR 0060 §5, §6):
// its connection to the SFU, the end-to-end state of its join, and what
// the client does on its own in the background. It polls both sub-chains
// every second from where it stands (iOS ConferenceCallE2EContext.swift:
// 234, 266-279) and applies what comes, sends the broadcasts of the emoji
// verification, reports to the SFU every 5 s, and joins again with a new
// key by the rule of self-healing, within its limits.

// maxChainJoinAttempts bounds the attempts of one join on a chain that
// keeps moving: one per member of the group state at most (ADR 0060 §5).
const maxChainJoinAttempts = 100

// callRoom is what the clients of one call know of each other: the user
// of every source, which tgcalls has from the participants
// (GroupInstanceCustomImpl.cpp:1395-1400).
type callRoom struct {
	mu      sync.Mutex
	sources map[uint32]int64
	members []*member
}

var rooms = struct {
	sync.Mutex
	m map[int64]*callRoom
}{m: map[int64]*callRoom{}}

func roomOf(callID int64) *callRoom {
	rooms.Lock()
	defer rooms.Unlock()
	r := rooms.m[callID]
	if r == nil {
		r = &callRoom{sources: map[uint32]int64{}}
		rooms.m[callID] = r
	}
	return r
}

// peerSources are the sources of a connection.
func peerSources(p *groupcallpeer.Peer) []uint32 {
	out := []uint32{p.AudioSSRC()}
	for _, l := range p.Layers() {
		out = append(out, l.SSRC, l.RTX)
	}
	return out
}

// add names the sources of a connection of userID to every member.
func (r *callRoom) add(p *groupcallpeer.Peer, userID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range peerSources(p) {
		r.sources[s] = userID
	}
	for _, m := range r.members {
		if e := m.e2e(); e != nil {
			for s, u := range r.sources {
				e.SetSource(s, u)
			}
		}
	}
}

// learn names every known source to e.
func (r *callRoom) learn(e *groupcallpeer.E2E) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for s, u := range r.sources {
		e.SetSource(s, u)
	}
}

type memberOptions struct {
	cfg  groupcallpeer.Config
	edit func(*mtproto.TLPhoneJoinGroupCall)
	// wrongKey makes the first join encrypt with a wrong key.
	wrongKey bool
	// noHeal turns the rule of self-healing off from the start.
	noHeal bool
}

type member struct {
	u    *user
	call *mtproto.InputGroupCall
	opts memberOptions
	room *callRoom

	mu      sync.Mutex
	p       *groupcallpeer.Peer
	e       *groupcallpeer.E2E
	key     ed25519.PrivateKey
	row     *mtproto.GroupCallParticipant
	version int32
	heal    bool
	rejoins groupcallpeer.Rejoins
	// healed are the reasons of the rejoins the rule made; failures what
	// went wrong in the background.
	healed   []string
	failures []string
	stop     context.CancelFunc
}

func (m *member) peer() *groupcallpeer.Peer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.p
}

func (m *member) e2e() *groupcallpeer.E2E {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.e
}

func (m *member) id() int64 { return m.u.self.GetId() }

func (m *member) name() string { return m.u.self.GetFirstName().GetValue() }

func (m *member) setHeal(on bool) {
	m.mu.Lock()
	m.heal = on
	m.mu.Unlock()
}

// heals are the rejoins the rule of self-healing made.
func (m *member) heals() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.healed...)
}

func (m *member) fail(format string, args ...any) {
	m.mu.Lock()
	m.failures = append(m.failures, fmt.Sprintf(format, args...))
	m.mu.Unlock()
}

// joinMember joins u to call as a patched client and starts what it does
// in the background until the test ends or the member leaves.
func joinMember(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, opts memberOptions) *member {
	t.Helper()
	m := &member{u: u, call: call, opts: opts, room: roomOf(call.GetId()), heal: !opts.noHeal}
	key, err := peer.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.join(ctx, key, opts.wrongKey); err != nil {
		t.Fatalf("%s: %v", m.name(), err)
	}
	m.room.mu.Lock()
	m.room.members = append(m.room.members, m)
	m.room.mu.Unlock()
	bctx, cancel := context.WithCancel(ctx)
	m.stop = cancel
	t.Cleanup(func() {
		cancel()
		if p := m.peer(); p != nil {
			p.Close()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, f := range m.failures {
			t.Logf("%s in the background: %s", m.name(), f)
		}
	})
	go m.run(bctx)
	return m
}

// lastServerBlock is the last block of sub-chain 0 in the server form, nil
// without blocks, as a client reads it before it joins.
func lastServerBlock(ctx context.Context, u *user, call *mtproto.InputGroupCall) ([]byte, error) {
	up, err := chainUpdate(ctx, u, call, 0, -1, 1)
	if err != nil {
		return nil, err
	}
	if b := up.GetBlocks(); len(b) > 0 {
		return b[len(b)-1], nil
	}
	return nil, nil
}

// chainUpdate reads a sub-chain of call as u.
func chainUpdate(ctx context.Context, u *user, call *mtproto.InputGroupCall, sub, offset, limit int32) (*mtproto.Update, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCallChainBlocks{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_getGroupCallChainBlocks),
		Call:        call, SubChainId: sub, Offset: offset, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	ups, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		return nil, fmt.Errorf("phone.getGroupCallChainBlocks returned %T", o)
	}
	for _, up := range ups.To_Updates().GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateGroupCallChainBlocks && up.GetSubChainId() == sub {
			return up, nil
		}
	}
	return nil, errors.New("phone.getGroupCallChainBlocks without its update")
}

// join joins with key: with the key of the current join it is the client
// restoring its transport, which keeps its end-to-end state (iOS
// PresentationGroupCall.swift:1909-1916, 2050-2077); with another key a
// new join. A new connection either way.
func (m *member) join(ctx context.Context, key ed25519.PrivateKey, wrongKey bool) error {
	m.mu.Lock()
	old, oldE, same := m.p, m.e, m.key != nil && m.key.Equal(key)
	m.mu.Unlock()
	if same {
		// The state goes up to the last block first: the join builds on it.
		m.sync(ctx)
	}
	p, err := groupcallpeer.New(m.opts.cfg)
	if err != nil {
		return err
	}
	params, err := p.JoinParams()
	if err != nil {
		p.Close()
		return err
	}
	req := joinRequest(m.u, m.call, params)
	req.VideoStopped = m.opts.cfg.VideoLayers == 0
	if m.opts.edit != nil {
		m.opts.edit(req)
	}
	pub := peer.Public(key)
	req.PublicKey = pub[:]
	// Another join may take the height first: the client reads the last
	// block again and rewrites its own (tdesktop joinFail,
	// calls_group_call.cpp:1863-1872). It gives up after 10 refusals in a
	// row only while the last block stays the same: a chain that moves is
	// the race of joins at once, as after an SFU restart (ADR 0060 §5).
	var (
		o    mtproto.TLObject
		prev []byte
	)
	for still, attempt := 0, 0; ; attempt++ {
		var last []byte
		if last, err = lastServerBlock(ctx, m.u, m.call); err != nil {
			p.Close()
			return fmt.Errorf("the last block: %w", err)
		}
		if attempt == 0 || !bytes.Equal(last, prev) {
			still = 0
		}
		prev = last
		if req.Block, err = groupcallpeer.E2EJoinBlock(m.id(), key, last); err != nil {
			p.Close()
			return err
		}
		o, err = m.u.c.Invoke(ctx, req)
		if !mtclient.IsRPCError(err, "CONF_WRITE_CHAIN_INVALID") || attempt == maxChainJoinAttempts-1 {
			break
		}
		if still++; still == 10 {
			break
		}
	}
	if err != nil {
		p.Close()
		return fmt.Errorf("phone.joinGroupCall: %w", err)
	}
	r, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		p.Close()
		return fmt.Errorf("phone.joinGroupCall returned %T", o)
	}
	ups := r.To_Updates()
	// Both sub-chains (iOS GroupCalls.swift:922-944): the block of the join
	// in the server form with the height after it, and no broadcast.
	subs := map[int32]*mtproto.Update{}
	var conn *mtproto.Update
	for _, x := range ups.GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateGroupCallChainBlocks:
			subs[x.GetSubChainId()] = x
		case mtproto.Predicate_updateGroupCallConnection:
			conn = x
		}
	}
	b0, b1 := subs[0], subs[1]
	if b0 == nil || len(b0.GetBlocks()) != 1 || !tde2e.IsFromServer(b0.GetBlocks()[0]) || b1 == nil || len(b1.GetBlocks()) != 0 {
		p.Close()
		return fmt.Errorf("the chain in the answer of the join: %v", ups.GetUpdates())
	}
	gc := groupCallIn(ups)
	me, v := rowIn(ups, m.call.GetId(), m.id())
	if conn == nil || conn.GetPresentation() || gc.GetId() != m.call.GetId() || me == nil {
		p.Close()
		return fmt.Errorf("phone.joinGroupCall answered %v", ups.GetUpdates())
	}
	// Never a broadcast stream, whose parts come unencrypted (ADR 0060 §4).
	if strings.Contains(conn.GetParams().GetData(), `"stream"`) {
		p.Close()
		return fmt.Errorf("the connection asks for a stream: %s", conn.GetParams().GetData())
	}
	// A join of one still in the call (a join again after the SFU lost
	// him) is not a new join: just_joined and the next version belong to
	// a first join only, which the tests check where it is one.
	if !me.GetSelf() || me.GetMin() || me.GetLeft() || uint32(me.GetSource()) != p.AudioSSRC() || v != gc.GetVersion() {
		p.Close()
		return fmt.Errorf("own row %v at %d, call version %d", me, v, gc.GetVersion())
	}
	e := oldE
	if same {
		// Blocks of others may have come between the state and the join:
		// the client applies everything up to its own block.
		m.sync(ctx)
		if next0, _ := e.Offsets(); next0 < b0.GetNextOffset() || e.Call().Err() != nil {
			p.Close()
			return fmt.Errorf("the state after the join again: at %d of %d: %v", next0, b0.GetNextOffset(), e.Call().Err())
		}
	} else {
		var opts []groupcallpeer.E2EOption
		if wrongKey {
			opts = append(opts, groupcallpeer.WithWrongKey())
		}
		if e, err = groupcallpeer.NewE2E(m.id(), key, b0.GetBlocks()[0], b1.GetNextOffset(), opts...); err != nil {
			p.Close()
			return fmt.Errorf("the call from the block of the join: %w", err)
		}
	}
	m.room.learn(e)
	p.SetE2E(e)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := p.Connect(cctx, []byte(conn.GetParams().GetData())); err != nil {
		p.Close()
		return fmt.Errorf("connect to the SFU: %w", err)
	}
	p.StartReports(ctx)
	m.mu.Lock()
	m.p, m.e, m.key, m.row, m.version = p, e, key, me, v
	m.mu.Unlock()
	m.room.add(p, m.id())
	if old != nil {
		old.Close()
	}
	return nil
}

// sync polls both sub-chains once, applies what came and sends the
// broadcasts of the emoji verification. Its errors are dropped, as the
// clients drop them: Android and iOS ignore the errors of a poll
// (ConferenceCall.java:578-590; GroupCalls.swift:3418-3422), a failure of
// the chain stays in the call for the rule, and GROUPCALL_FORBIDDEN on a
// write is the rule's condition 3.
func (m *member) sync(ctx context.Context) {
	e := m.e2e()
	next0, next1 := e.Offsets()
	if up, err := chainUpdate(ctx, m.u, m.call, 0, next0, 100); err == nil {
		_ = e.Apply(0, up.GetBlocks(), up.GetNextOffset())
	}
	if up, err := chainUpdate(ctx, m.u, m.call, 1, next1, 100); err == nil {
		_ = e.Apply(1, up.GetBlocks(), up.GetNextOffset())
	}
	out, err := e.Broadcasts()
	if err != nil {
		return
	}
	for _, b := range out {
		_, err := m.u.c.Invoke(ctx, &mtproto.TLPhoneSendConferenceCallBroadcast{
			Constructor: m.u.c.Constructor(mtproto.Predicate_phone_sendConferenceCallBroadcast), Call: m.call, Block: b,
		})
		if mtclient.IsRPCError(err, "GROUPCALL_FORBIDDEN") {
			e.Forbidden()
		}
	}
}

// run is the client's background until ctx ends or its connection closes
// without a rejoin.
func (m *member) run(ctx context.Context) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		p := m.peer()
		select {
		case <-p.Done():
			return
		default:
		}
		m.sync(ctx)
		m.mu.Lock()
		heal := m.heal
		m.mu.Unlock()
		if !heal {
			continue
		}
		why, ok := m.e2e().NeedsRejoin()
		if !ok {
			continue
		}
		delay, ok := m.rejoins.Next(time.Now())
		if !ok {
			m.fail("the limit of rejoins is spent (%s)", why)
			m.setHeal(false)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		key, err := peer.GenerateKey()
		if err != nil {
			m.fail("a key: %v", err)
			continue
		}
		if err := m.join(ctx, key, false); err != nil {
			m.fail("a rejoin (%s): %v", why, err)
			continue
		}
		m.mu.Lock()
		m.healed = append(m.healed, why)
		m.mu.Unlock()
	}
}

// reconnect is a client back from a network loss: a new connection, the
// same key and end-to-end state.
func (m *member) reconnect(ctx context.Context, t *testing.T) {
	t.Helper()
	m.mu.Lock()
	key := m.key
	m.mu.Unlock()
	if err := m.join(ctx, key, false); err != nil {
		t.Fatalf("%s joins again with the same key: %v", m.name(), err)
	}
}

// leave leaves the call and ends the member.
func (m *member) leave(ctx context.Context, t *testing.T) *mtproto.Updates {
	t.Helper()
	p := m.peer()
	ups := leaveGroupCall(ctx, t, m.u, m.call, p.AudioSSRC())
	m.stop()
	p.Close()
	return ups
}

// waitHealed waits for the rule of self-healing to have rejoined n times.
func (m *member) waitHealed(ctx context.Context, t *testing.T, n int, within time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if h := m.heals(); len(h) >= n {
			return h
		}
		if time.Now().After(deadline) {
			m.mu.Lock()
			defer m.mu.Unlock()
			t.Fatalf("%s has rejoined %v, want %d; failures %v", m.name(), m.healed, n, m.failures)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// hearsMember checks that listener decrypts the sound of speaker's current
// connection.
func hearsMember(ctx context.Context, t *testing.T, listener, speaker *member) {
	t.Helper()
	hears(ctx, t, listener.name()+" hears "+speaker.name(), listener.peer(), speaker.peer().AudioSSRC())
}

// speakMember sends sound from whatever connection m has, also after a
// rejoin.
func speakMember(ctx context.Context, t *testing.T, m *member) {
	t.Helper()
	sctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() {
		tk := time.NewTicker(groupcallpeer.AudioInterval)
		defer tk.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-tk.C:
				_ = m.peer().SendAudio(30, true)
			}
		}
	}()
}
