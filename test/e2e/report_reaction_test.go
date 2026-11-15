//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// androidReportReaction is messages.reportReaction#61422a48 peer:InputPeer
// id:int user_id:InputUser, the request Android sends (tgnet/TLRPC.java,
// TL_messages_reportReaction): the schema of the server no longer has it,
// so the test writes its bytes.
type androidReportReaction struct {
	peer   *mtproto.InputPeer
	id     int32
	userId int64
	hash   int64
}

func (r *androidReportReaction) Encode(x *mtproto.EncodeBuf, layer int32) error {
	x.UInt(0x61422a48)
	if err := r.peer.Encode(x, layer); err != nil {
		return err
	}
	x.Int(r.id)
	x.UInt(0xf21158c6) // inputUser user_id:long access_hash:long
	x.Long(r.userId)
	x.Long(r.hash)
	return nil
}

func (r *androidReportReaction) Decode(*mtproto.DecodeBuf) error {
	return fmt.Errorf("androidReportReaction is only sent")
}

func (r *androidReportReaction) String() string { return "messages.reportReaction#61422a48" }

// TestReportReaction (task 098): in a public supergroup of Alice, Bob
// reacts to her message and Carol, a member, reports his reaction: the
// report is in the moderation queue as a reaction report (kind 8) against
// Bob. A report about someone who did not react, or about oneself, is
// USER_ID_INVALID. In a private supergroup a member may not report
// (CHAT_ADMIN_REQUIRED), its creator may. The request Android sends, with
// the older constructor, is served the same.
func TestReportReaction(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	type group struct {
		id   int64
		peer func(u *user) *mtproto.InputPeer
		msg  int32
	}
	// newGroup is a supergroup of Alice with Bob and Carol in it, a message
	// of Alice and Bob's 👍 to it.
	newGroup := func(public bool) group {
		t.Helper()
		_, ch := createChannel(ctx, t, alice, fmt.Sprintf("report reaction %d", randomID()), true)
		hash := ch.GetAccessHash_FLAGINT64().GetValue()
		input := func(u *user) *mtproto.InputChannel {
			return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
		}
		g := group{id: ch.GetId(), peer: func(u *user) *mtproto.InputPeer {
			return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
		}}
		if public {
			if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
				Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
				Channel:     input(alice),
				Username:    fmt.Sprintf("pluma_e2e_%d", ch.GetId()),
			}); err != nil {
				t.Fatalf("channels.updateUsername: %v", err)
			}
		}
		invite := channelInviteHash(ctx, t, alice, input(alice))
		for _, u := range []*user{bob, carol} {
			if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
				Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
				Hash:        invite,
			}); err != nil {
				t.Fatalf("messages.importChatInvite: %v", err)
			}
		}
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        g.peer(alice), Message: "react to me", RandomId: randomID(),
		})
		if err != nil {
			t.Fatalf("messages.sendMessage: %v", err)
		}
		for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
				g.msg = u.GetId_INT32()
			}
		}
		if g.msg == 0 {
			t.Fatal("no id of the message")
		}
		if _, err = sendReaction(ctx, bob, g.peer(bob), g.msg, "👍"); err != nil {
			t.Fatalf("messages.sendReaction: %v", err)
		}
		return g
	}
	report := func(u *user, g group, reactionPeer *mtproto.InputPeer) error {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesReportReaction{
			Constructor:  u.c.Constructor(mtproto.Predicate_messages_reportReaction),
			Peer:         g.peer(u),
			Id:           g.msg,
			ReactionPeer: reactionPeer,
		})
		if err != nil {
			return err
		}
		if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
			t.Errorf("messages.reportReaction returned %v", o)
		}
		return nil
	}
	// queued finds the open reaction report of reporter against target in
	// the supergroup.
	queued := func(g group, reporter, target *user) bool {
		t.Helper()
		peer := fmt.Sprintf("%d/%d", mtproto.PEER_CHANNEL, g.id)
		for _, line := range strings.Split(admin(t, "moderation", "reports"), "\n") {
			f := strings.Fields(line)
			if len(f) >= 6 && f[1] == fmt.Sprint(reporter.self.GetId()) && f[2] == "8" && f[3] == peer &&
				f[4] == fmt.Sprint(target.self.GetId()) && f[5] == "reaction" {
				return true
			}
		}
		return false
	}

	public := newGroup(true)
	if err := report(carol, public, peerOf(ctx, t, carol, bob)); err != nil {
		t.Fatalf("Carol reports Bob's reaction: %v", err)
	}
	if !queued(public, carol, bob) {
		t.Errorf("no reaction report of Carol against Bob in the queue")
	}
	if err := report(carol, public, peerOf(ctx, t, carol, alice)); !mtclient.IsRPCError(err, "USER_ID_INVALID") {
		t.Errorf("a report about one who did not react: %v, want USER_ID_INVALID", err)
	}
	self := &mtproto.InputPeer{Constructor: carol.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	if err := report(carol, public, self); !mtclient.IsRPCError(err, "USER_ID_INVALID") {
		t.Errorf("a report about oneself: %v, want USER_ID_INVALID", err)
	}

	// Android: the creator reports Bob with the older constructor.
	bobPeer := peerOf(ctx, t, alice, bob)
	o, err := alice.c.Invoke(ctx, &androidReportReaction{peer: public.peer(alice), id: public.msg, userId: bobPeer.GetUserId(), hash: bobPeer.GetAccessHash()})
	if err != nil {
		t.Fatalf("messages.reportReaction#61422a48: %v", err)
	}
	if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
		t.Errorf("messages.reportReaction#61422a48 returned %v", o)
	}
	if !queued(public, alice, bob) {
		t.Errorf("no reaction report of Alice (Android's request) against Bob in the queue")
	}

	private := newGroup(false)
	if err := report(carol, private, peerOf(ctx, t, carol, bob)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member of a private supergroup reports: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if err := report(alice, private, peerOf(ctx, t, alice, bob)); err != nil {
		t.Fatalf("the creator of a private supergroup reports: %v", err)
	}
	if !queued(private, alice, bob) {
		t.Errorf("no reaction report of Alice against Bob in the private supergroup")
	}
}
