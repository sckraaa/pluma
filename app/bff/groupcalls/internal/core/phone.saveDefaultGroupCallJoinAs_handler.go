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
)

// PhoneSaveDefaultGroupCallJoinAs
// phone.saveDefaultGroupCallJoinAs#575e1f8c peer:InputPeer join_as:InputPeer = Bool;
//
// The peer the account joins the calls of a chat as. Only the account
// itself is offered (joinAs), and it is what the clients take when
// groupcall_default_join_as is unset, so there is nothing to keep: the
// account is accepted, any other peer is JOIN_AS_PEER_INVALID.
func (c *GroupCallsCore) PhoneSaveDefaultGroupCallJoinAs(in *mtproto.TLPhoneSaveDefaultGroupCallJoinAs) (*mtproto.Bool, error) {
	a, err := c.loadInputPeer(in.GetPeer(), c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		return nil, mtproto.ErrChannelPrivate
	}
	if err = c.joinAs(in.GetJoinAs()); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
