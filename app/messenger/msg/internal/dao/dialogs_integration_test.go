//go:build integration

package dao_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/prometheus"
)

// Dialog rows and their counters under the box transactions: the row of an
// emptied dialog goes in the transaction of the last portion, counters stay
// exact without reading the whole dialog, and concurrent box transactions do
// not deadlock.

// go-zero records metrics only once Prometheus is enabled.
func init() { prometheus.Enable() }

// lockRetries returns how many box transactions were tried again after a
// deadlock (1213) and after a lock wait timeout (1205), in this process.
func lockRetries(t *testing.T) (deadlocks, lockWaits float64) {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "userbox_tx_retries_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				switch l.GetValue() {
				case "1213":
					deadlocks += m.GetCounter().GetValue()
				case "1205":
					lockWaits += m.GetCounter().GetValue()
				}
			}
		}
	}
	return deadlocks, lockWaits
}

// noLockRetries fails t when box transactions were retried after deadlocks
// or lock wait timeouts since before was taken: Run hides up to two of them
// per operation, so the operations' success alone does not show them.
func noLockRetries(t *testing.T, before [2]float64) {
	t.Helper()
	deadlocks, lockWaits := lockRetries(t)
	if d, l := deadlocks-before[0], lockWaits-before[1]; d != 0 || l != 0 {
		t.Errorf("box transactions retried: %v after deadlocks, %v after lock wait timeouts", d, l)
	}
}

func lockRetriesNow(t *testing.T) [2]float64 {
	d, l := lockRetries(t)
	return [2]float64{d, l}
}

// hasDialog tells whether the user has a dialog row with the peer.
func (w *world) hasDialog(userId int64, peerType int32, peerId int64) bool {
	w.t.Helper()
	var n int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		userId, peerType, peerId).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n > 0
}

// checkDialog checks the user's dialog with the peer against its messages:
// the row is there exactly when messages are, with the newest of them as
// its top message and the unread incoming ones counted.
func (w *world) checkDialog(userId int64, peerType int32, peerId int64) {
	w.t.Helper()
	did := mtproto.MakeDialogId(userId, peerType, peerId)
	var alive, top int32
	if err := w.check.QueryRow("SELECT COUNT(*), COALESCE(MAX(user_message_box_id), 0) FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0",
		userId, did.A, did.B).Scan(&alive, &top); err != nil {
		w.t.Fatal(err)
	}
	if !w.hasDialog(userId, peerType, peerId) {
		if alive > 0 {
			w.t.Errorf("user %d: %d messages with %d/%d, no dialog row", userId, alive, peerType, peerId)
		}
		return
	}
	if alive == 0 {
		w.t.Errorf("user %d: dialog row with %d/%d without messages", userId, peerType, peerId)
	}
	d := w.dialog(userId, peerType, peerId)
	if d.TopMessage != top {
		w.t.Errorf("user %d: dialog with %d/%d has top message %d, newest message %d", userId, peerType, peerId, d.TopMessage, top)
	}
	if actual := w.unreadIncoming(userId, peerType, peerId, d.ReadInboxMaxId); d.UnreadCount != actual {
		w.t.Errorf("user %d: dialog with %d/%d has unread_count %d, %d unread messages", userId, peerType, peerId, d.UnreadCount, actual)
	}
}

// deleteHistory deletes userId's history with the peer, repeating the call
// while portions are left, as clients do.
func (w *world) deleteHistory(userId int64, peerType int32, peerId int64, revoke bool) {
	w.t.Helper()
	for range 100 {
		r, err := w.rpc.MsgDeleteHistory(context.Background(), &msg.TLMsgDeleteHistory{
			UserId: userId, AuthKeyId: userId * 10, PeerType: peerType, PeerId: peerId, Revoke: revoke,
		})
		if err != nil {
			w.t.Errorf("delete history of %d with %d/%d: %v", userId, peerType, peerId, err)
			return
		}
		if r.GetOffset() == 0 {
			return
		}
	}
	w.t.Errorf("delete history of %d with %d/%d: portions do not end", userId, peerType, peerId)
}

// TestDeleteHistoryWithIncoming: messages keep arriving while a user deletes
// the private chat for both. Whatever the interleaving, each side ends with
// a dialog row exactly when it has messages, with exact counters: the row
// goes in the transaction of the last portion, never after it.
func TestDeleteHistoryWithIncoming(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b := newUser(), newUser()
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = "m"
	}
	for range 30 {
		w.send(a, mtproto.PEER_USER, b, texts...)
	}
	w.pub.drain()
	before := lockRetriesNow(t)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 40 {
			w.send(b, mtproto.PEER_USER, a, fmt.Sprintf("in %d", i))
		}
	})
	wg.Go(func() { w.deleteHistory(a, mtproto.PEER_USER, b, true) })
	wg.Wait()
	w.pub.drain()

	for _, u := range [][2]int64{{a, b}, {b, a}} {
		w.checkBox(u[0])
		w.checkDialog(u[0], mtproto.PEER_USER, u[1])
	}
	noLockRetries(t, before)

	// A message after the deletion makes the dialog again, as a new one.
	w.deleteHistory(a, mtproto.PEER_USER, b, true)
	w.pub.drain()
	for _, u := range [][2]int64{{a, b}, {b, a}} {
		if w.hasDialog(u[0], mtproto.PEER_USER, u[1]) {
			t.Errorf("user %d keeps the dialog with %d after the deletion", u[0], u[1])
		}
	}
	w.send(b, mtproto.PEER_USER, a, "again")
	w.pub.drain()
	for _, u := range [][2]int64{{a, b}, {b, a}} {
		w.checkDialog(u[0], mtproto.PEER_USER, u[1])
	}
	if d := w.dialog(a, mtproto.PEER_USER, b); d.UnreadCount != 1 {
		t.Errorf("new dialog of %d: unread_count %d, want 1", a, d.UnreadCount)
	}
}

// TestDeleteChatHistories: messages.deleteChat deletes every member's
// history of the chat with msg.deleteHistory repeated while portions are
// left: each member ends without messages and without the dialog row. With
// revoke a member's deletion empties the other members' histories, and they
// keep their dialog rows: they stay in the group.
func TestDeleteChatHistories(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}
	texts := make([]string, 50)
	for i := range texts {
		texts[i] = "m"
	}
	for range 25 {
		w.send(a, mtproto.PEER_CHAT, chat, texts...)
	}
	w.pub.drain()

	w.deleteHistory(b, mtproto.PEER_CHAT, chat, true)
	w.pub.drain()
	if w.hasDialog(b, mtproto.PEER_CHAT, chat) {
		t.Errorf("user %d keeps the dialog after deleting its history", b)
	}
	for _, u := range []int64{a, c} {
		if left := alive(w.messages(u)); len(left) != 0 {
			t.Errorf("user %d keeps %d messages after the revoke", u, len(left))
		}
		if !w.hasDialog(u, mtproto.PEER_CHAT, chat) {
			t.Errorf("member %d lost the dialog with the chat", u)
		} else if d := w.dialog(u, mtproto.PEER_CHAT, chat); d.TopMessage != 0 || d.UnreadCount != 0 {
			t.Errorf("member %d: dialog %+v after the revoke", u, d)
		}
	}

	w.send(c, mtproto.PEER_CHAT, chat, texts...)
	for range 22 {
		w.send(a, mtproto.PEER_CHAT, chat, texts...)
	}
	w.pub.drain()
	for _, u := range []int64{a, b, c} {
		w.deleteHistory(u, mtproto.PEER_CHAT, chat, false)
	}
	w.pub.drain()
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
		if left := alive(w.messages(u)); len(left) != 0 {
			t.Errorf("user %d keeps %d messages of the deleted chat", u, len(left))
		}
		if w.hasDialog(u, mtproto.PEER_CHAT, chat) {
			t.Errorf("user %d keeps the dialog with the deleted chat", u)
		}
	}
}

// innodbRowsRead returns the server's count of rows read.
func (w *world) innodbRowsRead() int64 {
	w.t.Helper()
	var name, value string
	if err := w.check.QueryRow("SHOW GLOBAL STATUS LIKE 'Innodb_rows_read'").Scan(&name, &value); err != nil {
		w.t.Fatal(err)
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

// TestDialogCountersWithoutScan: reading and deleting in a long dialog read
// the rows they change and the dialog's index, not every message of the
// dialog: the box lock is held for as long as a short dialog would take.
func TestDialogCountersWithoutScan(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b := newUser(), newUser()
	w.send(a, mtproto.PEER_USER, b, "first")
	w.pub.drain()

	// n more messages from a in b's box, written directly: n box
	// transactions would take minutes. The box counter and the dialog
	// follow them as the transactions would have left them.
	const n = 20_000
	data := w.messages(b)[0].Data
	base := w.state(b).LastMessageBoxId
	did := mtproto.MakeDialogId(b, mtproto.PEER_USER, a)
	const batch = 1000
	for from := 0; from < n; from += batch {
		var (
			values []string
			args   []any
		)
		for i := from + 1; i <= from+batch; i++ {
			values = append(values, "(?, ?, ?, ?, ?, ?, ?, ?, ?, 'm')")
			args = append(args, b, base+int32(i), did.A, did.B, 9_000_000_000+int64(i), a, mtproto.PEER_USER, a, data)
		}
		if _, err := w.check.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_data, message) VALUES "+
			strings.Join(values, ", "), args...); err != nil {
			t.Fatal(err)
		}
	}
	top := base + n
	if _, err := w.check.Exec("UPDATE user_state SET last_message_box_id = ? WHERE user_id = ?", top, b); err != nil {
		t.Fatal(err)
	}
	if _, err := w.check.Exec("UPDATE dialogs SET top_message = ?, unread_count = unread_count + ? WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		top, n, b, mtproto.PEER_USER, a); err != nil {
		t.Fatal(err)
	}
	w.checkDialog(b, mtproto.PEER_USER, a)

	// The bulk of the dialog is read once, as a client catching up does.
	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_USER, PeerId: a, MaxId: top - 10}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	const most = 2_000 // a scan of the dialog reads n
	measure := func(what string, f func() error) {
		t.Helper()
		start := w.innodbRowsRead()
		if err := f(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		w.pub.drain()
		if read := w.innodbRowsRead() - start; read > most {
			t.Errorf("%s read %d rows in a dialog of %d messages, want at most %d", what, read, n, most)
		}
		w.checkDialog(b, mtproto.PEER_USER, a)
	}
	measure("read the newest messages", func() error {
		_, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_USER, PeerId: a, MaxId: top - 5})
		return err
	})
	measure("delete an unread message", func() error {
		_, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: b, AuthKeyId: b * 10, Id: []int32{top - 2}})
		return err
	})
	measure("delete the top message", func() error {
		_, err := w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: b, AuthKeyId: b * 10, Id: []int32{top}})
		return err
	})
	if d := w.dialog(b, mtproto.PEER_USER, a); d.TopMessage != top-1 || d.UnreadCount != 3 {
		t.Errorf("dialog %+v, want top %d and 3 unread", d, top-1)
	}
}
