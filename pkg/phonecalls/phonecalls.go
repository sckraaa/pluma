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

// Package phonecalls is the state of one-to-one calls (task 068): one row
// per call, moved between states by conditional updates, so that of two
// devices that race the first one wins. The server only relays the key
// exchange (g_a_hash, g_b, g_a); the media go between the clients.
package phonecalls

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// State of a call.
type State int8

const (
	StateRequested State = 1 // waiting for the participant to answer
	StateAccepted  State = 2 // answered: waiting for phone.confirmCall
	StateActive    State = 3 // keys exchanged, the call is on
	StateDiscarded State = 4 // ended, whatever the reason
)

// Reason why a call was discarded.
type Reason int8

const (
	ReasonNone       Reason = 0
	ReasonMissed     Reason = 1
	ReasonDisconnect Reason = 2
	ReasonHangup     Reason = 3
	ReasonBusy       Reason = 4
	// ReasonMigrate: the call moved to a conference
	// (phoneCallDiscardReasonMigrateConferenceCall); Call.DiscardSlug is the
	// slug of its link (task 117).
	ReasonMigrate Reason = 5
)

// The timeouts of the clients (call_receive_timeout_ms, call_ring_timeout_ms
// and call_connect_timeout_ms of help.getConfig, teamgramd/bin/config.json;
// a test keeps them equal). The server waits Grace longer than the clients,
// so that it only ends a call its clients abandoned.
const (
	ReceiveTimeout = 20 * time.Second
	RingTimeout    = 90 * time.Second
	ConnectTimeout = 30 * time.Second
	Grace          = 10 * time.Second
	// MaxActive bounds a call both of whose devices were lost: nothing else
	// ends it. It is also how long the TURN credentials of a call live.
	MaxActive = 48 * time.Hour
	// ReofferAfter is how long a device that was offered a ringing call may
	// take to confirm it with phone.receivedCall, which the clients send as
	// soon as they start ringing.
	ReofferAfter = 5 * time.Second
	// maxSweepOffers bounds the offers of a call to a device that is listed
	// as online and does not confirm: it may not really be connected.
	maxSweepOffers = 3
	// MissedKeep is how long after a call ended a device that returns still
	// hears about it.
	MissedKeep = RingTimeout + Grace
)

// Protocol is what a client said about its call library (phoneCallProtocol).
type Protocol struct {
	UdpP2P          bool     `json:"p2p"`
	UdpReflector    bool     `json:"ref"`
	MinLayer        int32    `json:"min"`
	MaxLayer        int32    `json:"max"`
	LibraryVersions []string `json:"lib"`
}

// ProtocolOf converts p.
func ProtocolOf(p *mtproto.PhoneCallProtocol) Protocol {
	return Protocol{
		UdpP2P:          p.GetUdpP2P(),
		UdpReflector:    p.GetUdpReflector(),
		MinLayer:        p.GetMinLayer(),
		MaxLayer:        p.GetMaxLayer(),
		LibraryVersions: p.GetLibraryVersions(),
	}
}

// TL returns the protocol as the clients read it.
func (p Protocol) TL() *mtproto.PhoneCallProtocol {
	v := p.LibraryVersions
	if v == nil {
		v = []string{}
	}
	return mtproto.MakeTLPhoneCallProtocol(&mtproto.PhoneCallProtocol{
		UdpP2P:          p.UdpP2P,
		UdpReflector:    p.UdpReflector,
		MinLayer:        p.MinLayer,
		MaxLayer:        p.MaxLayer,
		LibraryVersions: v,
	}).To_PhoneCallProtocol()
}

// Negotiate returns the protocol of the call that clients a (the admin) and
// b (the participant) can both speak: their common layers and one library
// version. The clients start the library named first (Telegram-iOS
// CallSessionManager.swift:1226, Android VoIPService.java:3480, tdesktop
// calls_call.cpp:1045), so the answer names just that one: the greatest of
// the common versions compared as strings, the one Telegram-iOS and tdesktop
// list first (the sorted registry reversed: OngoingCallThreadLocalContext.mm:1651
// and CallSessionManager.swift:558, calls_call.cpp:195). The order of the
// caller's list cannot decide it: Android sends the registry unreversed,
// "10.0.0" first (tgcalls Instance.cpp, std::map), and drops the camera of
// its outgoing call on a version that sorts below "2.7.7"
// (VoIPService.java:3465). ok is false when they have none in common.
func Negotiate(a, b Protocol) (out Protocol, ok bool) {
	lo, hi := a.MinLayer, a.MaxLayer
	if b.MinLayer > lo {
		lo = b.MinLayer
	}
	if b.MaxLayer < hi {
		hi = b.MaxLayer
	}
	if lo > hi {
		return Protocol{}, false
	}
	version := ""
	for _, v := range a.LibraryVersions {
		if v > version && slices.Contains(b.LibraryVersions, v) {
			version = v
		}
	}
	if version == "" {
		return Protocol{}, false
	}
	return Protocol{
		UdpP2P:          a.UdpP2P && b.UdpP2P,
		UdpReflector:    a.UdpReflector && b.UdpReflector,
		MinLayer:        lo,
		MaxLayer:        hi,
		LibraryVersions: []string{version},
	}, true
}

// Call is a row of phone_calls.
type Call struct {
	Id                   int64
	AccessHash           int64
	AdminId              int64
	ParticipantId        int64
	AdminAuthKeyId       int64
	ParticipantAuthKeyId int64
	RandomId             int32
	Video                bool
	P2PAllowed           bool
	State                State
	GAHash               []byte
	GB                   []byte
	GA                   []byte
	KeyFingerprint       int64
	AdminProtocol        Protocol
	AcceptProtocol       Protocol
	ConfirmProtocol      Protocol
	Date                 int32
	ReceiveDate          int32
	StartDate            int32
	EndDate              int32
	Deadline             int32
	Reason               Reason
	// DiscardSlug is the slug of the conference of ReasonMigrate.
	DiscardSlug   string
	Duration      int32
	MessagePosted bool
}

// Role is who looks at a call.
type Role int

const (
	Admin Role = iota + 1
	Participant
)

// RoleOf returns the role of userId in the call, 0 for a stranger.
func (c *Call) RoleOf(userId int64) Role {
	switch userId {
	case c.AdminId:
		return Admin
	case c.ParticipantId:
		return Participant
	}
	return 0
}

// ReasonOf reads a discard reason a client sent; anything else is a plain
// hang-up, the move to a conference among them until the caller checked
// its slug (MigrateSlug).
func ReasonOf(r *mtproto.PhoneCallDiscardReason) Reason {
	switch r.GetPredicateName() {
	case mtproto.Predicate_phoneCallDiscardReasonMissed:
		return ReasonMissed
	case mtproto.Predicate_phoneCallDiscardReasonDisconnect:
		return ReasonDisconnect
	case mtproto.Predicate_phoneCallDiscardReasonBusy:
		return ReasonBusy
	}
	return ReasonHangup
}

// MigrateSlug is the slug of a phoneCallDiscardReasonMigrateConferenceCall,
// "" for any other reason.
func MigrateSlug(r *mtproto.PhoneCallDiscardReason) string {
	if r.GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonMigrateConferenceCall {
		return ""
	}
	return r.GetSlug()
}

// DiscardReason is the reason of the call as the clients read it: the
// move to a conference with its slug, which the other side joins by
// (Android VoIPService.java:1807-1819, iOS CallSessionManager.swift:
// 1160-1162, tdesktop calls_call.cpp:950-975, Web K callsController.ts:
// 716-738); nil for none.
func (c *Call) DiscardReason() *mtproto.PhoneCallDiscardReason {
	if c.Reason == ReasonMigrate {
		return mtproto.MakeTLPhoneCallDiscardReasonMigrateConferenceCall(&mtproto.PhoneCallDiscardReason{Slug: c.DiscardSlug}).To_PhoneCallDiscardReason()
	}
	return TLReason(c.Reason)
}

// TLReason returns the reason as the clients read it, nil for none (and
// for ReasonMigrate, whose slug is the call's: Call.DiscardReason).
func TLReason(r Reason) *mtproto.PhoneCallDiscardReason {
	switch r {
	case ReasonMissed:
		return mtproto.MakeTLPhoneCallDiscardReasonMissed(nil).To_PhoneCallDiscardReason()
	case ReasonDisconnect:
		return mtproto.MakeTLPhoneCallDiscardReasonDisconnect(nil).To_PhoneCallDiscardReason()
	case ReasonHangup:
		return mtproto.MakeTLPhoneCallDiscardReasonHangup(nil).To_PhoneCallDiscardReason()
	case ReasonBusy:
		return mtproto.MakeTLPhoneCallDiscardReasonBusy(nil).To_PhoneCallDiscardReason()
	}
	return nil
}

// Media is what an active call gives the call library of the clients
// besides the keys.
type Media struct {
	Connections []*mtproto.PhoneConnection
	// Parameters is the JSON of custom_parameters (CustomParameters), ""
	// for none.
	Parameters string
}

// TL returns the call as it is shown to role: the participant sees the
// admin's request and then waits, the admin waits and then sees the answer,
// each gets the key material of the other (core.telegram.org/api/end-to-end/
// voice-calls). m is the media of an active call; both sides get the same.
// conference_supported is always set: every client of Pluma takes a call
// to a conference, and the clients offer it only with the flag (Android
// VoIPFragment.java:2054, iOS CallSessionManager.swift:1204, tdesktop
// calls_call.cpp:1106; decision 4 of task 117).
func (c *Call) TL(role Role, m *Media) *mtproto.PhoneCall {
	p := &mtproto.PhoneCall{
		ConferenceSupported: true,
		Id:                  c.Id,
		Video:               c.Video,
		AccessHash:          c.AccessHash,
		Date:                c.Date,
		AdminId:             c.AdminId,
		ParticipantId:       c.ParticipantId,
	}
	switch c.State {
	case StateDiscarded:
		d := &mtproto.PhoneCall{Id: c.Id, Video: c.Video, Reason: c.DiscardReason()}
		if c.Duration > 0 {
			d.Duration = &wrapperspb.Int32Value{Value: c.Duration}
		}
		return mtproto.MakeTLPhoneCallDiscarded(d).To_PhoneCall()
	case StateActive:
		p.P2PAllowed = c.P2PAllowed
		p.KeyFingerprint = c.KeyFingerprint
		p.Protocol = c.ConfirmProtocol.TL()
		p.StartDate = c.StartDate
		if m != nil {
			p.Connections = m.Connections
			if m.Parameters != "" {
				p.CustomParameters = mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: m.Parameters}).To_DataJSON()
			}
		}
		if role == Admin {
			p.GAOrB = c.GB
		} else {
			p.GAOrB = c.GA
		}
		return mtproto.MakeTLPhoneCall(p).To_PhoneCall()
	case StateAccepted:
		p.Protocol = c.AcceptProtocol.TL()
		if role == Admin {
			p.GB = c.GB
			return mtproto.MakeTLPhoneCallAccepted(p).To_PhoneCall()
		}
		p.ReceiveDate = int32Value(c.ReceiveDate)
		return mtproto.MakeTLPhoneCallWaiting(p).To_PhoneCall()
	default: // StateRequested
		p.Protocol = c.AdminProtocol.TL()
		if role == Participant {
			p.GAHash = c.GAHash
			return mtproto.MakeTLPhoneCallRequested(p).To_PhoneCall()
		}
		p.ReceiveDate = int32Value(c.ReceiveDate)
		return mtproto.MakeTLPhoneCallWaiting(p).To_PhoneCall()
	}
}

func int32Value(v int32) *wrapperspb.Int32Value {
	if v == 0 {
		return nil
	}
	return &wrapperspb.Int32Value{Value: v}
}

// ViewOf returns who authKeyId is in the call and the call as that device
// must see it: a device of the participant that did not accept, however the
// call went on, sees it end without a reason (tdesktop calls_call.cpp:849,
// "ended by another device").
func (c *Call) ViewOf(authKeyId int64) (Role, *Call) {
	if authKeyId == c.AdminAuthKeyId {
		return Admin, c
	}
	if authKeyId == c.ParticipantAuthKeyId || c.ParticipantAuthKeyId == 0 {
		return Participant, c
	}
	lost := *c
	lost.State, lost.Reason, lost.Duration = StateDiscarded, ReasonNone, 0
	return Participant, &lost
}

// RequestedUpdate is the update that makes a device ring.
func (c *Call) RequestedUpdate() *mtproto.Update {
	return mtproto.MakeTLUpdatePhoneCall(&mtproto.Update{PhoneCall: c.TL(Participant, nil)}).To_Update()
}

func (p Protocol) marshal() (string, error) {
	b, err := json.Marshal(p)
	return string(b), err
}

func unmarshalProtocol(s string) (Protocol, error) {
	var p Protocol
	if s == "" {
		return p, nil
	}
	err := json.Unmarshal([]byte(s), &p)
	return p, err
}
