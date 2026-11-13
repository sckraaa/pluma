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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/sfuclient"
	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The room of a call lives on one node (ADR 0059 §3). The first join
// places it and writes the node into the call's row; a join that finds the
// node gone (an SFU restart loses the room, a node that is down) places it
// again, on the same node or another one, with a conditional update: of
// two joins that race, both end on the node that won (§10).

// placer moves the room of a call between nodes: Store.SetNode.
type placer interface {
	SetNode(ctx context.Context, id int64, old, node string) (*groupcalls.Call, bool, error)
}

// place puts the room of call on node if it is still on old, and returns
// the node it is on afterwards.
func place(ctx context.Context, st placer, call *groupcalls.Call, old, node string) (string, error) {
	after, won, err := st.SetNode(ctx, call.Id, old, node)
	if err != nil {
		return "", err
	}
	if !won && after.Discarded {
		return "", mtproto.ErrGroupcallAlreadyDiscarded
	}
	return after.SfuNode, nil
}

// unreachable reports whether a node did not answer, as opposed to
// refusing.
func unreachable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	}
	return errors.Is(err, sfuclient.ErrUnknownNode)
}

// connect connects one connection of a participant to the room of call,
// placing the room first when it has none or its node is gone, and
// returns the node and the SFU's answer. The errors are those of the API.
func connect(ctx context.Context, sfu sfuclient.API, st placer, call *groupcalls.Call, req *pb.JoinRequest) (string, *pb.JoinResponse, error) {
	if !sfu.Enabled() {
		return "", nil, errNotConfigured
	}
	node := call.SfuNode
	if node == "" || !sfu.Has(node) {
		pick, err := sfu.Pick("")
		if err != nil {
			return "", nil, errNotConfigured
		}
		if node, err = place(ctx, st, call, call.SfuNode, pick); err != nil {
			return "", nil, err
		}
	}
	resp, err := joinRoom(ctx, sfu, node, req)
	if err != nil && unreachable(err) {
		// The node is down: the room goes to another one, if any.
		if other, perr := sfu.Pick(node); perr == nil && other != node {
			logx.WithContext(ctx).Errorf("groupcalls: SFU node %s of call %d: %v; moving to %s", node, call.Id, err, other)
			if node, err = place(ctx, st, call, node, other); err != nil {
				return "", nil, err
			}
			resp, err = joinRoom(ctx, sfu, node, req)
		}
	}
	if err != nil {
		return "", nil, joinError(ctx, call.Id, node, err)
	}
	return node, resp, nil
}

// joinRoom opens the room (an open one is kept) and joins it; a room lost
// in between is opened again once.
func joinRoom(ctx context.Context, sfu sfuclient.API, node string, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	for attempt := 0; ; attempt++ {
		if err := sfu.CreateRoom(ctx, node, req.GetCallId()); err != nil {
			return nil, err
		}
		resp, err := sfu.Join(ctx, node, req)
		if status.Code(err) == codes.NotFound && attempt == 0 {
			continue
		}
		return resp, err
	}
}

// joinError maps a refusal of the SFU to the error of the API.
func joinError(ctx context.Context, callId int64, node string, err error) error {
	switch status.Code(err) {
	case codes.AlreadyExists:
		// The client joins again with new sources (tdesktop
		// calls_group_call.cpp:1877-1884, 2020-2024).
		return mtproto.ErrGroupcallSsrcDuplicateMuch
	case codes.ResourceExhausted:
		return errParticipantsTooMuch
	case codes.InvalidArgument:
		return mtproto.ErrDataJsonInvalid
	}
	logx.WithContext(ctx).Errorf("groupcalls: join of call %d on SFU node %s: %v", callId, node, err)
	return mtproto.ErrGroupcallAddParticipantsFailed
}

// held returns the sources of the connections of participant that the room
// of call holds; none when the call has no room (an SFU restart lost it).
func held(ctx context.Context, sfu sfuclient.API, call *groupcalls.Call, participant int64) ([]uint32, error) {
	if call.SfuNode == "" || !sfu.Has(call.SfuNode) {
		return nil, nil
	}
	list, err := sfu.Sources(ctx, call.SfuNode, call.Id)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, cs := range list {
		if cs.GetParticipant() == participant {
			out = append(out, cs.GetSources()...)
		}
	}
	return out, nil
}
