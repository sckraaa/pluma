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
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) asks(channelId int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_join_requests WHERE channel_id = ?", channelId).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// requestUpdates counts the seq inbox events of recipient: each request a
// change of the requests tells it.
func (f *diffFixture) requestUpdates(recipient int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND topic = ? AND method = ?",
		recipient, itInboxTopic, "inbox.TL_inbox_seqUpdatesToInbox").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *diffFixture) hide(self, channelId, userId int64, approved bool) (*mtproto.Updates, error) {
	return f.c.ChannelHideChatJoinRequests(&channel.TLChannelHideChatJoinRequests{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId,
		Approved: mtproto.ToBool(approved), All: mtproto.BoolFalse, UserId: userId,
	})
}

func (f *diffFixture) hideAll(self, channelId int64, approved bool, link string) (*mtproto.Updates, error) {
	return f.c.ChannelHideChatJoinRequests(&channel.TLChannelHideChatJoinRequests{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId,
		Approved: mtproto.ToBool(approved), All: mtproto.BoolTrue, Link: link,
	})
}

func (f *diffFixture) export(self, channelId int64, requestNeeded bool, expire, limit int32, title string) (*mtproto.ExportedChatInvite, error) {
	return f.c.ChannelExportChatInvite(&channel.TLChannelExportChatInvite{
		SelfId: self, ChannelId: channelId, LegacyRevokePermanent: mtproto.BoolFalse,
		RequestNeeded: mtproto.ToBool(requestNeeded), ExpireDate: expire, UsageLimit: limit, Title: title,
	})
}

func (f *diffFixture) toggleRequests(self, channelId int64, on, applyToInvites bool) (*mtproto.Updates, error) {
	return f.c.ChannelToggleJoinRequest(&channel.TLChannelToggleJoinRequest{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Enabled: mtproto.ToBool(on), ApplyToInvites: mtproto.ToBool(applyToInvites),
	})
}

func inviteHash(t *testing.T, e *mtproto.ExportedChatInvite) string {
	t.Helper()
	l := e.GetLink()
	if len(l) < 20 {
		t.Fatalf("link %q", l)
	}
	return l[len(l)-20:]
}

// TestJoinRequests: a link that needs approval and a public channel with
// join_request write one request each, the admins see them, a member sees
// nothing; approving makes the user a member with one service message and
// an inbox event, a second approval changes nothing; a declined user is
// not a member; those who may not answer get errors; a group with
// join_to_send off keeps its toggle.
func TestJoinRequests(t *testing.T) {
	f := newDiffFixture(t)
	creator, admin, anna, bob, carol := f.user("Creator"), f.user("Admin"), f.user("Anna"), f.user("Bob"), f.user("Carol")
	id := f.create(creator)
	f.public(id)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 2)", id, admin, channel.RoleAdmin, mtproto.ADMIN_INVITE_USERS)
	f.exec("UPDATE channels SET participants_count = 2 WHERE id = ?", id)

	// One who may not answer turns nothing on.
	if _, err := f.toggleRequests(anna, id, true, false); err == nil {
		t.Error("a non-member toggles the setting")
	}
	if _, err := f.toggleRequests(admin, id, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.toggleRequests(admin, id, true, false); !errors.Is(err, mtproto.ErrChatNotModified) {
		t.Errorf("the same value: %v", err)
	}

	// channels.joinChannel of a public channel with join_request.
	before := f.requestUpdates(creator)
	if _, err := f.join(anna, id, ""); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Fatalf("the join: %v", err)
	}
	if _, err := f.join(anna, id, ""); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Errorf("asking again: %v", err)
	}
	if f.asks(id) != 1 || f.requestUpdates(creator) != before+1 {
		t.Errorf("requests %d, updates of the creator %d (was %d)", f.asks(id), f.requestUpdates(creator), before)
	}
	if n, _ := f.counts(id); n != 2 {
		t.Errorf("a request counted as a member: %d", n)
	}

	// The admin sees the request; an ordinary member does not ask for it.
	imp, err := f.c.ChannelGetChatInviteImporters(&channel.TLChannelGetChatInviteImporters{SelfId: admin, ChannelId: id, Requested: mtproto.BoolTrue, Limit: 10})
	if err != nil || imp.GetCount() != 1 || imp.GetImporters()[0].GetUserId() != anna {
		t.Fatalf("importers %v, %v", imp, err)
	}
	if _, err = f.c.ChannelGetPendingJoinRequests(&channel.TLChannelGetPendingJoinRequests{SelfId: anna, ChannelId: id}); !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Errorf("getPendingJoinRequests of one not in: %v", err)
	}

	// Approved: a member, one service message, one inbox event.
	r, err := f.hide(admin, id, anna, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, state, _, _, _ := f.row(id, anna); state != channel.StateIn {
		t.Errorf("state %d", state)
	}
	var msg *mtproto.Message
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			msg = u.GetMessage_MESSAGE()
		}
		if u.GetPredicateName() == mtproto.Predicate_updatePendingJoinRequests && u.GetRequestsPending() != 0 {
			t.Errorf("requests left %v", u)
		}
	}
	if msg.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatJoinedByRequest || msg.GetFromId().GetUserId() != anna {
		t.Errorf("service message %v", msg)
	}
	if n, _ := f.counts(id); n != 3 || f.asks(id) != 0 || f.invitedEvents(anna) != 1 {
		t.Errorf("count %d, requests %d, events %d", n, f.asks(id), f.invitedEvents(anna))
	}

	// The approval again: nothing changes.
	if _, err = f.hide(admin, id, anna, true); err != nil {
		t.Errorf("a second approval: %v", err)
	}
	if n, _ := f.counts(id); n != 3 || f.invitedEvents(anna) != 1 {
		t.Errorf("a second approval counted: %d, events %d", n, f.invitedEvents(anna))
	}

	// A declined user stays out.
	if _, err = f.join(bob, id, ""); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Fatal(err)
	}
	if _, err = f.hide(creator, id, bob, false); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.counts(id); n != 3 || f.asks(id) != 0 {
		t.Errorf("decline: %d, requests %d", n, f.asks(id))
	}

	// Who is added otherwise stops asking; the admin sees it.
	if _, err = f.join(carol, id, ""); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Fatal(err)
	}
	if _, err = f.invite(creator, id, carol); err != nil {
		t.Fatal(err)
	}
	if f.asks(id) != 0 {
		t.Errorf("the request of one added stands")
	}

	// A plain member may not answer.
	if _, err = f.hide(bob, id, carol, true); !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Errorf("a stranger answers: %v", err)
	}
}

// TestJoinRequestByLink: a link with request_needed asks, approving through
// it counts one use; a kick ends a request; expiry and revocation answer
// INVITE_HASH_EXPIRED; a link with a usage limit that needs no approval
// stops at its limit, a user back again does not use it up.
func TestJoinRequestByLink(t *testing.T) {
	f := newDiffFixture(t)
	creator, anna, bob := f.user("Creator"), f.user("Anna"), f.user("Bob")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)

	asked, err := f.export(creator, id, true, 0, 5, "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if !asked.GetRequestNeeded() || asked.GetUsageLimit() != nil || asked.GetTitle().GetValue() != "reviewed" {
		t.Errorf("exported %v", asked)
	}
	hash := inviteHash(t, asked)
	if _, err = f.join(anna, 0, hash); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Fatalf("join by link: %v", err)
	}
	got, err := f.c.ChannelGetExportedChatInvite(&channel.TLChannelGetExportedChatInvite{SelfId: creator, ChannelId: id, Link: hash})
	if err != nil || got.GetRequested().GetValue() != 1 || got.GetUsage() != nil {
		t.Errorf("before: %v, %v", got, err)
	}
	if _, err = f.hide(creator, id, anna, true); err != nil {
		t.Fatal(err)
	}
	got, err = f.c.ChannelGetExportedChatInvite(&channel.TLChannelGetExportedChatInvite{SelfId: creator, ChannelId: id, Link: hash})
	if err != nil || got.GetUsage().GetValue() != 1 || got.GetRequested() != nil {
		t.Errorf("after: %v, %v", got, err)
	}
	imp, err := f.c.ChannelGetChatInviteImporters(&channel.TLChannelGetChatInviteImporters{SelfId: creator, ChannelId: id, Requested: mtproto.BoolFalse, Link: hash, Limit: 10})
	if err != nil || imp.GetCount() != 1 || imp.GetImporters()[0].GetApprovedBy().GetValue() != creator {
		t.Errorf("importers %v, %v", imp, err)
	}

	// A kick ends the request; the kicked user cannot ask again.
	if _, err = f.join(bob, 0, hash); !errors.Is(err, mtproto.ErrInviteRequestSent) {
		t.Fatal(err)
	}
	if _, err = f.editBanned(creator, id, bob, mtproto.BANNED_VIEW_MESSAGES, 0); err != nil {
		t.Fatal(err)
	}
	if f.asks(id) != 0 {
		t.Errorf("the request of one kicked stands")
	}

	// Expiry, revocation and the limit.
	gone := f.link(id, creator, time.Now().Unix()-10, 0)
	if _, err = f.join(bob, 0, gone); err == nil {
		t.Errorf("an expired link works")
	}
	carol, dave := f.user("Carol"), f.user("Dave")
	limited, err := f.export(creator, id, false, 0, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	h2 := inviteHash(t, limited)
	if _, err = f.join(carol, 0, h2); err != nil {
		t.Fatal(err)
	}
	if _, err = f.join(dave, 0, h2); !errors.Is(err, mtproto.ErrInviteHashExpired) {
		t.Errorf("used up: %v", err)
	}
	if _, err = f.c.ChannelEditExportedChatInvite(&channel.TLChannelEditExportedChatInvite{
		SelfId: creator, ChannelId: id, Link: h2, Revoked: mtproto.BoolTrue, RequestNeeded: mtproto.BoolFalse,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.leave(carol, id); err != nil {
		t.Fatal(err)
	}
	if _, err = f.join(carol, 0, h2); !errors.Is(err, mtproto.ErrInviteHashExpired) {
		t.Errorf("a revoked link: %v", err)
	}
}

// TestJoinRequestsConcurrent: 16 users ask and two admins answer the same
// requests concurrently, approving all at once and one by one: no one is
// counted twice and the count matches the rows.
func TestJoinRequestsConcurrent(t *testing.T) {
	f := newDiffFixture(t)
	creator, admin := f.user("Creator"), f.user("Admin")
	id := f.create(creator)
	f.public(id)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 2)", id, admin, channel.RoleAdmin, mtproto.ADMIN_INVITE_USERS)
	f.exec("UPDATE channels SET participants_count = 2 WHERE id = ?", id)
	if _, err := f.toggleRequests(creator, id, true, false); err != nil {
		t.Fatal(err)
	}
	users := make([]int64, 0, 16)
	for i := range 16 {
		users = append(users, f.user(fmt.Sprint("U", i)))
	}
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.join(u, id, ""); !errors.Is(err, mtproto.ErrInviteRequestSent) {
				t.Errorf("join %d: %v", u, err)
			}
		}()
	}
	wg.Wait()
	if f.asks(id) != len(users) {
		t.Fatalf("requests %d", f.asks(id))
	}
	for _, by := range []int64{creator, admin} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.hideAll(by, id, true, ""); err != nil {
				t.Errorf("approve all by %d: %v", by, err)
			}
		}()
	}
	for _, u := range users[:4] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.hide(admin, id, u, true); err != nil {
				t.Errorf("approve %d: %v", u, err)
			}
		}()
	}
	wg.Wait()

	var rows, msgs int32
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = 0", id).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND JSON_EXTRACT(message_data, '$.action.predicate_name') = 'messageActionChatJoinedByRequest'", id).Scan(&msgs); err != nil {
		t.Fatal(err)
	}
	n, _ := f.counts(id)
	if rows != 18 || n != 18 || msgs != 16 || f.asks(id) != 0 {
		t.Errorf("rows %d, count %d, service messages %d, requests %d", rows, n, msgs, f.asks(id))
	}
}

// TestInviteLinks: export, the page of links, the permanent link replaced,
// edits of the kinds a link may have, delete; the links of others are the
// creator's only.
func TestInviteLinks(t *testing.T) {
	f := newDiffFixture(t)
	creator, admin, member := f.user("Creator"), f.user("Admin"), f.user("Member")
	id := f.create(creator)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 2), (?, ?, 0, 0, 3)", id, admin, channel.RoleAdmin, mtproto.ADMIN_INVITE_USERS, id, member)
	f.exec("UPDATE channels SET participants_count = 3 WHERE id = ?", id)

	if _, err := f.export(member, id, false, 0, 0, ""); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Errorf("a member makes a link: %v", err)
	}
	if _, err := f.export(creator, id, false, int32(time.Now().Unix()-5), 0, ""); !errors.Is(err, mtproto.ErrExpireDateInvalid) {
		t.Errorf("a past expire date: %v", err)
	}
	if _, err := f.export(creator, id, false, 0, -1, ""); !errors.Is(err, mtproto.ErrUsageLimitInvalid) {
		t.Errorf("a negative limit: %v", err)
	}

	// The permanent link.
	perm, err := f.c.ChannelExportChatInvite(&channel.TLChannelExportChatInvite{SelfId: creator, ChannelId: id, LegacyRevokePermanent: mtproto.BoolTrue, RequestNeeded: mtproto.BoolFalse})
	if err != nil || !perm.GetPermanent() {
		t.Fatalf("permanent: %v, %v", perm, err)
	}
	mine, err := f.export(creator, id, false, 0, 0, "t1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.c.ChannelGetExportedChatInvites(&channel.TLChannelGetExportedChatInvites{SelfId: creator, ChannelId: id, AdminId: creator, Revoked: mtproto.BoolFalse, Limit: 10})
	if err != nil || page.GetCount() != 2 || !page.GetInvites()[0].GetPermanent() {
		t.Fatalf("page %v, %v", page, err)
	}
	if next, err := f.c.ChannelGetExportedChatInvites(&channel.TLChannelGetExportedChatInvites{SelfId: creator, ChannelId: id, AdminId: creator, Revoked: mtproto.BoolFalse, OffsetLink: page.GetInvites()[0].GetLink(), Limit: 10}); err != nil || len(next.GetInvites()) != 1 || next.GetInvites()[0].GetLink() != mine.GetLink() {
		t.Errorf("next page %v, %v", next, err)
	}

	// An admin reads its own, not the creator's.
	if _, err = f.c.ChannelGetExportedChatInvites(&channel.TLChannelGetExportedChatInvites{SelfId: admin, ChannelId: id, AdminId: creator, Revoked: mtproto.BoolFalse, Limit: 10}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Errorf("an admin reads another's links: %v", err)
	}
	if _, err = f.export(admin, id, false, 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	admins, err := f.c.ChannelGetAdminsWithInvites(&channel.TLChannelGetAdminsWithInvites{SelfId: creator, ChannelId: id})
	if err != nil || len(admins.GetAdmins()) != 2 {
		t.Errorf("admins %v, %v", admins, err)
	}
	if _, err = f.c.ChannelGetAdminsWithInvites(&channel.TLChannelGetAdminsWithInvites{SelfId: admin, ChannelId: id}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Errorf("an admin reads the admins: %v", err)
	}

	edit := func(req *channel.TLChannelEditExportedChatInvite) (*mtproto.Messages_ExportedChatInvite, error) {
		req.SelfId, req.ChannelId = creator, id
		if req.Revoked == nil {
			req.Revoked = mtproto.BoolFalse
		}
		if req.RequestNeeded == nil {
			req.RequestNeeded = mtproto.BoolFalse
		}
		return f.c.ChannelEditExportedChatInvite(req)
	}
	// A permanent link takes a title but no limit.
	if _, err = edit(&channel.TLChannelEditExportedChatInvite{Link: perm.GetLink(), Fields: channel.InviteFieldUsageLimit, UsageLimit: 5}); !errors.Is(err, mtproto.ErrChatInvitePermanent) {
		t.Errorf("a limit on a permanent link: %v", err)
	}
	if r, err := edit(&channel.TLChannelEditExportedChatInvite{Link: perm.GetLink(), Fields: channel.InviteFieldTitle, Title: "main"}); err != nil || r.GetInvite().GetTitle().GetValue() != "main" {
		t.Errorf("a title on a permanent link: %v, %v", r, err)
	}
	// Approval takes the limit away.
	if r, err := edit(&channel.TLChannelEditExportedChatInvite{Link: mine.GetLink(), Fields: channel.InviteFieldUsageLimit, UsageLimit: 3}); err != nil || r.GetInvite().GetUsageLimit().GetValue() != 3 {
		t.Errorf("a limit: %v, %v", r, err)
	}
	if r, err := edit(&channel.TLChannelEditExportedChatInvite{Link: mine.GetLink(), Fields: channel.InviteFieldRequestNeeded, RequestNeeded: mtproto.BoolTrue}); err != nil || !r.GetInvite().GetRequestNeeded() || r.GetInvite().GetUsageLimit() != nil {
		t.Errorf("approval: %v, %v", r, err)
	}

	// A permanent link revoked is replaced by a new one.
	r, err := edit(&channel.TLChannelEditExportedChatInvite{Link: perm.GetLink(), Revoked: mtproto.BoolTrue})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_exportedChatInviteReplaced || !r.GetInvite().GetRevoked() || r.GetNewInvite().GetLink() == perm.GetLink() {
		t.Fatalf("replaced: %v, %v", r, err)
	}
	if _, err = edit(&channel.TLChannelEditExportedChatInvite{Link: perm.GetLink(), Fields: channel.InviteFieldTitle, Title: "x"}); !errors.Is(err, mtproto.ErrInviteHashExpired) {
		t.Errorf("an edit of a revoked link: %v", err)
	}

	// Delete: only revoked links; the revoked ones all at once.
	if _, err = f.c.ChannelDeleteExportedChatInvite(&channel.TLChannelDeleteExportedChatInvite{SelfId: creator, ChannelId: id, Link: mine.GetLink()}); !errors.Is(err, mtproto.ErrInviteRevokedMissing) {
		t.Errorf("delete of a working link: %v", err)
	}
	if _, err = f.c.ChannelDeleteRevokedExportedChatInvites(&channel.TLChannelDeleteRevokedExportedChatInvites{SelfId: creator, ChannelId: id, AdminId: creator}); err != nil {
		t.Fatal(err)
	}
	rev, err := f.c.ChannelGetExportedChatInvites(&channel.TLChannelGetExportedChatInvites{SelfId: creator, ChannelId: id, AdminId: creator, Revoked: mtproto.BoolTrue, Limit: 10})
	if err != nil || rev.GetCount() != 0 {
		t.Errorf("revoked left %v, %v", rev, err)
	}
}

// TestLeftChannels: the channels one left, in pages; a channel one was
// kicked from or is still in is not among them.
func TestLeftChannels(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	var left []int64
	for range 3 {
		id := f.create(creator)
		f.public(id)
		if _, err := f.join(bob, id, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.leave(bob, id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	stay := f.create(creator)
	f.public(stay)
	if _, err := f.join(bob, stay, ""); err != nil {
		t.Fatal(err)
	}
	r, err := f.c.ChannelGetLeftChannels(&channel.TLChannelGetLeftChannels{SelfId: bob})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_chats || len(r.GetChats()) != 3 {
		t.Fatalf("left: %v, %v", r, err)
	}
	ids := []int64{}
	for _, c := range r.GetChats() {
		ids = append(ids, c.GetId())
		if c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() {
			t.Errorf("a public channel left: %v", c)
		}
	}
	slices.Sort(ids)
	slices.Sort(left)
	if !slices.Equal(ids, left) {
		t.Errorf("left %v, want %v", ids, left)
	}
	if r, err = f.c.ChannelGetLeftChannels(&channel.TLChannelGetLeftChannels{SelfId: bob, Offset: 2}); err != nil || r.GetPredicateName() != mtproto.Predicate_messages_chatsSlice || r.GetCount() != 3 || len(r.GetChats()) != 1 {
		t.Errorf("second page: %v, %v", r, err)
	}
}
