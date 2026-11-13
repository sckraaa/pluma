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

// Package room keeps the calls of the SFU: who is connected to each, what
// they send, and what every receiver gets of it.
package room

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// Kind is the kind of a participant's connection.
type Kind int

const (
	// KindMain carries the participant's sound and camera.
	KindMain Kind = iota
	// KindPresentation carries a shared screen
	// (phone.joinGroupCallPresentation).
	KindPresentation
)

func (k Kind) String() string {
	if k == KindPresentation {
		return "presentation"
	}
	return "main"
}

// Errors of the manager.
var (
	ErrRoomNotFound   = errors.New("room: no such room")
	ErrBadParams      = errors.New("room: bad join params")
	ErrSSRCInUse      = errors.New("room: source used by another participant")
	ErrRoomFull       = errors.New("room: too many participants")
	ErrNotParticipant = errors.New("room: not in the room")
)

// Config is the behaviour of the rooms.
type Config struct {
	// Addresses are the public addresses clients reach the SFU at.
	Addresses []tgcallsjson.Address

	MaxParticipants int
	// MaxVideos is how many senders' videos are forwarded at once; the
	// earliest to join with video are.
	MaxVideos int
	// LastN is how many speakers are forwarded at once; AudioHold keeps a
	// speaker after it stops being among the loudest.
	LastN     int
	AudioHold time.Duration
	// PacketCache is the packets kept per video layer for retransmissions.
	PacketCache int
	// KeyframeInterval is the least time between key frame requests to one
	// layer of a sender.
	KeyframeInterval time.Duration

	InitialBitrate float64
	MinBitrate     float64
	MaxBitrate     float64
	// UplinkLimit caps what the node sends in all, bits per second; 0 is
	// no cap. Layers go down before it is reached (ADR 0059 §9).
	UplinkLimit float64

	Transport transport.EndpointConfig
}

func (c *Config) setDefaults() {
	if c.MaxParticipants <= 0 {
		c.MaxParticipants = 100
	}
	if c.MaxVideos <= 0 {
		c.MaxVideos = 30
	}
	if c.LastN <= 0 {
		c.LastN = 5
	}
	if c.AudioHold <= 0 {
		c.AudioHold = 1500 * time.Millisecond
	}
	if c.PacketCache <= 0 {
		c.PacketCache = 512
	}
	if c.KeyframeInterval <= 0 {
		c.KeyframeInterval = 500 * time.Millisecond
	}
	if c.InitialBitrate <= 0 {
		c.InitialBitrate = 2_500_000
	}
	if c.MinBitrate <= 0 {
		c.MinBitrate = 150_000
	}
	if c.MaxBitrate <= 0 {
		c.MaxBitrate = 30_000_000
	}
}

// Manager holds the rooms of one SFU node.
type Manager struct {
	cfg    Config
	mux    *transport.Mux
	cert   *transport.Certificate
	events Sink

	mu    sync.Mutex
	rooms map[int64]*Room
}

// NewManager makes a manager that connects clients through mux.
func NewManager(cfg Config, mux *transport.Mux, cert *transport.Certificate, events Sink) (*Manager, error) {
	cfg.setDefaults()
	if len(cfg.Addresses) == 0 {
		return nil, errors.New("room: no public addresses")
	}
	metrics.UplinkLimit.Set(cfg.UplinkLimit)
	return &Manager{cfg: cfg, mux: mux, cert: cert, events: events, rooms: map[int64]*Room{}}, nil
}

// CreateRoom opens a room for a call; an open room is kept. created
// reports whether it is new. Every room is end-to-end encrypted (ADR 0060
// §3): nothing here reads a frame.
func (m *Manager) CreateRoom(callID int64) (created bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[callID]; ok {
		return false, nil
	}
	r := newRoom(m, callID)
	m.rooms[callID] = r
	metrics.Rooms.Set(float64(len(m.rooms)))
	go r.run()
	return true, nil
}

// CloseRoom closes a room and every connection in it.
func (m *Manager) CloseRoom(callID int64) bool {
	m.mu.Lock()
	r, ok := m.rooms[callID]
	delete(m.rooms, callID)
	metrics.Rooms.Set(float64(len(m.rooms)))
	m.mu.Unlock()
	if ok {
		r.close()
	}
	return ok
}

// Close closes every room.
func (m *Manager) Close() {
	m.mu.Lock()
	rooms := m.rooms
	m.rooms = map[int64]*Room{}
	metrics.Rooms.Set(0)
	m.mu.Unlock()
	for _, r := range rooms {
		r.close()
	}
}

func (m *Manager) room(callID int64) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[callID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return r, nil
}

// JoinResult is the answer to a join.
type JoinResult struct {
	// Params is the JSON of updateGroupCallConnection.params.
	Params []byte
	// Endpoint is the connection's id in Colibri messages and in
	// groupCallParticipantVideo.endpoint.
	Endpoint string
	// Sources are the SSRCs the connection may send.
	Sources []uint32
}

// Join connects a participant's connection of the kind to a room with the
// client's join params. A participant already connected with the kind is
// replaced: clients join again with new params after a network change or
// an SFU restart (tdesktop calls_group_call.cpp:3746-3776).
func (m *Manager) Join(callID, participant int64, kind Kind, params []byte, muted bool, endpoint string) (*JoinResult, error) {
	req, err := tgcallsjson.ParseJoinRequest(params)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadParams, err)
	}
	r, err := m.room(callID)
	if err != nil {
		return nil, err
	}
	return r.join(participant, kind, req, muted, endpoint)
}

// Leave disconnects a participant's connection of the kind.
func (m *Manager) Leave(callID, participant int64, kind Kind) error {
	r, err := m.room(callID)
	if err != nil {
		return err
	}
	return r.leave(participant, kind)
}

// SetMuted mutes or unmutes a participant's sound on the server: muted
// sound is not forwarded at all.
func (m *Manager) SetMuted(callID, participant int64, muted bool) error {
	r, err := m.room(callID)
	if err != nil {
		return err
	}
	return r.setMuted(participant, muted)
}

// ParticipantSources are the sources of one connection.
type ParticipantSources struct {
	Participant int64
	Kind        Kind
	Connected   bool
	Sources     []uint32
	// Muted: SetMuted holds the main connection muted.
	Muted bool
}

// Sources lists the connections of a room and their sources, in
// participant order.
func (m *Manager) Sources(callID int64) ([]ParticipantSources, error) {
	r, err := m.room(callID)
	if err != nil {
		return nil, err
	}
	out := r.sources()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Participant != out[j].Participant {
			return out[i].Participant < out[j].Participant
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// Rooms lists the open rooms.
func (m *Manager) Rooms() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int64, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (m *Manager) publish(ev Event) {
	if m.events != nil {
		m.events.Publish(ev)
	}
}
