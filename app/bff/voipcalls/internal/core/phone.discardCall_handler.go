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
	"time"

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
	if call.State != phonecalls.StateDiscarded && role == phonecalls.Participant &&
		in.GetReason().GetPredicateName() == mtproto.Predicate_phoneCallDiscardReasonBusy {
		repeated, err := c.busyOfThisCall(call)
		if err != nil {
			c.Logger.Errorf("phone.discardCall - error: %v", err)
			return nil, err
		}
		if repeated {
			return c.noUpdates(), nil
		}
	}
	wasRequested := call.State == phonecalls.StateRequested
	if call.State != phonecalls.StateDiscarded {
		updated, won, err := c.discard(call.Id, role, in)
		if err != nil {
			c.Logger.Errorf("phone.discardCall - error: %v", err)
			return nil, err
		}
		if !won && updated.State != phonecalls.StateDiscarded && role == phonecalls.Participant &&
			updated.ParticipantAuthKeyId != c.MD.PermAuthKeyId {
			return nil, mtproto.ErrCallAlreadyAccepted
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

// discard ends the call for the side that asked: a device of the participant
// loses to one that accepted meanwhile.
func (c *VoipCallsCore) discard(id int64, role phonecalls.Role, in *mtproto.TLPhoneDiscardCall) (*phonecalls.Call, bool, error) {
	reason := phonecalls.ReasonOf(in.GetReason())
	if role == phonecalls.Participant {
		return c.svcCtx.Dao.Store.DiscardAsParticipant(c.ctx, id, c.MD.PermAuthKeyId, reason, in.GetVideo())
	}
	return c.svcCtx.Dao.Store.Discard(c.ctx, id, reason, in.GetVideo())
}

// busyOfThisCall tells whether a Busy from this device of the participant is
// about the call itself: a device that answered it cannot be busy with
// something else, and Android declines any repeated phoneCallRequested
// (Delivery.Repeated).
func (c *VoipCallsCore) busyOfThisCall(call *phonecalls.Call) (bool, error) {
	if call.ParticipantAuthKeyId == c.MD.PermAuthKeyId {
		return true, nil
	}
	d, err := c.svcCtx.Dao.Store.DeliveryOf(c.ctx, call.Id, c.MD.PermAuthKeyId)
	if err != nil {
		return false, err
	}
	return d.Repeated(), nil
}

// noUpdates is the answer that changes nothing: Android applies the answer
// of its automatic Busy (MessagesController.java:20390-20394), and a
// phoneCallDiscarded in it would end the call on the device.
func (c *VoipCallsCore) noUpdates() *mtproto.Updates {
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates()
}
