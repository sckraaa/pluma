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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

// Every group call is end-to-end encrypted (ADR 0060): its clients keep a
// blockchain whose blocks the server checks and orders (pkg/groupcalls,
// pkg/tde2e) but cannot write, for it never knows the key. Sub-chain 0
// holds the group state and the shared key: a join adds the caller
// (phone.joinGroupCall with public_key and block), a removal takes out
// those who left or were kicked (phone.deleteConferenceCallParticipants).
// Sub-chain 1 holds the commits and reveals of the emoji verification
// (phone.sendConferenceCallBroadcast). Both are read with
// phone.getGroupCallChainBlocks. Every write is answered and sent to the
// group state as updateGroupCallChainBlocks. The order of a write is the
// transaction, then the SFU, then the updates (§2): a failure of the SFU
// does not undo an accepted block, and a lost update is made up by the
// clients' polls.

// Errors of the chain the clients tell apart (ADR 0060 §2, «Ошибки»):
// CONF_WRITE_CHAIN_INVALID by prefix (tdesktop calls_group_call.cpp:1865,
// iOS GroupCalls.swift:746-747), on which they read the last block again
// and rewrite theirs.
var (
	errChainInvalid = status.Error(mtproto.ErrBadRequest, "CONF_WRITE_CHAIN_INVALID")
	errBlockInvalid = status.Error(mtproto.ErrBadRequest, "BLOCK_INVALID")
	// errSubChainInvalid: a sub-chain other than 0 and 1. Telegram's text
	// for it is not verified; no client asks for another.
	errSubChainInvalid = status.Error(mtproto.ErrBadRequest, "SUB_CHAIN_ID_INVALID")
)

// maxChainLimit bounds the blocks of one read (ADR 0060 §2); tdesktop asks
// for 50 (tde2e_api.cpp:31-32), iOS for 10.
const maxChainLimit = 100

var metricChainRefusals = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "groupcalls",
	Name:      "e2e_refusals_total",
	Help:      "Blocks and broadcasts of the encryption refused, by the tde2e code (SERVER_POLICY: the server's own rules).",
	Labels:    []string{"code"},
})

// chainError maps a refusal of the chain to the error of the API; any other
// error goes through storeError.
func chainError(err error) error {
	var e *tde2e.Error
	if !errors.As(err, &e) {
		if errors.Is(err, groupcalls.ErrNoChainBlock) {
			return mtproto.ErrGroupcallForbidden
		}
		return storeError(err)
	}
	metricChainRefusals.Inc(e.Code.String())
	switch e.Kind {
	case tde2e.KindStale:
		return errChainInvalid
	case tde2e.KindForbidden:
		return mtproto.ErrGroupcallForbidden
	case tde2e.KindTooMany:
		return errParticipantsTooMuch
	}
	return errBlockInvalid
}

// chainUpdate is updateGroupCallChainBlocks of a sub-chain: blocks in the
// server form, and the index after the last.
func chainUpdate(call *mtproto.InputGroupCall, subChain int32, blocks [][]byte, next int32) *mtproto.Update {
	if blocks == nil {
		blocks = [][]byte{}
	}
	return mtproto.MakeTLUpdateGroupCallChainBlocks(&mtproto.Update{
		Call_INPUTGROUPCALL: call,
		SubChainId:          subChain,
		Blocks:              blocks,
		NextOffset:          next,
	}).To_Update()
}

// serverForm hands out stored blocks with the magic plus one.
func serverForm(list ...*groupcalls.ChainBlock) ([][]byte, error) {
	out := make([][]byte, 0, len(list))
	for _, b := range list {
		if b == nil {
			continue
		}
		data, err := b.Server()
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, nil
}

// writeUpdate is the update of a write of a sub-chain.
func writeUpdate(input *mtproto.InputGroupCall, subChain int32, w *groupcalls.ChainWrite) (*mtproto.Update, error) {
	blocks, err := serverForm(w.Block)
	if err != nil {
		return nil, err
	}
	return chainUpdate(input, subChain, blocks, w.Next), nil
}

// deliverChain sends the update of a write to the group state it concerns
// (ChainWrite.Members); the device that wrote has it in its answer. A
// failed send is logged: the clients poll the chain anyway.
func (c *GroupCallsCore) deliverChain(up *mtproto.Update, members []int64) {
	for _, userId := range uniq(members) {
		c.send(userId, c.envelope(userId, []*mtproto.Update{up}, nil, nil))
	}
}
