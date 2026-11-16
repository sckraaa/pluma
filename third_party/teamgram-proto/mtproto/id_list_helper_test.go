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

package mtproto

import (
	"slices"
	"testing"
)

// pluma: the next owner a supergroup's service message names is among the
// users sent with it (task 078).
func TestPickNewCreator(t *testing.T) {
	for _, action := range []*MessageAction{
		MakeTLMessageActionNewCreatorPending(&MessageAction{NewCreatorId: 42}).To_MessageAction(),
		MakeTLMessageActionChangeCreator(&MessageAction{NewCreatorId: 42}).To_MessageAction(),
	} {
		h := NewIDListHelper()
		h.PickByMessageAction(action)
		if !slices.Contains(h.UserIdList, 42) {
			t.Errorf("%s: users %v", action.GetPredicateName(), h.UserIdList)
		}
	}
}

// pluma: the peers a user shared with a bot are among the users and chats
// sent with the user's copy of the message (task 106).
func TestPickRequestedPeers(t *testing.T) {
	h := NewIDListHelper()
	h.PickByMessageAction(MakeTLMessageActionRequestedPeer(&MessageAction{
		ButtonId:         1,
		Peers_VECTORPEER: []*Peer{MakePeerUser(7), MakePeerChat(8), MakePeerChannel(9)},
	}).To_MessageAction())
	if !slices.Contains(h.UserIdList, 7) || !slices.Contains(h.ChatIdList, 8) || !slices.Contains(h.ChannelIdList, 9) {
		t.Errorf("users %v, chats %v, channels %v", h.UserIdList, h.ChatIdList, h.ChannelIdList)
	}
}
