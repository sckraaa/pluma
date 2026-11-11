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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestAdminLogEvents: Alice's supergroup keeps her slow mode, her edit of a
// link, Bob joining by the link and Carol let in on a request; each comes
// under its flag of the filter, with the users it names.
func TestAdminLogEvents(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	_, ch := createChannel(ctx, t, alice, fmt.Sprintf("events %d", randomID()), true)
	peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleSlowMode{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleSlowMode),
		Channel:     input,
		Seconds:     60,
	}); err != nil {
		t.Fatalf("channels.toggleSlowMode: %v", err)
	}

	export := func(requestNeeded bool, title string) string {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesExportChatInvite{
			Constructor:   alice.c.Constructor(mtproto.Predicate_messages_exportChatInvite),
			RequestNeeded: requestNeeded,
			Peer:          peer,
			Title:         wrapperspb.String(title),
		})
		if err != nil {
			t.Fatalf("messages.exportChatInvite: %v", err)
		}
		return o.(interface {
			To_ExportedChatInvite() *mtproto.ExportedChatInvite
		}).To_ExportedChatInvite().GetLink()
	}
	hashOf := func(link string) string { return link[strings.LastIndex(link, "/+")+2:] }
	join := func(u *user, link string) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hashOf(link),
		})
		return err
	}

	plain := export(false, "first")
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditExportedChatInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editExportedChatInvite),
		Peer:        peer,
		Link:        plain,
		Title:       wrapperspb.String("second"),
	}); err != nil {
		t.Fatalf("messages.editExportedChatInvite: %v", err)
	}
	if err := join(bob, plain); err != nil {
		t.Fatalf("Bob's import: %v", err)
	}
	asking := export(true, "ask")
	if err := join(carol, asking); !mtclient.IsRPCError(err, "INVITE_REQUEST_SENT") {
		t.Fatalf("Carol's import: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesHideChatJoinRequest{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_hideChatJoinRequest),
		Approved:    true,
		Peer:        peer,
		UserId:      inputUserOf(ctx, t, alice, carol),
	}); err != nil {
		t.Fatalf("messages.hideChatJoinRequest: %v", err)
	}

	getLog := func(filter *mtproto.ChannelAdminLogEventsFilter) *mtproto.Channels_AdminLogResults {
		t.Helper()
		filter.Constructor = alice.c.Constructor(mtproto.Predicate_channelAdminLogEventsFilter)
		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetAdminLog{
			Constructor:  alice.c.Constructor(mtproto.Predicate_channels_getAdminLog),
			Channel:      input,
			EventsFilter: filter,
			Limit:        50,
		})
		if err != nil {
			t.Fatalf("channels.getAdminLog: %v", err)
		}
		return o.(interface {
			To_Channels_AdminLogResults() *mtproto.Channels_AdminLogResults
		}).To_Channels_AdminLogResults()
	}
	hasUser := func(r *mtproto.Channels_AdminLogResults, id int64) bool {
		for _, u := range r.GetUsers() {
			if u.GetId() == id {
				return true
			}
		}
		return false
	}

	info := getLog(&mtproto.ChannelAdminLogEventsFilter{Info: true})
	if len(info.GetEvents()) != 1 || info.GetEvents()[0].GetAction().GetPredicateName() != mtproto.Predicate_channelAdminLogEventActionToggleSlowMode ||
		info.GetEvents()[0].GetAction().GetNewValue_INT32() != 60 {
		t.Errorf("info: %v", info.GetEvents())
	}

	invites := getLog(&mtproto.ChannelAdminLogEventsFilter{Invites: true})
	if len(invites.GetEvents()) != 1 {
		t.Fatalf("invites: %v", invites.GetEvents())
	}
	if a := invites.GetEvents()[0].GetAction(); a.GetPredicateName() != mtproto.Predicate_channelAdminLogEventActionExportedInviteEdit ||
		a.GetPrevInvite().GetTitle().GetValue() != "first" || a.GetNewInvite().GetTitle().GetValue() != "second" {
		t.Errorf("invite edit: %v", a)
	}

	joins := getLog(&mtproto.ChannelAdminLogEventsFilter{Join: true})
	var byLink, byRequest *mtproto.ChannelAdminLogEvent
	for _, e := range joins.GetEvents() {
		switch e.GetAction().GetPredicateName() {
		case mtproto.Predicate_channelAdminLogEventActionParticipantJoinByInvite:
			byLink = e
		case mtproto.Predicate_channelAdminLogEventActionParticipantJoinByRequest:
			byRequest = e
		}
	}
	if byLink == nil || byLink.GetUserId() != bob.self.GetId() || byLink.GetAction().GetInvite().GetLink() != plain {
		t.Errorf("join by link: %v", joins.GetEvents())
	}
	if byRequest == nil || byRequest.GetUserId() != carol.self.GetId() || byRequest.GetAction().GetApprovedBy() != alice.self.GetId() ||
		byRequest.GetAction().GetInvite().GetLink() != asking {
		t.Errorf("join by request: %v", joins.GetEvents())
	}
	for _, id := range []int64{alice.self.GetId(), bob.self.GetId(), carol.self.GetId()} {
		if !hasUser(joins, id) {
			t.Errorf("user %d missing from the joins' users", id)
		}
	}
}
