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

// PhoneSendConferenceCallBroadcast
// phone.sendConferenceCallBroadcast#c6701900 call:InputGroupCall block:bytes = Updates;
//
// Appends a commit or a reveal of the emoji verification to sub-chain 1 of
// a call (ADR 0060 §2; tde2e Call.cpp:96-245): one of a user of the group
// state, signed by his key, for the current height, one commit and one
// reveal each, a reveal only once all committed. One for an earlier height
// is answered and not kept: the clients skip it (Call.cpp:112-116). One not
// in the group state (a removed participant) is GROUPCALL_FORBIDDEN, on
// which tdesktop and Web K join again (calls_group_call.cpp:1930-1934,
// groupCallInstance.ts:784-798). The broadcast goes to the group state.
func (c *GroupCallsCore) PhoneSendConferenceCallBroadcast(in *mtproto.TLPhoneSendConferenceCallBroadcast) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	w, err := c.svcCtx.Dao.Store.AppendBroadcast(c.ctx, call.Id, c.MD.UserId, in.GetBlock())
	if err != nil {
		return nil, chainError(err)
	}
	up, err := writeUpdate(call.Input(), groupcalls.SubChainBroadcast, w)
	if err != nil {
		return nil, err
	}
	if w.Block != nil {
		c.deliverChain(up, w.Members)
	}
	return c.envelope(c.MD.UserId, []*mtproto.Update{up}, nil, nil), nil
}
