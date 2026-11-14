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
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// Every room is end-to-end encrypted (ADR 0060 §3): the SFU forwards frames
// it cannot read, and drops sound that is not such a frame (audioFrame). What it may read is what the format leaves open, and that
// is what the diagnostics of the encryption use (§5): the hashes of the
// epochs in the head of a sound frame, and the status reports the clients
// send over the data channel. Neither tells a key.

const (
	// sendEpochsEvery: the epochs of a sender are published again this
	// often while it sends, so that the signalling side sees who sends
	// and with what.
	sendEpochsEvery = 5 * time.Second
	// e2eReportEvery is the least time between two reports of one
	// connection; the clients send one every 5 s.
	e2eReportEvery = time.Second
	// maxFrameEpochs is tde2e's limit of epochs in a frame
	// (MAX_ACTIVE_EPOCHS, Call.h:99).
	maxFrameEpochs = 15
	epochHashSize  = 32
	// epochPrefixSize is the part of an epoch's hash the reports name.
	epochPrefixSize = 8
)

var errNotFrame = errors.New("room: not a frame of tde2e")

// frameEpochs returns header_a of a sound frame of tde2e: an int32 head
// whose low byte counts the epochs and whose other bytes are 0, then the
// 32-byte hash of each epoch. A frame is prefix ‖ header_a ‖ header_b ‖
// packet ‖ the prefix's size in 4 bytes (CallEncryption::encrypt and
// decrypt, tde2e Call.cpp:285-399); sound has no unencrypted prefix
// (encryptGroupAudioFrame, tgcalls GroupFrameTransformer.h:79-86 at
// c1de0882), so header_a starts the payload.
func frameEpochs(payload []byte) ([]byte, error) {
	if len(payload) < 8 {
		return nil, errNotFrame
	}
	if binary.LittleEndian.Uint32(payload[len(payload)-4:]) != 0 {
		return nil, errNotFrame
	}
	head := binary.LittleEndian.Uint32(payload)
	n := int(head & 0xff)
	if head>>8 != 0 || n == 0 || n > maxFrameEpochs {
		return nil, errNotFrame
	}
	// header_a, a header_b of 32 bytes per epoch and the trailer at least.
	end := 4 + n*epochHashSize
	if len(payload) < end+n*epochHashSize+4 {
		return nil, errNotFrame
	}
	return payload[:end], nil
}

// epochPrefixes are the prefixes of the epochs of header_a, in hex.
func epochPrefixes(headerA []byte) []string {
	n := (len(headerA) - 4) / epochHashSize
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		at := 4 + i*epochHashSize
		out = append(out, hex.EncodeToString(headerA[at:at+epochPrefixSize]))
	}
	return out
}

// e2eState is what a member's connection tells of its encryption.
type e2eState struct {
	mu         sync.Mutex
	headerA    []byte
	published  time.Time
	lastReport time.Time
}

// audioFrame tells whether a sender's sound packet may go on: a frame of
// tde2e, or a packet with no payload (padding, which carries nothing). Any
// other payload is sound sent unencrypted or broken, and the SFU drops it:
// no group call carries sound that is not end-to-end encrypted (task 096).
// The epochs of a main connection's frames are published by observeEpochs.
func (r *Room) audioFrame(sender *Member, payload []byte, now time.Time) bool {
	if len(payload) == 0 {
		return true
	}
	headerA, err := frameEpochs(payload)
	if err != nil {
		metrics.E2EUnparsedAudio.Inc()
		return false
	}
	if sender.kind == KindMain {
		r.observeEpochs(sender, headerA, now)
	}
	return true
}

// observeEpochs publishes the epochs of a sender's frame (its header_a)
// when they differ from the last ones or sendEpochsEvery has passed.
func (r *Room) observeEpochs(sender *Member, headerA []byte, now time.Time) {
	st := &sender.e2e
	st.mu.Lock()
	if bytes.Equal(st.headerA, headerA) && now.Sub(st.published) < sendEpochsEvery {
		st.mu.Unlock()
		return
	}
	st.headerA = append(st.headerA[:0], headerA...)
	st.published = now
	st.mu.Unlock()
	r.mgr.publish(Event{
		Type: EventSendEpochs, CallID: r.id, Participant: sender.participant, Kind: sender.kind, Time: now,
		SendEpochs: epochPrefixes(headerA),
	})
}

// onE2EStatus takes a PlumaE2EStatus report of a member's data channel:
// one checked against the schema, from a main connection, at most one per
// e2eReportEvery, goes to the signalling side; any other is counted and
// dropped. The report is never logged.
func (m *Member) onE2EStatus(msg []byte, now time.Time) {
	if m.kind != KindMain {
		metrics.E2EReports.WithLabelValues("kind").Inc()
		return
	}
	st := &m.e2e
	st.mu.Lock()
	if now.Sub(st.lastReport) < e2eReportEvery {
		st.mu.Unlock()
		metrics.E2EReports.WithLabelValues("rate").Inc()
		return
	}
	st.lastReport = now
	st.mu.Unlock()
	report, err := tgcallsjson.ParseE2EStatus(msg)
	if err != nil {
		reason, ok := tgcallsjson.IsE2EStatusError(err)
		if !ok {
			reason = tgcallsjson.E2EDropSchema
		}
		metrics.E2EReports.WithLabelValues(reason).Inc()
		return
	}
	metrics.E2EReports.WithLabelValues("accepted").Inc()
	m.room.mgr.publish(Event{
		Type: EventE2EStatus, CallID: m.room.id, Participant: m.participant, Kind: m.kind, Time: now,
		E2EStatus: report,
	})
}
