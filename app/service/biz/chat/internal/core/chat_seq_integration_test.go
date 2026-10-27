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
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The updates with seq a change of a basic group gives (ADR 0021): the
// operator's own account journals them in the transaction of the change
// and pushes them (an echo), every other recipient gets an inbox event that
// msg applies in its account.

// lastEvent is the newest event written.
func lastEvent(t *testing.T) int64 {
	t.Helper()
	return int64(count(t, "SELECT COALESCE(MAX(id), 0) FROM mq_outbox"))
}

// describe names an update of a basic group.
func describe(u *mtproto.Update) string {
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateChatParticipants:
		ps := u.GetParticipants_CHATPARTICIPANTS()
		if ps.GetPredicateName() == mtproto.Predicate_chatParticipantsForbidden {
			return "forbidden"
		}
		var ids []int64
		for _, p := range ps.GetParticipants() {
			ids = append(ids, p.GetUserId())
		}
		return fmt.Sprintf("members v%d %v", ps.GetVersion(), ids)
	case mtproto.Predicate_updateChat:
		return "chat"
	case mtproto.Predicate_updateChatDefaultBannedRights:
		return fmt.Sprintf("rights v%d", u.GetVersion())
	case mtproto.Predicate_updatePendingJoinRequests:
		return fmt.Sprintf("requests %d %v", u.GetRequestsPending(), u.GetRecentRequesters())
	}
	return u.GetPredicateName()
}

// chatOf names a chat of a push as its recipient sees it.
func chatOf(c *mtproto.Chat) string {
	switch {
	case c.GetPredicateName() != mtproto.Predicate_chat:
		return c.GetPredicateName()
	case c.GetDeactivated():
		return "deactivated"
	case c.GetLeft():
		return "left"
	}
	return "chat"
}

// seqSince describes the events written after mark, in order: a push with
// seq to the operator's own account as "<user> echo <update> <users> users
// <chats>", each update of an inbox event as "<user> <update>". Each push
// is checked against its row of the seq journal, each event against its
// initiator.
func seqSince(t *testing.T, mark int64) []string {
	t.Helper()
	rows, err := itRaw.Query("SELECT initiator_id, topic, recipient_id, method, payload FROM mq_outbox WHERE id > ? ORDER BY id", mark)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			initiator, recipient int64
			topic, method        string
			payload              []byte
		)
		if err = rows.Scan(&initiator, &topic, &recipient, &method, &payload); err != nil {
			t.Fatal(err)
		}
		switch method {
		case "sync.TL_sync_pushUpdates":
			var in syncpb.TLSyncPushUpdates
			if err = jsonx.Unmarshal(payload, &in); err != nil {
				t.Fatal(err)
			}
			c := in.GetUpdates()
			if topic != "Sync-T" || recipient != initiator || c.GetSeq() == 0 || len(c.GetUpdates()) != 1 {
				t.Fatalf("push to %d of %d on %s: %v", recipient, initiator, topic, c)
			}
			u := c.GetUpdates()[0]
			if n := count(t, "SELECT COUNT(*) FROM user_seq_updates WHERE user_id = ? AND seq = ? AND update_type = ?", recipient, c.GetSeq(), u.GetPredicateName()); n != 1 {
				t.Errorf("push of seq %d to %d: no row of the seq journal", c.GetSeq(), recipient)
			}
			var chats []string
			for _, ch := range c.GetChats() {
				chats = append(chats, chatOf(ch))
			}
			out = append(out, fmt.Sprintf("%d echo %s %d users %v", recipient, describe(u), len(c.GetUsers()), chats))
		case "inbox.TL_inbox_seqUpdatesToInbox":
			var in inboxpb.TLInboxSeqUpdatesToInbox
			if err = jsonx.Unmarshal(payload, &in); err != nil {
				t.Fatal(err)
			}
			if topic != "Inbox-T" || in.GetFromId() != initiator || in.GetUserId() != recipient {
				t.Fatalf("event to %d of %d on %s: %v", recipient, initiator, topic, &in)
			}
			for _, u := range in.GetUpdates() {
				out = append(out, fmt.Sprintf("%d %s", recipient, describe(u)))
			}
		default:
			t.Errorf("event %s to %d", method, recipient)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// expect checks the events written after *mark and moves it on.
func expect(t *testing.T, mark *int64, what string, want ...string) {
	t.Helper()
	got := seqSince(t, *mark)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s: events\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	*mark = lastEvent(t)
}

func stateSeq(t *testing.T, userId int64) int32 {
	t.Helper()
	st, err := userbox.Read(t.Context(), itSvc.Dao.BoxDB, userId)
	if err != nil {
		t.Fatal(err)
	}
	return st.Seq
}

// Every change of the members gives every member the whole list with the
// version after the change, the removed user the forbidden list.
func TestMembersWithSeq(t *testing.T) {
	u := newUsers(4)
	a, b, c, d := u[0], u[1], u[2], u[3]
	id := newChat(t, a, b, c)
	cc := chatCore()
	mark := lastEvent(t)

	if _, err := cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: d}); err != nil {
		t.Fatal(err)
	}
	members := fmt.Sprintf("members v2 %v", []int64{a, b, c, d})
	expect(t, &mark, "add",
		fmt.Sprintf("%d echo %s 4 users [chat]", a, members),
		fmt.Sprintf("%d %s", b, members), fmt.Sprintf("%d %s", c, members), fmt.Sprintf("%d %s", d, members))

	if _, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: a, DeleteUserId: c}); err != nil {
		t.Fatal(err)
	}
	members = fmt.Sprintf("members v3 %v", []int64{a, b, d})
	expect(t, &mark, "removal",
		fmt.Sprintf("%d echo %s 3 users [chat]", a, members),
		fmt.Sprintf("%d %s", b, members), fmt.Sprintf("%d %s", d, members),
		fmt.Sprintf("%d forbidden", c))

	if _, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: b, DeleteUserId: b}); err != nil {
		t.Fatal(err)
	}
	members = fmt.Sprintf("members v4 %v", []int64{a, d})
	expect(t, &mark, "leave",
		fmt.Sprintf("%d %s", a, members), fmt.Sprintf("%d %s", d, members),
		fmt.Sprintf("%d echo forbidden 0 users [left]", b))

	// The server's admin adds as the creator, whose account gets the echo.
	if _, err := cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, UserId: c}); err != nil {
		t.Fatal(err)
	}
	members = fmt.Sprintf("members v5 %v", []int64{a, c, d})
	expect(t, &mark, "add by the admin",
		fmt.Sprintf("%d echo %s 3 users [chat]", a, members),
		fmt.Sprintf("%d %s", c, members), fmt.Sprintf("%d %s", d, members))

	// Refused and unchanged: nothing.
	if _, err := cc.ChatAddChatUser(&chat.TLChatAddChatUser{ChatId: id, InviterId: a, UserId: c}); err == nil {
		t.Errorf("added twice")
	}
	expect(t, &mark, "refused")

	// The operators' accounts have their echoes; the others get theirs
	// when msg applies the events.
	if sa, sb, sd := stateSeq(t, a), stateSeq(t, b), stateSeq(t, d); sa != userbox.BaseSeq+3 || sb != userbox.BaseSeq+1 || sd != userbox.BaseSeq {
		t.Errorf("seq of a %d, b %d, d %d", sa, sb, sd)
	}
}

func TestAdminsAndRanksWithSeq(t *testing.T) {
	u := newUsers(3)
	a, b, c := u[0], u[1], u[2]
	id := newChat(t, a, b, c)
	cc := chatCore()
	mark := lastEvent(t)
	members := func(v int) []string {
		m := fmt.Sprintf("members v%d %v", v, []int64{a, b, c})
		return []string{fmt.Sprintf("%d echo %s 3 users [chat]", a, m), fmt.Sprintf("%d %s", b, m), fmt.Sprintf("%d %s", c, m)}
	}

	if _, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: b, IsAdmin: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "promotion", members(2)...)
	if _, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: b, IsAdmin: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "promotion that changes nothing")
	if _, err := cc.ChatEditChatParticipantRank(&chat.TLChatEditChatParticipantRank{SelfId: a, ChatId: id, Participant: c, Rank: "helper"}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "rank", members(3)...)
	if _, err := cc.ChatEditChatParticipantRank(&chat.TLChatEditChatParticipantRank{SelfId: a, ChatId: id, Participant: c, Rank: "helper"}); err == nil {
		t.Errorf("the same rank again")
	}
	expect(t, &mark, "the same rank")

	// The admin in the list.
	var in inboxpb.TLInboxSeqUpdatesToInbox
	var payload []byte
	if err := itRaw.QueryRow("SELECT payload FROM mq_outbox WHERE recipient_id = ? AND method = 'inbox.TL_inbox_seqUpdatesToInbox' ORDER BY id DESC LIMIT 1", c).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := jsonx.Unmarshal(payload, &in); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, p := range in.GetUpdates()[0].GetParticipants_CHATPARTICIPANTS().GetParticipants() {
		kinds = append(kinds, p.GetPredicateName()+" "+p.GetRank().GetValue())
	}
	if want := []string{"chatParticipantCreator ", "chatParticipantAdmin ", "chatParticipant helper"}; !slices.Equal(kinds, want) {
		t.Errorf("participants %v, want %v", kinds, want)
	}
}

func TestChatStateWithSeq(t *testing.T) {
	u := newUsers(3)
	a, b, c := u[0], u[1], u[2]
	id := newChat(t, a, b, c)
	cc := chatCore()
	mark := lastEvent(t)
	all := func(update string, chats ...string) []string {
		return []string{fmt.Sprintf("%d echo %s 0 users %v", a, update, chats), fmt.Sprintf("%d %s", b, update), fmt.Sprintf("%d %s", c, update)}
	}

	rights := mtproto.MakeTLChatBannedRights(&mtproto.ChatBannedRights{SendPolls: true}).To_ChatBannedRights()
	if _, err := cc.ChatEditChatDefaultBannedRights(&chat.TLChatEditChatDefaultBannedRights{ChatId: id, OperatorId: a, BannedRights: rights}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "default rights", all("rights v2", "chat")...)
	if _, err := cc.ChatToggleNoForwards(&chat.TLChatToggleNoForwards{ChatId: id, OperatorId: a, Enabled: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "no forwards", all("chat", "chat")...)

	// The service message or chatFull carries these.
	if _, err := cc.ChatEditChatTitle(&chat.TLChatEditChatTitle{ChatId: id, EditUserId: a, Title: "renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.ChatEditChatAbout(&chat.TLChatEditChatAbout{ChatId: id, EditUserId: a, About: "about"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.ChatEditChatPhoto(&chat.TLChatEditChatPhoto{ChatId: id, EditUserId: a, ChatPhoto: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 7002}).To_Photo()}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "title, about, photo")

	// A member who left keeps the chat as left: the deletion goes to the
	// members.
	if _, err := cc.ChatDeleteChatUser(&chat.TLChatDeleteChatUser{ChatId: id, OperatorId: c, DeleteUserId: c}); err != nil {
		t.Fatal(err)
	}
	mark = lastEvent(t)
	if _, err := cc.ChatDeleteChat(&chat.TLChatDeleteChat{ChatId: id, OperatorId: a}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "deletion", fmt.Sprintf("%d echo chat 0 users [deactivated]", a), fmt.Sprintf("%d chat", b))
	if _, err := cc.ChatDeleteChat(&chat.TLChatDeleteChat{ChatId: id, OperatorId: a}); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "deleted again")
}

func TestJoinWithSeq(t *testing.T) {
	u := newUsers(3)
	a, b, e := u[0], u[1], u[2]
	id := newChat(t, a, b)
	link := newLink(t, id, a, false, 0, false)
	mark := lastEvent(t)
	if _, err := join(e, link); err != nil {
		t.Fatal(err)
	}
	members := fmt.Sprintf("members v2 %v", []int64{a, b, e})
	expect(t, &mark, "join by link",
		fmt.Sprintf("%d %s", a, members), fmt.Sprintf("%d %s", b, members),
		fmt.Sprintf("%d echo %s 3 users [chat]", e, members))
}

// The requests to join go to the creator and the admins, not to the other
// members.
func TestJoinRequestsWithSeq(t *testing.T) {
	u := newUsers(5)
	a, b, c, f, g := u[0], u[1], u[2], u[3], u[4]
	id := newChat(t, a, b, c)
	cc := chatCore()
	if _, err := cc.ChatEditChatAdmin(&chat.TLChatEditChatAdmin{ChatId: id, OperatorId: a, EditChatAdminId: b, IsAdmin: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	link := newLink(t, id, a, true, 0, false)
	mark := lastEvent(t)

	if _, err := join(f, link); err != nil {
		t.Fatal(err)
	}
	requests := fmt.Sprintf("requests 1 %v", []int64{f})
	expect(t, &mark, "request", fmt.Sprintf("%d %s", a, requests), fmt.Sprintf("%d %s", b, requests))
	if _, err := join(f, link); err != nil {
		t.Fatal(err)
	}
	expect(t, &mark, "request again")
	if _, err := join(g, link); err != nil {
		t.Fatal(err)
	}
	requests = fmt.Sprintf("requests 2 %v", []int64{g, f})
	expect(t, &mark, "second request", fmt.Sprintf("%d %s", a, requests), fmt.Sprintf("%d %s", b, requests))

	hide := func(self, user int64, approved bool) {
		t.Helper()
		if _, err := cc.ChatHideChatJoinRequests(&chat.TLChatHideChatJoinRequests{SelfId: self, ChatId: id, Approved: approved, UserId: mtproto.MakeFlagsInt64(user)}); err != nil {
			t.Fatal(err)
		}
	}
	hide(b, f, true)
	members := fmt.Sprintf("members v3 %v", []int64{a, b, c, f})
	requests = fmt.Sprintf("requests 1 %v", []int64{g})
	expect(t, &mark, "approval",
		fmt.Sprintf("%d %s", a, members), fmt.Sprintf("%d echo %s 4 users [chat]", b, members),
		fmt.Sprintf("%d %s", c, members), fmt.Sprintf("%d %s", f, members),
		fmt.Sprintf("%d %s", a, requests), fmt.Sprintf("%d echo %s 1 users [chat]", b, requests))
	hide(a, g, false)
	expect(t, &mark, "decline", fmt.Sprintf("%d echo requests 0 [] 0 users [chat]", a), fmt.Sprintf("%d requests 0 []", b))
}
