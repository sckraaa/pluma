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
	"net/netip"
	"strings"
	"testing"
)

// e2eStatusExample is the example of ADR 0060 §5.
const e2eStatusExample = `{"colibriClass":"PlumaE2EStatus","v":1,"seq":12,
 "height":41,"lastBlock":"9f3c0a1b2c3d4e5f","offset0":42,"offset1":17,
 "error":0,
 "sendEpochs":["9f3c0a1b2c3d4e5f","77aa01b2c3d4e5f6"],
 "decryptEpochs":["9f3c0a1b2c3d4e5f"],
 "unknownEpochs":[],
 "encrypted":250,"encryptFailed":0,
 "senders":[{"user":1000002,"ok":248,"unknownEpoch":0,"tooOld":0,"duplicate":1,"failed":0,"lastOkMs":40}]}`

func TestParseE2EStatus(t *testing.T) {
	s, err := ParseE2EStatus([]byte(e2eStatusExample))
	if err != nil {
		t.Fatal(err)
	}
	if s.Seq != 12 || s.Height != 41 || s.LastBlock != "9f3c0a1b2c3d4e5f" || s.Offset0 != 42 || s.Offset1 != 17 ||
		len(s.SendEpochs) != 2 || len(s.UnknownEpochs) != 0 || s.Encrypted != 250 ||
		len(s.Senders) != 1 || s.Senders[0] != (E2ESender{User: 1000002, OK: 248, Duplicate: 1, LastOkMs: 40}) {
		t.Fatalf("%+v", s)
	}
	// Before the join: no state.
	if _, err := ParseE2EStatus([]byte(strings.NewReplacer(`"height":41`, `"height":-1`, `"lastBlock":"9f3c0a1b2c3d4e5f"`, `"lastBlock":""`).Replace(e2eStatusExample))); err != nil {
		t.Fatalf("a report without state: %v", err)
	}
}

// What a client marshals the parser takes back unchanged, empty lists
// included.
func TestMarshalE2EStatus(t *testing.T) {
	want, err := ParseE2EStatus([]byte(e2eStatusExample))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseE2EStatus(MarshalE2EStatus(want))
	if err != nil {
		t.Fatalf("the marshaled report is refused: %v", err)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if !bytes.Equal(a, b) {
		t.Fatalf("%s\nwant %s", a, b)
	}
	empty := MarshalE2EStatus(&E2EStatus{Seq: 1, Height: -1, Offset0: -1, Offset1: -1})
	if _, err := ParseE2EStatus(empty); err != nil {
		t.Fatalf("a report without state or frames: %v: %s", err, empty)
	}
}

func TestParseE2EStatusRefuses(t *testing.T) {
	big := strings.Replace(e2eStatusExample, `"encrypted":250`, `"encrypted":250`+strings.Repeat(" ", E2EStatusMaxSize), 1)
	senders := make([]string, E2EStatusMaxSenders+1)
	for i := range senders {
		senders[i] = `{"user":` + itoa(i+1) + `,"ok":1,"unknownEpoch":0,"tooOld":0,"duplicate":0,"failed":0,"lastOkMs":1}`
	}
	tests := []struct {
		name, old, new, reason string
	}{
		{"an extra field", `"error":0,`, `"error":0,"key":"00",`, E2EDropSchema},
		{"a field missing", `"offset1":17,`, ``, E2EDropSchema},
		{"another version", `"v":1`, `"v":2`, E2EDropVersion},
		{"another version with other fields", `"v":1,"seq":12,`, `"v":2,"sequence":12,`, E2EDropVersion},
		{"another class", `"PlumaE2EStatus"`, `"Other"`, E2EDropSchema},
		{"a fraction", `"seq":12`, `"seq":12.5`, E2EDropSchema},
		{"an exponent", `"seq":12`, `"seq":1e3`, E2EDropSchema},
		{"a string for a number", `"seq":12`, `"seq":"12"`, E2EDropSchema},
		{"seq 0", `"seq":12`, `"seq":0`, E2EDropSchema},
		{"a negative counter", `"encrypted":250`, `"encrypted":-1`, E2EDropSchema},
		{"height below -1", `"height":41`, `"height":-2`, E2EDropSchema},
		{"height without a block", `"lastBlock":"9f3c0a1b2c3d4e5f"`, `"lastBlock":""`, E2EDropSchema},
		{"a whole hash", `"lastBlock":"9f3c0a1b2c3d4e5f"`, `"lastBlock":"` + strings.Repeat("ab", 32) + `"`, E2EDropSchema},
		{"upper case", `"lastBlock":"9f3c0a1b2c3d4e5f"`, `"lastBlock":"9F3C0A1B2C3D4E5F"`, E2EDropSchema},
		{"too many epochs", `"sendEpochs":["9f3c0a1b2c3d4e5f","77aa01b2c3d4e5f6"]`, `"sendEpochs":[` + strings.TrimSuffix(strings.Repeat(`"9f3c0a1b2c3d4e5f",`, 16), ",") + `]`, E2EDropSchema},
		{"too many unknown epochs", `"unknownEpochs":[]`, `"unknownEpochs":[` + strings.TrimSuffix(strings.Repeat(`"9f3c0a1b2c3d4e5f",`, 9), ",") + `]`, E2EDropSchema},
		{"a sender with an extra field", `"lastOkMs":40}`, `"lastOkMs":40,"nonce":"00"}`, E2EDropSchema},
		{"a sender twice", `"lastOkMs":40}]`, `"lastOkMs":40},{"user":1000002,"ok":1,"unknownEpoch":0,"tooOld":0,"duplicate":0,"failed":0,"lastOkMs":1}]`, E2EDropSchema},
		{"too many senders", `[{"user":1000002,"ok":248,"unknownEpoch":0,"tooOld":0,"duplicate":1,"failed":0,"lastOkMs":40}]`, `[` + strings.Join(senders, ",") + `]`, E2EDropSchema},
		{"null senders", `"senders":[{"user":1000002,"ok":248,"unknownEpoch":0,"tooOld":0,"duplicate":1,"failed":0,"lastOkMs":40}]`, `"senders":null`, E2EDropSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := strings.Replace(e2eStatusExample, tt.old, tt.new, 1)
			if msg == e2eStatusExample {
				t.Fatal("the case changes nothing")
			}
			_, err := ParseE2EStatus([]byte(msg))
			if reason, ok := IsE2EStatusError(err); !ok || reason != tt.reason {
				t.Fatalf("error %v, want reason %s", err, tt.reason)
			}
		})
	}
	if _, err := ParseE2EStatus([]byte(big)); err == nil {
		t.Fatal("a report over the size limit accepted")
	} else if reason, _ := IsE2EStatusError(err); reason != E2EDropSize {
		t.Fatalf("reason %s", reason)
	}
}

// A report that came by other means than the parser is held to the same
// limits: what ParseE2EStatus takes passes, a value it would refuse does
// not.
func TestE2EStatusCheck(t *testing.T) {
	good, err := ParseE2EStatus([]byte(e2eStatusExample))
	if err != nil {
		t.Fatal(err)
	}
	if err := good.Check(); err != nil {
		t.Fatalf("the example: %v", err)
	}
	if err := (&E2EStatus{Seq: 1, Height: -1, Offset0: -1, Offset1: -1}).Check(); err != nil {
		t.Fatalf("a report without state: %v", err)
	}
	many := make([]E2ESender, E2EStatusMaxSenders+1)
	for i := range many {
		many[i] = E2ESender{User: int64(i + 1), LastOkMs: -1}
	}
	epochs := func(n int) []string {
		return strings.Split(strings.TrimSuffix(strings.Repeat("9f3c0a1b2c3d4e5f,", n), ","), ",")
	}
	tests := map[string]func(s *E2EStatus){
		"seq 0":                   func(s *E2EStatus) { s.Seq = 0 },
		"height below -1":         func(s *E2EStatus) { s.Height = -2 },
		"offset below -1":         func(s *E2EStatus) { s.Offset1 = -2 },
		"a whole hash":            func(s *E2EStatus) { s.LastBlock = strings.Repeat("ab", 32) },
		"height without a block":  func(s *E2EStatus) { s.LastBlock = "" },
		"too many epochs":         func(s *E2EStatus) { s.SendEpochs = epochs(E2EStatusMaxEpochs + 1) },
		"too many decrypted":      func(s *E2EStatus) { s.DecryptEpochs = epochs(E2EStatusMaxEpochs + 1) },
		"too many unknown epochs": func(s *E2EStatus) { s.UnknownEpochs = epochs(E2EStatusMaxUnknownEpochs + 1) },
		"not a prefix":            func(s *E2EStatus) { s.SendEpochs = []string{"key"} },
		"a negative counter":      func(s *E2EStatus) { s.EncryptFailed = -1 },
		"too many senders":        func(s *E2EStatus) { s.Senders = many },
		"a sender twice":          func(s *E2EStatus) { s.Senders = append(s.Senders, s.Senders[0]) },
		"a sender of user 0":      func(s *E2EStatus) { s.Senders[0].User = 0 },
		"a negative sender count": func(s *E2EStatus) { s.Senders[0].Failed = -1 },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := ParseE2EStatus([]byte(e2eStatusExample))
			edit(s)
			if reason, ok := IsE2EStatusError(s.Check()); !ok || reason != E2EDropSchema {
				t.Fatalf("accepted: %+v", s)
			}
		})
	}
	if !ValidE2EEpochs(nil, 0) || ValidE2EEpochs(epochs(2), 1) {
		t.Fatal("ValidE2EEpochs")
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// The answer to a join never asks for a broadcast stream: with "stream"
// tdesktop and iOS take parts unencrypted from the server (ADR 0060 §4).
func TestJoinResponseHasNoStream(t *testing.T) {
	r, err := BuildResponse(ResponseParams{
		Ufrag: "abcd", Pwd: "abcdefghijklmnopqrstuvwx", FingerprintSHA256: bytes.Repeat([]byte{1}, 32),
		Addresses: []Address{{IP: netip.MustParseAddr("127.0.0.1"), Port: 1}}, Endpoint: "e",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["stream"]; ok || bytes.Contains(data, []byte(`"stream"`)) {
		t.Fatalf("the answer names a stream: %s", data)
	}
}
