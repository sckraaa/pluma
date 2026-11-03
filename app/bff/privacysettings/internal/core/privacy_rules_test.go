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
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func names(rules []*mtproto.PrivacyRule) []string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = r.GetPredicateName()
	}
	return out
}

func TestNormalizeRules(t *testing.T) {
	var (
		all      = mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()
		contacts = mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()
		nobody   = mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()
		users    = func(ids ...int64) *mtproto.PrivacyRule {
			return mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: ids}).To_PrivacyRule()
		}
		notUsers = func(ids ...int64) *mtproto.PrivacyRule {
			return mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: ids}).To_PrivacyRule()
		}
		chats = func(ids ...int64) *mtproto.PrivacyRule {
			return mtproto.MakeTLPrivacyValueAllowChatParticipants(&mtproto.PrivacyRule{Chats: ids}).To_PrivacyRule()
		}
		premiumRule  = mtproto.MakeTLPrivacyValueAllowPremium(nil).To_PrivacyRule()
		closeFriends = mtproto.MakeTLPrivacyValueAllowCloseFriends(nil).To_PrivacyRule()
	)
	many := make([]int64, maxPrivacyEntries+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	for _, tc := range []struct {
		name    string
		key     int
		rules   []*mtproto.PrivacyRule
		premium bool
		want    []string
		err     error
	}{
		{"Android: nobody except B, with an empty chat rule", mtproto.PROFILE_PHOTO,
			[]*mtproto.PrivacyRule{users(5), chats(), nobody}, false,
			[]string{mtproto.Predicate_privacyValueAllowUsers, mtproto.Predicate_privacyValueDisallowAll}, nil},
		{"Web K: base first", mtproto.STATUS_TIMESTAMP,
			[]*mtproto.PrivacyRule{contacts, notUsers(5)}, false,
			[]string{mtproto.Predicate_privacyValueDisallowUsers, mtproto.Predicate_privacyValueAllowContacts}, nil},
		{"no base rule is nobody", mtproto.FORWARDS,
			[]*mtproto.PrivacyRule{users(5)}, false,
			[]string{mtproto.Predicate_privacyValueAllowUsers, mtproto.Predicate_privacyValueDisallowAll}, nil},
		{"two base rules, the most open stays", mtproto.ABOUT,
			[]*mtproto.PrivacyRule{contacts, nobody}, false,
			[]string{mtproto.Predicate_privacyValueAllowContacts}, nil},
		{"contacts and Premium users", mtproto.CHAT_INVITE,
			[]*mtproto.PrivacyRule{contacts, premiumRule}, false,
			[]string{mtproto.Predicate_privacyValueAllowPremium, mtproto.Predicate_privacyValueAllowContacts}, nil},
		{"close friends outside stories", mtproto.PROFILE_PHOTO,
			[]*mtproto.PrivacyRule{closeFriends, nobody}, false, nil, mtproto.ErrPrivacyValueInvalid},
		{"too many users", mtproto.PROFILE_PHOTO,
			[]*mtproto.PrivacyRule{users(many...), nobody}, false, nil, mtproto.ErrPrivacyTooLong},
		{"voice messages restricted without Premium", mtproto.VOICE_MESSAGES,
			[]*mtproto.PrivacyRule{contacts}, false, nil, mtproto.ErrPremiumAccountRequired},
		{"voice messages restricted with Premium", mtproto.VOICE_MESSAGES,
			[]*mtproto.PrivacyRule{contacts}, true, []string{mtproto.Predicate_privacyValueAllowContacts}, nil},
		{"voice messages back to everybody without Premium", mtproto.VOICE_MESSAGES,
			[]*mtproto.PrivacyRule{all}, false, []string{mtproto.Predicate_privacyValueAllowAll}, nil},
	} {
		got, err := normalizeRules(tc.key, tc.rules, tc.premium)
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.err)
			continue
		}
		if err == nil && !reflect.DeepEqual(names(got), tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, names(got), tc.want)
		}
	}
}
