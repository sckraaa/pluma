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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
)

// PhoneEditGroupCallParticipant
// phone.editGroupCallParticipant#a5273abf flags:# call:InputGroupCall participant:InputPeer muted:flags.0?Bool volume:flags.1?int raise_hand:flags.2?Bool video_stopped:flags.3?Bool video_paused:flags.4?Bool presentation_paused:flags.5?Bool = Updates;
//
// One change of a participant: tdesktop sends one flag a request
// (calls_group_call.cpp:3915-3945), Android likewise (VoIPService.java:
// 3797-3830). What one may change is groupcalls.ApplyEdit; a change of the
// row goes to everyone in the call with the current version, the account's
// own setting of another participant only to itself. A camera that goes on
// beyond the limit is GROUPCALL_VIDEO_TOO_MUCH, on which Android reloads
// the call (VoIPService.java:3840).
func (c *GroupCallsCore) PhoneEditGroupCallParticipant(in *mtproto.TLPhoneEditGroupCallParticipant) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	tp := mtproto.FromInputPeer2(c.MD.UserId, in.GetParticipant())
	if !tp.IsUser() && !tp.IsChannel() {
		return nil, mtproto.ErrGroupCallParticipantInvalid
	}
	target := participantKey(tp)
	row, err := c.svcCtx.Dao.Store.ByPeer(c.ctx, call.Id, target)
	switch {
	case errors.Is(err, groupcalls.ErrNotFound) || (err == nil && !row.InCall):
		if tp.IsUser() && tp.PeerId == c.MD.UserId {
			return nil, mtproto.ErrGroupcallJoinMissing
		}
		return nil, mtproto.ErrGroupCallParticipantInvalid
	case err != nil:
		return nil, err
	}
	a, err := c.loadPeer(call.PeerType, call.PeerId, c.MD.UserId, row.UserId)
	if err != nil {
		return nil, err
	}
	if !a.canView(c.MD.UserId) {
		// One who lost the chat is out of its call too (the collector
		// takes him out).
		return nil, mtproto.ErrGroupcallForbidden
	}
	e := groupcalls.Edit{
		Muted:              boolOf(in.GetMuted()),
		RaiseHand:          boolOf(in.GetRaiseHand()),
		VideoStopped:       boolOf(in.GetVideoStopped()),
		VideoPaused:        boolOf(in.GetVideoPaused()),
		PresentationPaused: boolOf(in.GetPresentationPaused()),
	}
	if v := in.GetVolume(); v != nil {
		e.Volume = &v.Value
	}
	actor := groupcalls.Actor{UserId: c.MD.UserId, Manage: a.canManage(c.MD.UserId)}
	targetManages := a.canManage(row.UserId)
	prefs, err := c.svcCtx.Dao.Store.Prefs(c.ctx, call.Id, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	pref := prefs[target]
	limit := c.svcCtx.VideoLimit
	var res groupcalls.EditResult
	after, p, _, err := c.svcCtx.Dao.Store.Edit(c.ctx, call.Id, target, func(p *groupcalls.Participant) (bool, error) {
		next := pref
		cameraOn := e.VideoStopped != nil && !*e.VideoStopped && p.VideoStopped && p.UserId == c.MD.UserId
		if cameraOn && !p.CameraOn() && call.VideoCount >= limit {
			return false, errVideoTooMuch
		}
		r, err := groupcalls.ApplyEdit(p, &next, e, actor, targetManages, time.Now())
		if err != nil {
			return false, err
		}
		if cameraOn && !p.VideoJoined {
			// A camera may go on now that the call has room for one.
			p.VideoJoined, r.Row = true, true
		}
		res, pref = r, next
		return r.Row, nil
	})
	if err != nil {
		return nil, storeError(err)
	}
	if res.Pref {
		if err = c.svcCtx.Dao.Store.SetPref(c.ctx, call.Id, c.MD.UserId, target, pref); err != nil {
			return nil, err
		}
	}
	changes := []groupcalls.Change{{Participant: p}}
	if res.Row {
		if e.Muted != nil && p.UserId != c.MD.UserId && actor.Manage {
			c.setMuted(after, p)
		}
		if res.ScreenEnded {
			c.leaveScreen(after, p.UserId)
		}
		c.deliver(a, after, fanOut{changes: changes})
	}
	return c.answer(a, after, changes, nil, false), nil
}

func boolOf(b *mtproto.Bool) *bool {
	if b == nil {
		return nil
	}
	v := mtproto.FromBool(b)
	return &v
}
