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

package groupcalls

import (
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Who may do what in a call (ADR 0059 §6). The creator of the chat and its
// admins with manage_call (api.tl chatAdminRights) manage the call: they
// start and end it, change its settings, take and give the word and set the
// volume of others for everyone. What anyone else does to another
// participant stays his own (muted_by_you, his volume). The rules are
// those tdesktop applies to its own copy of a participant before the
// answer comes (calls_group_call.cpp:2125-2145).

// JoinState is muted and can_self_unmute of a participant who joins.
// manage: he manages the call; speakerLink: he came by the link of a
// speaker (phone.exportGroupCallInvite can_self_unmute); wantMuted: the
// muted flag of phone.joinGroupCall; prev: his row from an earlier join,
// nil for none. A word an admin took stays taken across a leave, so that
// leaving is no way around it. One who joins again while still in the
// call (a new connection after a network change or an SFU restart, which
// the clients make by themselves: tdesktop calls_group_call.cpp:3746-3776
// when checkGroupCall misses its source) keeps the word he has, given by
// an admin included; join_muted is for those who come in.
func JoinState(call *Call, prev *Participant, manage, speakerLink, wantMuted bool) (muted, canSelfUnmute bool) {
	switch {
	case manage || speakerLink:
		return wantMuted, true
	case prev != nil && prev.AdminMuted():
		return true, false
	case prev != nil && prev.InCall && prev.CanSelfUnmute:
		return wantMuted, true
	case call.JoinMuted:
		return true, false
	}
	return wantMuted, true
}

// Edit is phone.editGroupCallParticipant: each field is set when the
// request has its flag.
type Edit struct {
	Muted              *bool
	Volume             *int32
	RaiseHand          *bool
	VideoStopped       *bool
	VideoPaused        *bool
	PresentationPaused *bool
}

// Actor is who edits a participant.
type Actor struct {
	UserId int64
	// Manage: the actor manages the call.
	Manage bool
}

// EditResult tells what an edit changed: the participant's row for
// everyone, the actor's own setting of the participant, or both.
type EditResult struct {
	Row  bool
	Pref bool
	// ScreenEnded: an admin took the word of one who shared his screen.
	ScreenEnded bool
}

// ApplyEdit applies e of actor to p (in the call) and to the actor's own
// setting pref of p. targetManages: p manages the call himself; an admin
// cannot take the word from another admin. now assigns the rating of a
// raised hand. The error is the one of the API.
func ApplyEdit(p *Participant, pref *Pref, e Edit, actor Actor, targetManages bool, now time.Time) (EditResult, error) {
	var r EditResult
	if e.Volume != nil && (*e.Volume < 1 || *e.Volume > VolumeMax) {
		return r, mtproto.ErrGroupCallParticipantInvalid
	}
	self := actor.UserId == p.UserId
	if self {
		if e.Muted != nil {
			if !*e.Muted && !p.CanSelfUnmute {
				return r, mtproto.ErrChatAdminRequired
			}
			if p.Muted != *e.Muted {
				p.Muted, r.Row = *e.Muted, true
			}
			if !*e.Muted && p.RaiseHandRating != 0 {
				// Speaking lowers the hand.
				p.RaiseHandRating, r.Row = 0, true
			}
		}
		if e.RaiseHand != nil {
			switch {
			case *e.RaiseHand && p.RaiseHandRating == 0:
				p.RaiseHandRating, r.Row = HandRating(now), true
			case !*e.RaiseHand && p.RaiseHandRating != 0:
				p.RaiseHandRating, r.Row = 0, true
			}
		}
		if e.VideoStopped != nil && p.VideoStopped != *e.VideoStopped {
			if !*e.VideoStopped && !p.CanSelfUnmute {
				// Without the word no camera either: tdesktop refuses it
				// itself (calls_group_call.cpp:2780, MutedNoCamera).
				return r, mtproto.ErrChatAdminRequired
			}
			p.VideoStopped, r.Row = *e.VideoStopped, true
		}
		if e.VideoPaused != nil && p.VideoPaused != *e.VideoPaused {
			p.VideoPaused, r.Row = *e.VideoPaused, true
		}
		if e.PresentationPaused != nil && p.PresentationPaused != *e.PresentationPaused {
			p.PresentationPaused, r.Row = *e.PresentationPaused, true
		}
		// One's own volume of oneself means nothing; the clients do not send
		// it.
		return r, nil
	}
	if e.VideoStopped != nil || e.VideoPaused != nil || e.PresentationPaused != nil {
		return r, mtproto.ErrChatAdminRequired
	}
	if !actor.Manage {
		if e.RaiseHand != nil {
			return r, mtproto.ErrChatAdminRequired
		}
		if e.Muted != nil && pref.Muted != *e.Muted {
			pref.Muted, r.Pref = *e.Muted, true
		}
		if e.Volume != nil && pref.Volume != *e.Volume {
			pref.Volume, r.Pref = *e.Volume, true
		}
		return r, nil
	}
	if e.Muted != nil {
		if *e.Muted {
			// The word is taken; an admin keeps the right to take it back.
			if !p.Muted || p.CanSelfUnmute != targetManages {
				p.Muted, p.CanSelfUnmute, r.Row = true, targetManages, true
			}
			if !p.CanSelfUnmute {
				// With the word go the camera and the shared screen, whose
				// sound would speak for him: tdesktop turns both off itself
				// when it is muted by an admin (calls_group_call.cpp:
				// 2315-2318). The caller ends the screen's connection.
				if !p.VideoStopped {
					p.VideoStopped, r.Row = true, true
				}
				if p.Presentation != nil || p.PresentationSource != 0 {
					p.Presentation, p.PresentationSource, p.PresentationPaused, r.Row = nil, 0, false, true
					r.ScreenEnded = true
				}
			}
		} else if !p.CanSelfUnmute {
			// The word is given: the participant unmutes himself.
			p.CanSelfUnmute, r.Row = true, true
		}
		if p.RaiseHandRating != 0 {
			p.RaiseHandRating, r.Row = 0, true
		}
	}
	if e.Volume != nil && p.Volume != *e.Volume {
		p.Volume, r.Row = *e.Volume, true
	}
	if e.RaiseHand != nil && !*e.RaiseHand && p.RaiseHandRating != 0 {
		p.RaiseHandRating, r.Row = 0, true
	}
	return r, nil
}

// handEpoch is far enough ahead that every rating stays positive.
const handEpoch = int64(1) << 52

// HandRating is the raise_hand_rating of a hand raised at now. The clients
// list raised hands by it, highest first (iOS GroupCalls.swift:1377-1423);
// an earlier hand gets a higher rating, so that hands are taken in the
// order they were raised. Which order Telegram gives is not verified.
func HandRating(now time.Time) int64 {
	return handEpoch - now.UnixMilli()
}
