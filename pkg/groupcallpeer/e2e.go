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

package groupcallpeer

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/peer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// The end-to-end encryption of a participant (ADR 0060 §6): the chain of
// the call as the client follows it, its frames encrypted and decrypted as
// tgcalls does through tde2e, the status reports PlumaE2EStatus v1 and the
// rule of self-healing of ADR 0060 §5. The signalling (the RPCs of the
// chain) is the caller's: E2E takes what the server answers and says what
// to send.

const (
	// e2eChannel is the channel of every frame: tdesktop encrypts all
	// with channel 0 (tde2e/tde2e_api.cpp:78-106).
	e2eChannel = 0
	// ReportEvery is the period of the status reports; the first goes
	// one period after the join (ADR 0060 §5).
	ReportEvery = 5 * time.Second
	// BehindFor is how long sub-chain 0 may lag behind the server before
	// the client joins again: the 10 s an epoch outlives the next block
	// (Call.h:98) and a margin (condition 2).
	BehindFor = 12 * time.Second
	// UndecryptedFor is how long frames of every sender may fail before the
	// client joins again (condition 4), as Web K's watchdog of 15 s
	// (tweb src/lib/calls/groupCallInstance.ts:51, 1274-1280).
	UndecryptedFor = 15 * time.Second
	// vp8KeyPrefix and vp8DeltaPrefix are the bytes of a VP8 frame left
	// open (calculateVp8FramePlaintextHeaderSize,
	// GroupInstanceCustomImpl.cpp:1254-1280).
	vp8KeyPrefix   = 10
	vp8DeltaPrefix = 1
)

// Broken is a fault a participant may be put in, for the tests of the
// diagnostics (ADR 0060 §6).
type Broken uint8

const (
	// Healthy is a participant without fault.
	Healthy Broken = iota
	// StaleChain stops applying blocks of sub-chain 0: the participant
	// encrypts with its old epoch and polls from its old height while the
	// server goes on.
	StaleChain
	// NoCommit never sends the commit of the emoji verification.
	NoCommit
)

// ErrNotEncrypted: a frame the encryption refused is not sent, as
// tgcalls drops a frame its transform returns empty for
// (GroupInstanceCustomImpl.cpp:1456-1490).
var ErrNotEncrypted = errors.New("groupcallpeer: frame not encrypted")

type senderCount struct {
	ok, unknownEpoch, tooOld, duplicate, failed int64
}

type senderState struct {
	lastFrame time.Time
	lastOK    time.Time
}

// E2E is the end-to-end state of one join of a participant. A join with a
// new key is a new E2E; the transport may be replaced under the same one
// (a new Peer with SetE2E), as iOS restores its transport with the same
// key (PresentationGroupCall.swift:1909-1916). E2E is safe for concurrent
// use.
type E2E struct {
	mu      sync.Mutex
	call    *peer.Call
	userID  int64
	started time.Time
	now     func() time.Time
	broken  Broken

	// next0 and next1 are the next index of each sub-chain the client
	// expects: what it polls from. serverNext0 is the end of sub-chain 0
	// the server last named; behindSince when it went past next0 without
	// a block being applied.
	next0, next1 int32
	serverNext0  int32
	behindSince  time.Time
	forbidden    bool

	sources map[uint32]int64

	// The report since the last one sent.
	seq           int64
	encrypted     int64
	encryptFailed int64
	sendEpochs    []string
	decryptEpochs []string
	unknownEpochs []string
	senders       map[int64]*senderCount
	state         map[int64]*senderState
}

// E2EOption changes an E2E for a test.
type E2EOption func(*e2eOptions)

type e2eOptions struct {
	call []peer.Option
	now  func() time.Time
}

// WithWrongKey makes the participant encrypt and decrypt with a key other
// than the call's (peer.WithBrokenKey).
func WithWrongKey() E2EOption {
	return func(o *e2eOptions) { o.call = append(o.call, peer.WithBrokenKey()) }
}

// WithE2EClock makes the E2E read the time from now.
func WithE2EClock(now func() time.Time) E2EOption {
	return func(o *e2eOptions) {
		o.now = now
		o.call = append(o.call, peer.WithClock(now))
	}
}

// E2EJoinBlock is the block a client sends with phone.joinGroupCall:
// block 0 with itself alone when the call has no block, otherwise the
// self-add block on the last one, which the client reads first
// (getGroupCallChainBlocks with offset -1, tdesktop
// calls_group_call.cpp:1632-1650). last is in the server form.
func E2EJoinBlock(userID int64, key ed25519.PrivateKey, last []byte) ([]byte, error) {
	if last == nil {
		return peer.ZeroBlock(userID, key)
	}
	return peer.JoinBlock(userID, key, last)
}

// NewE2E starts the encryption of a join from the answer of
// phone.joinGroupCall: the block of the join in the server form, the end
// of sub-chain 1 (Call::create from the last block, tdesktop applies the
// join blocks first, calls_group_call.cpp:1797-1816).
func NewE2E(userID int64, key ed25519.PrivateKey, joinBlock []byte, next1 int32, opts ...E2EOption) (*E2E, error) {
	o := e2eOptions{now: time.Now}
	for _, f := range opts {
		f(&o)
	}
	call, err := peer.New(userID, key, joinBlock, o.call...)
	if err != nil {
		return nil, err
	}
	h := call.Height()
	return &E2E{
		call: call, userID: userID, started: o.now(), now: o.now,
		next0: h + 1, next1: next1, serverNext0: h + 1,
		sources: map[uint32]int64{}, senders: map[int64]*senderCount{}, state: map[int64]*senderState{},
	}, nil
}

// Call is the participant's tde2e call.
func (e *E2E) Call() *peer.Call { return e.call }

// Break puts the participant in a fault from now on.
func (e *E2E) Break(b Broken) {
	e.mu.Lock()
	e.broken = b
	e.mu.Unlock()
}

// Offsets are where the client polls each sub-chain from.
func (e *E2E) Offsets() (next0, next1 int32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.next0, e.next1
}

// Apply takes blocks of a sub-chain as an answer of
// getGroupCallChainBlocks or an updateGroupCallChainBlocks gives them: the
// first is at index next − len(blocks) (tdesktop tde2e_api.cpp:452-517);
// blocks already applied are skipped. An error of sub-chain 0 is the
// call's failure, which stays.
func (e *E2E) Apply(subChain int32, blocks [][]byte, next int32) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	first := next - int32(len(blocks))
	if subChain == 1 {
		for i, b := range blocks {
			if first+int32(i) < e.next1 {
				continue
			}
			// A broadcast that does not apply is dropped: tde2e does not
			// fail the call for it (Call.h:208-214).
			_ = e.call.ApplyBroadcast(b)
			e.next1 = first + int32(i) + 1
		}
		e.next1 = max(e.next1, next)
		return nil
	}
	e.serverNext0 = max(e.serverNext0, next)
	applied := false
	var err error
	if e.broken != StaleChain {
		for i, b := range blocks {
			if first+int32(i) < e.next0 {
				continue
			}
			if err = e.call.ApplyBlock(b); err != nil {
				break
			}
			e.next0, applied = first+int32(i)+1, true
		}
	}
	if applied || e.serverNext0 <= e.next0 {
		e.behindSince = time.Time{}
	}
	if e.serverNext0 > e.next0 && e.behindSince.IsZero() {
		e.behindSince = now
	}
	return err
}

// Broadcasts are the broadcasts of the emoji verification to send with
// phone.sendConferenceCallBroadcast, in the local form.
func (e *E2E) Broadcasts() ([][]byte, error) {
	out, err := e.call.PullBroadcasts()
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	broken := e.broken
	e.mu.Unlock()
	if broken == NoCommit {
		out = slices.DeleteFunc(out, func(b []byte) bool {
			x, err := tde2e.ParseBroadcast(b)
			return err == nil && x.Kind == tde2e.BroadcastCommit
		})
	}
	return out, nil
}

// RemoveBlock is the block of phone.deleteConferenceCallParticipants that
// takes ids out of the call (tdesktop data_group_call.cpp:73-96).
func (e *E2E) RemoveBlock(ids ...int64) ([]byte, error) {
	return e.call.ChangeBlock(ids...)
}

// Forbidden records GROUPCALL_FORBIDDEN on a write to the chain: the
// client joins again (condition 3).
func (e *E2E) Forbidden() {
	e.mu.Lock()
	e.forbidden = true
	e.mu.Unlock()
}

// SetSource names the participant a source of the call belongs to, as
// tgcalls learns it from the participants (the user id a frame is
// decrypted for, GroupInstanceCustomImpl.cpp:1395-1400).
func (e *E2E) SetSource(ssrc uint32, userID int64) {
	e.mu.Lock()
	e.sources[ssrc] = userID
	e.mu.Unlock()
}

func epochPrefix(h [32]byte) string {
	return hex.EncodeToString(h[:8])
}

func addPrefix(list []string, p string, limit int) []string {
	if len(list) >= limit || slices.Contains(list, p) {
		return list
	}
	return append(list, p)
}

// encrypt encrypts a frame of the participant, leaving prefix bytes open.
func (e *E2E) encrypt(frame []byte, prefix int) ([]byte, error) {
	out, err := e.call.Encrypt(e2eChannel, frame, prefix)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err != nil {
		e.encryptFailed++
		return nil, ErrNotEncrypted
	}
	e.encrypted++
	if f, err := peer.ParseFrame(out); err == nil {
		e.sendEpochs = e.sendEpochs[:0]
		for _, h := range f.Epochs {
			e.sendEpochs = append(e.sendEpochs, epochPrefix(h))
		}
	}
	return out, nil
}

// encryptAudio is the encryptor of an Opus frame: the frame, the flag 0x01
// and the level and speech byte, encrypted without an open prefix
// (GroupInstanceCustomImpl.cpp:1466-1490). The byte carries the speech
// flag and 127 less the RFC 6464 level of the packet.
func (e *E2E) encryptAudio(opus []byte, level uint8, voice bool) ([]byte, error) {
	trailer := (127 - min(level, 127)) & 0x7f
	if voice {
		trailer |= 0x80
	}
	return e.encrypt(append(slices.Clone(opus), 0x01, trailer), 0)
}

// encryptVideo encrypts a VP8 frame: 10 bytes of a key frame open, 1 of
// another.
func (e *E2E) encryptVideo(frame []byte, keyframe bool) ([]byte, error) {
	prefix := vp8DeltaPrefix
	if keyframe {
		prefix = vp8KeyPrefix
	}
	return e.encrypt(frame, min(prefix, len(frame)))
}

// vp8DescriptorSize is the length of the VP8 payload descriptor (RFC 7741
// §4.2) the packetizer puts before the frame.
func vp8DescriptorSize(p []byte) (int, bool) {
	if len(p) < 1 {
		return 0, false
	}
	n := 1
	if p[0]&0x80 == 0 {
		return n, true
	}
	if len(p) < 2 {
		return 0, false
	}
	ext := p[1]
	n++
	if ext&0x80 != 0 { // I: picture id, 7 or 15 bits
		if len(p) <= n {
			return 0, false
		}
		if p[n]&0x80 != 0 {
			n += 2
		} else {
			n++
		}
	}
	if ext&0x40 != 0 { // L: TL0PICIDX
		n++
	}
	if ext&0x30 != 0 { // T or K
		n++
	}
	return n, n <= len(p)
}

// open decrypts a received packet in place, as tgcalls' decryptor does
// before the decoder: a frame that does not decrypt is dropped, and the
// sender then seems silent (GroupInstanceCustomImpl.cpp:1427, 1492-1525).
func (e *E2E) open(pkt *rtp.Packet) bool {
	e.mu.Lock()
	sender, known := e.sources[pkt.SSRC]
	e.mu.Unlock()
	if !known {
		return false
	}
	switch pkt.PayloadType {
	case tgcallsjson.PayloadTypeOpus:
		plain, ok := e.decrypt(sender, pkt.Payload)
		if !ok || len(plain) < 1 {
			return false
		}
		// The flag says whether the level byte follows.
		if len(plain) >= 2 && plain[len(plain)-2]&0x01 != 0 {
			plain = plain[:len(plain)-2]
		} else {
			plain = plain[:len(plain)-1]
		}
		pkt.Payload = plain
		return true
	case tgcallsjson.PayloadTypeVP8:
		n, ok := vp8DescriptorSize(pkt.Payload)
		if !ok {
			return false
		}
		plain, ok := e.decrypt(sender, pkt.Payload[n:])
		if !ok {
			return false
		}
		pkt.Payload = append(slices.Clone(pkt.Payload[:n]), plain...)
		return true
	case tgcallsjson.PayloadTypeVP8RTX:
		if len(pkt.Payload) < 2 {
			return false
		}
		n, ok := vp8DescriptorSize(pkt.Payload[2:])
		if !ok {
			return false
		}
		plain, ok := e.decrypt(sender, pkt.Payload[2+n:])
		if !ok {
			return false
		}
		pkt.Payload = append(slices.Clone(pkt.Payload[:2+n]), plain...)
		return true
	}
	return false
}

// decrypt opens a frame of sender and counts the outcome for the report
// (ADR 0060 §5).
func (e *E2E) decrypt(sender int64, frame []byte) ([]byte, bool) {
	plain, err := e.call.Decrypt(sender, e2eChannel, frame)
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	c := e.senders[sender]
	if c == nil {
		c = &senderCount{}
		e.senders[sender] = c
	}
	st := e.state[sender]
	if st == nil {
		st = &senderState{}
		e.state[sender] = st
	}
	st.lastFrame = now
	f, perr := peer.ParseFrame(frame)
	switch {
	case err == nil:
		c.ok++
		st.lastOK = now
		if perr == nil {
			for _, h := range f.Epochs {
				e.decryptEpochs = addPrefix(e.decryptEpochs, epochPrefix(h), tgcallsjson.E2EStatusMaxEpochs)
			}
		}
		return plain, true
	case peer.ErrorCode(err) == tde2e.CodeDecryptUnknownEpoch:
		c.unknownEpoch++
		if perr == nil {
			for _, h := range f.Epochs {
				e.unknownEpochs = addPrefix(e.unknownEpochs, epochPrefix(h), tgcallsjson.E2EStatusMaxUnknownEpochs)
			}
		}
	case errors.Is(err, peer.ErrTooOld):
		c.tooOld++
	case errors.Is(err, peer.ErrDuplicate):
		c.duplicate++
	default:
		c.failed++
	}
	return nil, false
}

// Report is the next PlumaE2EStatus v1 of the participant (ADR 0060 §5).
// The counters and lists of the interval are taken only by Sent: a report
// that did not go out leaves them for the next.
func (e *E2E) Report() *tgcallsjson.E2EStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	s := &tgcallsjson.E2EStatus{
		Seq: e.seq + 1, Height: -1, Offset0: e.next0, Offset1: e.next1,
		SendEpochs: slices.Clone(e.sendEpochs), DecryptEpochs: slices.Clone(e.decryptEpochs), UnknownEpochs: slices.Clone(e.unknownEpochs),
		Encrypted: e.encrypted, EncryptFailed: e.encryptFailed,
	}
	if err := e.call.Err(); err != nil {
		s.Error = int32(peer.ErrorCode(err))
	} else {
		s.Height = e.call.Height()
		s.LastBlock = epochPrefix(e.call.LastHash())
	}
	users := make([]int64, 0, len(e.senders))
	for u := range e.senders {
		users = append(users, u)
	}
	slices.Sort(users)
	for _, u := range users {
		c := e.senders[u]
		last := int64(-1)
		if t := e.state[u].lastOK; !t.IsZero() {
			last = now.Sub(t).Milliseconds()
		}
		s.Senders = append(s.Senders, tgcallsjson.E2ESender{
			User: u, OK: c.ok, UnknownEpoch: c.unknownEpoch, TooOld: c.tooOld, Duplicate: c.duplicate, Failed: c.failed, LastOkMs: last,
		})
	}
	// The limits: the senders heard longest ago go first.
	for len(s.Senders) > tgcallsjson.E2EStatusMaxSenders || len(tgcallsjson.MarshalE2EStatus(s)) > tgcallsjson.E2EStatusMaxSize {
		worst := 0
		for i, x := range s.Senders {
			if x.LastOkMs == -1 || s.Senders[worst].LastOkMs != -1 && x.LastOkMs > s.Senders[worst].LastOkMs {
				worst = i
			}
		}
		s.Senders = slices.Delete(s.Senders, worst, worst+1)
	}
	return s
}

// Sent takes the interval of a report that went out: its counters and
// lists start again.
func (e *E2E) Sent(s *tgcallsjson.E2EStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq = s.Seq
	e.encrypted -= s.Encrypted
	e.encryptFailed -= s.EncryptFailed
	e.decryptEpochs = slices.DeleteFunc(e.decryptEpochs, func(p string) bool { return slices.Contains(s.DecryptEpochs, p) })
	e.unknownEpochs = slices.DeleteFunc(e.unknownEpochs, func(p string) bool { return slices.Contains(s.UnknownEpochs, p) })
	for _, x := range s.Senders {
		c := e.senders[x.User]
		if c == nil {
			continue
		}
		c.ok -= x.OK
		c.unknownEpoch -= x.UnknownEpoch
		c.tooOld -= x.TooOld
		c.duplicate -= x.Duplicate
		c.failed -= x.Failed
		if *c == (senderCount{}) {
			delete(e.senders, x.User)
		}
	}
}

// Rejoin reasons of the rule of ADR 0060 §5.
const (
	RejoinError       = "tde2e error"
	RejoinBehind      = "behind"
	RejoinForbidden   = "forbidden"
	RejoinUndecrypted = "nothing decrypts"
)

// NeedsRejoin applies the rule of self-healing of ADR 0060 §5: the client
// joins again with a new key when its call failed (1), when sub-chain 0
// lagged behind the server for BehindFor without a block applied (2),
// after GROUPCALL_FORBIDDEN on a write (3), or when for UndecryptedFor not
// one frame of any sender who sent in that time decrypted while its chain
// keeps up (4). One sender failing alone is his fault, not the receiver's.
func (e *E2E) NeedsRejoin() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	if e.call.Err() != nil {
		return RejoinError, true
	}
	behind := e.serverNext0 > e.next0
	if behind && !e.behindSince.IsZero() && now.Sub(e.behindSince) >= BehindFor {
		return RejoinBehind, true
	}
	if e.forbidden {
		return RejoinForbidden, true
	}
	if behind || now.Sub(e.started) < UndecryptedFor {
		return "", false
	}
	heard := 0
	for _, st := range e.state {
		if now.Sub(st.lastFrame) > UndecryptedFor {
			continue
		}
		if !st.lastOK.IsZero() && now.Sub(st.lastOK) < UndecryptedFor {
			return "", false
		}
		heard++
	}
	if heard > 0 {
		return RejoinUndecrypted, true
	}
	return "", false
}

// Rejoins limits the joins of the rule of self-healing (ADR 0060 §5): at
// most 3 in a rolling 5 minutes, after 0, 2 and 5 s plus up to 1 s at
// random, as tdesktop's kE2ERejoinsLimit, kE2ERejoinsPeriod,
// kE2ERejoinDelays and kE2ERejoinJitter (pluma-desktop ci/patch.py:
// 4479-4494, 4621-4641). The zero value is ready.
type Rejoins struct {
	mu    sync.Mutex
	times []time.Time
}

// Rejoin limits of ADR 0060 §5.
const (
	RejoinsLimit  = 3
	RejoinsPeriod = 5 * time.Minute
	rejoinJitter  = time.Second
)

var rejoinDelays = [RejoinsLimit]time.Duration{0, 2 * time.Second, 5 * time.Second}

// Next books a rejoin at now and returns how long to wait before it;
// false when the limit is spent and the client should leave the call
// with its message.
func (r *Rejoins) Next(now time.Time) (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.times = slices.DeleteFunc(r.times, func(t time.Time) bool { return now.Sub(t) >= RejoinsPeriod })
	if len(r.times) >= RejoinsLimit {
		return 0, false
	}
	var b [8]byte
	// crypto/rand.Read never fails.
	_, _ = rand.Read(b[:])
	d := rejoinDelays[len(r.times)] + time.Duration(binary.LittleEndian.Uint64(b[:])%uint64(rejoinJitter+1))
	r.times = append(r.times, now)
	return d, true
}
