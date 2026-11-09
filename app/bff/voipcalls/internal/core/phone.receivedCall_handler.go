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

// PhoneReceivedCall: the first device of the callee to get the call makes
// the caller's phone ring back (tdesktop calls_call.cpp:789-800,
// phoneCallWaiting.receive_date).
func (c *VoipCallsCore) PhoneReceivedCall(in *mtproto.TLPhoneReceivedCall) (*mtproto.Bool, error) {
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if role != phonecalls.Participant {
		return nil, mtproto.ErrCallPeerInvalid
	}
	if call.State == phonecalls.StateDiscarded {
		return nil, mtproto.ErrCallAlreadyDeclined
	}
	// First the record, then the state: an accept that follows sees this
	// device among those that ring, one that came before is seen below.
	if err := c.svcCtx.Dao.Store.MarkReceived(c.ctx, call.Id, c.MD.PermAuthKeyId); err != nil {
		c.Logger.Errorf("phone.receivedCall - error: %v", err)
	}
	updated, changed, err := c.svcCtx.Dao.Store.Received(c.ctx, call.Id)
	if err != nil {
		c.Logger.Errorf("phone.receivedCall - error: %v", err)
		return nil, err
	}
	// Answered or ended elsewhere: a device woken by a push rings from the
	// payload and no update will stop it, an error ends the ringing
	// (Telegram-iOS CallSessionManager.swift:759-773, Android
	// VoIPService.java:1212-1216). core.telegram.org/method/phone.receivedCall
	// has only this error for it.
	if updated.State != phonecalls.StateRequested && updated.ParticipantAuthKeyId != c.MD.PermAuthKeyId {
		return nil, mtproto.ErrCallAlreadyDeclined
	}
	if changed {
		c.toAuthIfOnline(updated.AdminId, updated.AdminAuthKeyId,
			c.phoneCallUpdates(updated.AdminId, updated.TL(phonecalls.Admin, nil), updated.ParticipantId))
	}
	return mtproto.BoolTrue, nil
}
