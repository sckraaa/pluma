//go:build integration

package dao_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	updates_helper "github.com/teamgram/teamgram-server/app/service/biz/updates"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/updates"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Updates with seq to another user's account (ADR 0021): the initiator's
// transaction writes the inbox event, the recipient's applies it once.

func peerSettingsOf(userId int64) *mtproto.Update {
	return mtproto.MakeTLUpdatePeerSettings(&mtproto.Update{
		Peer_PEER: mtproto.MakePeerUser(userId),
		Settings:  mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{}).To_PeerSettings(),
	}).To_Update()
}

// emitSeqInbox writes, in a box transaction of from, the event of updates
// to to; it returns the event's id and created_at.
func (w *world) emitSeqInbox(from, to int64, updates ...*mtproto.Update) (id, createdAt int64) {
	w.t.Helper()
	res, err := w.d.RunBox(context.Background(), from, nil, func(b *userbox.Box) error {
		return inbox.EmitSeqInbox(b, itTopics.Inbox, to, updates...)
	})
	if err != nil {
		w.t.Fatal(err)
	}
	if res.Emitted != 1 {
		w.t.Fatalf("%d events emitted", res.Emitted)
	}
	if err = w.check.QueryRow("SELECT id, created_at FROM mq_outbox WHERE initiator_id = ? AND recipient_id = ? AND method = 'inbox.TL_inbox_seqUpdatesToInbox' ORDER BY id DESC LIMIT 1",
		from, to).Scan(&id, &createdAt); err != nil {
		w.t.Fatal(err)
	}
	return id, createdAt
}

type seqJournalRow struct {
	seq, pts int32
	kind     string
	date     int64
}

func (w *world) seqJournal(userId int64) []seqJournalRow {
	w.t.Helper()
	rows, err := w.check.Query("SELECT seq, pts, update_type, date2 FROM user_seq_updates WHERE user_id = ? ORDER BY seq", userId)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []seqJournalRow
	for rows.Next() {
		var r seqJournalRow
		if err = rows.Scan(&r.seq, &r.pts, &r.kind, &r.date); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// seqPushes are the pushes of updates with seq to userId, as seq:update
// [users] [chats].
func seqPushes(pushes []push) []string {
	var out []string
	for _, p := range pushes {
		c := p.updates
		if c.GetSeq() == 0 {
			continue
		}
		var users, chats []int64
		for _, u := range c.GetUsers() {
			users = append(users, u.GetId())
		}
		for _, ch := range c.GetChats() {
			chats = append(chats, ch.GetId())
		}
		var kinds []string
		for _, u := range c.GetUpdates() {
			kinds = append(kinds, u.GetPredicateName())
		}
		out = append(out, fmt.Sprintf("%d:%v %v %v", c.GetSeq(), kinds, users, chats))
	}
	return out
}

func TestSeqUpdatesToAnotherUser(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	group := newChat()
	w.chats.mu.Lock()
	w.chats.members[group] = []int64{a, b}
	w.chats.mu.Unlock()

	// A refused update rolls the initiator back: no event, nothing in the
	// recipient's lane.
	_, err := w.d.RunBox(ctx, a, nil, func(bx *userbox.Box) error {
		return inbox.EmitSeqInbox(bx, itTopics.Inbox, b, peerSettingsOf(a), mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{}).To_Update())
	})
	if !errors.Is(err, userbox.ErrInvalidEvent) {
		t.Fatalf("draft with seq: %v, want %v", err, userbox.ErrInvalidEvent)
	}
	var n int
	if err = w.check.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE initiator_id = ?", a).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d events of the refused transaction (%v)", n, err)
	}

	// One event, two updates: each its own seq and push, in the
	// recipient's transaction that takes the event.
	group1 := mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: group}).To_Update()
	id, createdAt := w.emitSeqInbox(a, b, peerSettingsOf(a), group1)
	w.pub.drain()
	waitState(t, id)
	if st := w.state(b); st.Seq != userbox.BaseSeq+2 || st.Pts != userbox.BasePts {
		t.Errorf("B's state %+v, want seq %d and pts untouched", st, userbox.BaseSeq+2)
	}
	if st := w.state(a); st.Seq != userbox.BaseSeq {
		t.Errorf("A's seq %d: the initiator's account gets nothing", st.Seq)
	}
	journal := w.seqJournal(b)
	if len(journal) != 2 || journal[0].kind != mtproto.Predicate_updatePeerSettings || journal[1].kind != mtproto.Predicate_updateChat ||
		journal[0].seq != userbox.BaseSeq+1 || journal[1].seq != userbox.BaseSeq+2 {
		t.Fatalf("B's seq journal %+v", journal)
	}
	want := []string{
		fmt.Sprintf("%d:[updatePeerSettings] [%d] []", userbox.BaseSeq+1, a),
		fmt.Sprintf("%d:[updateChat] [] [%d]", userbox.BaseSeq+2, group),
	}
	if got := seqPushes(w.pub.pushesTo(b)); !slices.Equal(got, want) {
		t.Errorf("pushes to B %v, want %v", got, want)
	}
	for _, p := range w.pub.pushesTo(b) {
		if p.notMe || p.updates.GetDate() != int32(journal[0].date) {
			t.Errorf("push to B: not me %v, date %d, row date %d", p.notMe, p.updates.GetDate(), journal[0].date)
		}
	}
	if rep, err := userbox.CheckSeqJournal(ctx, w.check, b, userbox.BaseSeq); err != nil || !rep.OK() {
		t.Errorf("B's seq journal: %+v, %v", rep, err)
	}

	// A copy delivered again finds no row: no second seq, no second push.
	before := inboxOutcomes(t, "duplicate")
	copyEvent(t, id, b, createdAt)
	eventually(t, 10*time.Second, "copy handled", func() bool { return inboxOutcomes(t, "duplicate")-before >= 1 })
	w.pub.drain()
	if st := w.state(b); st.Seq != userbox.BaseSeq+2 {
		t.Errorf("B's seq %d after the copy", st.Seq)
	}
	if got := seqPushes(w.pub.pushesTo(b)); len(got) != 2 {
		t.Errorf("pushes to B after the copy %v", got)
	}

	// An offline device of B gets both from getDifference, with the peers
	// they refer to.
	diff, err := updates_helper.New(updates_helper.Config{Mysql: sqlx.Config{DSN: boxDSN("")}}).UpdatesGetDifferenceV2(ctx, &updates.TLUpdatesGetDifferenceV2{
		UserId: b, Pts: userbox.BasePts, Date: journal[0].date,
	})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, u := range diff.GetOtherUpdates() {
		kinds = append(kinds, u.GetPredicateName())
	}
	if diff.GetPredicateName() != updates.Predicate_difference || diff.GetState().GetSeq() != userbox.BaseSeq+2 ||
		!slices.Equal(kinds, []string{mtproto.Predicate_updatePeerSettings, mtproto.Predicate_updateChat}) {
		t.Fatalf("difference %s %v, state %v", diff.GetPredicateName(), kinds, diff.GetState())
	}
	h := mtproto.NewIDListHelper()
	seqpeers.Pick(h, b, diff.GetOtherUpdates()...)
	if !slices.Equal(h.UserIdList, []int64{a}) || !slices.Equal(h.ChatIdList, []int64{group}) {
		t.Errorf("peers of the difference: users %v, chats %v", h.UserIdList, h.ChatIdList)
	}
	w.checkBox(b)
}

// A bot gets no updates with seq: the event goes, nothing is journaled.
func TestSeqUpdatesToBot(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, bot := newUser(), newUser()
	w.users.mu.Lock()
	w.users.bots[bot] = true
	w.users.mu.Unlock()
	id, _ := w.emitSeqInbox(a, bot, peerSettingsOf(a))
	w.pub.drain()
	waitState(t, id)
	if st := w.state(bot); st.Seq != userbox.BaseSeq {
		t.Errorf("the bot's seq %d", st.Seq)
	}
	if n := len(w.pub.pushesTo(bot)); n != 0 {
		t.Errorf("%d pushes to the bot", n)
	}
}

// The event written as JSON is what the consumer decodes: a row with an
// update a writer let in past EmitSeqInbox is dead, not journaled.
func TestSeqUpdatesToInboxWithoutSeq(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	w.pub.deadOK = true
	a, b := newUser(), newUser()
	data, err := json.Marshal(&inbox.TLInboxSeqUpdatesToInbox{
		UserId: b, FromId: a,
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{Peer_PEER: mtproto.MakePeerUser(a)}).To_Update()},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := insertEvent(t, userbox.Event{Topic: itTopics.Inbox, Recipient: b, Method: "inbox.TL_inbox_seqUpdatesToInbox", Data: data}, userbox.EventPending)
	waitState(t, id, userbox.EventDead)
	if st := w.state(b); st.Seq != userbox.BaseSeq {
		t.Errorf("B's seq %d", st.Seq)
	}
	discard(t, id)
}

// Another user's name and photo (biz user tells the users who see them,
// ADR 0021): each change is journaled in the recipient's account and pushed
// with that user as the recipient sees them; a difference keeps the last
// name.
func TestSeqUserUpdatesToAnotherUser(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	name := func(first string) *mtproto.Update {
		return mtproto.MakeTLUpdateUserName(&mtproto.Update{
			UserId:    a,
			FirstName: first,
			Usernames: []*mtproto.Username{mtproto.MakeTLUsername(&mtproto.Username{Editable: true, Active: true, Username: "ann"}).To_Username()},
		}).To_Update()
	}
	w.emitSeqInbox(a, b, name("Ann"))
	w.emitSeqInbox(a, b, mtproto.MakeTLUpdateUser(&mtproto.Update{UserId: a}).To_Update())
	id, _ := w.emitSeqInbox(a, b, name("Anna"))
	w.pub.drain()
	waitState(t, id)

	want := []string{
		fmt.Sprintf("%d:[updateUserName] [%d] []", userbox.BaseSeq+1, a),
		fmt.Sprintf("%d:[updateUser] [%d] []", userbox.BaseSeq+2, a),
		fmt.Sprintf("%d:[updateUserName] [%d] []", userbox.BaseSeq+3, a),
	}
	if got := seqPushes(w.pub.pushesTo(b)); !slices.Equal(got, want) {
		t.Errorf("pushes to B %v, want %v", got, want)
	}
	for _, p := range w.pub.pushesTo(b) {
		if u := p.updates.GetUsers(); len(u) != 1 || u[0].GetFirstName().GetValue() != fmt.Sprint("U", a) || u[0].GetSelf() {
			t.Errorf("push of seq %d: users %v, want A as B sees them", p.updates.GetSeq(), u)
		}
	}
	journal := w.seqJournal(b)
	if len(journal) != 3 || w.state(b).Seq != userbox.BaseSeq+3 || w.state(a).Seq != userbox.BaseSeq {
		t.Fatalf("B's seq journal %+v, seq of B %d, of A %d", journal, w.state(b).Seq, w.state(a).Seq)
	}

	diff, err := updates_helper.New(updates_helper.Config{Mysql: sqlx.Config{DSN: boxDSN("")}}).UpdatesGetDifferenceV2(ctx, &updates.TLUpdatesGetDifferenceV2{
		UserId: b, Pts: userbox.BasePts, Date: journal[0].date,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range diff.GetOtherUpdates() {
		got = append(got, u.GetPredicateName()+" "+u.GetFirstName())
	}
	if want := []string{"updateUser ", "updateUserName Anna"}; !slices.Equal(got, want) || diff.GetState().GetSeq() != userbox.BaseSeq+3 {
		t.Errorf("difference %v, state %v; want %v", got, diff.GetState(), want)
	}
}

// A peer biz leaves out of a look-up (a failed load) is waited for while
// the event is young: the event stays until the peer is found and goes
// with it. An event older than the wait goes with the peers found.
func TestSeqUpdatesWaitForPeers(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	missing := func(id int64, gone bool) {
		w.users.mu.Lock()
		defer w.users.mu.Unlock()
		w.users.missing[id] = gone
	}
	missing(c, true)
	id, _ := w.emitSeqInbox(a, b, peerSettingsOf(a), peerSettingsOf(c))
	time.Sleep(1500 * time.Millisecond)
	var n int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE id = ?", id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("young event with a peer not found: %d rows (%v), want it kept", n, err)
	}
	if st := w.state(b); st.Seq != userbox.BaseSeq {
		t.Fatalf("B's seq %d while a peer is not found", st.Seq)
	}
	missing(c, false)
	w.pub.drain()
	waitState(t, id)
	want := []string{
		fmt.Sprintf("%d:[updatePeerSettings] [%d] []", userbox.BaseSeq+1, a),
		fmt.Sprintf("%d:[updatePeerSettings] [%d] []", userbox.BaseSeq+2, c),
	}
	if got := seqPushes(w.pub.pushesTo(b)); !slices.Equal(got, want) {
		t.Errorf("pushes to B %v, want %v", got, want)
	}

	// Written before the wait: it goes now, without the peer.
	missing(c, true)
	data, err := json.Marshal(&inbox.TLInboxSeqUpdatesToInbox{UserId: b, FromId: a, Updates: []*mtproto.Update{peerSettingsOf(c)}})
	if err != nil {
		t.Fatal(err)
	}
	id = insertEventAt(t, userbox.Event{Topic: itTopics.Inbox, Recipient: b, Method: "inbox.TL_inbox_seqUpdatesToInbox", Data: data},
		userbox.EventPending, time.Now().Add(-4*time.Minute).UnixMilli())
	w.pub.drain()
	waitState(t, id)
	want = append(want, fmt.Sprintf("%d:[updatePeerSettings] [] []", userbox.BaseSeq+3))
	if got := seqPushes(w.pub.pushesTo(b)); !slices.Equal(got, want) {
		t.Errorf("pushes to B %v, want %v", got, want)
	}
	missing(c, false)
}

// The members of a basic group as biz chat gives them to a member and to a
// removed user (ADR 0021): journaled with seq in each account, pushed with
// the users and the chat as each sees them, and in getDifference.
func TestSeqChatParticipantsToMembers(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	group := newChat()
	w.chats.mu.Lock()
	w.chats.members[group] = []int64{a, b, c}
	w.chats.mu.Unlock()

	member := func(userId int64, creator bool) *mtproto.ChatParticipant {
		if creator {
			return mtproto.MakeTLChatParticipantCreator(&mtproto.ChatParticipant{UserId: userId}).To_ChatParticipant()
		}
		return mtproto.MakeTLChatParticipant(&mtproto.ChatParticipant{UserId: userId, InviterId: a, Date: 1}).To_ChatParticipant()
	}
	members := mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: mtproto.MakeTLChatParticipants(&mtproto.ChatParticipants{
			ChatId:       group,
			Participants: []*mtproto.ChatParticipant{member(a, true), member(b, false), member(c, false)},
			Version:      2,
		}).To_ChatParticipants(),
	}).To_Update()
	forbidden := mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: mtproto.MakeTLChatParticipantsForbidden(&mtproto.ChatParticipants{ChatId: group}).To_ChatParticipants(),
	}).To_Update()
	w.emitSeqInbox(a, b, members)
	id, _ := w.emitSeqInbox(a, c, forbidden)
	w.pub.drain()
	waitState(t, id)

	if got, want := seqPushes(w.pub.pushesTo(b)), []string{fmt.Sprintf("%d:[updateChatParticipants] [%d %d %d] [%d]", userbox.BaseSeq+1, a, b, c, group)}; !slices.Equal(got, want) {
		t.Errorf("pushes to B %v, want %v", got, want)
	}
	if got, want := seqPushes(w.pub.pushesTo(c)), []string{fmt.Sprintf("%d:[updateChatParticipants] [] [%d]", userbox.BaseSeq+1, group)}; !slices.Equal(got, want) {
		t.Errorf("pushes to C %v, want %v", got, want)
	}

	journal := w.seqJournal(b)
	diff, err := updates_helper.New(updates_helper.Config{Mysql: sqlx.Config{DSN: boxDSN("")}}).UpdatesGetDifferenceV2(ctx, &updates.TLUpdatesGetDifferenceV2{
		UserId: b, Pts: userbox.BasePts, Date: journal[0].date,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.GetOtherUpdates()) != 1 || diff.GetState().GetSeq() != userbox.BaseSeq+1 {
		t.Fatalf("difference %v, state %v", diff.GetOtherUpdates(), diff.GetState())
	}
	got := diff.GetOtherUpdates()[0].GetParticipants_CHATPARTICIPANTS()
	if got.GetChatId() != group || got.GetVersion() != 2 || len(got.GetParticipants()) != 3 ||
		got.GetParticipants()[0].GetPredicateName() != mtproto.Predicate_chatParticipantCreator {
		t.Errorf("members in the difference %v", got)
	}
}
