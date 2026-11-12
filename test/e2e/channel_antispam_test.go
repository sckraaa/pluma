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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// antispamUserId is the antispam account (telegram_antispam_user_id of the
// app config).
const antispamUserId = 1000001

// TestChannelAntispam (task 082) end to end: the creator turns the antispam
// of a supergroup on once it has telegram_antispam_group_size_min members,
// a member may not; the full channel says so. The link of Bob, who joined
// just now, is deleted for him by the antispam: the recent actions show the
// deletion from the antispam account, whom they bring along. Alice reports
// it as wrong, true; a message the antispam did not delete is false; Bob's
// next link stays.
func TestChannelAntispam(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, gr := createChannel(ctx, t, alice, "antispam", true)
	hash := gr.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	toggle := func(u *user, on bool) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsToggleAntiSpam{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_toggleAntiSpam),
			Channel:     input(u),
			Enabled:     mtproto.ToBool(on),
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	report := func(id int32) (bool, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsReportAntiSpamFalsePositive{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_reportAntiSpamFalsePositive),
			Channel:     input(alice),
			MsgId:       id,
		})
		if err != nil {
			return false, err
		}
		_, ok := o.(*mtproto.TLBoolTrue)
		return ok, nil
	}

	if _, err := toggle(alice, true); !mtclient.IsRPCError(err, "PARTICIPANTS_TOO_FEW") {
		t.Errorf("a group of two: %v, want PARTICIPANTS_TOO_FEW", err)
	}
	setParticipantsCount(t, gr.GetId(), 10)
	if _, err := toggle(bob, true); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("Bob turns it on: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	r, err := toggle(alice, true)
	if err != nil {
		t.Fatalf("channels.toggleAntiSpam: %v", err)
	}
	if len(r.GetUpdates()) == 0 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Errorf("the answer: %v", r.GetUpdates())
	}
	if _, err = toggle(alice, true); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("again: %v, want CHAT_NOT_MODIFIED", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input(alice),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	if full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat(); !full.GetAntispam() {
		t.Errorf("the full channel has no antispam: %v", full)
	}

	getState(ctx, t, bob)
	spam := sendMessageID(ctx, t, bob, peer(bob), "cheap followers at https://spam.example", randomID())
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateDeleteChannelMessages && u.GetChannelId() == gr.GetId() && slices.Contains(u.GetMessages(), spam) {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob's link is not deleted: %v", err)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetAdminLog{
		Constructor:  alice.c.Constructor(mtproto.Predicate_channels_getAdminLog),
		Channel:      input(alice),
		EventsFilter: &mtproto.ChannelAdminLogEventsFilter{Constructor: alice.c.Constructor(mtproto.Predicate_channelAdminLogEventsFilter), Delete: true},
		Limit:        50,
	})
	if err != nil {
		t.Fatalf("channels.getAdminLog: %v", err)
	}
	log := o.(interface {
		To_Channels_AdminLogResults() *mtproto.Channels_AdminLogResults
	}).To_Channels_AdminLogResults()
	var found bool
	for _, e := range log.GetEvents() {
		m := e.GetAction().GetMessage()
		if e.GetUserId() == antispamUserId && m.GetId() == spam && m.GetFromId().GetUserId() == bob.self.GetId() {
			found = true
		}
	}
	if !found {
		t.Errorf("the recent actions have no deletion of %d by the antispam: %v", spam, log.GetEvents())
	}
	if !slices.ContainsFunc(log.GetUsers(), func(u *mtproto.User) bool { return u.GetId() == antispamUserId }) {
		t.Errorf("the recent actions do not bring the antispam account: %v", log.GetUsers())
	}

	if ok, err := report(spam); err != nil || !ok {
		t.Errorf("channels.reportAntiSpamFalsePositive: %v, %v; want true", ok, err)
	}
	if ok, err := report(1); err != nil || ok {
		t.Errorf("a message the antispam did not delete: %v, %v; want false", ok, err)
	}

	// Alice vouched for Bob: his next link stays.
	kept := sendMessageID(ctx, t, bob, peer(bob), "the docs are at https://docs.example", randomID())
	time.Sleep(3 * time.Second)
	o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetMessages{
		Constructor:           bob.c.Constructor(mtproto.Predicate_channels_getMessages),
		Channel:               input(bob),
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{mtproto.MakeTLInputMessageID(&mtproto.InputMessage{Id: kept}).To_InputMessage()},
	})
	if err != nil {
		t.Fatalf("channels.getMessages: %v", err)
	}
	if ms := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(ms) != 1 ||
		ms[0].GetPredicateName() != mtproto.Predicate_message {
		t.Errorf("Bob's link after the report: %v", ms)
	}
}

// TestChannelAutotranslationAndGeo (task 082) end to end: the creator of a
// broadcast channel translates it for all, the answer says so, and again is
// CHAT_NOT_MODIFIED; a supergroup has no such setting. Geo groups do not
// exist: an address is MEGAGROUP_GEO_REQUIRED in a supergroup and
// MEGAGROUP_REQUIRED in a channel, a geo group is not created, and people
// nearby are not served.
func TestChannelAutotranslationAndGeo(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	_, ch := createChannel(ctx, t, alice, "translated", false)
	_, gr := createChannel(ctx, t, alice, "no address", true)
	input := func(c *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	toggle := func(c *mtproto.Chat) (*mtproto.Updates, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleAutotranslation{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleAutotranslation),
			Channel:     input(c),
			Enabled:     mtproto.BoolTrue,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	r, err := toggle(ch)
	if err != nil {
		t.Fatalf("channels.toggleAutotranslation: %v", err)
	}
	if len(r.GetChats()) == 0 || !r.GetChats()[0].GetAutotranslation() {
		t.Errorf("the answer's channel: %v", r.GetChats())
	}
	if _, err = toggle(ch); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("again: %v, want CHAT_NOT_MODIFIED", err)
	}
	if _, err = toggle(gr); !mtclient.IsRPCError(err, "CHANNEL_INVALID") {
		t.Errorf("a supergroup: %v, want CHANNEL_INVALID", err)
	}

	point := &mtproto.InputGeoPoint{Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPoint), Lat: 52.52, Long: 13.4}
	for c, want := range map[*mtproto.Chat]string{gr: "MEGAGROUP_GEO_REQUIRED", ch: "MEGAGROUP_REQUIRED"} {
		if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsEditLocation{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_editLocation),
			Channel:     input(c),
			GeoPoint:    point,
			Address:     "Street 1",
		}); !mtclient.IsRPCError(err, want) {
			t.Errorf("channels.editLocation of %s: %v, want %s", c.GetTitle(), err, want)
		}
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsCreateChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_createChannel),
		Megagroup:   true,
		Title:       "nearby",
		GeoPoint:    point,
		Address:     wrapperspb.String("Street 1"),
	}); !mtclient.IsRPCError(err, "CHANNELS_ADMIN_LOCATED_TOO_MUCH") {
		t.Errorf("a geo group: %v, want CHANNELS_ADMIN_LOCATED_TOO_MUCH", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLContactsGetLocated{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_getLocated),
		GeoPoint:    point,
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("contacts.getLocated: %v, want ERR_ENTERPRISE_IS_BLOCKED", err)
	}
}
