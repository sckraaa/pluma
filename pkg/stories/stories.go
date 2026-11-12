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

// Package stories keeps the stories of users and channels (task 084,
// ADR 0054): the tables of migrate-20261114-stories.sql and the rules that
// do not depend on who asks. The stories service (app/bff/stories) owns the
// writes; the services of users and of channels read from here what their
// objects carry (user.stories_max_id, channel.stories_max_id,
// channel.stories_hidden).
package stories

import (
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
)

// The limits of stories. Premium is open to everyone (ADR 0041), so they
// are the Premium ones the official clients fall back to when the server's
// config names none (Android MessagesController.java:1662-1709, 1781).
const (
	// MaxActive is the most stories of a peer in the feeds at once
	// (story_expiring_limit_premium).
	MaxActive = 100
	// MaxWeekly and MaxMonthly are the most stories a peer posts in seven
	// and in thirty days (stories_sent_weekly_limit_premium,
	// stories_sent_monthly_limit_premium).
	MaxWeekly  = 70
	MaxMonthly = 300
	// MaxCaption is the longest caption, in UTF-16 units as the clients
	// count it (story_caption_length_limit_premium).
	MaxCaption = 2048
	// MaxPinnedToTop is the most stories at the top of a profile
	// (stories_pinned_to_top_count_max).
	MaxPinnedToTop = 3
	// MaxSuggestedReactions is the most reaction areas of a story
	// (stories_suggested_reactions_limit_premium).
	MaxSuggestedReactions = 5
	// MaxMediaAreas bounds the areas of a story; Telegram's own bound is
	// not known.
	MaxMediaAreas = 30
	// MaxIds bounds the ids of one request.
	MaxIds = 100
	// MaxViewers bounds the viewers of a story that one list reads; with
	// the hundred accounts of a server every story stays far below it.
	MaxViewers = 5000
)

// DefaultPeriod is how long a story stays in the feeds when the request
// names no period, and Periods are the ones a request may name: 6, 12, 24
// and 48 hours (Android CaptionStory.java:63).
const DefaultPeriod = 86400

var Periods = []int32{6 * 3600, 12 * 3600, 24 * 3600, 48 * 3600}

// The stealth mode: what it hides and for how long (the periods of the
// clients' config, Android MessagesController.java:1691-1699).
const (
	StealthPast     = 5 * 60
	StealthFuture   = 25 * 60
	StealthCooldown = 60 * 60
)

var (
	// ErrTooMany: the peer has MaxActive stories in the feeds.
	ErrTooMany = errors.New("stories: too many active stories")
	// ErrCooldown: the stealth mode was used too recently.
	ErrCooldown = errors.New("stories: stealth mode is cooling down")
	// ErrNotPinned: a story named for the top of the profile is not in it.
	ErrNotPinned = errors.New("stories: story is not pinned")
)

// FloodError is the weekly or the monthly limit of a peer: no story until
// Until.
type FloodError struct {
	Monthly bool
	Until   int32
}

func (e *FloodError) Error() string {
	if e.Monthly {
		return fmt.Sprintf("stories: monthly limit until %d", e.Until)
	}
	return fmt.Sprintf("stories: weekly limit until %d", e.Until)
}

// Peer is whose stories: a user or a channel.
type Peer struct {
	Type int32
	Id   int64
}

// UserPeer and ChannelPeer make a Peer.
func UserPeer(id int64) Peer    { return Peer{Type: mtproto.PEER_USER, Id: id} }
func ChannelPeer(id int64) Peer { return Peer{Type: mtproto.PEER_CHANNEL, Id: id} }

// IsUser tells the stories of a user from those of a channel.
func (p Peer) IsUser() bool { return p.Type == mtproto.PEER_USER }

// ToPeer is the peer of the schema.
func (p Peer) ToPeer() *mtproto.Peer {
	if p.IsUser() {
		return mtproto.MakePeerUser(p.Id)
	}
	return mtproto.MakePeerChannel(p.Id)
}

// Story is one story as it is stored.
type Story struct {
	Peer     Peer
	Id       int32
	RandomId int64
	// SenderId is the user who posted it: the owner, or an admin of the
	// channel.
	SenderId   int64
	Date       int32
	ExpireDate int32
	Pinned     bool
	// PinnedToTop is its place at the top of the profile, from 1; 0 for a
	// story that is not there.
	PinnedToTop int32
	Noforwards  bool
	Edited      bool
	// Privacy is who sees a user's story; a channel's is for everyone who
	// sees the channel and has none.
	Privacy    []*mtproto.PrivacyRule
	Caption    string
	Entities   []*mtproto.MessageEntity
	Media      *mtproto.MessageMedia
	MediaAreas []*mtproto.MediaArea
	// FwdFrom is the header of a repost, and FwdPeer with FwdStoryId the
	// story it reposts, kept also when the header hides its author.
	FwdFrom       *mtproto.StoryFwdHeader
	FwdPeer       Peer
	FwdStoryId    int32
	ForwardsCount int32
	// Music is the track of the story (storyItem.music, layer 233), nil
	// for none.
	Music *mtproto.Document
	// Mentions are the users the caption mentions, who get a message with
	// the story (messageMediaStory with via_mention).
	Mentions []int64
}

// Active tells whether the story is in the feeds at now.
func (s *Story) Active(now int32) bool {
	return s.ExpireDate > now
}

// View is one viewer of a story, with the reaction it left ("" for none,
// else the key of pkg/reactions).
type View struct {
	ViewerId     int64
	Date         int32
	Reaction     string
	ReactionDate int32
}

// ReactionCount is how many viewers left one reaction.
type ReactionCount struct {
	Reaction string
	Count    int32
}

// Stats are the counters of a story.
type Stats struct {
	Views     int32
	Reactions int32
	ByKind    []ReactionCount
	// Recent are the last viewers, newest first, three at most.
	Recent []int64
}

// Read is up to which story of a peer a user has read.
type Read struct {
	Peer  Peer
	MaxId int32
}

// Stealth is the stealth mode of a user: until when its views are hidden
// and until when it cannot be turned on again; 0 for neither.
type Stealth struct {
	ActiveUntil   int32
	CooldownUntil int32
}

// Active tells whether views are hidden at now.
func (s Stealth) Active(now int32) bool {
	return s.ActiveUntil > now
}

// ValidPeriod tells whether a request may name the period.
func ValidPeriod(period int32) bool {
	for _, p := range Periods {
		if p == period {
			return true
		}
	}
	return false
}
