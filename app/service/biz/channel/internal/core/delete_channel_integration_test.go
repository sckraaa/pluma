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

package core

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) deleteChannel(self, channelId int64) (*mtproto.Updates, error) {
	return f.c.ChannelDeleteChannel(&channel.TLChannelDeleteChannel{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId})
}

// deletedEvents counts the inbox events that tell recipient a channel it
// was in is deleted.
func (f *diffFixture) deletedEvents(recipient int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND topic = ? AND method = ?",
		recipient, itInboxTopic, "inbox.TL_inbox_channelDeletedToInbox").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestDeleteChannel: the creator deletes a public channel for everyone
// (the owner's report, task 055): it is marked deleted, everyone leaves,
// its username and links go, the creator's dialog goes and its devices get
// updateChannel with the channel as channelForbidden; each other member
// gets an inbox event; nobody counts it against the channel limit. A
// member may not delete it, nor anyone a deleted one.
func TestDeleteChannel(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol := f.user("Creator"), f.user("Bob"), f.user("Carol")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)
	name := fmt.Sprintf("del%d", id)
	if err := f.setUsername(creator, id, name); err != nil {
		t.Fatal(err)
	}
	link := f.link(id, creator, 0, 0)
	if _, err := f.join(bob, 0, link); err != nil {
		t.Fatal(err)
	}
	if _, err := f.join(carol, id, ""); err != nil {
		t.Fatal(err)
	}

	_, err := f.deleteChannel(bob, id)
	wantErr(t, "a member deletes", err, mtproto.ErrChatAdminRequired)

	r, err := f.deleteChannel(creator, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetChats()) != 1 || r.GetChats()[0].GetPredicateName() != mtproto.Predicate_channelForbidden ||
		len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Errorf("answer: updates %v, chats %v", r.GetUpdates(), r.GetChats())
	}

	var deleted, members, names, links, dialogs int
	if err = f.box.QueryRow("SELECT deleted FROM channels WHERE id = ?", id).Scan(&deleted); err != nil || deleted != 1 {
		t.Errorf("channel deleted %d, %v", deleted, err)
	}
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = ?", id, channel.StateIn).Scan(&members); err != nil || members != 0 {
		t.Errorf("%d members left in, %v", members, err)
	}
	if err = f.box.QueryRow("SELECT COUNT(*) FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, id).Scan(&names); err != nil || names != 0 {
		t.Errorf("%d usernames, %v", names, err)
	}
	if err = f.box.QueryRow("SELECT COUNT(*) FROM chat_invites WHERE chat_id = ? AND revoked = 0", id).Scan(&links); err != nil || links != 0 {
		t.Errorf("%d links not revoked, %v", links, err)
	}
	if err = f.box.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0", creator, mtproto.PEER_CHANNEL, id).Scan(&dialogs); err != nil || dialogs != 0 {
		t.Errorf("the creator's dialog: %d, %v", dialogs, err)
	}
	if n := f.seqChannelUpdates(creator, id); n == 0 {
		t.Error("no updateChannel for the creator's devices")
	}
	for _, u := range []int64{bob, carol} {
		if n := f.deletedEvents(u); n != 1 {
			t.Errorf("user %d: %d deletion events, want 1", u, n)
		}
	}
	if n := f.deletedEvents(creator); n != 0 {
		t.Errorf("the creator got %d deletion events", n)
	}
	// The name is free again, and the channel counts against no limit.
	if free, err := f.checkUsername(bob, 0, name); err != nil || !free {
		t.Errorf("the name after deletion: free %v, %v", free, err)
	}
	var joined int
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_participants WHERE user_id = ? AND state = 0", bob).Scan(&joined); err != nil || joined != 0 {
		t.Errorf("Bob still counts %d channels, %v", joined, err)
	}

	_, err = f.deleteChannel(creator, id)
	wantErr(t, "a second deletion", err, mtproto.ErrChannelInvalid)
	_, err = f.join(bob, 0, link)
	if err == nil {
		t.Error("joined a deleted channel by its link")
	}
}
