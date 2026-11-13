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

package room

import (
	"time"

	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// EventType tells what happened.
type EventType int

const (
	// EventConnected: a connection of a participant is up (ICE and DTLS).
	EventConnected EventType = iota + 1
	// EventDisconnected: a connection is gone without Leave: ICE failed,
	// consent was lost, DTLS failed, or the client never came.
	EventDisconnected
	// EventSpeaking: participants who spoke since the last event, with
	// their loudest level.
	EventSpeaking
	// EventVideoLayers: the largest height anyone watches of a sender's
	// video changed (what SenderVideoConstraints told it).
	EventVideoLayers
	// EventE2EStatus: a participant's client sent a status report of its
	// encryption over the data channel (ADR 0060 §5).
	EventE2EStatus
	// EventSendEpochs: the epochs a participant encrypts its sound with,
	// when they change and every sendEpochsEvery while it sends.
	EventSendEpochs
)

// Event is one notice for the signalling side.
type Event struct {
	Type        EventType
	CallID      int64
	Participant int64
	Kind        Kind
	Time        time.Time

	// EventConnected: the transport of the selected pair (udp4, udp6,
	// tcp4, tcp6), the SFU's ICE role and how long the connection took.
	Network     string
	Role        string
	ConnectTime time.Duration

	// EventDisconnected: the reason and the sound source of the
	// connection.
	Reason string
	Source uint32

	// EventSpeaking: participant → RFC 6464 level (0 loudest).
	Levels map[int64]uint8

	// EventVideoLayers: the height the sender is asked to send.
	IdealHeight int

	// EventE2EStatus: the report, checked against its schema.
	E2EStatus *tgcallsjson.E2EStatus

	// EventSendEpochs: prefixes of the hashes of the epochs.
	SendEpochs []string
}

// Sink takes events. Publish must not block: the media path calls it.
type Sink interface {
	Publish(ev Event)
}
