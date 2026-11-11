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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// setParticipantsCount gives the channel n members in its counter, in the
// stack's MySQL: the clients offer a broadcast group only near
// megagroup_size_max, which a test does not fill with real accounts. The
// password stays in the container's environment.
func setParticipantsCount(t *testing.T, channelId int64, n int32) {
	t.Helper()
	stack(t, "exec", "-T", "mysql", "sh", "-c", fmt.Sprintf(
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -e "UPDATE channels SET participants_count = %d WHERE id = %d"`,
		n, channelId))
}

// TestChannelGigagroup (task 080) end to end: only the creator of a large
// supergroup turns it into a broadcast group, not a member, not a small
// group, not a broadcast channel. The answer and Bob's updateChannel carry
// gigagroup and the default restrictions of sending; from then on Bob, a
// member, may not write, while Carol, an admin, and Alice may. The
// conversion is for good, and the permissions of the group stay as they
// are.
func TestChannelGigagroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, gr := createChannel(ctx, t, alice, "giga", true)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: gr.GetAccessHash_FLAGINT64().GetValue()}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: gr.GetAccessHash_FLAGINT64().GetValue()}
	}
	hash := channelInviteHash(ctx, t, alice, input(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditAdmin{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_editAdmin),
		Channel:     input(alice),
		UserId:      inputUserOf(ctx, t, alice, carol),
		AdminRights: &mtproto.ChatAdminRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatAdminRights), DeleteMessages: true},
	}); err != nil {
		t.Fatalf("channels.editAdmin: %v", err)
	}

	convert := func(u *user, ch *mtproto.InputChannel) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsConvertToGigagroup{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_convertToGigagroup),
			Channel:     ch,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	send := func(u *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer(u), Message: "giga", RandomId: randomID(),
		})
		return err
	}

	if _, err := convert(alice, input(alice)); !mtclient.IsRPCError(err, "PARTICIPANTS_TOO_FEW") {
		t.Errorf("a group of three: %v, want PARTICIPANTS_TOO_FEW", err)
	}
	_, broadcast := createChannel(ctx, t, alice, "giga broadcast", false)
	if _, err := convert(alice, &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   broadcast.GetId(), AccessHash: broadcast.GetAccessHash_FLAGINT64().GetValue(),
	}); !mtclient.IsRPCError(err, "CHANNEL_INVALID") {
		t.Errorf("a broadcast channel: %v, want CHANNEL_INVALID", err)
	}

	setParticipantsCount(t, gr.GetId(), 199_000)
	for name, u := range map[string]*user{"Bob": bob, "Carol": carol} {
		if _, err := convert(u, input(u)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
			t.Errorf("%s converts: %v, want CHAT_ADMIN_REQUIRED", name, err)
		}
	}
	if err := send(bob); err != nil {
		t.Fatalf("Bob writes to the supergroup: %v", err)
	}

	getState(ctx, t, bob)
	r, err := convert(alice, input(alice))
	if err != nil {
		t.Fatalf("channels.convertToGigagroup: %v", err)
	}
	if len(r.GetChats()) == 0 || !r.GetChats()[0].GetGigagroup() || !r.GetChats()[0].GetMegagroup() ||
		!r.GetChats()[0].GetDefaultBannedRights().GetSendMessages() || !r.GetChats()[0].GetDefaultBannedRights().GetSendPlain() {
		t.Errorf("the answer's channel: %v", r.GetChats())
	}
	if len(r.GetUpdates()) == 0 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Errorf("the answer's updates: %v", r.GetUpdates())
	}
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, c := range us.GetChats() {
			if c.GetId() == gr.GetId() && c.GetGigagroup() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob does not learn of the broadcast group: %v", err)
	}

	if err = send(bob); !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN") {
		t.Errorf("Bob writes to the broadcast group: %v, want CHAT_WRITE_FORBIDDEN", err)
	}
	for name, u := range map[string]*user{"Carol": carol, "Alice": alice} {
		if err = send(u); err != nil {
			t.Errorf("%s writes to the broadcast group: %v", name, err)
		}
	}
	if _, err = convert(alice, input(alice)); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("converting again: %v, want CHAT_NOT_MODIFIED", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesEditChatDefaultBannedRights{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_editChatDefaultBannedRights),
		Peer:         peer(alice),
		BannedRights: &mtproto.ChatBannedRights{Constructor: alice.c.Constructor(mtproto.Predicate_chatBannedRights)},
	}); !mtclient.IsRPCError(err, "CHANNEL_INVALID") {
		t.Errorf("lifting the restrictions of a broadcast group: %v, want CHANNEL_INVALID", err)
	}
}
