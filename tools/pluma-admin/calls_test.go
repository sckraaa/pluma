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

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

func TestRenderCall(t *testing.T) {
	now := time.Now()
	ms := now.Add(-3 * time.Second).UnixMilli()
	v := &callView{
		Call:      &groupcalls.Call{Id: 9, PeerType: 4, PeerId: 77, Version: 5, ParticipantsCount: 1, SfuNode: "main", ChainHeight: 2, ChainHash: [32]byte{0xab}, BroadcastCount: 3},
		Members:   []int64{1, 2},
		Phase:     tde2e.PhaseReveal,
		Committed: map[int64]bool{1: true, 2: true},
		Revealed:  map[int64]bool{1: true},
		InCall:    map[int64]bool{1: true},
		Heights:   map[string]int32{"ab00000000000000": 2},
		Diags: map[int64]groupcalls.StoredDiag{
			1: {UserId: 1, Diag: &groupcalls.E2EDiag{
				Poll:       [2]groupcalls.E2EPoll{{Offset: 3, At: ms}, {Offset: 3, At: ms}},
				SendEpochs: []string{"ab00000000000000"}, SendAt: ms,
				Report: &tgcallsjson.E2EStatus{Seq: 4, Height: 2, LastBlock: "ab00000000000000",
					Senders: []tgcallsjson.E2ESender{{User: 2, OK: 10, UnknownEpoch: 1, LastOkMs: 5}}},
				ReportAt: ms, Rejoins: 1,
			}},
			2: {UserId: 2, Diag: &groupcalls.E2EDiag{Alarms: []string{"not_in_call", "verification_stuck"}}},
		},
	}
	var out bytes.Buffer
	renderCall(&out, v, now)
	s := out.String()
	for _, want := range []string{
		"call 9 (peer 77 of type 4): live, version 5, 1 in the call",
		"chain: height 2, block ab00000000000000, 3 broadcasts, emoji verification at the height: reveal",
		"yes/no",
		"3 (3s ago)",
		"2 (3s ago)",
		"#4 (3s ago)",
		"ab00000000000000 (2)",
		"2:10/1/0/0/0",
		"not_in_call,verification_stuck",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q in\n%s", want, s)
		}
	}
	if lines := strings.Count(s, "\n"); lines != 5 {
		t.Errorf("%d lines:\n%s", lines, s)
	}
}

func TestCallsUsage(t *testing.T) {
	for _, args := range [][]string{{"calls"}, {"calls", "show"}, {"calls", "show", "x"}, {"calls", "list", "1"}} {
		if err := callsCommand(args, "", &bytes.Buffer{}); err != errUsage {
			t.Errorf("%v: %v", args, err)
		}
	}
}
