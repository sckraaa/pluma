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
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

func ptr[T any](v T) *T { return &v }

func TestJoinState(t *testing.T) {
	open, muted := &Call{}, &Call{JoinMuted: true}
	taken := &Participant{Muted: true, CanSelfUnmute: false}
	// An admin gave the word in a join_muted call; the participant is still
	// in it, or has left.
	given := &Participant{InCall: true, Muted: true, CanSelfUnmute: true}
	givenGone := &Participant{InCall: false, Muted: true, CanSelfUnmute: true}
	for _, tc := range []struct {
		name                      string
		call                      *Call
		prev                      *Participant
		manage, speaker, wantMute bool
		muted, canSelfUnmute      bool
	}{
		{"open call", open, nil, false, false, false, false, true},
		{"open call, joins muted", open, nil, false, false, true, true, true},
		{"join_muted", muted, nil, false, false, false, true, false},
		{"join_muted, admin", muted, nil, true, false, false, false, true},
		{"join_muted, speaker link", muted, nil, false, true, false, false, true},
		{"word taken before leaving", open, taken, false, false, false, true, false},
		{"word taken, speaker link", open, taken, false, true, false, false, true},
		{"word given, joins again", muted, given, false, false, false, false, true},
		{"word given, joins again muted", muted, given, false, false, true, true, true},
		{"word given, comes back after leaving", muted, givenGone, false, false, false, true, false},
	} {
		m, c := JoinState(tc.call, tc.prev, tc.manage, tc.speaker, tc.wantMute)
		if m != tc.muted || c != tc.canSelfUnmute {
			t.Errorf("%s: muted %v can_self_unmute %v, want %v %v", tc.name, m, c, tc.muted, tc.canSelfUnmute)
		}
	}
}

func TestApplyEditSelf(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := &Participant{UserId: 1, InCall: true, Muted: true, CanSelfUnmute: false, VideoStopped: true}
	pref := &Pref{}
	if _, err := ApplyEdit(p, pref, Edit{Muted: ptr(false)}, Actor{UserId: 1}, false, now); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("unmute without the word: %v", err)
	}
	if _, err := ApplyEdit(p, pref, Edit{VideoStopped: ptr(false)}, Actor{UserId: 1}, false, now); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("camera without the word: %v", err)
	}
	r, err := ApplyEdit(p, pref, Edit{RaiseHand: ptr(true)}, Actor{UserId: 1}, false, now)
	if err != nil || !r.Row || p.RaiseHandRating != HandRating(now) {
		t.Fatalf("raise hand: %+v %v rating %d", r, err, p.RaiseHandRating)
	}
	p.CanSelfUnmute = true
	if r, err = ApplyEdit(p, pref, Edit{Muted: ptr(false)}, Actor{UserId: 1}, false, now); err != nil || !r.Row || p.Muted || p.RaiseHandRating != 0 {
		t.Fatalf("unmute with the word: %+v %v %+v", r, err, p)
	}
	if r, err = ApplyEdit(p, pref, Edit{VideoStopped: ptr(false)}, Actor{UserId: 1}, false, now); err != nil || !r.Row || p.VideoStopped {
		t.Fatalf("camera with the word: %+v %v", r, err)
	}
	if r, err = ApplyEdit(p, pref, Edit{VideoStopped: ptr(true), VideoPaused: ptr(true), PresentationPaused: ptr(true)}, Actor{UserId: 1}, false, now); err != nil || !r.Row || !p.VideoStopped || !p.VideoPaused || !p.PresentationPaused {
		t.Fatalf("video flags: %+v %v", r, err)
	}
	if r, err = ApplyEdit(p, pref, Edit{VideoStopped: ptr(true)}, Actor{UserId: 1}, false, now); err != nil || r.Row {
		t.Fatalf("no change: %+v %v", r, err)
	}
}

func TestApplyEditOthers(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	p := &Participant{UserId: 2, InCall: true, CanSelfUnmute: true, RaiseHandRating: 7}
	pref := &Pref{}
	// A member mutes and turns down another for himself only.
	r, err := ApplyEdit(p, pref, Edit{Muted: ptr(true), Volume: ptr(int32(5000))}, Actor{UserId: 1}, false, now)
	if err != nil || r.Row || !r.Pref || !pref.Muted || pref.Volume != 5000 || p.Muted {
		t.Fatalf("personal mute: %+v %v %+v %+v", r, err, pref, p)
	}
	if _, err = ApplyEdit(p, pref, Edit{RaiseHand: ptr(false)}, Actor{UserId: 1}, false, now); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("a member lowers a hand: %v", err)
	}
	if _, err = ApplyEdit(p, pref, Edit{VideoStopped: ptr(true)}, Actor{UserId: 1, Manage: true}, false, now); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("an admin stops a camera: %v", err)
	}
	if _, err = ApplyEdit(p, pref, Edit{Volume: ptr(int32(0))}, Actor{UserId: 1}, false, now); !errors.Is(err, mtproto.ErrGroupCallParticipantInvalid) {
		t.Fatalf("volume 0: %v", err)
	}
	// An admin takes the word: muted, no right to unmute, the hand down,
	// the camera off and the screen ended.
	p.Presentation, p.PresentationSource = &Media{Endpoint: "s"}, 9
	if r, err = ApplyEdit(p, pref, Edit{Muted: ptr(true)}, Actor{UserId: 1, Manage: true}, false, now); err != nil || !r.Row || !p.Muted || p.CanSelfUnmute || p.RaiseHandRating != 0 {
		t.Fatalf("admin mute: %+v %v %+v", r, err, p)
	}
	if !r.ScreenEnded || !p.VideoStopped || p.Presentation != nil || p.PresentationSource != 0 {
		t.Fatalf("admin mute keeps camera or screen: %+v %+v", r, p)
	}
	// And gives it back: the participant unmutes himself.
	if r, err = ApplyEdit(p, pref, Edit{Muted: ptr(false)}, Actor{UserId: 1, Manage: true}, false, now); err != nil || !r.Row || !p.Muted || !p.CanSelfUnmute {
		t.Fatalf("admin unmute: %+v %v %+v", r, err, p)
	}
	// From another admin the word cannot be taken.
	q := &Participant{UserId: 3, InCall: true, CanSelfUnmute: true}
	if r, err = ApplyEdit(q, &Pref{}, Edit{Muted: ptr(true)}, Actor{UserId: 1, Manage: true}, true, now); err != nil || !q.Muted || !q.CanSelfUnmute || q.VideoStopped || r.ScreenEnded {
		t.Fatalf("admin mutes an admin: %v %+v", err, q)
	}
	if r, err = ApplyEdit(p, pref, Edit{Volume: ptr(int32(15000))}, Actor{UserId: 1, Manage: true}, false, now); err != nil || !r.Row || p.Volume != 15000 {
		t.Fatalf("admin volume: %+v %v", r, err)
	}
}

func TestHandRatingOrder(t *testing.T) {
	early, late := HandRating(time.Unix(100, 0)), HandRating(time.Unix(200, 0))
	if early <= late || late <= 0 {
		t.Fatalf("an earlier hand must rate higher: %d %d", early, late)
	}
}

func TestParticipantTL(t *testing.T) {
	cam := &Media{Endpoint: "e1", Groups: []tgcallsjson.SourceGroup{{Semantics: "SIM", Sources: []uint32{2, 4, 0xfffffff0}}}}
	p := &Participant{
		PeerType: mtproto.PEER_USER, PeerId: 2, UserId: 2, InCall: true, Source: 0x80000001, Muted: true, CanSelfUnmute: true,
		Volume: 12000, VideoJoined: true, Video: cam, Date: 5, ActiveDate: 6, RaiseHandRating: 9,
		Presentation: &Media{Endpoint: "e2", Groups: []tgcallsjson.SourceGroup{{Semantics: "SIM", Sources: []uint32{20}}}}, PresentationSource: 19,
	}
	other := p.TL(ParticipantView{Viewer: 1, Min: true, Pref: Pref{Muted: true, Volume: 300}, JustJoined: true, Versioned: true})
	if !other.Min || other.Self || other.MutedByYou || other.GetVolume().GetValue() != 12000 || !other.VolumeByAdmin {
		t.Errorf("min object: %+v", other)
	}
	if !other.JustJoined || !other.Versioned || other.Source != int32(-2147483647) || other.Left {
		t.Errorf("flags: %+v", other)
	}
	if v := other.GetVideo(); v.GetEndpoint() != "e1" || v.GetSourceGroups()[0].GetSources()[2] != int32(-16) || v.GetAudioSource() != nil {
		t.Errorf("video: %+v", v)
	}
	if v := other.GetPresentation(); v.GetEndpoint() != "e2" || v.GetAudioSource().GetValue() != 19 {
		t.Errorf("presentation: %+v", v)
	}
	mine := p.TL(ParticipantView{Viewer: 1, Pref: Pref{Muted: true, Volume: 300}})
	if mine.Min || !mine.MutedByYou || mine.GetVolume().GetValue() != 300 || mine.VolumeByAdmin {
		t.Errorf("the viewer's own setting: %+v", mine)
	}
	self := p.TL(ParticipantView{Viewer: 2, Min: true})
	if !self.Self || self.Min {
		t.Errorf("own row: %+v", self)
	}
	p.VideoStopped = true
	if p.TL(ParticipantView{Viewer: 1}).GetVideo() != nil {
		t.Error("a stopped camera is shown")
	}
	p.InCall = false
	if left := p.TL(ParticipantView{Viewer: 1}); !left.Left || left.GetRaiseHandRating() != nil || left.GetPresentation() != nil {
		t.Errorf("left: %+v", left)
	}
}

func TestCallTL(t *testing.T) {
	c := &Call{Id: 1, AccessHash: 2, Version: 3, ParticipantsCount: 4, VideoCount: 30, Title: "t", ScheduleDate: 9}
	g := c.TL(CallView{CanManage: true, Subscribed: true})
	if g.GetPredicateName() != mtproto.Predicate_groupCall || g.CanStartVideo || g.UnmutedVideoLimit != DefaultVideoLimit || !g.CanChangeJoinMuted {
		t.Errorf("call: %+v", g)
	}
	if g.GetScheduleDate().GetValue() != 9 || !g.ScheduleStartSubscribed || g.GetTitle().GetValue() != "t" || g.Version != 3 {
		t.Errorf("scheduled call: %+v", g)
	}
	c.StartDate = 10
	if g = c.TL(CallView{Subscribed: true}); g.GetScheduleDate() != nil || g.ScheduleStartSubscribed {
		t.Errorf("a started call is scheduled: %+v", g)
	}
	c.Discarded, c.Duration = true, 60
	if g = c.TL(CallView{}); g.GetPredicateName() != mtproto.Predicate_groupCallDiscarded || g.Duration != 60 {
		t.Errorf("discarded: %+v", g)
	}
	if st := StateOf(c); st.Active || st.Call != nil {
		t.Errorf("state of a discarded call: %+v", st)
	}
}

func TestSweep(t *testing.T) {
	now := time.Unix(1000, 0)
	in := []*Participant{
		{UserId: 1, InCall: true, SeenAt: now.Add(-time.Minute).UnixMilli()},
		{UserId: 2, InCall: true, SeenAt: now.Add(-time.Minute).UnixMilli()},
		{UserId: 3, InCall: true, SeenAt: now.Add(-5 * time.Second).UnixMilli()},
		{UserId: 4, InCall: false},
	}
	seen, gone := Sweep(in, map[int64]bool{1: true}, now, 30*time.Second)
	if len(seen) != 1 || seen[0] != 1 || len(gone) != 1 || gone[0].UserId != 2 {
		t.Fatalf("seen %v gone %v", seen, gone)
	}
}

func TestOffset(t *testing.T) {
	if d, pt, id, err := parseOffset("5:2:77"); err != nil || d != 5 || pt != 2 || id != 77 {
		t.Fatalf("offset: %d %d %d %v", d, pt, id, err)
	}
	for _, s := range []string{"x", "1:2", "a:b:c", "1:2:3:4"} {
		if _, _, _, err := parseOffset(s); !errors.Is(err, ErrBadOffset) {
			t.Errorf("%q: %v", s, err)
		}
	}
}
