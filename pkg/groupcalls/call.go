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

// Package groupcalls is the state of the group calls of chats and channels
// (task 093, ADR 0059): the calls, their participants with the version the
// clients follow, what each viewer set for himself and the subscriptions
// to scheduled calls. The media are the SFU's; nothing of them is stored
// here.
package groupcalls

import (
	"encoding/json"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// MaxParticipants is the participants a call takes at once (ADR 0059
	// §9: the whole audience).
	MaxParticipants = 100
	// DefaultVideoLimit is how many cameras are on at once
	// (groupCall.unmuted_video_limit, appConfig
	// groupcall_video_participants_max; ADR 0059 §9).
	DefaultVideoLimit = 30
	// TitleMax is the length of a call's title in characters; a longer one
	// is cut. Telegram's own limit is not verified.
	TitleMax = 64
	// VolumeDefault is 100%: the clients send 1..20000 and read a missing
	// volume as this (tdesktop calls_group_common.h:89-90).
	VolumeDefault = 10000
	VolumeMax     = 20000
)

// Posted bits of a call: which of its service messages are in the history
// (messageActionGroupCallScheduled, messageActionGroupCall at the start and
// with the duration at the end).
const (
	PostedScheduled = 1 << iota
	PostedStart
	PostedEnd
)

// Call is one group call of a chat or a channel.
type Call struct {
	Id         int64
	AccessHash int64
	PeerType   int32
	PeerId     int64
	CreatorId  int64
	RandomId   int32
	Title      string
	JoinMuted  bool
	// Version grows by one with every join and leave; never 0.
	Version int32
	// ParticipantsCount and VideoCount are read with the call: those in it
	// and those of them with the camera on.
	ParticipantsCount int32
	VideoCount        int32
	ScheduleDate      int32
	Date              int32
	// StartDate is when the call started; 0 while it is scheduled.
	StartDate int32
	EndDate   int32
	Duration  int32
	// Discarded: the call is over (groupCallDiscarded).
	Discarded bool
	// ListenerHash and SpeakerHash are the invite hashes of the links
	// (phone.exportGroupCallInvite); "" until the first export.
	ListenerHash string
	SpeakerHash  string
	// SfuNode is the SFU node the call's room is on; "" until the first join.
	SfuNode string
	Posted  int8
	// ChainHeight is the height of the last block of sub-chain 0 of the
	// call's end-to-end encryption, -1 before the first; ChainHash its
	// hash (ADR 0060 §2).
	ChainHeight int32
	ChainHash   [32]byte
	// BroadcastCount is the broadcasts of sub-chain 1 so far: the index of
	// the next one.
	BroadcastCount int32
}

// Scheduled reports whether the call waits for its start.
func (c *Call) Scheduled() bool { return !c.Discarded && c.StartDate == 0 }

// Peer is the chat or channel of the call.
func (c *Call) Peer() *mtproto.Peer {
	return PeerOf(c.PeerType, c.PeerId)
}

// Input is the inputGroupCall of the call.
func (c *Call) Input() *mtproto.InputGroupCall {
	return mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: c.Id, AccessHash: c.AccessHash}).To_InputGroupCall()
}

// CallView is what the groupCall object says of its viewer.
type CallView struct {
	// CanManage: the viewer may change the settings (can_change_join_muted).
	CanManage bool
	// Subscribed: the viewer waits for the scheduled call.
	Subscribed bool
	// VideoLimit is unmuted_video_limit.
	VideoLimit int32
}

// TL is the call as the viewer of v sees it: groupCallDiscarded once it is
// over. rtmp_stream, record_video_active and messages_enabled are never set
// (task 093, stage 1).
func (c *Call) TL(v CallView) *mtproto.GroupCall {
	if c.Discarded {
		return mtproto.MakeTLGroupCallDiscarded(&mtproto.GroupCall{
			Id: c.Id, AccessHash: c.AccessHash, Duration: c.Duration,
		}).To_GroupCall()
	}
	limit := v.VideoLimit
	if limit <= 0 {
		limit = DefaultVideoLimit
	}
	g := &mtproto.GroupCall{
		JoinMuted:          c.JoinMuted,
		CanChangeJoinMuted: v.CanManage,
		// can_start_video while a camera may still go on (Android takes it
		// as the right to send video at all, ChatObject.java:420).
		CanStartVideo:     c.VideoCount < limit,
		Id:                c.Id,
		AccessHash:        c.AccessHash,
		ParticipantsCount: c.ParticipantsCount,
		UnmutedVideoCount: wrapperspb.Int32(c.VideoCount),
		UnmutedVideoLimit: limit,
		Version:           c.Version,
	}
	if c.Title != "" {
		g.Title = wrapperspb.String(c.Title)
	}
	if c.Scheduled() && c.ScheduleDate != 0 {
		g.ScheduleDate = wrapperspb.Int32(c.ScheduleDate)
		g.ScheduleStartSubscribed = v.Subscribed
	}
	return mtproto.MakeTLGroupCall(g).To_GroupCall()
}

// Media is a participant's camera or shared screen: the SFU endpoint of
// the connection and the source groups of the join params.
type Media struct {
	Endpoint string                    `json:"endpoint"`
	Groups   []tgcallsjson.SourceGroup `json:"groups"`
}

// marshalMedia is the column form of m: "" for none.
func marshalMedia(m *Media) (string, error) {
	if m == nil {
		return "", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func unmarshalMedia(s string) (*Media, error) {
	if s == "" {
		return nil, nil
	}
	m := new(Media)
	if err := json.Unmarshal([]byte(s), m); err != nil {
		return nil, err
	}
	return m, nil
}

// Participant is one peer of a call, in it or gone.
type Participant struct {
	CallId int64
	// PeerType and PeerId are the peer of groupCallParticipant: the account
	// itself, as joining as a channel is not offered (ADR 0060).
	PeerType int32
	PeerId   int64
	// UserId is the account; it is in a call once.
	UserId    int64
	AuthKeyId int64
	InCall    bool
	// Source is the audio SSRC of the main connection; 0 out of the call.
	Source uint32
	// PresentationSource is the sound SSRC of the shared screen; 0 for none.
	PresentationSource uint32
	Muted              bool
	CanSelfUnmute      bool
	// Volume set by an admin for everyone; 0 for the default.
	Volume          int32
	RaiseHandRating int64
	// VideoJoined: the participant may send video (it joined while a
	// camera could go on); tdesktop turns its camera and screen off on a
	// self row without it (calls_group_call.cpp:3514-3520).
	VideoJoined  bool
	VideoStopped bool
	VideoPaused  bool
	Video        *Media
	Presentation *Media
	// PresentationPaused: the screen is paused.
	PresentationPaused bool
	Date               int32
	ActiveDate         int32
	// SeenAt is when the SFU last held the connection (unix ms).
	SeenAt int64
}

// PeerKey names a participant within a call.
type PeerKey struct {
	Type int32
	Id   int64
}

// Key is the participant's PeerKey.
func (p *Participant) Key() PeerKey { return PeerKey{p.PeerType, p.PeerId} }

// Peer is the peer the participant joined as.
func (p *Participant) Peer() *mtproto.Peer { return PeerOf(p.PeerType, p.PeerId) }

// CameraOn reports whether the camera's video is sent.
func (p *Participant) CameraOn() bool {
	return p.InCall && p.VideoJoined && !p.VideoStopped && p.Video != nil && len(p.Video.Groups) > 0
}

// AdminMuted reports whether an admin took the word: muted without the
// right to unmute.
func (p *Participant) AdminMuted() bool { return p.Muted && !p.CanSelfUnmute }

// Pref is what a viewer set for a participant for himself.
type Pref struct {
	Muted  bool
	Volume int32
}

// ParticipantView is how a participant object is made for its viewer.
type ParticipantView struct {
	// Viewer is the account the object goes to.
	Viewer int64
	// Min leaves out what only the viewer knows (muted_by_you, his
	// volume): the clients keep their own (tdesktop data_group_call.cpp:
	// 742-752). The viewer's own row is never min.
	Min  bool
	Pref Pref
	// JustJoined and Versioned mark a join; Left a leave (the version
	// grew by one with either).
	JustJoined bool
	Versioned  bool
}

// TL is the participant as v makes it.
func (p *Participant) TL(v ParticipantView) *mtproto.GroupCallParticipant {
	self := p.UserId == v.Viewer
	min := v.Min && !self
	g := &mtproto.GroupCallParticipant{
		Muted:         p.Muted,
		Left:          !p.InCall,
		CanSelfUnmute: p.CanSelfUnmute,
		JustJoined:    v.JustJoined,
		Versioned:     v.Versioned,
		Min:           min,
		Self:          self,
		VideoJoined:   p.VideoJoined,
		Peer:          p.Peer(),
		Date:          p.Date,
		Source:        int32(p.Source),
	}
	if p.ActiveDate != 0 {
		g.ActiveDate = wrapperspb.Int32(p.ActiveDate)
	}
	switch {
	case !min && !self && v.Pref.Volume > 0:
		g.Volume = wrapperspb.Int32(v.Pref.Volume)
	case p.Volume > 0:
		g.Volume = wrapperspb.Int32(p.Volume)
		g.VolumeByAdmin = true
	}
	if !min && !self {
		g.MutedByYou = v.Pref.Muted
	}
	if p.InCall && p.RaiseHandRating != 0 {
		g.RaiseHandRating = wrapperspb.Int64(p.RaiseHandRating)
	}
	if p.CameraOn() {
		g.Video = videoTL(p.Video, p.VideoPaused, 0)
	}
	if p.InCall && p.Presentation != nil {
		g.Presentation = videoTL(p.Presentation, p.PresentationPaused, p.PresentationSource)
	}
	return mtproto.MakeTLGroupCallParticipant(g).To_GroupCallParticipant()
}

// videoTL is a groupCallParticipantVideo; audioSource is the sound of a
// shared screen (tdesktop GetAdditionalAudioSsrc), 0 for none.
func videoTL(m *Media, paused bool, audioSource uint32) *mtproto.GroupCallParticipantVideo {
	groups := make([]*mtproto.GroupCallParticipantVideoSourceGroup, 0, len(m.Groups))
	for _, g := range m.Groups {
		sources := make([]int32, 0, len(g.Sources))
		for _, s := range g.Sources {
			sources = append(sources, int32(s))
		}
		groups = append(groups, mtproto.MakeTLGroupCallParticipantVideoSourceGroup(&mtproto.GroupCallParticipantVideoSourceGroup{
			Semantics: g.Semantics,
			Sources:   sources,
		}).To_GroupCallParticipantVideoSourceGroup())
	}
	v := &mtproto.GroupCallParticipantVideo{Paused: paused, Endpoint: m.Endpoint, SourceGroups: groups}
	if audioSource != 0 {
		v.AudioSource = wrapperspb.Int32(int32(audioSource))
	}
	return mtproto.MakeTLGroupCallParticipantVideo(v).To_GroupCallParticipantVideo()
}

// PeerOf is the peer of a type and an id.
func PeerOf(peerType int32, id int64) *mtproto.Peer {
	switch peerType {
	case mtproto.PEER_CHAT:
		return mtproto.MakePeerChat(id)
	case mtproto.PEER_CHANNEL:
		return mtproto.MakePeerChannel(id)
	}
	return mtproto.MakePeerUser(id)
}

// MediaOf is the camera or screen media of join params: the source groups
// the client sends and the endpoint the SFU gave the connection; nil
// without video.
func MediaOf(req *tgcallsjson.JoinRequest, endpoint string) *Media {
	if req == nil || !req.HasVideo() {
		return nil
	}
	return &Media{Endpoint: endpoint, Groups: req.SourceGroups}
}
