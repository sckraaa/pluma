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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneDeleteConferenceCallParticipants
// phone.deleteConferenceCallParticipants#8ca60525 flags:# only_left:flags.0?true kick:flags.1?true call:InputGroupCall ids:Vector<long> block:bytes = Updates;
//
// Appends a block that takes ids out of the group state of a call's chain
// (ADR 0060 §2): exactly them, signed by the caller's key in the chain.
// only_left removes those who left the media: every remaining client tries
// to remove one who left (tdesktop data_group_call.cpp:73-96,
// calls_group_call.cpp:822-860), the first block is taken and the others
// are CONF_WRITE_CHAIN_INVALID. kick is an admin's (the creator, or one
// with manage_call) and takes them out of the call too. The block goes to
// the group state before it, the removed included: his tde2e then fails
// with InvalidCallGroupState_NotParticipant and he joins again with a new
// key (§5). A writer not in the group state is GROUPCALL_FORBIDDEN, on
// which tdesktop joins again (calls_group_call.cpp:848-853).
func (c *GroupCallsCore) PhoneDeleteConferenceCallParticipants(in *mtproto.TLPhoneDeleteConferenceCallParticipants) (*mtproto.Updates, error) {
	if in.GetOnlyLeft() == in.GetKick() {
		// tdesktop sends exactly one of them (calls_group_call.cpp:842).
		return nil, errBlockInvalid
	}
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	var a *access
	if in.GetKick() {
		if a, err = c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId); err != nil {
			return nil, err
		}
		if !a.canManage(c.MD.UserId) {
			return nil, mtproto.ErrGroupcallForbidden
		}
	}
	w, err := c.svcCtx.Dao.Store.RemoveFromChain(c.ctx, groupcalls.RemoveParams{
		CallId: call.Id, CallerId: c.MD.UserId, Ids: in.GetIds(), Block: in.GetBlock(), Kick: in.GetKick(),
		Check: func(_ *groupcalls.Call, rows []*groupcalls.Participant) error {
			if in.GetKick() {
				return nil
			}
			for _, p := range rows {
				if p.InCall {
					return errBlockInvalid
				}
			}
			return nil
		},
	})
	if err != nil {
		return nil, chainError(err)
	}
	c.svcCtx.E2E.Chain(call.Id, w.Call.ChainHeight, w.Call.ChainHash, without(w.Members, in.GetIds()))
	up, err := writeUpdate(call.Input(), groupcalls.SubChainMain, w)
	if err != nil {
		return nil, err
	}
	for _, ch := range w.Changes {
		c.disconnect(w.Call, ch.Participant.UserId)
		c.svcCtx.E2E.Left(call.Id, ch.Participant.UserId)
		metricLeaves.Inc("kick")
	}
	// The block goes out before the rows of the kicked: tdesktop takes one
	// who leaves the call while still in the chain for one to remove with
	// only_left (data_group_call.cpp:73-84), which this block has done
	// already. A join goes the other way round (PhoneJoinGroupCall).
	c.deliverChain(up, w.Members)
	if len(w.Changes) == 0 {
		return c.envelope(c.MD.UserId, []*mtproto.Update{up}, nil, nil), nil
	}
	flip := (call.ParticipantsCount == 0) != (w.Call.ParticipantsCount == 0)
	a = c.deliver(a, w.Call, fanOut{changes: w.Changes, call: true, chats: flip})
	answer := c.answer(a, w.Call, w.Changes, nil, true)
	answer.Updates = append(answer.Updates, up)
	return answer, nil
}

// without is ids less those of out.
func without(ids, out []int64) []int64 {
	res := make([]int64, 0, len(ids))
	for _, id := range ids {
		gone := false
		for _, o := range out {
			gone = gone || o == id
		}
		if !gone {
			res = append(res, id)
		}
	}
	return res
}
