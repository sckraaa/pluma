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

package tgcallsjson

import (
	"encoding/json"
	"fmt"
)

// Colibri message classes of the data channel. tgcalls speaks the Colibri v1
// dialect of Jitsi Videobridge (GroupInstanceCustomImpl.cpp:1097-1099).
const (
	ClassReceiverVideoConstraints = "ReceiverVideoConstraints"
	ClassSenderVideoConstraints   = "SenderVideoConstraints"
)

// Heights the clients ask for and the SFU announces: thumbnail, medium and
// full (GroupInstanceCustomImpl.cpp:3530-3565; Web groupCallConnectionInstance.ts).
const (
	HeightThumbnail = 180
	HeightMedium    = 360
	HeightFull      = 720
)

// MaxColibriMessage bounds a data channel message the SFU reads. A client
// asks for at most a few dozen endpoints of about 20 bytes each.
const MaxColibriMessage = 64 << 10

// maxConstrainedEndpoints bounds the endpoints of one ReceiverVideoConstraints.
const maxConstrainedEndpoints = 256

// VideoConstraint limits the height of one endpoint's video. MaxHeight 0
// means the receiver does not want it.
type VideoConstraint struct {
	MinHeight int `json:"minHeight,omitempty"`
	MaxHeight int `json:"maxHeight"`
}

// ReceiverVideoConstraints is what a receiver wants to see: the endpoints on
// stage first, and a height range per endpoint. Endpoints not listed get
// DefaultConstraints, which clients send as maxHeight 0.
type ReceiverVideoConstraints struct {
	ColibriClass       string                     `json:"colibriClass"`
	DefaultConstraints VideoConstraint            `json:"defaultConstraints"`
	OnStageEndpoints   []string                   `json:"onStageEndpoints"`
	Constraints        map[string]VideoConstraint `json:"constraints"`
}

// SenderVideoConstraints tells a sender the largest height anyone watches;
// tgcalls turns off the layers above it (GroupInstanceCustomImpl.cpp:3347-3383,
// 2596-2615: layer i is active while idealHeight >= 180, 360, 720).
type SenderVideoConstraints struct {
	ColibriClass     string `json:"colibriClass"`
	VideoConstraints struct {
		IdealHeight int `json:"idealHeight"`
	} `json:"videoConstraints"`
}

// ColibriClass returns the class of a data channel message, "" if it has
// none.
func ColibriClass(data []byte) (string, error) {
	if len(data) > MaxColibriMessage {
		return "", fmt.Errorf("%w: message of %d bytes", ErrMalformed, len(data))
	}
	var head struct {
		ColibriClass string `json:"colibriClass"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return head.ColibriClass, nil
}

// ParseReceiverVideoConstraints parses a ReceiverVideoConstraints message.
func ParseReceiverVideoConstraints(data []byte) (*ReceiverVideoConstraints, error) {
	if len(data) > MaxColibriMessage {
		return nil, fmt.Errorf("%w: message of %d bytes", ErrMalformed, len(data))
	}
	var m ReceiverVideoConstraints
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if m.ColibriClass != ClassReceiverVideoConstraints {
		return nil, fmt.Errorf("%w: class %q", ErrMalformed, m.ColibriClass)
	}
	if len(m.Constraints) > maxConstrainedEndpoints || len(m.OnStageEndpoints) > maxConstrainedEndpoints {
		return nil, fmt.Errorf("%w: too many endpoints", ErrMalformed)
	}
	if m.Constraints == nil {
		m.Constraints = map[string]VideoConstraint{}
	}
	return &m, nil
}

// MarshalSenderVideoConstraints builds a SenderVideoConstraints message.
func MarshalSenderVideoConstraints(idealHeight int) []byte {
	var m SenderVideoConstraints
	m.ColibriClass = ClassSenderVideoConstraints
	m.VideoConstraints.IdealHeight = idealHeight
	data, _ := json.Marshal(m) // a struct of a string and an int always marshals
	return data
}

// MarshalReceiverVideoConstraints builds a ReceiverVideoConstraints message
// as tgcalls does. The test client uses it.
func MarshalReceiverVideoConstraints(onStage []string, constraints map[string]VideoConstraint) []byte {
	m := ReceiverVideoConstraints{
		ColibriClass:     ClassReceiverVideoConstraints,
		OnStageEndpoints: onStage,
		Constraints:      constraints,
	}
	if m.OnStageEndpoints == nil {
		m.OnStageEndpoints = []string{}
	}
	if m.Constraints == nil {
		m.Constraints = map[string]VideoConstraint{}
	}
	data, _ := json.Marshal(m) // strings and ints only
	return data
}
