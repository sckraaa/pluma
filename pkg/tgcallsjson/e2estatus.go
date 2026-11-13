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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// The status report of the end-to-end encryption, PlumaE2EStatus v1 (ADR
// 0060 §5): every 5 seconds a patched client tells the SFU, over the data
// channel of its main connection, how far its chain is and how its frames
// encrypt and decrypt. It holds counters and prefixes of hashes the server
// can compute itself, never a key, a nonce, an emoji or a byte of a frame.
// The schema is strict: a report with a field more or less, of another
// version, of another type or over a limit is dropped whole.

// ClassPlumaE2EStatus is the Colibri class of the report.
const ClassPlumaE2EStatus = "PlumaE2EStatus"

// Limits of a report v1.
const (
	E2EStatusVersion = 1
	// E2EStatusMaxSize bounds the serialized report; 100 senders take
	// about 11 KiB.
	E2EStatusMaxSize = 16 << 10
	// E2EStatusMaxEpochs bounds sendEpochs and decryptEpochs: tde2e keeps
	// at most 15 epochs (Call.h:99).
	E2EStatusMaxEpochs = 15
	// E2EStatusMaxUnknownEpochs bounds unknownEpochs.
	E2EStatusMaxUnknownEpochs = 8
	// E2EStatusMaxSenders bounds senders: conference_call_size_limit.
	E2EStatusMaxSenders = 100
)

// Reasons a report is dropped, for the metrics.
const (
	E2EDropSize    = "size"
	E2EDropVersion = "version"
	E2EDropSchema  = "schema"
)

// E2EStatusError is a report that does not hold to the schema.
type E2EStatusError struct {
	// Reason is one of the E2EDrop* constants.
	Reason string
	Msg    string
}

func (e *E2EStatusError) Error() string {
	return "PlumaE2EStatus: " + e.Reason + ": " + e.Msg
}

// E2ESender is what a receiver counted of one sender since its last
// report.
type E2ESender struct {
	User         int64 `json:"user"`
	OK           int64 `json:"ok"`
	UnknownEpoch int64 `json:"unknownEpoch"`
	TooOld       int64 `json:"tooOld"`
	Duplicate    int64 `json:"duplicate"`
	Failed       int64 `json:"failed"`
	// LastOkMs is how long before the report the last frame of the sender
	// decrypted; -1 when none did.
	LastOkMs int64 `json:"lastOkMs"`
}

// E2EStatus is one report. The counters of frames and the lists of
// decrypted and unknown epochs are increments since the client's last
// report; the rest are current values.
type E2EStatus struct {
	Seq           int64       `json:"seq"`
	Height        int32       `json:"height"`
	LastBlock     string      `json:"lastBlock"`
	Offset0       int32       `json:"offset0"`
	Offset1       int32       `json:"offset1"`
	Error         int32       `json:"error"`
	SendEpochs    []string    `json:"sendEpochs"`
	DecryptEpochs []string    `json:"decryptEpochs"`
	UnknownEpochs []string    `json:"unknownEpochs"`
	Encrypted     int64       `json:"encrypted"`
	EncryptFailed int64       `json:"encryptFailed"`
	Senders       []E2ESender `json:"senders"`
}

var e2eStatusFields = []string{"colibriClass", "v", "seq", "height", "lastBlock", "offset0", "offset1", "error",
	"sendEpochs", "decryptEpochs", "unknownEpochs", "encrypted", "encryptFailed", "senders"}

var e2eSenderFields = []string{"user", "ok", "unknownEpoch", "tooOld", "duplicate", "failed", "lastOkMs"}

func schemaError(format string, args ...any) error {
	return &E2EStatusError{Reason: E2EDropSchema, Msg: fmt.Sprintf(format, args...)}
}

// object decodes a JSON object whose keys must be exactly fields.
func object(data []byte, fields []string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return nil, schemaError("not an object")
	}
	if len(m) != len(fields) {
		return nil, schemaError("%d fields, want %d", len(m), len(fields))
	}
	for _, f := range fields {
		if _, ok := m[f]; !ok {
			return nil, schemaError("no field %s", f)
		}
	}
	return m, nil
}

// integer reads a JSON integer within [lo, hi]: no fraction, no exponent.
func integer(raw json.RawMessage, name string, lo, hi int64) (int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return 0, schemaError("%s is not an integer", name)
	}
	v, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, schemaError("%s is not an integer", name)
	}
	if v < lo || v > hi {
		return 0, schemaError("%s out of range", name)
	}
	return v, nil
}

// isPrefix reports whether s is the prefix of a hash as the report gives
// it: 8 bytes in lowercase hex.
func isPrefix(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		hex := c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
		if !hex {
			return false
		}
	}
	return true
}

func prefixes(raw json.RawMessage, name string, limit int) ([]string, error) {
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil || list == nil {
		return nil, schemaError("%s is not a list of strings", name)
	}
	if len(list) > limit {
		return nil, schemaError("%s has %d entries, limit %d", name, len(list), limit)
	}
	for _, s := range list {
		if !isPrefix(s) {
			return nil, schemaError("%s holds a value that is not a prefix of a hash", name)
		}
	}
	return list, nil
}

// ParseE2EStatus checks and reads a PlumaE2EStatus message of the data
// channel. The error is an *E2EStatusError.
func ParseE2EStatus(data []byte) (*E2EStatus, error) {
	if len(data) > E2EStatusMaxSize {
		return nil, &E2EStatusError{Reason: E2EDropSize, Msg: fmt.Sprintf("%d bytes, limit %d", len(data), E2EStatusMaxSize)}
	}
	m, err := object(data, e2eStatusFields)
	if err != nil {
		// A report of another version may have other fields: the version
		// decides the reason when it can be read.
		var head struct {
			V json.RawMessage `json:"v"`
		}
		if json.Unmarshal(data, &head) == nil && head.V != nil {
			if v, verr := integer(head.V, "v", math.MinInt64, math.MaxInt64); verr == nil && v != E2EStatusVersion {
				return nil, &E2EStatusError{Reason: E2EDropVersion, Msg: fmt.Sprintf("version %d", v)}
			}
		}
		return nil, err
	}
	var class string
	if json.Unmarshal(m["colibriClass"], &class) != nil || class != ClassPlumaE2EStatus {
		return nil, schemaError("colibriClass is not %s", ClassPlumaE2EStatus)
	}
	v, err := integer(m["v"], "v", math.MinInt64, math.MaxInt64)
	if err != nil {
		return nil, err
	}
	if v != E2EStatusVersion {
		return nil, &E2EStatusError{Reason: E2EDropVersion, Msg: fmt.Sprintf("version %d", v)}
	}
	s := &E2EStatus{}
	ints := []struct {
		name   string
		lo, hi int64
		set    func(int64)
	}{
		{"seq", 1, math.MaxInt64, func(x int64) { s.Seq = x }},
		{"height", -1, math.MaxInt32, func(x int64) { s.Height = int32(x) }},
		{"offset0", -1, math.MaxInt32, func(x int64) { s.Offset0 = int32(x) }},
		{"offset1", -1, math.MaxInt32, func(x int64) { s.Offset1 = int32(x) }},
		{"error", math.MinInt32, math.MaxInt32, func(x int64) { s.Error = int32(x) }},
		{"encrypted", 0, math.MaxInt64, func(x int64) { s.Encrypted = x }},
		{"encryptFailed", 0, math.MaxInt64, func(x int64) { s.EncryptFailed = x }},
	}
	for _, f := range ints {
		x, err := integer(m[f.name], f.name, f.lo, f.hi)
		if err != nil {
			return nil, err
		}
		f.set(x)
	}
	if json.Unmarshal(m["lastBlock"], &s.LastBlock) != nil || (s.LastBlock != "" && !isPrefix(s.LastBlock)) {
		return nil, schemaError("lastBlock is not a prefix of a hash")
	}
	if (s.Height < 0) != (s.LastBlock == "") {
		return nil, schemaError("height and lastBlock disagree")
	}
	if s.SendEpochs, err = prefixes(m["sendEpochs"], "sendEpochs", E2EStatusMaxEpochs); err != nil {
		return nil, err
	}
	if s.DecryptEpochs, err = prefixes(m["decryptEpochs"], "decryptEpochs", E2EStatusMaxEpochs); err != nil {
		return nil, err
	}
	if s.UnknownEpochs, err = prefixes(m["unknownEpochs"], "unknownEpochs", E2EStatusMaxUnknownEpochs); err != nil {
		return nil, err
	}
	var senders []json.RawMessage
	if err := json.Unmarshal(m["senders"], &senders); err != nil || senders == nil {
		return nil, schemaError("senders is not a list")
	}
	if len(senders) > E2EStatusMaxSenders {
		return nil, schemaError("senders has %d entries, limit %d", len(senders), E2EStatusMaxSenders)
	}
	seen := make(map[int64]bool, len(senders))
	for _, raw := range senders {
		f, err := object(raw, e2eSenderFields)
		if err != nil {
			return nil, err
		}
		var e E2ESender
		vals := []struct {
			name   string
			lo     int64
			target *int64
		}{
			{"user", 1, &e.User}, {"ok", 0, &e.OK}, {"unknownEpoch", 0, &e.UnknownEpoch}, {"tooOld", 0, &e.TooOld},
			{"duplicate", 0, &e.Duplicate}, {"failed", 0, &e.Failed}, {"lastOkMs", -1, &e.LastOkMs},
		}
		for _, v := range vals {
			if *v.target, err = integer(f[v.name], v.name, v.lo, math.MaxInt64); err != nil {
				return nil, err
			}
		}
		if seen[e.User] {
			return nil, schemaError("a sender twice")
		}
		seen[e.User] = true
		s.Senders = append(s.Senders, e)
	}
	if s.Senders == nil {
		s.Senders = []E2ESender{}
	}
	return s, nil
}

// ValidE2EEpochs reports whether list is a list of epochs as a report gives
// it: at most limit prefixes of hashes.
func ValidE2EEpochs(list []string, limit int) bool {
	if len(list) > limit {
		return false
	}
	for _, s := range list {
		if !isPrefix(s) {
			return false
		}
	}
	return true
}

// Check checks the values of a report read by other means than
// ParseE2EStatus, such as the control API of the SFU, against the limits
// of the schema. The error is an *E2EStatusError.
func (s *E2EStatus) Check() error {
	switch {
	case s.Seq < 1:
		return schemaError("seq out of range")
	case s.Height < -1 || s.Offset0 < -1 || s.Offset1 < -1:
		return schemaError("a height or an offset out of range")
	case s.LastBlock != "" && !isPrefix(s.LastBlock):
		return schemaError("lastBlock is not a prefix of a hash")
	case (s.Height < 0) != (s.LastBlock == ""):
		return schemaError("height and lastBlock disagree")
	case !ValidE2EEpochs(s.SendEpochs, E2EStatusMaxEpochs) || !ValidE2EEpochs(s.DecryptEpochs, E2EStatusMaxEpochs) ||
		!ValidE2EEpochs(s.UnknownEpochs, E2EStatusMaxUnknownEpochs):
		return schemaError("a list of epochs over its limit or not of prefixes of hashes")
	case s.Encrypted < 0 || s.EncryptFailed < 0:
		return schemaError("a counter of frames out of range")
	case len(s.Senders) > E2EStatusMaxSenders:
		return schemaError("senders has %d entries, limit %d", len(s.Senders), E2EStatusMaxSenders)
	}
	seen := make(map[int64]bool, len(s.Senders))
	for _, e := range s.Senders {
		if e.User < 1 || e.OK < 0 || e.UnknownEpoch < 0 || e.TooOld < 0 || e.Duplicate < 0 || e.Failed < 0 || e.LastOkMs < -1 {
			return schemaError("a sender out of range")
		}
		if seen[e.User] {
			return schemaError("a sender twice")
		}
		seen[e.User] = true
	}
	return nil
}

// IsE2EStatusError reports whether err is a report refused by the schema,
// and its reason.
func IsE2EStatusError(err error) (string, bool) {
	var e *E2EStatusError
	if errors.As(err, &e) {
		return e.Reason, true
	}
	return "", false
}

// MarshalE2EStatus serializes a report v1 as a client sends it; nil lists
// are sent empty, since the schema has every field.
func MarshalE2EStatus(s *E2EStatus) []byte {
	orEmpty := func(l []string) []string {
		if l == nil {
			return []string{}
		}
		return l
	}
	senders := s.Senders
	if senders == nil {
		senders = []E2ESender{}
	}
	msg := struct {
		ColibriClass string `json:"colibriClass"`
		V            int    `json:"v"`
		E2EStatus
	}{ClassPlumaE2EStatus, E2EStatusVersion, *s}
	msg.SendEpochs, msg.DecryptEpochs, msg.UnknownEpochs = orEmpty(s.SendEpochs), orEmpty(s.DecryptEpochs), orEmpty(s.UnknownEpochs)
	msg.Senders = senders
	out, err := json.Marshal(msg)
	if err != nil {
		// Only integers, strings and lists of them.
		panic(err)
	}
	return out
}
