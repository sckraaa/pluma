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

// PhoneGetGroupCallChainBlocks
// phone.getGroupCallChainBlocks#ee9f88a6 call:InputGroupCall sub_chain_id:int offset:int limit:int = Updates;
//
// Reads a sub-chain of a call's encryption (ADR 0060 §2): from the index
// offset (a height in sub-chain 0, the number of a broadcast in sub-chain
// 1) at most limit blocks, or with offset -1 the last one, which tdesktop
// asks for before it joins (calls_group_call.cpp:1696-1726). next_offset is
// the index after the last block given; the clients take the first as
// next_offset minus the count (tdesktop tde2e_api.cpp:452-517). One who is
// not in the group state gets only the last block of sub-chain 0, whatever
// he asks (core.telegram.org/api/end-to-end/group-calls), and nothing of
// sub-chain 1: so a participant removed from the chain finds out, and one
// who may see the call can join on top of its last block. Every client
// polls this every 1 to 5 s (ADR 0060 §2, «Опрос»): the call's chain state
// is read by its primary key and the group state is cached by height.
func (c *GroupCallsCore) PhoneGetGroupCallChainBlocks(in *mtproto.TLPhoneGetGroupCallChainBlocks) (*mtproto.Updates, error) {
	sub := in.GetSubChainId()
	if sub != groupcalls.SubChainMain && sub != groupcalls.SubChainBroadcast {
		return nil, errSubChainInvalid
	}
	if in.GetLimit() <= 0 {
		return nil, mtproto.ErrLimitInvalid
	}
	input := in.GetCall()
	if input.GetPredicateName() != mtproto.Predicate_inputGroupCall {
		return nil, mtproto.ErrGroupcallInvalid
	}
	head, err := c.svcCtx.Dao.Store.Head(c.ctx, input.GetId())
	if err != nil {
		return nil, storeError(err)
	}
	if head.AccessHash != input.GetAccessHash() {
		return nil, mtproto.ErrGroupcallInvalid
	}
	if head.Discarded {
		return nil, mtproto.ErrGroupcallAlreadyDiscarded
	}
	member, err := c.svcCtx.Members.IsMember(c.ctx, c.svcCtx.Dao.Store, head, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !member {
		a, err := c.loadPeer(head.PeerType, head.PeerId, c.MD.UserId)
		if err != nil {
			return nil, err
		}
		if !a.canView(c.MD.UserId) {
			return nil, mtproto.ErrGroupcallForbidden
		}
	}
	count := head.Height + 1
	if sub == groupcalls.SubChainBroadcast {
		count = head.Broadcasts
	}
	limit := min(in.GetLimit(), maxChainLimit)
	var from int32
	switch {
	case !member && sub == groupcalls.SubChainBroadcast:
		from = count
	case !member || in.GetOffset() < 0:
		from = max(count-1, 0)
	default:
		from = min(in.GetOffset(), count)
	}
	to := count
	if member && in.GetOffset() >= 0 {
		to = min(from+limit, count)
	}
	list, err := c.svcCtx.Dao.Store.Blocks(c.ctx, head.Id, int8(sub), from, to)
	if err != nil {
		return nil, err
	}
	blocks, err := serverForm(list...)
	if err != nil {
		return nil, err
	}
	if member {
		c.svcCtx.E2E.Poll(head.Id, c.MD.UserId, sub, in.GetOffset())
	}
	input = mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: head.Id, AccessHash: head.AccessHash}).To_InputGroupCall()
	return c.envelope(c.MD.UserId, []*mtproto.Update{chainUpdate(input, sub, blocks, from+int32(len(blocks)))}, nil, nil), nil
}
