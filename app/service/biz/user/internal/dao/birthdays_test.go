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
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestBirthdayNear(t *testing.T) {
	// 01:30 on the 1st of January in Moscow is still the 31st of December
	// in UTC: the window is around the UTC day.
	now := time.Date(2027, time.January, 1, 1, 30, 0, 0, time.FixedZone("MSK", 3*3600))
	for _, c := range []struct {
		month, day int32
		want       bool
	}{
		{12, 30, true},
		{12, 31, true},
		{1, 1, true},
		{1, 2, false},
		{12, 29, false},
		{2, 29, false},
	} {
		b := mtproto.MakeTLBirthday(&mtproto.Birthday{Month: c.month, Day: c.day}).To_Birthday()
		if got := birthdayNear(b, now); got != c.want {
			t.Errorf("%02d.%02d: %v", c.day, c.month, got)
		}
	}
}

// Until the user sets them, the birthday is for contacts only.
func TestBirthdayDefaultRules(t *testing.T) {
	rules := makeDefaultPrivacyRules(mtproto.BIRTHDAY)
	if len(rules) != 1 || rules[0].GetPredicateName() != mtproto.Predicate_privacyValueAllowContacts {
		t.Errorf("default rules of the birthday %v", rules)
	}
}
