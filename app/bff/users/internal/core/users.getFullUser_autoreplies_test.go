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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// The automatic replies of the caller in its own userFull
// (business_greeting_message, business_away_message), each when it is on.
func TestSetAutoReplies(t *testing.T) {
	full := &mtproto.UserFull{}
	setAutoReplies(full, nil)
	if full.BusinessGreetingMessage != nil || full.BusinessAwayMessage != nil {
		t.Fatalf("replies nobody set: %v %v", full.BusinessGreetingMessage, full.BusinessAwayMessage)
	}

	setAutoReplies(full, []*business.AutoReply{{Kind: business.KindAway, ShortcutID: 4, Schedule: business.AwayOutsideWorkHours,
		Recipients: business.Recipients{NonContacts: true}}})
	if full.BusinessGreetingMessage != nil {
		t.Errorf("a greeting nobody set: %v", full.BusinessGreetingMessage)
	}
	if a := full.BusinessAwayMessage; a.GetShortcutId() != 4 || !a.GetRecipients().GetNonContacts() ||
		a.GetSchedule().GetPredicateName() != mtproto.Predicate_businessAwayMessageScheduleOutsideWorkHours {
		t.Errorf("away message %v", a)
	}

	full = &mtproto.UserFull{}
	setAutoReplies(full, []*business.AutoReply{
		{Kind: business.KindGreeting, ShortcutID: 3, NoActivityDays: 21, Recipients: business.Recipients{ExistingChats: true}},
		{Kind: business.KindAway, ShortcutID: 4, Schedule: business.AwayAlways, OfflineOnly: true, Recipients: business.Recipients{ExcludeSelected: true}},
	})
	if g := full.BusinessGreetingMessage; g.GetShortcutId() != 3 || g.GetNoActivityDays() != 21 || !g.GetRecipients().GetExistingChats() {
		t.Errorf("greeting %v", g)
	}
	if a := full.BusinessAwayMessage; a.GetShortcutId() != 4 || !a.GetOfflineOnly() {
		t.Errorf("away message %v", a)
	}
}
