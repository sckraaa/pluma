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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

func TestMutedDialog(t *testing.T) {
	const now = 1000
	setting := func(peerType int32, peerId int64, until int32) *userpb.PeerPeerNotifySettings {
		return &userpb.PeerPeerNotifySettings{
			PeerType: peerType,
			PeerId:   peerId,
			Settings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: mtproto.MakeFlagsInt32(until)}).To_PeerNotifySettings(),
		}
	}
	all := []*userpb.PeerPeerNotifySettings{
		setting(mtproto.PEER_USER, 1, now+60),   // user 1 muted
		setting(mtproto.PEER_USER, 2, now-60),   // user 2's mute is over
		setting(mtproto.PEER_USERS, 0, now+600), // private chats muted by class
		setting(mtproto.PEER_BROADCASTS, 0, now+600),
	}
	cases := []struct {
		name      string
		peer      *mtproto.PeerUtil
		broadcast bool
		want      bool
	}{
		{"own mute running", mtproto.MakePeerUtil(mtproto.PEER_USER, 1), false, true},
		{"own mute over beats the class", mtproto.MakePeerUtil(mtproto.PEER_USER, 2), false, false},
		{"class of private chats", mtproto.MakePeerUtil(mtproto.PEER_USER, 3), false, true},
		{"group: class not muted", mtproto.MakePeerUtil(mtproto.PEER_CHAT, 5), false, false},
		{"broadcast: class muted", mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, 7), true, true},
		{"supergroup: class of groups", mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, 8), false, false},
	}
	for _, c := range cases {
		if got := mutedDialog(all, c.peer, c.broadcast, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
