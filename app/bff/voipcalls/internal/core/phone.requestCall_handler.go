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
	"errors"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	statuspb "github.com/teamgram/teamgram-server/app/service/status/status"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
)

// PhoneRequestCall: the privacy checks of the callee come first, then the
// call rings on all its devices (core.telegram.org/api/end-to-end/voice-calls,
// step 3).
func (c *VoipCallsCore) PhoneRequestCall(in *mtproto.TLPhoneRequestCall) (*mtproto.Phone_PhoneCall, error) {
	if c.svcCtx.Ice.Empty() {
		// Telegram-iOS takes connections.first! of phoneCall
		// (CallSessionManager.swift:1226): a call without servers cannot be
		// offered.
		return nil, errCallsNotAvailable
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetUserId())
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId == c.MD.UserId {
		return nil, mtproto.ErrUserIdInvalid
	}
	if len(in.GetGAHash()) != gAHashSize {
		return nil, mtproto.ErrCallProtocolFlagsInvalid
	}
	if err := checkProtocol(in.GetProtocol()); err != nil {
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{c.MD.UserId, peer.PeerId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId, peer.PeerId},
	})
	if err != nil {
		c.Logger.Errorf("phone.requestCall - error: %v", err)
		return nil, err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrInputUserDeactivated
	}
	callee, ok := users.GetImmutableUser(peer.PeerId)
	if !ok || callee.IsBot() {
		return nil, mtproto.ErrUserIdInvalid
	}
	if callee.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	blocked, err := c.svcCtx.Dao.UserClient.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{UserId: peer.PeerId, PeerUserId: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("phone.requestCall - error: %v", err)
		return nil, err
	}
	if mtproto.FromBool(blocked) {
		return nil, errBlocked
	}
	if !callee.CheckPrivacyFor(mtproto.PHONE_CALL, me) {
		return nil, mtproto.ErrUserPrivacyRestricted
	}
	id, err := positiveId()
	if err != nil {
		return nil, err
	}
	accessHash, err := positiveId()
	if err != nil {
		return nil, err
	}
	call, created, err := c.svcCtx.Dao.Store.Create(c.ctx, &phonecalls.Call{
		Id:             id,
		AccessHash:     accessHash,
		AdminId:        c.MD.UserId,
		ParticipantId:  peer.PeerId,
		AdminAuthKeyId: c.MD.PermAuthKeyId,
		RandomId:       in.GetRandomId(),
		Video:          in.GetVideo(),
		P2PAllowed:     callee.CheckPrivacyFor(mtproto.PHONE_P2P, me) && me.CheckPrivacyFor(mtproto.PHONE_P2P, callee),
		GAHash:         in.GetGAHash(),
		AdminProtocol:  phonecalls.ProtocolOf(in.GetProtocol()),
	})
	switch {
	case errors.Is(err, phonecalls.ErrDuplicate):
		return nil, mtproto.ErrRandomIdDuplicate
	case err != nil:
		c.Logger.Errorf("phone.requestCall - error: %v", err)
		return nil, err
	}
	if created {
		c.ring(call)
	}
	return mtproto.MakeTLPhonePhoneCall(&mtproto.Phone_PhoneCall{
		PhoneCall: call.TL(phonecalls.Admin, nil),
		Users:     c.usersOf(c.MD.UserId, peer.PeerId),
	}).To_Phone_PhoneCall(), nil
}

// ring records which devices of the callee the live update reaches, then
// sends it to all of them; the others learn of the call through a push and
// updates.getDifference. Android declines a second phoneCallRequested as
// busy (MessagesController.java:20390), hence the bookkeeping.
func (c *VoipCallsCore) ring(call *phonecalls.Call) {
	sessions, err := c.svcCtx.Dao.StatusClient.StatusGetUserOnlineSessions(c.ctx, &statuspb.TLStatusGetUserOnlineSessions{UserId: call.ParticipantId})
	if err != nil {
		c.Logger.Errorf("calls: online sessions of user %d: %v", call.ParticipantId, err)
	} else {
		for _, s := range sessions.GetUserSessions() {
			key := s.GetAuthKeyId()
			if s.GetPermAuthKeyId() != 0 {
				key = s.GetPermAuthKeyId()
			}
			if err := c.svcCtx.Dao.Store.MarkDelivered(c.ctx, call.Id, key); err != nil {
				c.Logger.Errorf("calls: delivery of call %d: %v", call.Id, err)
			}
		}
	}
	c.syncAll(call.ParticipantId, c.phoneCallUpdates(call.ParticipantId, call.TL(phonecalls.Participant, nil), call.AdminId))
}
