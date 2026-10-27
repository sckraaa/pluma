//go:build integration

package dao_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Updates with seq that msg journals in the box transactions of the
// account (ADR 0020): the unpin of a deleted dialog.

// pin pins userId's dialog with a peer in a folder (0 the main list, 1 the
// archive), as biz dialog stores it.
func (w *world) pin(userId int64, peerType int32, peerId int64, folderId int32) {
	w.t.Helper()
	column := "pinned"
	if folderId != 0 {
		column = "folder_pinned"
	}
	r, err := w.check.Exec("UPDATE dialogs SET folder_id = ?, "+column+" = 5 << 32 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		folderId, userId, peerType, peerId)
	if err != nil {
		w.t.Fatal(err)
	}
	if n, _ := r.RowsAffected(); n != 1 {
		w.t.Fatalf("user %d has no dialog with %d/%d", userId, peerType, peerId)
	}
}

// seqUpdates describes userId's pushes with seq, in order, by their update
// (seqUpdate), and checks them against the seq journal: one push per row,
// seq after seq, no users or chats.
func (w *world) seqUpdates(userId int64) []string {
	w.t.Helper()
	journal := w.seqJournal(userId)
	var out []string
	i := 0
	for _, p := range w.pub.pushesTo(userId) {
		c := p.updates
		if c.GetSeq() == 0 {
			continue
		}
		if p.notMe || len(c.GetUpdates()) != 1 || len(c.GetUsers()) != 0 || len(c.GetChats()) != 0 {
			w.t.Errorf("push of seq %d to %d: not me %v, %d updates, %d users, %d chats",
				c.GetSeq(), userId, p.notMe, len(c.GetUpdates()), len(c.GetUsers()), len(c.GetChats()))
			continue
		}
		u := c.GetUpdates()[0]
		if i >= len(journal) || journal[i].seq != c.GetSeq() || journal[i].kind != u.GetPredicateName() || int64(c.GetDate()) != journal[i].date {
			w.t.Errorf("push of seq %d %s to %d without its journal row (%+v)", c.GetSeq(), u.GetPredicateName(), userId, journal)
		}
		i++
		out = append(out, seqUpdate(u))
	}
	if i != len(journal) {
		w.t.Errorf("user %d: %d rows of the seq journal, %d pushed", userId, len(journal), i)
	}
	if st := w.state(userId); st.Seq != userbox.BaseSeq+int32(len(journal)) {
		w.t.Errorf("user %d: seq %d with %d rows of the seq journal", userId, st.Seq, len(journal))
	}
	return out
}

// seqUpdate is "unpin u5/0" (peer, folder) or "unread false u5".
func seqUpdate(u *mtproto.Update) string {
	p := u.GetPeer_DIALOGPEER().GetPeer()
	peer := fmt.Sprint("u", p.GetUserId())
	if p.GetPredicateName() == mtproto.Predicate_peerChat {
		peer = fmt.Sprint("c", p.GetChatId())
	}
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateDialogPinned:
		if u.GetPinned() {
			return fmt.Sprintf("pin %s/%d", peer, u.GetFolderId().GetValue())
		}
		return fmt.Sprintf("unpin %s/%d", peer, u.GetFolderId().GetValue())
	case mtproto.Predicate_updateDialogUnreadMark:
		return fmt.Sprintf("unread %v %s", u.GetUnread(), peer)
	}
	return u.GetPredicateName()
}

// deleteChatHistory deletes userId's history of a deleted chat as
// messages.deleteChat does: msg.deleteHistory without a device, repeated
// while portions are left.
func (w *world) deleteChatHistory(userId, chatId int64) {
	w.t.Helper()
	for range 100 {
		r, err := w.rpc.MsgDeleteHistory(context.Background(), &msg.TLMsgDeleteHistory{
			UserId: userId, PeerType: mtproto.PEER_CHAT, PeerId: chatId,
		})
		if err != nil {
			w.t.Fatalf("delete the history of %d in chat %d: %v", userId, chatId, err)
		}
		if r.GetOffset() <= 0 {
			return
		}
	}
	w.t.Fatalf("delete the history of %d in chat %d: portions do not end", userId, chatId)
}

// TestDeleteHistoryUnpins: the transaction that deletes a pinned dialog's
// row unpins it on every device of the account with seq, after the push of
// the last deleted messages: messages.deleteHistory, its revoke in the
// other user's box and messages.deleteChat. A dialog that was not pinned
// goes without an update with seq.
func TestDeleteHistoryUnpins(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c, d := newUser(), newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.mu.Lock()
	w.chats.members[chat] = []int64{a, c}
	w.chats.mu.Unlock()
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = "m"
	}
	for range 21 { // two portions
		w.send(a, mtproto.PEER_USER, b, texts...)
	}
	w.send(a, mtproto.PEER_USER, d, "x")
	w.send(a, mtproto.PEER_CHAT, chat, "x")
	w.pub.drain()
	w.pin(a, mtproto.PEER_USER, b, 0)
	w.pin(b, mtproto.PEER_USER, a, 1)
	w.pin(c, mtproto.PEER_CHAT, chat, 1)
	w.pin(a, mtproto.PEER_CHAT, chat, 0)

	// a deletes the dialog with b for both, in portions: the last one
	// unpins it, on each side.
	w.deleteHistory(a, mtproto.PEER_USER, b, true)
	w.pub.drain()
	for _, u := range []int64{a, b} {
		pushes := w.pub.pushesTo(u)
		if last := pushes[len(pushes)-1]; last.updates.GetSeq() == 0 {
			t.Errorf("user %d: the last push is not the unpin: %v", u, last.updates)
		}
		if j := w.seqJournal(u); len(j) != 1 || j[0].pts != w.state(u).Pts {
			t.Errorf("user %d: seq journal %+v at pts %d, want one row after the deleted messages", u, j, w.state(u).Pts)
		}
	}

	w.deleteHistory(a, mtproto.PEER_USER, d, false)
	w.deleteChatHistory(c, chat)
	w.pub.drain()
	for _, want := range []struct {
		user    int64
		updates []string
	}{
		{a, []string{fmt.Sprintf("unpin u%d/0", b)}},
		{b, []string{fmt.Sprintf("unpin u%d/1", a)}},
		{c, []string{fmt.Sprintf("unpin c%d/1", chat)}},
		{d, nil},
	} {
		if got := w.seqUpdates(want.user); !slices.Equal(got, want.updates) {
			t.Errorf("user %d: pushes with seq %v, want %v", want.user, got, want.updates)
		}
	}
	for _, u := range [][3]int64{{a, mtproto.PEER_USER, b}, {b, mtproto.PEER_USER, a}, {c, mtproto.PEER_CHAT, chat}, {a, mtproto.PEER_USER, d}} {
		if w.hasDialog(u[0], int32(u[1]), u[2]) {
			t.Errorf("user %d keeps the dialog with %d/%d", u[0], u[1], u[2])
		}
	}
	// The deletion of c's history leaves a's pinned dialog with the chat.
	if !w.hasDialog(a, mtproto.PEER_CHAT, chat) {
		t.Errorf("user %d lost the dialog with chat %d", a, chat)
	}
	for _, u := range []int64{a, b, c, d} {
		w.checkBox(u)
	}
}

// mark sets userId's unread mark of the dialog with a peer, as
// messages.markDialogUnread stores it.
func (w *world) mark(userId int64, peerType int32, peerId int64) {
	w.t.Helper()
	if _, err := w.check.Exec("UPDATE dialogs SET unread_mark = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?", userId, peerType, peerId); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) marked(userId int64, peerType int32, peerId int64) bool {
	w.t.Helper()
	var marked bool
	if err := w.check.QueryRow("SELECT unread_mark FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", userId, peerType, peerId).Scan(&marked); err != nil {
		w.t.Fatal(err)
	}
	return marked
}

// TestUnreadMarkResetWithSeq: a message sent to a dialog and a read of it
// clear its unread mark, and the transaction tells every device of the
// account with seq, after the message or the read. A mark that is not set
// writes nothing, and a message received leaves the mark.
func TestUnreadMarkResetWithSeq(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.mu.Lock()
	w.chats.members[chat] = []int64{c, a}
	w.chats.mu.Unlock()
	w.send(a, mtproto.PEER_USER, b, "x")
	w.send(c, mtproto.PEER_CHAT, chat, "x")
	w.send(a, mtproto.PEER_USER, a, "note")
	w.pub.drain()

	// A message sent clears the mark; the next one finds none.
	w.mark(a, mtproto.PEER_USER, b)
	w.send(a, mtproto.PEER_USER, b, "y")
	w.pub.drain()
	if j := w.seqJournal(a); len(j) != 1 || j[0].pts != w.state(a).Pts {
		t.Errorf("A's seq journal %+v at pts %d, want one row after the message", j, w.state(a).Pts)
	}
	w.send(a, mtproto.PEER_USER, b, "z")
	// Saved Messages: the send and the read that goes with it clear the
	// mark once.
	w.mark(a, mtproto.PEER_USER, a)
	w.send(a, mtproto.PEER_USER, a, "note 2")
	// A group: sent by a member.
	w.mark(a, mtproto.PEER_CHAT, chat)
	w.send(a, mtproto.PEER_CHAT, chat, "y")

	// A message received leaves the mark; a read clears it.
	w.mark(b, mtproto.PEER_USER, a)
	w.send(a, mtproto.PEER_USER, b, "w")
	w.pub.drain()
	if !w.marked(b, mtproto.PEER_USER, a) {
		t.Errorf("a message received cleared B's mark")
	}
	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_USER, PeerId: a}); err != nil {
		t.Fatal(err)
	}
	// c's mark of the group goes with the read of the message a sent.
	w.mark(c, mtproto.PEER_CHAT, chat)
	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: c, AuthKeyId: c * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	for _, want := range []struct {
		user    int64
		updates []string
	}{
		{a, []string{fmt.Sprintf("unread false u%d", b), fmt.Sprintf("unread false u%d", a), fmt.Sprintf("unread false c%d", chat)}},
		{b, []string{fmt.Sprintf("unread false u%d", a)}},
		{c, []string{fmt.Sprintf("unread false c%d", chat)}},
	} {
		if got := w.seqUpdates(want.user); !slices.Equal(got, want.updates) {
			t.Errorf("user %d: pushes with seq %v, want %v", want.user, got, want.updates)
		}
	}
	for _, d := range [][3]int64{{a, mtproto.PEER_USER, b}, {a, mtproto.PEER_USER, a}, {a, mtproto.PEER_CHAT, chat}, {b, mtproto.PEER_USER, a}, {c, mtproto.PEER_CHAT, chat}} {
		if w.marked(d[0], int32(d[1]), d[2]) {
			t.Errorf("user %d: the dialog with %d/%d is still marked", d[0], d[1], d[2])
		}
	}
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
	}
}
