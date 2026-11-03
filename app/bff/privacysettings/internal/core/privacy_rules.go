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
)

// maxPrivacyEntries is the limit of PRIVACY_TOO_LONG ("the limit is 1000",
// core.telegram.org/method/account.setPrivacy): users and chats of the
// exceptions together.
const maxPrivacyEntries = 1000

// normalizeRules checks the rules account.setPrivacy got for key and
// returns them as they are stored (task 034, ADR 0031):
//   - exception rules with an empty list go: Android sends both user and
//     chat rules when it has either (PrivacyControlActivity.java:1072-1126);
//   - there is exactly one base rule, the most open of those sent, and
//     DisallowAll when none was sent, as every client reads such a list;
//   - AllowCloseFriends only applies to stories ("can be used only when
//     posting stories", core.telegram.org/api/privacy): PRIVACY_VALUE_INVALID;
//   - more than maxPrivacyEntries users and chats: PRIVACY_TOO_LONG;
//   - who may send voice messages is a Premium setting: without Premium only
//     "everybody" may be set (PREMIUM_ACCOUNT_REQUIRED, which tdesktop shows
//     as the Premium offer, api_user_privacy.cpp:282-286).
func normalizeRules(key int, rules []*mtproto.PrivacyRule, premium bool) ([]*mtproto.PrivacyRule, error) {
	var (
		base     *mtproto.PrivacyRule
		out      = make([]*mtproto.PrivacyRule, 0, len(rules)+1)
		entries  int
		baseRank = func(r *mtproto.PrivacyRule) int {
			switch r.GetPredicateName() {
			case mtproto.Predicate_privacyValueAllowAll:
				return 3
			case mtproto.Predicate_privacyValueAllowContacts:
				return 2
			case mtproto.Predicate_privacyValueDisallowAll:
				return 1
			}
			return 0
		}
	)
	for _, r := range rules {
		switch r.GetPredicateName() {
		case mtproto.Predicate_privacyValueAllowAll,
			mtproto.Predicate_privacyValueAllowContacts,
			mtproto.Predicate_privacyValueDisallowAll:
			if base == nil || baseRank(r) > baseRank(base) {
				base = r
			}
		case mtproto.Predicate_privacyValueAllowUsers,
			mtproto.Predicate_privacyValueDisallowUsers:
			if len(r.GetUsers()) == 0 {
				continue
			}
			entries += len(r.GetUsers())
			out = append(out, r)
		case mtproto.Predicate_privacyValueAllowChatParticipants,
			mtproto.Predicate_privacyValueDisallowChatParticipants:
			if len(r.GetChats()) == 0 {
				continue
			}
			entries += len(r.GetChats())
			out = append(out, r)
		case mtproto.Predicate_privacyValueAllowCloseFriends:
			return nil, mtproto.ErrPrivacyValueInvalid
		case mtproto.Predicate_privacyValueAllowPremium,
			mtproto.Predicate_privacyValueAllowBots,
			mtproto.Predicate_privacyValueDisallowBots,
			mtproto.Predicate_privacyValueDisallowContacts:
			out = append(out, r)
		default:
			return nil, mtproto.ErrPrivacyValueInvalid
		}
	}
	if entries > maxPrivacyEntries {
		return nil, mtproto.ErrPrivacyTooLong
	}
	if base == nil {
		base = mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()
	}
	out = append(out, base)
	if key == mtproto.VOICE_MESSAGES && !premium && !opensToAll(out) {
		return nil, mtproto.ErrPremiumAccountRequired
	}
	return out, nil
}

// opensToAll tells rules that let everybody: AllowAll and no exception that
// keeps someone out.
func opensToAll(rules []*mtproto.PrivacyRule) bool {
	all := false
	for _, r := range rules {
		switch r.GetPredicateName() {
		case mtproto.Predicate_privacyValueAllowAll:
			all = true
		case mtproto.Predicate_privacyValueDisallowUsers,
			mtproto.Predicate_privacyValueDisallowChatParticipants,
			mtproto.Predicate_privacyValueDisallowContacts,
			mtproto.Predicate_privacyValueDisallowBots:
			return false
		}
	}
	return all
}
