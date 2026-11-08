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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

// TestDeleteHistoryUpdates: one portion is named by its update, none is no
// update, more than one is a difference to fetch.
func TestDeleteHistoryUpdates(t *testing.T) {
	const channelId = int64(7)
	if u := deleteHistoryUpdates(channelId, nil).GetUpdates(); len(u) != 0 {
		t.Errorf("nothing deleted: %v", u)
	}

	one := deleteHistoryUpdates(channelId, []*dao.Deleted{{Ids: []int32{2, 3}, Pts: 10, PtsCount: 2}}).GetUpdates()
	if len(one) != 1 || one[0].GetPredicateName() != mtproto.Predicate_updateDeleteChannelMessages ||
		one[0].GetPts_INT32() != 10 || one[0].GetPtsCount() != 2 || len(one[0].GetMessages()) != 2 {
		t.Errorf("one portion: %v", one)
	}

	two := deleteHistoryUpdates(channelId, []*dao.Deleted{{Pts: 5}, {Pts: 9}}).GetUpdates()
	if len(two) != 1 || two[0].GetPredicateName() != mtproto.Predicate_updateChannelTooLong ||
		two[0].GetChannelId() != channelId || two[0].GetPts_FLAGINT32().GetValue() != 9 {
		t.Errorf("two portions: %v", two)
	}
}

// TestHideHistoryUpdates: the update names the last hidden message, nothing
// for a channel with nothing hidden.
func TestHideHistoryUpdates(t *testing.T) {
	if u := hideHistoryUpdates(7, 0).GetUpdates(); len(u) != 0 {
		t.Errorf("nothing hidden: %v", u)
	}
	u := hideHistoryUpdates(7, 42).GetUpdates()
	if len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateChannelAvailableMessages ||
		u[0].GetAvailableMinId() != 42 || u[0].GetChannelId() != 7 {
		t.Errorf("hidden: %v", u)
	}
}
