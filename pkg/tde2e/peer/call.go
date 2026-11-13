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

// Package peer is the client side of tde2e's group call encryption, for
// the test client of group calls (pkg/groupcallpeer, ADR 0060 §6): the
// join blocks, the shared key, the frames and the emoji verification as
// tdlib's Call.cpp makes them at commit e894536b. The server never uses it:
// it holds no key.
//
// File references without a directory name files under tde2e/td/e2e/ of
// tdlib at that commit.
package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

const (
	// ForgetEpochDelay is how long the key of an epoch stays after the
	// next block (FORGET_EPOCH_DELAY, Call.h:98).
	ForgetEpochDelay = 10 * time.Second
	// MaxActiveEpochs bounds the epochs a frame names (Call.h:99).
	MaxActiveEpochs = 15
	// seenWindow is the replay window of a sender's channel
	// (CallEncryption::mark_as_seen, Call.cpp:490-497).
	seenWindow = 1024
	// maxChannelID bounds the channel of a frame (validate_channel_id,
	// Call.cpp:514-519).
	maxChannelID = 1023

	// The magics of e2e.callPacket and e2e.callPacketLargeMsgId
	// (td/generate/scheme/e2e_api.tl:83-84), the TL ids of the two lines,
	// which a frame's encryption and signature start with (Call.cpp:402-431).
	magicCallPacket           uint32 = 0x40a6bee9
	magicCallPacketLargeMsgID uint32 = 0x1ce56c2d

	// participantFlags are the flags every call client gives a participant
	// and the external permissions of a call (e2e_api.cpp:458-461,
	// to_group_state).
	participantFlags = tde2e.PermAddUsers | tde2e.PermRemoveUsers
)

// Errors of a frame that tde2e has no code for (td::Status::Error), which
// the status report counts apart (ADR 0060 §5).
var (
	ErrTooOld    = errors.New("tde2e: message is too old")
	ErrDuplicate = errors.New("tde2e: message is already processed")
	errOwnPacket = errors.New("tde2e: packet is encrypted by us")
)

// keysInUse are the public keys of the calls of this process: Call::create
// refuses a key a call already had (Call.cpp:608-617).
var keysInUse sync.Map

func codeError(code tde2e.Code, msg string) error {
	return &tde2e.Error{Kind: tde2e.KindInvalid, Code: code, Msg: msg}
}

// ErrorCode is the tde2e code of err, CodeUnknown for an error without one
// (utils.h:29-35), 0 for nil.
func ErrorCode(err error) tde2e.Code {
	if err == nil {
		return 0
	}
	var e *tde2e.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return tde2e.CodeUnknown
}

// GenerateKey makes the key of one join: a call client makes a new one
// for every call it creates (TemporaryKeyPair, tdesktop
// tde2e/tde2e_api.cpp:122-135).
func GenerateKey() (ed25519.PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	return key, err
}

// Public is the public key of key as the chain holds it.
func Public(key ed25519.PrivateKey) [32]byte {
	return [32]byte(key.Public().(ed25519.PublicKey))
}

// Self is the entry of a participant as a call client writes it
// (call_create_self_add_block, e2e_api.cpp:457-462): version 0.
func Self(userID int64, key ed25519.PrivateKey) tde2e.Participant {
	return tde2e.Participant{UserID: userID, PublicKey: Public(key), Flags: participantFlags}
}

// newSharedKey is Call::make_changes_for_new_state (Call.cpp:631-656): a
// random 32-byte key, encrypted with a one-time secret, which each
// participant gets in a header encrypted with the shared secret of his key
// and a fresh ephemeral key.
func newSharedKey(g *tde2e.GroupState) (*tde2e.SharedKey, error) {
	ephemeral, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	var groupKey, oneTime [32]byte
	if _, err := rand.Read(groupKey[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(oneTime[:]); err != nil {
		return nil, err
	}
	encrypted, _ := EncryptData(groupKey[:], oneTime[:], nil)
	k := &tde2e.SharedKey{EK: Public(ephemeral), EncryptedSharedKey: encrypted}
	for _, p := range g.Participants {
		secret, err := SharedSecret(ephemeral, p.PublicKey)
		if err != nil {
			return nil, err
		}
		header, err := EncryptHeader(oneTime[:], encrypted, secret)
		if err != nil {
			return nil, err
		}
		k.DestUserIDs = append(k.DestUserIDs, p.UserID)
		k.DestHeaders = append(k.DestHeaders, header)
	}
	return k, nil
}

// buildBlock is Blockchain::build_block (Blockchain.cpp:607-657) of the
// changes of a call: the group state g and a new shared key for it, on top
// of c, signed by key. It checks the block by applying it to c, which
// therefore must be a copy the caller drops.
func buildBlock(c *tde2e.Chain, key ed25519.PrivateKey, g *tde2e.GroupState) ([]byte, error) {
	g.ExternalPermissions = participantFlags
	shared, err := newSharedKey(g)
	if err != nil {
		return nil, err
	}
	b := &tde2e.Block{
		PrevBlockHash: c.LastHash(),
		Height:        c.Height() + 1,
		Changes: []tde2e.Change{
			{Kind: tde2e.ChangeSetGroupState, GroupState: g},
			{Kind: tde2e.ChangeSetSharedKey, SharedKey: shared},
		},
		// Both changed, so the proof holds only the key-value hash; calls
		// never write values.
		StateProof: tde2e.StateProof{KVHash: tde2e.EmptyKVHash},
	}
	b.Sign(key)
	if _, err := c.ApplyBlock(b); err != nil {
		return nil, err
	}
	return b.Marshal(), nil
}

// ZeroBlock is the first block of a call's chain, with the writer alone
// (Call::create_zero_block, Call.cpp:589-593), in the local form.
func ZeroBlock(userID int64, key ed25519.PrivateKey) ([]byte, error) {
	return buildBlock(tde2e.NewChain(), key, &tde2e.GroupState{Participants: []tde2e.Participant{Self(userID, key)}})
}

// restore starts a chain from a block in the server form.
func restore(serverBlock []byte) (*tde2e.Chain, []byte, error) {
	local, err := tde2e.ToLocal(serverBlock)
	if err != nil {
		return nil, nil, err
	}
	c, err := tde2e.RestoreChain(local)
	if err != nil {
		return nil, nil, err
	}
	return c, local, nil
}

// JoinBlock is the block that adds the writer to the call whose last block
// is lastServerBlock, in the server form: the group state without his
// earlier entry and with him under key (Call::create_self_add_block,
// Call.cpp:595-606), in the local form.
func JoinBlock(userID int64, key ed25519.PrivateKey, lastServerBlock []byte) ([]byte, error) {
	c, _, err := restore(lastServerBlock)
	if err != nil {
		return nil, err
	}
	g := c.GroupState()
	g.Participants = slices.DeleteFunc(g.Participants, func(p tde2e.Participant) bool { return p.UserID == userID })
	g.Participants = append(g.Participants, Self(userID, key))
	return buildBlock(c, key, g)
}

type epoch struct {
	hash   [32]byte
	secret []byte
	group  *tde2e.GroupState
}

type forget struct {
	at     time.Time
	height int32
}

type seenKey struct {
	publicKey [32]byte
	channel   int32
}

// Call is tde2e's Call (Call.cpp:579-734) of one participant: the chain
// as the client follows it, the keys of the active epochs, the frames and
// the emoji verification. Any error of applying a block stays: after it
// nothing encrypts, decrypts or verifies (Call::get_status, Call.h:177-182).
// A Call is safe for concurrent use.
type Call struct {
	mu     sync.Mutex
	userID int64
	key    ed25519.PrivateKey
	pub    [32]byte
	chain  *tde2e.Chain
	last   []byte
	status error

	epochs map[int32]*epoch
	byHash map[[32]byte]int32
	forget []forget
	seqno  map[int32]uint32
	seen   map[seenKey][]uint32

	ver verification

	brokenKey bool
	now       func() time.Time
}

// Option changes a Call for a test.
type Option func(*Call)

// WithClock makes the Call read the time from now.
func WithClock(now func() time.Time) Option {
	return func(c *Call) { c.now = now }
}

// WithBrokenKey makes the Call use, for every epoch, a key other than the
// one the block gave: it encrypts what nobody else opens and opens nothing
// of the others, as a client that opened its key wrong would. It is for
// the tests of the diagnostics of ADR 0060 §5 and nothing else.
func WithBrokenKey() Option {
	return func(c *Call) { c.brokenKey = true }
}

// New starts the call of userID with key from the last block of the chain
// in the server form, usually the block of his own join (Call::create,
// Call.cpp:608-623). A key can start one call of the process only.
func New(userID int64, key ed25519.PrivateKey, lastServerBlock []byte, opts ...Option) (*Call, error) {
	pub := Public(key)
	if _, used := keysInUse.LoadOrStore(pub, true); used {
		return nil, codeError(tde2e.CodeCallKeyAlreadyUsed, "")
	}
	chain, local, err := restore(lastServerBlock)
	if err != nil {
		return nil, err
	}
	c := &Call{
		userID: userID, key: key, pub: pub, chain: chain, last: local,
		epochs: map[int32]*epoch{}, byHash: map[[32]byte]int32{}, seqno: map[int32]uint32{}, seen: map[seenKey][]uint32{},
		now: time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	c.ver.userID, c.ver.key = userID, key
	if err := c.ver.onBlock(chain); err != nil {
		return nil, err
	}
	if err := c.updateSharedKey(); err != nil {
		return nil, err
	}
	return c, nil
}

// UserID is the participant's id.
func (c *Call) UserID() int64 { return c.userID }

// PublicKey is the participant's key in the chain.
func (c *Call) PublicKey() [32]byte { return c.pub }

func (c *Call) failed() error {
	if c.status != nil {
		return codeError(tde2e.CodeCallFailed, c.status.Error())
	}
	return nil
}

// Err is the error the call failed with, nil while it works.
func (c *Call) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Height is the height of the last applied block (Call::get_height).
func (c *Call) Height() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chain.Height()
}

// LastHash is the hash of the last applied block: the epoch it opened.
func (c *Call) LastHash() [32]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chain.LastHash()
}

// GroupState is the participants of the last applied block.
func (c *Call) GroupState() *tde2e.GroupState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chain.GroupState()
}

// ApplyBlock applies a block of sub-chain 0 in the server form
// (Call::apply_block, Call.cpp:668-686). A block that is not in the server
// form is refused and changes nothing; any other error stays.
func (c *Call) ApplyBlock(serverBlock []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return err
	}
	local, err := tde2e.ToLocal(serverBlock)
	if err != nil {
		return err
	}
	applied, err := c.chain.Apply(local)
	if err == nil {
		c.last = applied.Bytes
		if err = c.ver.onBlock(c.chain); err == nil {
			err = c.updateSharedKey()
		}
	}
	if err != nil {
		c.status = err
		return c.failed()
	}
	return nil
}

// ChangeBlock is a block on top of the call that sets the participants to
// those of the chain without ids (Call::build_change_state,
// Call.cpp:625-629), in the local form: how a client removes those who
// left.
func (c *Call) ChangeBlock(without ...int64) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return nil, err
	}
	copyOf, err := tde2e.RestoreChain(c.last)
	if err != nil {
		return nil, err
	}
	g := copyOf.GroupState()
	g.Participants = slices.DeleteFunc(g.Participants, func(p tde2e.Participant) bool { return slices.Contains(without, p.UserID) })
	return buildBlock(copyOf, c.key, g)
}

// sharedKey opens the key of the last block (Call::decrypt_shared_key,
// Call.cpp:688-705).
func (c *Call) sharedKey() ([]byte, error) {
	k := c.chain.SharedKey()
	for i, uid := range k.DestUserIDs {
		if uid != c.userID {
			continue
		}
		secret, err := SharedSecret(c.key, k.EK)
		if err != nil {
			return nil, err
		}
		oneTime, err := DecryptHeader(k.DestHeaders[i], k.EncryptedSharedKey, secret)
		if err != nil {
			return nil, err
		}
		key, _, err := DecryptData(k.EncryptedSharedKey, oneTime, nil)
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, errors.New("tde2e: invalid shared key (size != 32)")
		}
		return key, nil
	}
	return nil, errors.New("tde2e: could not find user_id in group_shared_key")
}

// groupVersion is GroupState::version (Blockchain.cpp:42-51).
func groupVersion(g *tde2e.GroupState) int32 {
	if len(g.Participants) == 0 {
		return 0
	}
	v := g.Participants[0].Version
	for _, p := range g.Participants {
		v = min(v, p.Version)
	}
	return max(0, min(v, 255))
}

// updateSharedKey is Call::update_group_shared_key (Call.cpp:707-734): the
// epoch before the last block is forgotten after ForgetEpochDelay, and the
// key of the last block opens a new epoch.
func (c *Call) updateSharedKey() error {
	height := c.chain.Height()
	c.sync()
	c.forget = append(c.forget, forget{at: c.now().Add(ForgetEpochDelay), height: height - 1})
	g := c.chain.GroupState()
	var self *tde2e.Participant
	for i := range g.Participants {
		if g.Participants[i].PublicKey == c.pub {
			self = &g.Participants[i]
		}
	}
	if self == nil {
		return codeError(tde2e.CodeNotParticipant, "")
	}
	if self.UserID != c.userID {
		return codeError(tde2e.CodeWrongUserID, "")
	}
	key, err := c.sharedKey()
	if err != nil {
		return err
	}
	hash := c.chain.LastHash()
	if groupVersion(g) >= 1 {
		key = hmacSHA512(key, hash[:])[:32]
	}
	if c.brokenKey {
		wrong := sha256.Sum256(key)
		key = wrong[:]
	}
	// CallEncryption::add_shared_key (Call.cpp:263-279).
	c.sync()
	c.byHash[hash] = height
	c.epochs[height] = &epoch{hash: hash, secret: key, group: g}
	return nil
}

// sync forgets the epochs whose time is up, and the oldest while there are
// more than MaxActiveEpochs (CallEncryption::sync, Call.cpp:499-512).
func (c *Call) sync() {
	now := c.now()
	for len(c.forget) > 0 && (!now.Before(c.forget[0].at) || len(c.epochs) > MaxActiveEpochs) {
		if e, ok := c.epochs[c.forget[0].height]; ok {
			delete(c.byHash, e.hash)
			delete(c.epochs, c.forget[0].height)
		}
		c.forget = c.forget[1:]
	}
}

// Epochs are the hashes of the active epochs, oldest first: those a frame
// is encrypted with now.
func (c *Call) Epochs() [][32]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sync()
	return c.activeHashes()
}

func (c *Call) activeHashes() [][32]byte {
	heights := make([]int32, 0, len(c.epochs))
	for h := range c.epochs {
		heights = append(heights, h)
	}
	slices.Sort(heights)
	out := make([][32]byte, 0, len(heights))
	for _, h := range heights {
		out = append(out, c.epochs[h].hash)
	}
	return out
}

func validChannel(channel int32) error {
	if channel < 0 || channel > maxChannelID {
		return codeError(tde2e.CodeInvalidCallChannelID, "")
	}
	return nil
}

// Encrypt encrypts a frame for channel, leaving its first prefix bytes
// open (CallEncryption::encrypt, Call.cpp:352-400): prefix ‖ header_a (the
// count of active epochs and their hashes) ‖ header_b (the one-time secret
// encrypted with the key of each epoch) ‖ the packet, signed ‖ the
// prefix's size as uint32.
func (c *Call) Encrypt(channel int32, data []byte, prefix int) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return nil, err
	}
	c.sync()
	if prefix < 0 || prefix > len(data) || prefix >= 1<<16 {
		return nil, errors.New("tde2e: unencrypted header length is too large")
	}
	if len(c.epochs) == 0 {
		return nil, codeError(tde2e.CodeEncryptUnknownEpoch, "")
	}
	hashes := c.activeHashes()
	headerA := binary.LittleEndian.AppendUint32(nil, uint32(len(hashes)))
	for _, h := range hashes {
		headerA = append(headerA, h[:]...)
	}
	var oneTime [32]byte
	if _, err := rand.Read(oneTime[:]); err != nil {
		return nil, err
	}
	packet, err := c.encryptPacket(channel, append(slices.Clone(headerA), data[:prefix]...), data[prefix:], oneTime[:])
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, prefix+len(headerA)+32*len(hashes)+len(packet)+4)
	out = append(out, data[:prefix]...)
	out = append(out, headerA...)
	for _, h := range hashes {
		header, err := EncryptHeader(oneTime[:], packet, c.epochs[c.byHash[h]].secret)
		if err != nil {
			return nil, err
		}
		out = append(out, header...)
	}
	out = append(out, packet...)
	return binary.LittleEndian.AppendUint32(out, uint32(prefix)), nil
}

func magic(m uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, m)
}

// encryptPacket is CallEncryption::encrypt_packet_with_secret
// (Call.cpp:408-432): the channel, the next seqno of the channel and the
// data, encrypted with the one-time secret over the open part, then the
// signature of the large msg_id.
func (c *Call) encryptPacket(channel int32, open, data, secret []byte) ([]byte, error) {
	if err := validChannel(channel); err != nil {
		return nil, err
	}
	if c.seqno[channel] == ^uint32(0) {
		return nil, errors.New("tde2e: seqno overflow")
	}
	c.seqno[channel]++
	payload := binary.LittleEndian.AppendUint32(nil, uint32(channel))
	payload = binary.LittleEndian.AppendUint32(payload, c.seqno[channel])
	payload = append(payload, data...)
	encrypted, largeMsgID := EncryptData(payload, secret, append(magic(magicCallPacket), open...))
	signature := ed25519.Sign(c.key, append(magic(magicCallPacketLargeMsgID), largeMsgID[:]...))
	return append(encrypted, signature...), nil
}

// Frame is what the open part of a frame says.
type Frame struct {
	// Prefix is the unencrypted prefix.
	Prefix []byte
	// Epochs are the hashes of the epochs the frame is encrypted with.
	Epochs  [][32]byte
	headerA []byte
	headers [][]byte
	packet  []byte
}

// ParseFrame reads the open part of an encrypted frame
// (CallEncryption::decrypt, Call.cpp:286-333).
func ParseFrame(frame []byte) (*Frame, error) {
	if len(frame) < 4 {
		return nil, errors.New("tde2e: packet too small")
	}
	n := len(frame) - 4
	prefix := int(binary.LittleEndian.Uint32(frame[n:]))
	if prefix > n || prefix >= 1<<16 {
		return nil, errors.New("tde2e: unencrypted prefix size is too large")
	}
	body := frame[prefix:n]
	if len(body) < 4 {
		return nil, errors.New("tde2e: no head")
	}
	head := binary.LittleEndian.Uint32(body)
	count := int(head & 0xff)
	if (head>>8)&0xff != 0 {
		return nil, errors.New("tde2e: unsupported protocol version")
	}
	if head>>16 != 0 {
		return nil, errors.New("tde2e: reserved part of head is not zero")
	}
	if count > MaxActiveEpochs {
		return nil, errors.New("tde2e: too many active epochs")
	}
	if len(body) < 4+64*count {
		return nil, errors.New("tde2e: frame too short for its epochs")
	}
	f := &Frame{Prefix: frame[:prefix], headerA: body[:4+32*count], packet: body[4+64*count:]}
	for i := 0; i < count; i++ {
		f.Epochs = append(f.Epochs, [32]byte(body[4+32*i:4+32*(i+1)]))
		at := 4 + 32*count + 32*i
		f.headers = append(f.headers, body[at:at+32])
	}
	return f, nil
}

// Decrypt opens a frame userID sent on channel (CallEncryption::decrypt,
// Call.cpp:286-350, and decrypt_packet_with_secret, :434-473) with the
// first epoch of the frame it has a key of: an unknown epoch is
// CodeDecryptUnknownEpoch, a seqno below the window ErrTooOld, one seen
// ErrDuplicate.
func (c *Call) Decrypt(userID int64, channel int32, frame []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return nil, err
	}
	c.sync()
	f, err := ParseFrame(frame)
	if err != nil {
		return nil, err
	}
	if userID == c.userID {
		return nil, errOwnPacket
	}
	for i, h := range f.Epochs {
		height, ok := c.byHash[h]
		if !ok {
			continue
		}
		e := c.epochs[height]
		secret, err := DecryptHeader(f.headers[i], f.packet, e.secret)
		if err != nil {
			return nil, err
		}
		return c.decryptPacket(userID, f, secret, e.group)
	}
	return nil, codeError(tde2e.CodeDecryptUnknownEpoch, "")
}

func (c *Call) decryptPacket(userID int64, f *Frame, secret []byte, g *tde2e.GroupState) ([]byte, error) {
	sender, ok := g.Participant(userID)
	if !ok {
		return nil, fmt.Errorf("tde2e: user %d is not in the epoch", userID)
	}
	if len(f.packet) < 64 {
		return nil, errors.New("tde2e: not enough encryption data")
	}
	encrypted, signature := f.packet[:len(f.packet)-64], f.packet[len(f.packet)-64:]
	open := append(append(magic(magicCallPacket), f.headerA...), f.Prefix...)
	payload, largeMsgID, err := DecryptData(encrypted, secret, open)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(sender.PublicKey[:], append(magic(magicCallPacketLargeMsgID), largeMsgID[:]...), signature) {
		return nil, codeError(tde2e.CodeInvalidSignature, "frame")
	}
	if len(payload) < 8 {
		return nil, errors.New("tde2e: packet too short")
	}
	channel := int32(binary.LittleEndian.Uint32(payload))
	if err := validChannel(channel); err != nil {
		return nil, err
	}
	seqno := binary.LittleEndian.Uint32(payload[4:])
	key := seenKey{publicKey: sender.PublicKey, channel: channel}
	if err := c.checkNotSeen(key, seqno); err != nil {
		return nil, err
	}
	c.markSeen(key, seqno)
	return append(slices.Clone(f.Prefix), payload[8:]...), nil
}

// checkNotSeen is CallEncryption::check_not_seen (Call.cpp:475-488).
func (c *Call) checkNotSeen(k seenKey, seqno uint32) error {
	s := c.seen[k]
	if len(s) == 0 {
		return nil
	}
	if seqno < s[0] {
		return ErrTooOld
	}
	if _, found := slices.BinarySearch(s, seqno); found {
		return ErrDuplicate
	}
	return nil
}

// markSeen is CallEncryption::mark_as_seen (Call.cpp:490-497).
func (c *Call) markSeen(k seenKey, seqno uint32) {
	s := c.seen[k]
	i := sort.Search(len(s), func(i int) bool { return s[i] >= seqno })
	s = slices.Insert(s, i, seqno)
	for len(s) > seenWindow || (len(s) > 0 && s[0]+seenWindow < seqno) {
		s = s[1:]
	}
	c.seen[k] = s
}

// PullBroadcasts returns the broadcasts of the emoji verification to send,
// in the local form (Call::pull_outbound_messages).
func (c *Call) PullBroadcasts() ([][]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return nil, err
	}
	out := c.ver.outbound
	c.ver.outbound = nil
	return out, nil
}

// ApplyBroadcast takes a broadcast of sub-chain 1 in the server form
// (Call::receive_inbound_message, Call.h:208-214). Its errors do not fail
// the call.
func (c *Call) ApplyBroadcast(serverBroadcast []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.failed(); err != nil {
		return err
	}
	local, err := tde2e.ToLocal(serverBroadcast)
	if err != nil {
		return err
	}
	return c.ver.receive(local)
}

// EmojiHash is the hash the emoji of the current height come from, once
// every participant revealed (Call.cpp:241-255).
func (c *Call) EmojiHash() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status != nil || c.ver.emoji == nil {
		return nil, false
	}
	return slices.Clone(c.ver.emoji), true
}
