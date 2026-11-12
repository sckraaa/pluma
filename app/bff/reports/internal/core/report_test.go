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
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

func TestTargetOf(t *testing.T) {
	const reporter = int64(1)
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 2}
	s := &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 1, FromId: reporter}, {Id: 2, FromId: 3}}}
	if got := targetOf(s, peer, reporter); got != 3 {
		t.Errorf("sender of the first foreign message: %d", got)
	}
	if got := targetOf(&moderation.Snapshot{}, peer, reporter); got != 2 {
		t.Errorf("the reported user: %d", got)
	}
	if got := targetOf(&moderation.Snapshot{}, &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: 2}, reporter); got != 0 {
		t.Errorf("a channel: %d", got)
	}
}

func TestFromIdOf(t *testing.T) {
	in := mtproto.MakeTLMessage(&mtproto.Message{PeerId: mtproto.MakePeerUser(7)}).To_Message()
	if got := fromIdOf(in, 1); got != 7 {
		t.Errorf("incoming private message without from_id: %d", got)
	}
	out := mtproto.MakeTLMessage(&mtproto.Message{Out: true, PeerId: mtproto.MakePeerUser(7)}).To_Message()
	if got := fromIdOf(out, 1); got != 1 {
		t.Errorf("outgoing: %d", got)
	}
	from := mtproto.MakeTLMessage(&mtproto.Message{FromId: mtproto.MakePeerUser(9)}).To_Message()
	if got := fromIdOf(from, 1); got != 9 {
		t.Errorf("from_id: %d", got)
	}
}

// A report names messages of the chat it is about, whatever else the
// reporter's box holds.
func TestInChat(t *testing.T) {
	private := &mtproto.Message{PeerId: mtproto.MakePeerUser(7)}
	group := &mtproto.Message{PeerId: mtproto.MakePeerChat(7)}
	saved := &mtproto.Message{PeerId: mtproto.MakePeerUser(1)}
	for _, tc := range []struct {
		m    *mtproto.Message
		peer *mtproto.PeerUtil
		want bool
	}{
		{private, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_USER, PeerId: 7}, true},
		{private, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_USER, PeerId: 8}, false},
		{private, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_CHAT, PeerId: 7}, false},
		{group, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_CHAT, PeerId: 7}, true},
		{group, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_USER, PeerId: 7}, false},
		{saved, &mtproto.PeerUtil{SelfId: 1, PeerType: mtproto.PEER_SELF, PeerId: 1}, true},
	} {
		if got := inChat(tc.m, tc.peer); got != tc.want {
			t.Errorf("inChat(%v, %v) = %v", tc.m.GetPeerId(), tc.peer, got)
		}
	}
}
