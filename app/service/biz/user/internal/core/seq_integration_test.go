//go:build integration

package core

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Updates with seq of the account (ADR 0020): the change, its row of the
// seq journal and its push are one box transaction.

// seqPush is a push of an update with seq, as the transaction wrote it.
type seqPush struct {
	seq    int32
	update *mtproto.Update
	users  int // users of the container
	chats  int
}

// seqPushes returns userId's pushes of updates with seq in mq_outbox, and
// checks each one: sync.pushUpdates to the account itself (every device),
// one update in an updates container with the seq and date of its row of
// the seq journal.
func seqPushes(t *testing.T, userId int64) []seqPush {
	t.Helper()
	var out []seqPush
	for _, ev := range eventsOf(t, userId) {
		if ev.method != "sync.TL_sync_pushUpdates" {
			continue
		}
		in := &sync.TLSyncPushUpdates{}
		if err := jsonx.Unmarshal(ev.payload, in); err != nil {
			t.Fatal(err)
		}
		c := in.GetUpdates()
		if c.GetSeq() == 0 {
			continue
		}
		if ev.topic != "Sync-T" || ev.recipient != userId || in.GetUserId() != userId {
			t.Errorf("push of seq %d: topic %s, recipient %d, user %d", c.GetSeq(), ev.topic, ev.recipient, in.GetUserId())
		}
		if c.GetPredicateName() != mtproto.Predicate_updates || len(c.GetUpdates()) != 1 {
			t.Fatalf("push of seq %d: %s with %d updates", c.GetSeq(), c.GetPredicateName(), len(c.GetUpdates()))
		}
		var (
			pts, date2 int64
			kind       string
		)
		if err := itRaw.QueryRow("SELECT pts, date2, update_type FROM user_seq_updates WHERE user_id = ? AND seq = ?", userId, c.GetSeq()).Scan(&pts, &date2, &kind); err != nil {
			t.Fatalf("seq %d has no row: %v", c.GetSeq(), err)
		}
		if int64(c.GetDate()) != date2 || kind != c.GetUpdates()[0].GetPredicateName() {
			t.Errorf("push of seq %d: date %d, %s; row: date %d, %s", c.GetSeq(), c.GetDate(), c.GetUpdates()[0].GetPredicateName(), date2, kind)
		}
		out = append(out, seqPush{seq: c.GetSeq(), update: c.GetUpdates()[0], users: len(c.GetUsers()), chats: len(c.GetChats())})
	}
	return out
}

// stateSeq returns userId's committed seq.
func stateSeq(t *testing.T, userId int64) int32 {
	t.Helper()
	st, err := userbox.Read(t.Context(), itSvc.Dao.BoxDB, userId)
	if err != nil {
		t.Fatal(err)
	}
	return st.Seq
}

func notifySettings(muteUntil int32) *mtproto.PeerNotifySettings {
	return mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{MuteUntil: mtproto.MakeFlagsInt32(muteUntil)}).To_PeerNotifySettings()
}

func TestNotifySettingsWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Alice")
	peer, _ := newUser(t, "Bob")
	bob := mtproto.MakeTLUser(&mtproto.User{Id: peer, FirstName: mtproto.MakeFlagsString("Bob")}).To_User()

	if _, err := c.UserSetNotifySettings(&user.TLUserSetNotifySettings{
		UserId: u, PeerType: mtproto.PEER_USER, PeerId: peer, Settings: notifySettings(1000), Users: []*mtproto.User{bob},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserSetNotifySettings(&user.TLUserSetNotifySettings{
		UserId: u, PeerType: mtproto.PEER_USERS, Settings: notifySettings(5),
	}); err != nil {
		t.Fatal(err)
	}
	// The stored settings are the new ones, also when a row was there.
	if _, err := c.UserSetNotifySettings(&user.TLUserSetNotifySettings{
		UserId: u, PeerType: mtproto.PEER_USER, PeerId: peer, Settings: notifySettings(2000), Users: []*mtproto.User{bob},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := c.svcCtx.Dao.GetUserNotifySettings(c.ctx, u, mtproto.PEER_USER, peer)
	if err != nil || got.GetMuteUntil().GetValue() != 2000 {
		t.Fatalf("settings %v, %v; want mute_until 2000", got, err)
	}

	pushes := seqPushes(t, u)
	var desc []string
	for _, p := range pushes {
		desc = append(desc, fmt.Sprintf("%d:%s/%d", p.seq, notifyPeer(p.update), p.update.GetNotifySettings().GetMuteUntil().GetValue()))
	}
	if want := fmt.Sprintf("[2:u%d/1000 3:users/5 4:u%d/2000]", peer, peer); fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
	if pushes[0].users != 1 || pushes[1].users != 0 {
		t.Errorf("users of the pushes %d, %d; want the peer with its settings only", pushes[0].users, pushes[1].users)
	}
	if s := stateSeq(t, u); s != userbox.BaseSeq+3 {
		t.Errorf("seq %d, want %d", s, userbox.BaseSeq+3)
	}

	// The reset: the default for each peer that had settings and for the
	// three classes, and nothing stored.
	if _, err = c.UserResetNotifySettings(&user.TLUserResetNotifySettings{UserId: u}); err != nil {
		t.Fatal(err)
	}
	desc = desc[:0]
	for _, p := range seqPushes(t, u)[3:] {
		desc = append(desc, fmt.Sprintf("%d:%s", p.seq, notifyPeer(p.update)))
	}
	if want := fmt.Sprintf("[5:u%d 6:users 7:chats 8:broadcasts]", peer); fmt.Sprint(desc) != want {
		t.Errorf("pushes of the reset %v, want %s", desc, want)
	}
	if got, err = c.svcCtx.Dao.GetUserNotifySettings(c.ctx, u, mtproto.PEER_USER, peer); err != nil || got.GetMuteUntil() != nil {
		t.Errorf("settings after the reset %v, %v", got, err)
	}
}

func notifyPeer(u *mtproto.Update) string {
	p := u.GetPeer_NOTIFYPEER()
	switch p.GetPredicateName() {
	case mtproto.Predicate_notifyPeer:
		return fmt.Sprintf("u%d", p.GetPeer().GetUserId())
	case mtproto.Predicate_notifyUsers:
		return "users"
	case mtproto.Predicate_notifyChats:
		return "chats"
	case mtproto.Predicate_notifyBroadcasts:
		return "broadcasts"
	}
	return p.GetPredicateName()
}
