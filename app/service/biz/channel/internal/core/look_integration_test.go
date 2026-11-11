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

func (f *diffFixture) updateColor(self, channelId int64, forProfile bool, color int32, emojiId int64) (*mtproto.Updates, error) {
	return f.c.ChannelUpdateColor(&channel.TLChannelUpdateColor{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, ForProfile: mtproto.ToBool(forProfile), Color: color, BackgroundEmojiId: emojiId,
	})
}

func (f *diffFixture) editUsernames(self, channelId int64, action int32, username string, active bool, order ...string) error {
	_, err := f.c.ChannelEditUsernames(&channel.TLChannelEditUsernames{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Action: action, Username: username, Active: mtproto.ToBool(active), Order: order,
	})
	return err
}

// TestUpdateColor: the creator and an admin who may change the info set
// the colors of a broadcast channel's name and profile, which the answer
// carries and the recent actions keep; the same again is not modified; a
// subscriber or an admin without the right may not; a supergroup has the
// color of its profile alone.
func TestUpdateColor(t *testing.T) {
	f := newDiffFixture(t)
	creator, editor, poster, member := itId(), itId(), itId(), itId()
	id := f.create(creator)
	f.broadcast(id)
	f.admin(id, editor, mtproto.ADMIN_CHANGE_INFO)
	f.admin(id, poster, mtproto.ADMIN_POST_MESSAGES)
	f.member(id, member)

	_, err := f.updateColor(member, id, false, 3, 0)
	wantErr(t, "a subscriber", err, mtproto.ErrChatAdminRequired)
	_, err = f.updateColor(poster, id, false, 3, 0)
	wantErr(t, "an admin without change_info", err, mtproto.ErrChatAdminRequired)

	r, err := f.updateColor(editor, id, false, 3, 777)
	if err != nil {
		t.Fatal(err)
	}
	c := r.GetChats()[0]
	if got := c.GetColor_FLAGPEERCOLOR(); got.GetColor().GetValue() != 3 || got.GetBackgroundEmojiId_FLAGINT64().GetValue() != 777 {
		t.Errorf("color: %v", got)
	}
	if c.GetProfileColor() != nil {
		t.Errorf("profile color before one is set: %v", c.GetProfileColor())
	}
	_, err = f.updateColor(creator, id, false, 3, 777)
	wantErr(t, "the same again", err, mtproto.ErrChatNotModified)

	// The profile: a pattern without a color.
	if r, err = f.updateColor(creator, id, true, channel.NoColor, 888); err != nil {
		t.Fatal(err)
	}
	c = r.GetChats()[0]
	if got := c.GetProfileColor(); got.GetColor() != nil || got.GetBackgroundEmojiId_FLAGINT64().GetValue() != 888 {
		t.Errorf("profile color: %v", got)
	}
	// No color and no pattern: none.
	if r, err = f.updateColor(creator, id, false, channel.NoColor, 0); err != nil {
		t.Fatal(err)
	}
	if got := r.GetChats()[0].GetColor_FLAGPEERCOLOR(); got != nil {
		t.Errorf("color after removing it: %v", got)
	}

	// The members read it from the channel too, a min channel included.
	mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{member}})
	if err != nil {
		t.Fatal(err)
	}
	if got := mc.GetChannel().ToMinChat().GetProfileColor(); got.GetBackgroundEmojiId_FLAGINT64().GetValue() != 888 {
		t.Errorf("min channel: profile color %v", got)
	}

	log := f.adminLog(creator, id)
	kinds := map[string]int{}
	for _, e := range log {
		kinds[e.GetAction().GetPredicateName()]++
	}
	if kinds[mtproto.Predicate_channelAdminLogEventActionChangePeerColor] != 2 || kinds[mtproto.Predicate_channelAdminLogEventActionChangeProfilePeerColor] != 1 {
		t.Errorf("recent actions: %v", kinds)
	}

	group := f.create(creator)
	_, err = f.updateColor(creator, group, false, 3, 0)
	wantErr(t, "the name color of a supergroup", err, mtproto.ErrChannelInvalid)
	if _, err = f.updateColor(creator, group, true, 3, 0); err != nil {
		t.Errorf("the profile color of a supergroup: %v", err)
	}
}

// TestUpdateEmojiStatus: a status set with its end, then removed; the same
// again is not modified.
func TestUpdateEmojiStatus(t *testing.T) {
	f := newDiffFixture(t)
	creator := itId()
	id := f.create(creator)
	f.broadcast(id)
	set := func(doc int64, until int32) (*mtproto.Updates, error) {
		return f.c.ChannelUpdateEmojiStatus(&channel.TLChannelUpdateEmojiStatus{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, DocumentId: doc, Until: until})
	}
	r, err := set(555, 2_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if s := r.GetChats()[0].GetEmojiStatus(); s.GetDocumentId() != 555 || s.GetUntil_FLAGINT32().GetValue() != 2_000_000_000 {
		t.Errorf("status: %v", s)
	}
	_, err = set(555, 2_000_000_000)
	wantErr(t, "the same again", err, mtproto.ErrChatNotModified)
	if r, err = set(0, 0); err != nil {
		t.Fatal(err)
	}
	if s := r.GetChats()[0].GetEmojiStatus(); s != nil {
		t.Errorf("status after removing it: %v", s)
	}
	_, err = set(0, 0)
	wantErr(t, "none again", err, mtproto.ErrChatNotModified)
}

// TestEditUsernames: with its editable username alone a channel has
// nothing to hide or order, and the answers say so; with another username
// (as a Fragment one would be), the creator hides, shows and orders them,
// the channel lists them all and the recent actions keep the changes; only
// the creator may.
func TestEditUsernames(t *testing.T) {
	f := newDiffFixture(t)
	creator, editor := itId(), itId()
	id := f.create(creator)
	f.broadcast(id)
	f.admin(id, editor, mtproto.ADMIN_CHANGE_INFO)
	main, extra := fmt.Sprintf("main%d", id), fmt.Sprintf("extra%d", id)
	if err := f.setUsername(creator, id, main); err != nil {
		t.Fatal(err)
	}

	wantErr(t, "an admin", f.editUsernames(editor, id, channel.UsernamesDeactivateAll, "", false), mtproto.ErrChatAdminRequired)
	if err := f.editUsernames(creator, id, channel.UsernamesDeactivateAll, "", false); err != nil {
		t.Errorf("nothing to hide: %v", err)
	}
	wantErr(t, "the order it has", f.editUsernames(creator, id, channel.UsernamesReorder, "", false, main), mtproto.ErrChatNotModified)
	wantErr(t, "showing the editable one", f.editUsernames(creator, id, channel.UsernamesToggle, main, true), mtproto.ErrUsernameNotModified)
	wantErr(t, "hiding the editable one", f.editUsernames(creator, id, channel.UsernamesToggle, main, false), mtproto.ErrUsernameInvalid)
	wantErr(t, "another's name", f.editUsernames(creator, id, channel.UsernamesToggle, "nobody"+main, true), mtproto.ErrUsernameInvalid)

	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 0, 0, ?)", extra, mtproto.PEER_CHANNEL, id, int64(9_000_000_000_000_000_000))
	usernames := func() []*mtproto.Username {
		t.Helper()
		mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{creator}})
		if err != nil {
			t.Fatal(err)
		}
		c := mc.ToChat(creator, 1)
		if c.GetUsername().GetValue() != main {
			t.Errorf("username %v, want the editable %s", c.GetUsername(), main)
		}
		return c.GetUsernames()
	}
	if err := f.editUsernames(creator, id, channel.UsernamesToggle, extra, true); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "showing it again", f.editUsernames(creator, id, channel.UsernamesToggle, extra, true), mtproto.ErrUsernameNotModified)
	wantErr(t, "an order without one", f.editUsernames(creator, id, channel.UsernamesReorder, "", false, extra), mtproto.ErrUsernameInvalid)
	wantErr(t, "an order with one twice", f.editUsernames(creator, id, channel.UsernamesReorder, "", false, extra, extra), mtproto.ErrUsernameInvalid)
	if err := f.editUsernames(creator, id, channel.UsernamesReorder, "", false, extra, main); err != nil {
		t.Fatal(err)
	}
	list := usernames()
	if len(list) != 2 || list[0].GetUsername() != extra || !list[0].GetActive() || list[0].GetEditable() || list[1].GetUsername() != main || !list[1].GetEditable() {
		t.Errorf("usernames after the order: %v", list)
	}
	if err := f.editUsernames(creator, id, channel.UsernamesDeactivateAll, "", false); err != nil {
		t.Fatal(err)
	}
	if list = usernames(); len(list) != 2 || list[0].GetActive() || !list[1].GetActive() {
		t.Errorf("usernames after hiding all: %v", list)
	}

	changes := 0
	for _, e := range f.adminLog(creator, id) {
		if e.GetAction().GetPredicateName() == mtproto.Predicate_channelAdminLogEventActionChangeUsernames {
			changes++
		}
	}
	if changes != 3 {
		t.Errorf("recent actions: %d changes of the usernames, want 3", changes)
	}

	// With the editable one and nine others shown, one more is too many.
	for i := 0; i < 9; i++ {
		f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 0, 1, 0)", fmt.Sprintf("shown%d_%d", i, id), mtproto.PEER_CHANNEL, id)
	}
	wantErr(t, "an eleventh shown", f.editUsernames(creator, id, channel.UsernamesToggle, extra, true), mtproto.ErrUsernamesActiveTooMuch)
	if err := f.editUsernames(creator, id, channel.UsernamesToggle, fmt.Sprintf("shown0_%d", id), false); err != nil {
		t.Errorf("hiding one of ten: %v", err)
	}
	if err := f.editUsernames(creator, id, channel.UsernamesToggle, extra, true); err != nil {
		t.Errorf("the tenth shown: %v", err)
	}
}

// adminLog is the channel's recent actions as self, an admin, reads them.
func (f *diffFixture) adminLog(self, channelId int64) []*mtproto.ChannelAdminLogEvent {
	f.t.Helper()
	r, err := f.c.ChannelGetAdminLog(&channel.TLChannelGetAdminLog{SelfId: self, ChannelId: channelId, Filter: -1, Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	return r.GetDatas()
}
