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

// PhoneConfirmCall: g_a and the fingerprint go to the device that accepted,
// the call starts with the connections of the call. tdesktop requires
// phoneCall in the answer (calls_call.cpp:985).
func (c *VoipCallsCore) PhoneConfirmCall(in *mtproto.TLPhoneConfirmCall) (*mtproto.Phone_PhoneCall, error) {
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if role != phonecalls.Admin || call.AdminAuthKeyId != c.MD.PermAuthKeyId {
		return nil, mtproto.ErrCallPeerInvalid
	}
	if len(in.GetGA()) == 0 || len(in.GetGA()) > maxKeyMaterial {
		return nil, mtproto.ErrCallPeerInvalid
	}
	if err := checkProtocol(in.GetProtocol()); err != nil {
		return nil, err
	}
	switch call.State {
	case phonecalls.StateDiscarded:
		return nil, mtproto.ErrCallAlreadyDeclined
	case phonecalls.StateAccepted:
	case phonecalls.StateActive:
		// A repeat of the same confirmation.
		if call.KeyFingerprint == in.GetKeyFingerprint() {
			if conns := c.connections(call); len(conns) > 0 {
				return c.activeAnswer(call, conns), nil
			}
			return nil, errCallNotActive
		}
		return nil, mtproto.ErrCallPeerInvalid
	default:
		return nil, mtproto.ErrCallPeerInvalid
	}
	final, ok := phonecalls.Negotiate(phonecalls.ProtocolOf(in.GetProtocol()), call.AcceptProtocol)
	if !ok {
		return nil, mtproto.ErrCallProtocolCompatLayerInvalid
	}
	// Telegram-iOS takes connections.first! of phoneCall
	// (CallSessionManager.swift:1226): with no server reachable the call
	// cannot go on.
	conns := c.connections(call)
	if len(conns) == 0 {
		c.Logger.Errorf("phone.confirmCall - no ICE server of call %d resolves", call.Id)
		if ended, won, err := c.svcCtx.Dao.Store.Discard(c.ctx, call.Id, phonecalls.ReasonDisconnect); err != nil {
			c.Logger.Errorf("phone.confirmCall - error: %v", err)
		} else if won {
			c.finish(ended, false, phonecalls.Admin, c.MD.PermAuthKeyId)
		}
		return nil, mtproto.ErrCallAlreadyDeclined
	}
	updated, won, err := c.svcCtx.Dao.Store.Confirm(c.ctx, call.Id, in.GetGA(), in.GetKeyFingerprint(), final)
	if err != nil {
		c.Logger.Errorf("phone.confirmCall - error: %v", err)
		return nil, err
	}
	switch updated.State {
	case phonecalls.StateDiscarded:
		return nil, mtproto.ErrCallAlreadyDeclined
	case phonecalls.StateActive:
		if !won && updated.KeyFingerprint != in.GetKeyFingerprint() {
			return nil, mtproto.ErrCallPeerInvalid
		}
	default:
		return nil, mtproto.ErrCallPeerInvalid
	}
	if won {
		c.toAuth(updated.ParticipantId, updated.ParticipantAuthKeyId,
			c.phoneCallUpdates(updated.ParticipantId, updated.TL(phonecalls.Participant, conns), updated.AdminId))
	}
	return c.activeAnswer(updated, conns), nil
}

func (c *VoipCallsCore) connections(call *phonecalls.Call) []*mtproto.PhoneConnection {
	return c.svcCtx.Ice.Connections(c.ctx, call.Id, phonecalls.MaxActive)
}

func (c *VoipCallsCore) activeAnswer(call *phonecalls.Call, conns []*mtproto.PhoneConnection) *mtproto.Phone_PhoneCall {
	return mtproto.MakeTLPhonePhoneCall(&mtproto.Phone_PhoneCall{
		PhoneCall: call.TL(phonecalls.Admin, conns),
		Users:     c.usersOf(c.MD.UserId, call.ParticipantId),
	}).To_Phone_PhoneCall()
}
