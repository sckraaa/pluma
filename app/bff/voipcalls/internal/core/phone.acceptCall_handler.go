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

// PhoneAcceptCall: the first device to accept wins; the others get
// phoneCallDiscarded (core.telegram.org/api/end-to-end/voice-calls, step 5).
func (c *VoipCallsCore) PhoneAcceptCall(in *mtproto.TLPhoneAcceptCall) (*mtproto.Phone_PhoneCall, error) {
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if role != phonecalls.Participant {
		return nil, mtproto.ErrCallPeerInvalid
	}
	if len(in.GetGB()) == 0 || len(in.GetGB()) > maxKeyMaterial {
		return nil, mtproto.ErrCallPeerInvalid
	}
	if err := checkProtocol(in.GetProtocol()); err != nil {
		return nil, err
	}
	accept := phonecalls.ProtocolOf(in.GetProtocol())
	if _, ok := phonecalls.Negotiate(call.AdminProtocol, accept); !ok {
		return nil, mtproto.ErrCallProtocolCompatLayerInvalid
	}
	updated, won, err := c.svcCtx.Dao.Store.Accept(c.ctx, call.Id, c.MD.PermAuthKeyId, in.GetGB(), accept)
	if err != nil {
		c.Logger.Errorf("phone.acceptCall - error: %v", err)
		return nil, err
	}
	switch updated.State {
	case phonecalls.StateDiscarded:
		return nil, mtproto.ErrCallAlreadyDeclined
	case phonecalls.StateAccepted:
		if !won && updated.ParticipantAuthKeyId != c.MD.PermAuthKeyId {
			return nil, mtproto.ErrCallAlreadyAccepted
		}
	default:
		return nil, mtproto.ErrCallAlreadyAccepted
	}
	if won {
		// A copy, so that phoneCallDiscarded carries the call id and no
		// reason; tdesktop ends it as "ended by another device"
		// (calls_call.cpp:849-857).
		losers := *updated
		losers.State, losers.Reason, losers.Duration = phonecalls.StateDiscarded, phonecalls.ReasonNone, 0
		c.syncNotMe(updated.ParticipantId, c.MD.PermAuthKeyId,
			c.phoneCallUpdates(updated.ParticipantId, losers.TL(phonecalls.Participant, nil), updated.AdminId))
		c.markRungOffline(updated, c.MD.PermAuthKeyId)
		c.toAuth(updated.Id, updated.AdminId, updated.AdminAuthKeyId,
			c.phoneCallUpdates(updated.AdminId, updated.TL(phonecalls.Admin, nil), updated.ParticipantId))
	}
	// tdesktop requires phoneCallWaiting in the answer (calls_call.cpp:498).
	return mtproto.MakeTLPhonePhoneCall(&mtproto.Phone_PhoneCall{
		PhoneCall: updated.TL(phonecalls.Participant, nil),
		Users:     c.usersOf(c.MD.UserId, updated.AdminId),
	}).To_Phone_PhoneCall(), nil
}
