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
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
)

// PhoneDiscardCall ends the call. A discard that lost the race to another
// device of the callee gets CALL_ALREADY_ACCEPTED (the error of
// core.telegram.org/method/phone.discardCall); a repeat answers as the first
// did: the clients read the update in the answer for need_rating
// (Telegram-iOS CallSessionManager.swift:1757-1799).
func (c *VoipCallsCore) PhoneDiscardCall(in *mtproto.TLPhoneDiscardCall) (*mtproto.Updates, error) {
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if role == phonecalls.Participant && call.ParticipantAuthKeyId != 0 && call.ParticipantAuthKeyId != c.MD.PermAuthKeyId {
		return nil, mtproto.ErrCallAlreadyAccepted
	}
	if role == phonecalls.Admin && call.AdminAuthKeyId != c.MD.PermAuthKeyId {
		return nil, mtproto.ErrCallPeerInvalid
	}
	wasRequested := call.State == phonecalls.StateRequested
	if call.State != phonecalls.StateDiscarded {
		updated, won, err := c.svcCtx.Dao.Store.Discard(c.ctx, call.Id, phonecalls.ReasonOf(in.GetReason()))
		if err != nil {
			c.Logger.Errorf("phone.discardCall - error: %v", err)
			return nil, err
		}
		call = updated
		if won {
			c.finish(call, wasRequested, role, c.MD.PermAuthKeyId)
		}
	}
	other := call.ParticipantId
	if role == phonecalls.Participant {
		other = call.AdminId
	}
	return c.phoneCallUpdates(c.MD.UserId, call.TL(role, nil), other), nil
}
