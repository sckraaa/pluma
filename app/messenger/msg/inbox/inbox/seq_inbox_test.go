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

package inbox

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func peerSettings(userId int64) *mtproto.Update {
	return mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
		Peer_PEER: mtproto.MakePeerUser(userId),
		Settings:  mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
	}).To_Update()
}

// The event of updates with seq to another account is checked when it is
// emitted: whatever is refused rolls the initiator back instead of making
// the event dead in the recipient's lane.
func TestCheckSeqInbox(t *testing.T) {
	if err := userbox.RegisterTopic("Inbox-T", userbox.TopicInbox); err != nil {
		t.Fatal(err)
	}
	if err := userbox.RegisterTopic("Sync-T", userbox.TopicPush); err != nil {
		t.Fatal(err)
	}
	const from, to = 3, 7
	chat := mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: 5}).To_Update()
	if err := checkSeqInbox(from, "Inbox-T", to, []*mtproto.Update{peerSettings(from), chat}); err != nil {
		t.Fatalf("valid event refused: %v", err)
	}
	for _, c := range []struct {
		name    string
		topic   string
		to      int64
		updates []*mtproto.Update
	}{
		{"nobody", "Inbox-T", 0, []*mtproto.Update{peerSettings(from)}},
		{"a negative id", "Inbox-T", -to, []*mtproto.Update{peerSettings(from)}},
		{"the initiator", "Inbox-T", from, []*mtproto.Update{peerSettings(from)}},
		{"no updates", "Inbox-T", to, nil},
		{"a nil update", "Inbox-T", to, []*mtproto.Update{peerSettings(from), nil}},
		{"an update with pts", "Inbox-T", to, []*mtproto.Update{
			mtproto.MakeTLUpdateReadHistoryOutbox(&mtproto.Update{Peer_PEER: mtproto.MakePeerUser(from)}).To_Update()}},
		{"a draft", "Inbox-T", to, []*mtproto.Update{mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{}).To_Update()}},
		{"a service notification", "Inbox-T", to, []*mtproto.Update{mtproto.MakeTLUpdateServiceNotification(&mtproto.Update{}).To_Update()}},
		{"a push topic", "Sync-T", to, []*mtproto.Update{peerSettings(from)}},
		{"an unknown topic", "Nowhere-T", to, []*mtproto.Update{peerSettings(from)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkSeqInbox(from, c.topic, c.to, c.updates)
			if !errors.Is(err, userbox.ErrInvalidEvent) || !userbox.Deterministic(err) {
				t.Errorf("error %v, want %v", err, userbox.ErrInvalidEvent)
			}
		})
	}
}
