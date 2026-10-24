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

package dao

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A phone number shared with one user (add_phone_privacy_exception,
// contacts.acceptContact) makes that user an exception of the phone number
// privacy, whatever the rules were.
func TestWithAllowedUser(t *testing.T) {
	const owner, peer = 1, 2
	allowAll := mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()
	disallowAll := mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()
	contacts := mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()
	users := func(wrap func(*mtproto.PrivacyRule) *mtproto.PrivacyRule, ids ...int64) *mtproto.PrivacyRule {
		return wrap(&mtproto.PrivacyRule{Users: ids})
	}
	allowUsers := func(r *mtproto.PrivacyRule) *mtproto.PrivacyRule {
		return mtproto.MakeTLPrivacyValueAllowUsers(r).To_PrivacyRule()
	}
	disallowUsers := func(r *mtproto.PrivacyRule) *mtproto.PrivacyRule {
		return mtproto.MakeTLPrivacyValueDisallowUsers(r).To_PrivacyRule()
	}

	for _, c := range []struct {
		name    string
		rules   []*mtproto.PrivacyRule
		changed bool
	}{
		{"nobody (the default of the phone number)", []*mtproto.PrivacyRule{disallowAll}, true},
		{"my contacts", []*mtproto.PrivacyRule{contacts}, true},
		{"nobody but others", []*mtproto.PrivacyRule{users(allowUsers, 7), disallowAll}, true},
		{"already an exception", []*mtproto.PrivacyRule{users(allowUsers, peer), disallowAll}, false},
		{"everybody", []*mtproto.PrivacyRule{allowAll}, false},
		{"everybody but the peer", []*mtproto.PrivacyRule{users(disallowUsers, peer, 7), allowAll}, true},
	} {
		before := len(c.rules)
		got, changed := withAllowedUser(c.rules, peer)
		if changed != c.changed {
			t.Errorf("%s: changed %v", c.name, changed)
		}
		allowed := mtproto.CheckPrivacyIsAllow(owner, got, peer, func(_, _ int64) bool { return false }, func(int64, []int64) bool { return false })
		if !allowed {
			t.Errorf("%s: the peer does not see the number: %v", c.name, got)
		}
		if len(c.rules) != before {
			t.Errorf("%s: the rules were changed", c.name)
		}
		for _, r := range got {
			if r.GetPredicateName() == mtproto.Predicate_privacyValueDisallowUsers {
				for _, id := range r.GetUsers() {
					if id == peer {
						t.Errorf("%s: the peer is still disallowed", c.name)
					}
				}
			}
		}
	}

	// Others stay where they were.
	got, _ := withAllowedUser([]*mtproto.PrivacyRule{users(disallowUsers, peer, 7), allowAll}, peer)
	if mtproto.CheckPrivacyIsAllow(owner, got, 7, func(_, _ int64) bool { return false }, func(int64, []int64) bool { return false }) {
		t.Errorf("user 7 sees the number: %v", got)
	}
	// The rules of the defaults are not touched.
	defaults := makeDefaultPrivacyRules(mtproto.PHONE_NUMBER)
	_, _ = withAllowedUser(defaults, peer)
	if len(defaults) != 1 || defaults[0].GetPredicateName() != mtproto.Predicate_privacyValueDisallowAll || len(defaults[0].GetUsers()) != 0 {
		t.Errorf("defaults changed: %v", defaults)
	}
}
