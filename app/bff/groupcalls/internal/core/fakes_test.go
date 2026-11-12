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
	"encoding/json"
	"sync"

	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/sfuclient"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeSFU is the nodes of the tests: rooms by node and call, with the
// connections in them, and the errors each method answers next.
type fakeSFU struct {
	mu    sync.Mutex
	nodes []string
	down  map[string]bool
	rooms map[string]map[int64]map[fakeConn]*pb.JoinRequest
	muted map[fakeConn]bool
	// joinErr answers the next Join instead of the room.
	joinErr []error
	// setMutedErr answers the next SetMuted instead of the room.
	setMutedErr []error
	// beforeJoin runs once at the next Join, before the room is looked
	// at: what happens on the other side while a join connects.
	beforeJoin func()
	calls      []string
}

type fakeConn struct {
	call, participant int64
	kind              pb.ConnectionKind
}

func newFakeSFU(nodes ...string) *fakeSFU {
	return &fakeSFU{nodes: nodes, down: map[string]bool{}, rooms: map[string]map[int64]map[fakeConn]*pb.JoinRequest{}, muted: map[fakeConn]bool{}}
}

var _ sfuclient.API = (*fakeSFU)(nil)

func (f *fakeSFU) log(s string) { f.calls = append(f.calls, s) }

func (f *fakeSFU) Enabled() bool { return len(f.nodes) > 0 }

func (f *fakeSFU) Has(node string) bool {
	for _, n := range f.nodes {
		if n == node {
			return true
		}
	}
	return false
}

func (f *fakeSFU) Pick(skip string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n != skip && !f.down[n] {
			return n, nil
		}
	}
	return "", sfuclient.ErrNoNode
}

func (f *fakeSFU) CreateRoom(_ context.Context, node string, callID int64, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("create " + node)
	if f.down[node] {
		return status.Error(codes.Unavailable, "down")
	}
	if f.rooms[node] == nil {
		f.rooms[node] = map[int64]map[fakeConn]*pb.JoinRequest{}
	}
	if f.rooms[node][callID] == nil {
		f.rooms[node][callID] = map[fakeConn]*pb.JoinRequest{}
	}
	return nil
}

func (f *fakeSFU) CloseRoom(_ context.Context, node string, callID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("close " + node)
	delete(f.rooms[node], callID)
	return nil
}

func (f *fakeSFU) Join(_ context.Context, node string, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	f.mu.Lock()
	hook := f.beforeJoin
	f.beforeJoin = nil
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("join " + node)
	if len(f.joinErr) > 0 {
		err := f.joinErr[0]
		f.joinErr = f.joinErr[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.down[node] {
		return nil, status.Error(codes.Unavailable, "down")
	}
	room := f.rooms[node][req.GetCallId()]
	if room == nil {
		return nil, status.Error(codes.NotFound, "no room")
	}
	k := fakeConn{req.GetCallId(), req.GetParticipant(), req.GetKind()}
	room[k] = req
	f.muted[fakeConn{req.GetCallId(), req.GetParticipant(), pb.ConnectionKind_CONNECTION_KIND_MAIN}] = req.GetMuted()
	return &pb.JoinResponse{Params: []byte(`{"transport":{}}`), Endpoint: "ep" + string(rune('0'+req.GetKind()))}, nil
}

func (f *fakeSFU) Leave(_ context.Context, node string, callID, participant int64, kind pb.ConnectionKind) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("leave " + node)
	room := f.rooms[node][callID]
	k := fakeConn{callID, participant, kind}
	if _, ok := room[k]; !ok {
		return status.Error(codes.NotFound, "no connection")
	}
	delete(room, k)
	return nil
}

func (f *fakeSFU) SetMuted(_ context.Context, _ string, callID, participant int64, muted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.setMutedErr) > 0 {
		err := f.setMutedErr[0]
		f.setMutedErr = f.setMutedErr[1:]
		if err != nil {
			return err
		}
	}
	f.muted[fakeConn{callID, participant, pb.ConnectionKind_CONNECTION_KIND_MAIN}] = muted
	return nil
}

func (f *fakeSFU) Sources(_ context.Context, node string, callID int64) ([]*pb.ConnectionSources, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down[node] {
		return nil, status.Error(codes.Unavailable, "down")
	}
	room, ok := f.rooms[node][callID]
	if !ok {
		return nil, status.Error(codes.NotFound, "no room")
	}
	var out []*pb.ConnectionSources
	for k, req := range room {
		out = append(out, &pb.ConnectionSources{
			Participant: k.participant, Kind: k.kind, Connected: true, Sources: fakeSources(req),
			Muted: k.kind == pb.ConnectionKind_CONNECTION_KIND_MAIN && f.muted[k],
		})
	}
	return out, nil
}

// fakeSources are the sources of the join params of a connection.
func fakeSources(req *pb.JoinRequest) []uint32 {
	if r, err := tgcallsjson.ParseJoinRequest(req.GetParams()); err == nil {
		return r.Sources()
	}
	return nil
}

// testParams are join params of tgcalls with the audio source ssrc and,
// with video, a camera of three layers.
func testParams(ssrc uint32, video bool) []byte {
	r := &tgcallsjson.JoinRequest{
		SSRC: ssrc, Ufrag: "Ufrg", Pwd: "abcdefghijklmnopqrstuvwx",
		Fingerprints: []tgcallsjson.Fingerprint{{Hash: "sha-256", Fingerprint: tgcallsjson.EncodeFingerprint(make([]byte, 32)), Setup: tgcallsjson.SetupPassive}},
	}
	if video {
		r.SourceGroups = []tgcallsjson.SourceGroup{
			{Semantics: "SIM", Sources: []uint32{ssrc + 1, ssrc + 3, ssrc + 5}},
			{Semantics: "FID", Sources: []uint32{ssrc + 1, ssrc + 2}},
			{Semantics: "FID", Sources: []uint32{ssrc + 3, ssrc + 4}},
			{Semantics: "FID", Sources: []uint32{ssrc + 5, ssrc + 6}},
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return b
}

// fakePlacer is Store.SetNode over one call.
type fakePlacer struct {
	mu   sync.Mutex
	call groupcalls.Call
}

func (p *fakePlacer) SetNode(_ context.Context, _ int64, old, node string) (*groupcalls.Call, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.call.Discarded {
		c := p.call
		return &c, false, nil
	}
	won := p.call.SfuNode == old
	if won {
		p.call.SfuNode = node
	}
	c := p.call
	return &c, won, nil
}
