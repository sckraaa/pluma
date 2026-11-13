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

// PhoneEditGroupCallTitle
// phone.editGroupCallTitle#1ca6ac0a call:InputGroupCall title:string = Updates;
//
// Renames the call, by one who manages it; the new title goes to the
// members with updateGroupCall.
func (c *GroupCallsCore) PhoneEditGroupCallTitle(in *mtproto.TLPhoneEditGroupCallTitle) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canManage(c.MD.UserId) {
		return nil, mtproto.ErrChatAdminRequired
	}
	after, changed, err := c.svcCtx.Dao.Store.SetTitle(c.ctx, call.Id, cutTitle(in.GetTitle()))
	if err != nil {
		return nil, storeError(err)
	}
	if changed {
		c.deliver(a, after, fanOut{call: true})
	}
	return c.answer(a, after, nil, nil, true), nil
}
