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

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func joinReq(call int64) *pb.JoinRequest {
	return &pb.JoinRequest{CallId: call, Participant: 1, Params: testParams(100, false)}
}

// The first join places the room on the first node that answers and
// opens it there.
func TestConnectPlaces(t *testing.T) {
	ctx := context.Background()
	if _, _, err := connect(ctx, newFakeSFU(), &fakePlacer{}, &groupcalls.Call{Id: 1}, joinReq(1)); !errors.Is(err, errNotConfigured) {
		t.Fatalf("no nodes: %v", err)
	}
	sfu := newFakeSFU("a", "b")
	sfu.down["a"] = true
	st := &fakePlacer{call: groupcalls.Call{Id: 1}}
	node, resp, err := connect(ctx, sfu, st, &st.call, joinReq(1))
	if err != nil || node != "b" || st.call.SfuNode != "b" || resp.GetEndpoint() == "" {
		t.Fatalf("placed on %q (row %q): %v", node, st.call.SfuNode, err)
	}
}

// A join that loses the race to place the room ends on the node that won.
func TestConnectLosesRace(t *testing.T) {
	sfu := newFakeSFU("a", "b")
	st := &fakePlacer{call: groupcalls.Call{Id: 1, SfuNode: "b"}}
	stale := &groupcalls.Call{Id: 1}
	node, _, err := connect(context.Background(), sfu, st, stale, joinReq(1))
	if err != nil || node != "b" {
		t.Fatalf("node %q: %v", node, err)
	}
}

// A room lost on its node (an SFU restart) is opened again; a node that is
// down moves the room to another one.
func TestConnectRecovers(t *testing.T) {
	ctx := context.Background()
	sfu := newFakeSFU("a", "b")
	st := &fakePlacer{call: groupcalls.Call{Id: 1, SfuNode: "a"}}
	sfu.joinErr = []error{status.Error(codes.NotFound, "no room")}
	if node, _, err := connect(ctx, sfu, st, &st.call, joinReq(1)); err != nil || node != "a" {
		t.Fatalf("lost room: %q %v", node, err)
	}
	sfu.down["a"] = true
	call := st.call
	if node, _, err := connect(ctx, sfu, st, &call, joinReq(1)); err != nil || node != "b" || st.call.SfuNode != "b" {
		t.Fatalf("node down: %q (row %q) %v", node, st.call.SfuNode, err)
	}
	// A configured node that is gone from the configuration places the
	// room anew.
	st.call.SfuNode = "old"
	call = st.call
	if node, _, err := connect(ctx, sfu, st, &call, joinReq(1)); err != nil || node != "b" {
		t.Fatalf("unknown node: %q %v", node, err)
	}
	st.call.Discarded = true
	st.call.SfuNode = ""
	call = st.call
	call.Discarded = false
	if _, _, err := connect(ctx, sfu, st, &call, joinReq(1)); !errors.Is(err, mtproto.ErrGroupcallAlreadyDiscarded) {
		t.Fatalf("a call ended meanwhile: %v", err)
	}
}

// The SFU's refusals are the errors the clients know.
func TestJoinErrors(t *testing.T) {
	for code, want := range map[codes.Code]error{
		codes.AlreadyExists:     mtproto.ErrGroupcallSsrcDuplicateMuch,
		codes.ResourceExhausted: errParticipantsTooMuch,
		codes.InvalidArgument:   mtproto.ErrDataJsonInvalid,
		codes.Internal:          mtproto.ErrGroupcallAddParticipantsFailed,
	} {
		sfu := newFakeSFU("a")
		sfu.joinErr = []error{status.Error(code, "x")}
		st := &fakePlacer{call: groupcalls.Call{Id: 1, SfuNode: "a"}}
		if _, _, err := connect(context.Background(), sfu, st, &st.call, joinReq(1)); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", code, err, want)
		}
	}
}

// A join and a leave are versioned, one above the change before; the
// call's version is that of the last change.
func TestVersions(t *testing.T) {
	call := &groupcalls.Call{Version: 7}
	got := versions(call, []groupcalls.Change{{Versioned: true}, {}, {Versioned: true}})
	if got[0] != 6 || got[1] != 6 || got[2] != 7 {
		t.Fatalf("versions %v", got)
	}
	if got = versions(call, []groupcalls.Change{{}}); got[0] != 7 {
		t.Fatalf("an edit: %v", got)
	}
}

// The service messages due of a call, in their order, and their random
// ids: stable per call and message, different between them.
func TestServiceMessages(t *testing.T) {
	call := &groupcalls.Call{Id: 5, ScheduleDate: 100}
	if d := due(call); len(d) != 1 || d[0] != groupcalls.PostedScheduled {
		t.Fatalf("scheduled: %v", d)
	}
	call.StartDate, call.Posted = 120, groupcalls.PostedScheduled
	if d := due(call); len(d) != 1 || d[0] != groupcalls.PostedStart {
		t.Fatalf("started: %v", d)
	}
	call.Discarded, call.Duration = true, 60
	if d := due(call); len(d) != 2 || d[1] != groupcalls.PostedEnd {
		t.Fatalf("ended without the start posted: %v", d)
	}
	if a := actionOf(call, groupcalls.PostedEnd); a.GetDuration().GetValue() != 60 || a.GetCall().GetId() != 5 {
		t.Fatalf("end: %v", a)
	}
	if a := actionOf(call, groupcalls.PostedStart); a.GetDuration() != nil {
		t.Fatalf("start has a duration: %v", a)
	}
	ids := map[int64]bool{}
	for _, bit := range []int{groupcalls.PostedScheduled, groupcalls.PostedStart, groupcalls.PostedEnd} {
		id := messageRandomId(5, bit)
		if id <= 0 || id != messageRandomId(5, bit) || ids[id] {
			t.Fatalf("random id %d of %d", id, bit)
		}
		ids[id] = true
	}
	if messageRandomId(6, groupcalls.PostedStart) == messageRandomId(5, groupcalls.PostedStart) {
		t.Fatal("two calls share a random id")
	}
}
