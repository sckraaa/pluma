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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// TestGhostNotAdded: nobody adds the support ghost (ADR 0048) to a basic
// group, where it would be shown added; the group stays as it was.
func TestGhostNotAdded(t *testing.T) {
	u := newUsers(3)
	a, b, support := u[0], u[1], u[2]
	if _, err := itRaw.Exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, ?, 'US', 'Support', 1)",
		support, fmt.Sprint("1777", support)); err != nil {
		t.Fatal(err)
	}
	id := newChat(t, a, b)
	before := rowOf(t, id)
	_, err := chatCore().ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: support})
	wantErr(t, "add the ghost", err, mtproto.ErrUserPrivacyRestricted)
	if p := participantOf(t, id, support); p != nil {
		t.Errorf("the ghost's row: %+v", p)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the chat row %+v, want %+v", after, before)
	}
}

// newGhostUser writes the account of the support ghost.
func newGhostUser(t *testing.T, id int64) {
	t.Helper()
	if _, err := itRaw.Exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, ?, 'US', 'Support', 1)",
		id, fmt.Sprint("1777", id)); err != nil {
		t.Fatal(err)
	}
}

func hiddenOf(t *testing.T, chatId, userId int64) bool {
	t.Helper()
	return count(t, "SELECT COUNT(*) FROM chat_participants WHERE chat_id = ? AND user_id = ? AND state = 0 AND hidden = 1", chatId, userId) == 1
}

// TestGhostInBasicGroup: the ghost comes into a basic group hidden, by the
// operator's command or by a link, in the creator's name: its row, the list
// of the members with itself to it alone and the event that brings it the
// history; the others' list, its version and the number of members stay,
// and so do they when it leaves or an admin removes it. A change of the
// members tells the others the list without it and the ghost with it. It
// is made no admin and no creator; it is never the common chat of another,
// nor in the preview of a link. ghost set shows and hides its rows again.
func TestGhostInBasicGroup(t *testing.T) {
	u := newUsers(4)
	a, b, c, support := u[0], u[1], u[2], u[3]
	newGhostUser(t, support)
	id := newChat(t, a, b)
	cc := chatCore()
	before := rowOf(t, id)
	mark := lastEvent(t)

	_, err := cc.ChatJoinHidden(&chat.TLChatJoinHidden{UserId: a, ChatId: id})
	wantErr(t, "no ghost", err, mtproto.ErrUserIdInvalid)

	r, err := cc.ChatJoinHidden(&chat.TLChatJoinHidden{UserId: support, ChatId: id})
	if err != nil || !mtproto.FromBool(r) {
		t.Fatalf("join: %v, %v", r, err)
	}
	if !hiddenOf(t, id, support) {
		t.Error("no hidden row")
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the chat row %+v, want %+v", after, before)
	}
	expect(t, &mark, "hidden join",
		fmt.Sprintf("%d history of %d mention false", support, id),
		fmt.Sprintf("%d members v%d %v", support, before.version, []int64{a, b, support}))
	if r, err = cc.ChatJoinHidden(&chat.TLChatJoinHidden{UserId: support, ChatId: id}); err != nil || mtproto.FromBool(r) {
		t.Errorf("again: %v, %v", r, err)
	}
	expect(t, &mark, "again")

	shown := cached(t, id)
	if n := len(shown.ToChatParticipants(a).GetParticipants()); n != 2 {
		t.Errorf("a sees %d members", n)
	}
	if n := len(shown.ToChatParticipants(support).GetParticipants()); n != 3 {
		t.Errorf("the ghost sees %d members", n)
	}
	if v := shown.ToUnsafeChat(support); v.GetLeft() || v.GetParticipantsCount_INT32() != 2 {
		t.Errorf("the ghost's chat %+v", v)
	}

	if _, err = cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: c}); err != nil {
		t.Fatal(err)
	}
	v := before.version + 1
	members := fmt.Sprintf("members v%d %v", v, []int64{a, b, c})
	expect(t, &mark, "add",
		fmt.Sprintf("%d echo %s 3 users [chat]", a, members),
		fmt.Sprintf("%d %s", b, members), fmt.Sprintf("%d %s", c, members),
		fmt.Sprintf("%d members v%d %v", support, v, []int64{a, b, support, c}))
	if got := rowOf(t, id).count; got != 3 {
		t.Errorf("count %d after an add, want 3", got)
	}

	_, err = cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: support, IsAdmin: mtproto.BoolTrue})
	wantErr(t, "make it an admin", err, mtproto.ErrUserPrivacyRestricted)
	_, err = cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: support, IsAdmin: mtproto.BoolFalse})
	wantErr(t, "take back a right it has not", err, mtproto.ErrUserNotParticipant)
	_, err = cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: a, ChatId: id, UserId: support})
	wantErr(t, "make it the creator", err, mtproto.ErrUserPrivacyRestricted)
	expect(t, &mark, "refused")

	common, err := cc.ChatGetUsersChatIdList(&chat.TLChatGetUsersChatIdList{Id: []int64{support}})
	if err != nil || len(common.GetDatas()) != 0 {
		t.Errorf("common chats of the ghost %v, %v", common, err)
	}

	// An admin removes it: only the ghost hears of it.
	before = rowOf(t, id)
	if _, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: support}); err != nil {
		t.Fatal(err)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("removal changed the chat %+v, want %+v", after, before)
	}
	expect(t, &mark, "removal", fmt.Sprintf("%d forbidden", support))

	// Back by a link: hidden, no use of the link, no request.
	link := newLink(t, id, a, true, 0, false)
	joined, err := join(support, link)
	if err != nil || joined.GetRequesters() != nil || !hiddenOf(t, id, support) {
		t.Fatalf("by a link: %v, %v", joined, err)
	}
	if p, _ := joined.GetChat().GetImmutableChatParticipant(support); !p.GetHidden() {
		t.Error("the answer does not show the join hidden")
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_invite_participants WHERE chat_id = ? AND user_id = ?", id, support); n != 0 {
		t.Errorf("%d uses of the link", n)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the link join changed the chat %+v", after)
	}
	expect(t, &mark, "by a link",
		fmt.Sprintf("%d history of %d mention false", support, id),
		fmt.Sprintf("%d members v%d %v", support, before.version, []int64{a, b, support, c}))
	_, err = join(support, link)
	wantErr(t, "by a link again", err, mtproto.ErrUserAlreadyParticipant)
	mark = lastEvent(t)

	// ghost set off and on: shown and counted, hidden again.
	n, err := cc.ChatSetUserHidden(&chat.TLChatSetUserHidden{UserId: support, Hidden: mtproto.BoolFalse})
	if err != nil || n.GetV() != 1 || hiddenOf(t, id, support) || rowOf(t, id).count != 4 {
		t.Errorf("shown: %v, %v, count %d", n, err, rowOf(t, id).count)
	}
	if n, err = cc.ChatSetUserHidden(&chat.TLChatSetUserHidden{UserId: support, Hidden: mtproto.BoolTrue}); err != nil || n.GetV() != 1 || !hiddenOf(t, id, support) || rowOf(t, id).count != 3 {
		t.Errorf("hidden: %v, %v, count %d", n, err, rowOf(t, id).count)
	}
	if n, err = cc.ChatSetUserHidden(&chat.TLChatSetUserHidden{UserId: support, Hidden: mtproto.BoolTrue}); err != nil || n.GetV() != 0 {
		t.Errorf("hidden again: %v, %v", n, err)
	}
	mark = lastEvent(t)

	// It leaves by the command: unseen.
	before = rowOf(t, id)
	if r, err = cc.ChatLeaveHidden(&chat.TLChatLeaveHidden{UserId: support, ChatId: id}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("leave: %v, %v", r, err)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the leave changed the chat %+v", after)
	}
	expect(t, &mark, "leave", fmt.Sprintf("%d echo forbidden 0 users [left]", support))
	if r, err = cc.ChatLeaveHidden(&chat.TLChatLeaveHidden{UserId: support, ChatId: id}); err != nil || mtproto.FromBool(r) {
		t.Errorf("leave again: %v, %v", r, err)
	}
}

// TestGhostRemovedWhenAbsent: an admin who removes the ghost from a basic
// group it is not in gets the answer of a removal from inside, not
// USER_NOT_PARTICIPANT, which would tell whether it was there; nobody hears
// of it, and a mention no longer brings it in. One who may not remove
// still gets CHAT_ADMIN_REQUIRED, and an ordinary user who is not in is
// still USER_NOT_PARTICIPANT.
func TestGhostRemovedWhenAbsent(t *testing.T) {
	u := newUsers(4)
	a, b, c, support := u[0], u[1], u[2], u[3]
	newGhostUser(t, support)
	id := newChat(t, a, b)
	cc := chatCore()
	before := rowOf(t, id)
	mark := lastEvent(t)

	_, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: b, DeleteUserId: support})
	wantErr(t, "a member who may not remove", err, mtproto.ErrChatAdminRequired)
	_, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: c})
	wantErr(t, "an ordinary user who is not in", err, mtproto.ErrUserNotParticipant)

	// Admin, creator and tag answer as they do for the ghost inside
	// (TestGhostInBasicGroup, ghostAnswers).
	ghostAnswers(t, cc, id, a, b, support, "absent")
	expect(t, &mark, "refused absent")

	r, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: support})
	if err != nil {
		t.Fatalf("remove the absent ghost: %v", err)
	}
	if p, _ := r.GetImmutableChatParticipant(support); !p.GetHidden() || !p.IsChatMemberStateKicked() {
		t.Errorf("the answer's row of the ghost %+v, want hidden and kicked", p)
	}
	if after := rowOf(t, id); after != before {
		t.Errorf("the removal changed the chat %+v, want %+v", after, before)
	}
	expect(t, &mark, "absent removal")
	if n := len(cached(t, id).ToChatParticipants(a).GetParticipants()); n != 2 {
		t.Errorf("a sees %d members", n)
	}
	if n := count(t, "SELECT COUNT(*) FROM chat_participants WHERE chat_id = ? AND user_id = ? AND state = ? AND hidden = 1", id, support, mtproto.ChatMemberStateKicked); n != 1 {
		t.Errorf("%d kicked hidden rows of the ghost", n)
	}

	// Again: the same answer.
	if _, err = cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: support}); err != nil {
		t.Errorf("again: %v", err)
	}
	expect(t, &mark, "absent removal again")

	// Inside, hidden: the same answers.
	if r, err := cc.ChatJoinHidden(&chat.TLChatJoinHidden{UserId: support, ChatId: id}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("join: %v, %v", r, err)
	}
	mark = lastEvent(t)
	ghostAnswers(t, cc, id, a, b, support, "inside")
	expect(t, &mark, "refused inside")
}

// ghostAnswers checks what the creator a and the plain member b of the
// basic group chatId are told about the ghost support, the same whether it
// is in the group or not (ADR 0048): no admin, no creator, no tag.
func ghostAnswers(t *testing.T, cc *ChatCore, chatId, a, b, support int64, where string) {
	t.Helper()
	_, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: chatId, OperatorId: a, EditChatAdminId: support, IsAdmin: mtproto.BoolTrue})
	wantErr(t, where+": make it an admin", err, mtproto.ErrUserPrivacyRestricted)
	_, err = cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: chatId, OperatorId: a, EditChatAdminId: support, IsAdmin: mtproto.BoolFalse})
	wantErr(t, where+": take back its admin right", err, mtproto.ErrUserNotParticipant)
	_, err = cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: chatId, OperatorId: b, EditChatAdminId: support, IsAdmin: mtproto.BoolTrue})
	wantErr(t, where+": a member makes it an admin", err, mtproto.ErrChatAdminRequired)
	_, err = cc.ChatEditChatCreator(&chat.TLChatEditChatCreator{SelfId: a, ChatId: chatId, UserId: support})
	wantErr(t, where+": make it the creator", err, mtproto.ErrUserPrivacyRestricted)
	_, err = cc.ChatEditChatParticipantRank(&chat.TLChatEditChatParticipantRank{SelfId: a, ChatId: chatId, Participant: support, Rank: "help"})
	wantErr(t, where+": give it a tag", err, mtproto.ErrPeerIdInvalid)
}
