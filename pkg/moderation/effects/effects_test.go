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

package effects

import (
	"context"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// A deletion leaves the reporter's own messages of the evidence.
func TestIdsToDelete(t *testing.T) {
	report := &moderation.Report{ReporterUserId: 1}
	got := idsToDelete(report, &moderation.Snapshot{Messages: []moderation.SnapshotMessage{
		{Id: 10, FromId: 2}, {Id: 11, FromId: 1}, {Id: 12, FromId: 2},
	}})
	if !slices.Equal(got, []int32{10, 12}) {
		t.Errorf("ids: %v", got)
	}
	if got = idsToDelete(report, &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 11, FromId: 1}}}); len(got) != 0 {
		t.Errorf("only the reporter's messages: %v", got)
	}
}

// noMessages fails the test when a deletion of messages is asked for.
type noMessages struct {
	Msg
	t *testing.T
}

func (m noMessages) MsgDeleteMessages(ctx context.Context, in *msgpb.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	m.t.Errorf("messages %v deleted for a report of stories", in.GetId())
	return nil, nil
}

type noChannelMessages struct{ t *testing.T }

func (c noChannelMessages) ChannelDeleteMessages(ctx context.Context, in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	c.t.Errorf("channel messages %v deleted for a report of stories", in.GetId())
	return nil, nil
}

type deletedStories struct {
	peer stories.Peer
	ids  []int32
}

func (d *deletedStories) Delete(ctx context.Context, peer stories.Peer, ids []int32, at int32) ([]*stories.Story, error) {
	d.peer, d.ids = peer, ids
	return nil, nil
}

// The deletion of a report of stories deletes the stories of the report's
// peer and never the messages that happen to have the same ids; without
// the stories' tables the step fails instead of passing for done.
func TestDeleteOfStoryReport(t *testing.T) {
	snapshot := &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 3, FromId: 7}, {Id: 4, FromId: 7}}}
	for _, peerType := range []int32{mtproto.PEER_USER, mtproto.PEER_CHANNEL} {
		report := &moderation.Report{Id: 1, Kind: moderation.KindStory, ReporterUserId: 9, PeerType: peerType, PeerId: 7}
		got := &deletedStories{}
		r := &Runner{Msg: noMessages{t: t}, Channel: noChannelMessages{t: t}, Stories: got}
		if err := r.deleteMessages(context.Background(), report, snapshot); err != nil {
			t.Fatalf("peer type %d: %v", peerType, err)
		}
		if got.peer != (stories.Peer{Type: peerType, Id: 7}) || !slices.Equal(got.ids, []int32{3, 4}) {
			t.Errorf("peer type %d: deleted %v of %v", peerType, got.ids, got.peer)
		}
		r.Stories = nil
		if err := r.deleteMessages(context.Background(), report, snapshot); err == nil {
			t.Errorf("peer type %d: no stories' tables, and the step passed", peerType)
		}
	}
}
