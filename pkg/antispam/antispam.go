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

// Package antispam holds the rules of the antispam of supergroups (task
// 082, ADR 0050): a supergroup's admin turns it on (channels.toggleAntiSpam),
// and from then on a message that breaks a rule is deleted by the antispam
// account, as Telegram's "aggressive anti-spam" does
// (core.telegram.org/api/antispam). The rules are deterministic and read
// only what the send transaction has: who sends, since when it is in the
// group, the message itself and how many groups it sent the same to.
package antispam

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
)

// UserId is the antispam account (teamgramd/deploy/sql/z_pluma-antispam-user.sql):
// the recent actions name it as the one who deleted a message, and the
// clients know such a deletion by it, telegram_antispam_user_id of the app
// config (Android ChannelAdminLogActivity.java:1659, tdesktop
// history_admin_log_inner.cpp:1322, iOS ChatRecentActionsControllerNode.swift:1410).
const UserId int64 = 1000001

// MembersMin is the fewest members of a supergroup that turns the antispam
// on: telegram_antispam_group_size_min of the app config, by which the
// clients lock their switch (Android ChatUsersActivity.java:1064-1090,
// tdesktop menu_antispam_validator.cpp:57-117, iOS
// ChannelAdminsController.swift:1057-1059, Web K
// chatAdministratorsSource.ts:37-46). Telegram's is 100, more than a server
// of about 100 accounts has in a group (ADR 0050).
const MembersMin = 10

// RulesVersion is the version of the rules below, kept with each deletion.
const RulesVersion = 1

// The thresholds of the rules (ADR 0050, p. 1.4-1.5).
const (
	// NewPeriod and NewMessages: a member is new in a group for its first
	// 72 hours there, while it has fewer than 5 messages kept there.
	NewPeriod   = 72 * 3600
	NewMessages = 5
	// MentionsMin is the fewest different users one message of a new
	// member mentions to be spam.
	MentionsMin = 5
	// CrossPostGroups is the fewest supergroups the same content goes to
	// within CrossPostWindow seconds to be spam.
	CrossPostGroups = 3
	CrossPostWindow = 600
	// CrossPostMinChars: a text alone takes part in the cross-post rule
	// from this many characters, so that short replies ("ok", "thanks")
	// sent to several groups are not spam; a link or a file always does.
	CrossPostMinChars = 30
)

// Rule is the rule a message broke; RuleNone for none.
type Rule int32

const (
	RuleNone Rule = 0
	// RuleLink: a new member posts a link or a link card.
	RuleLink Rule = 1
	// RuleForward: a new member forwards from another channel or group.
	RuleForward Rule = 2
	// RuleMentions: a new member mentions MentionsMin users or more.
	RuleMentions Rule = 3
	// RuleCrossPost: a checked member posts the same content to
	// CrossPostGroups supergroups within CrossPostWindow.
	RuleCrossPost Rule = 4
)

// Sender is the one who sends to a supergroup with the antispam on, as the
// send transaction reads it.
type Sender struct {
	// Exempt: the antispam does not check the sender: the creator or an
	// admin, a bot, a ghost (ADR 0048) or a service account, a message sent
	// as the group, or a member an admin vouched for by reporting a
	// deletion as wrong.
	Exempt bool
	// Joined is when the sender joined the group; Guest: it writes to a
	// discussion group without being in it.
	Joined int64
	Guest  bool
	// Kept is how many of its messages, up to NewMessages, the group has.
	Kept int
}

// New tells whether s is a new member of the group at now.
func (s Sender) New(now int64) bool {
	if s.Kept >= NewMessages {
		return false
	}
	return s.Guest || now-s.Joined < NewPeriod
}

// Input is what one message is judged by.
type Input struct {
	// ChannelId is the supergroup the message goes to.
	ChannelId int64
	Sender    Sender
	Message   *mtproto.Message
	// CrossPosts is the number of supergroups, this one included, the
	// sender sent this content to within CrossPostWindow (0 for a message
	// without one, ContentHash nil).
	CrossPosts int
	Now        int64
}

// Evaluate returns the rule the message breaks, false for none.
func Evaluate(in Input) (Rule, bool) {
	if in.Sender.Exempt || in.Message == nil {
		return RuleNone, false
	}
	if in.Sender.New(in.Now) {
		switch {
		case HasLink(in.Message):
			return RuleLink, true
		case ForwardedFromChat(in.Message, in.ChannelId):
			return RuleForward, true
		case Mentions(in.Message) >= MentionsMin:
			return RuleMentions, true
		}
	}
	if in.CrossPosts >= CrossPostGroups {
		return RuleCrossPost, true
	}
	return RuleNone, false
}

// HasLink tells whether m has a link: a link of its text or a link card.
func HasLink(m *mtproto.Message) bool {
	if m.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaWebPage {
		return true
	}
	for _, e := range m.GetEntities() {
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityTextUrl:
			return true
		}
	}
	return false
}

// ForwardedFromChat tells whether m is forwarded from a channel or a group
// other than channelId.
func ForwardedFromChat(m *mtproto.Message, channelId int64) bool {
	from := m.GetFwdFrom().GetFromId()
	switch from.GetPredicateName() {
	case mtproto.Predicate_peerChannel:
		return from.GetChannelId() != channelId
	case mtproto.Predicate_peerChat:
		return true
	}
	return false
}

// Mentions is the number of different users m mentions, by username or by
// name.
func Mentions(m *mtproto.Message) int {
	seen := map[string]bool{}
	var text []uint16
	for _, e := range m.GetEntities() {
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityMentionName, mtproto.Predicate_inputMessageEntityMentionName:
			id := e.GetUserId_INT64()
			if id == 0 {
				id = e.GetUserId_INPUTUSER().GetUserId()
			}
			seen["id"+strconv.FormatInt(id, 10)] = true
		case mtproto.Predicate_messageEntityMention:
			if text == nil {
				text = utf16.Encode([]rune(m.GetMessage()))
			}
			start, end := int(e.GetOffset()), int(e.GetOffset())+int(e.GetLength())
			if start < 0 || end > len(text) || start >= end {
				continue
			}
			seen["@"+strings.ToLower(string(utf16.Decode(text[start:end])))] = true
		}
	}
	return len(seen)
}

// ContentHash is the hash of what m says, the same for the same text
// (letter case and spacing aside) and the same files, which the cross-post
// rule counts by; nil for a message too slight for it: a short text
// without a link or a file. The text itself is never kept.
func ContentHash(m *mtproto.Message) []byte {
	text := strings.ToLower(strings.Join(strings.Fields(m.GetMessage()), " "))
	media := mediaKey(m.GetMedia())
	if media == "" && !HasLink(m) && utf8.RuneCountInString(text) < CrossPostMinChars {
		return nil
	}
	sum := sha256.Sum256([]byte(text + "\x00" + media))
	return sum[:]
}

// mediaKey names the file of a message's media, "" for none.
func mediaKey(media *mtproto.MessageMedia) string {
	if id := media.GetPhoto_FLAGPHOTO().GetId(); id != 0 {
		return "photo:" + strconv.FormatInt(id, 10)
	}
	if id := media.GetDocument().GetId(); id != 0 {
		return "document:" + strconv.FormatInt(id, 10)
	}
	return ""
}
