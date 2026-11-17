//go:build integration

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

package dao_test

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// forwardOf is a forward of the post of the channel from, as
// messages.forwardMessages gives it.
func forwardOf(sender, to, from int64, post int32) dao.Outgoing {
	o := channelText(sender, to, 0)
	o.Message.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
		FromId:      mtproto.MakePeerChannel(from),
		ChannelPost: mtproto.MakeFlagsInt32(post),
		Date:        1,
	}).To_MessageFwdHeader()
	return o
}

// storyShare is a message sharing the story id of a peer.
func storyShare(sender, to int64, peer *mtproto.Peer, id int32) dao.Outgoing {
	o := channelText(sender, to, 0)
	o.Message.Message = ""
	o.Message.Media = mtproto.MakeTLMessageMediaStory(&mtproto.MessageMedia{Peer: peer, Id: id}).To_MessageMedia()
	return o
}

// TestChannelSendPublicForwards: a forward of a post into a channel or a
// supergroup, and a story of a channel shared there, are recorded with
// their copy (task 103), once; a message of a group, a story of a user and
// the automatic copy of a post in its discussion group are none.
func TestChannelSendPublicForwards(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, member := newUser(), newUser()
	source := w.newChannelRow(creator, true)
	broadcast := w.newChannelRow(creator, true)
	group := w.newChannelRow(creator, false)
	w.member(group, member, channelperm.RoleMember)

	sent, err := w.sendOutgoing(creator, broadcast, forwardOf(creator, broadcast, source, 7))
	if err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE src_kind = ? AND src_peer_type = ? AND src_peer_id = ? AND src_id = 7 AND dst_channel_id = ? AND dst_message_id = ?",
		channelstats.ForwardPost, mtproto.PEER_CHANNEL, source, broadcast, sent[0].Message.Id); n != 1 {
		t.Fatalf("the forward into a channel: %d rows", n)
	}
	if sent, err = w.sendOutgoing(member, group, forwardOf(member, group, source, 7), channelText(member, group, 0)); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE src_peer_id = ? AND dst_channel_id = ? AND dst_message_id = ?",
		source, group, sent[0].Message.Id); n != 1 {
		t.Fatalf("the forward into a supergroup: %d rows", n)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE dst_channel_id = ?", group); n != 1 {
		t.Fatalf("the supergroup has %d rows, want the forward alone", n)
	}

	// A message of a group forwarded names its sender, with no channel_post.
	fromGroup := channelText(member, group, 0)
	fromGroup.Message.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerChannel(group), Date: 1}).To_MessageFwdHeader()
	if sent, err = w.sendOutgoing(member, group, fromGroup); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE dst_channel_id = ? AND dst_message_id = ?", group, sent[0].Message.Id); n != 0 {
		t.Fatalf("a message of a group: %d rows", n)
	}

	if sent, err = w.sendOutgoing(member, group, storyShare(member, group, mtproto.MakePeerChannel(source), 3), storyShare(member, group, mtproto.MakePeerUser(creator), 4)); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE src_kind = ? AND src_peer_type = ? AND src_peer_id = ? AND src_id = 3 AND dst_channel_id = ? AND dst_message_id = ?",
		channelstats.ForwardStory, mtproto.PEER_CHANNEL, source, group, sent[0].Message.Id); n != 1 {
		t.Fatalf("a story of a channel shared: %d rows", n)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE dst_channel_id = ? AND dst_message_id = ?", group, sent[1].Message.Id); n != 0 {
		t.Fatalf("a story of a user shared: %d rows", n)
	}

	// A post of a channel with a discussion group: its copy there is none.
	discussion := w.newChannelRow(creator, false)
	w.exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", discussion, source)
	if _, err = w.sendOutgoing(creator, source, channelText(creator, source, 0)); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", discussion); n == 0 {
		t.Fatal("the post has no copy in its discussion group")
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE dst_channel_id IN (?, ?)", source, discussion); n != 0 {
		t.Fatalf("the post and its discussion copy: %d rows", n)
	}
}
