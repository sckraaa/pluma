//go:build integration

package core

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// user.getStatusWatchers answers whoever sees the user: the users who have
// it as a contact, who have a private dialog with it, and the members of
// its live basic groups (task 032), without the user, deleted accounts,
// members who left, and groups that are gone.
func TestStatusWatchers(t *testing.T) {
	c := userCore(t)
	me, _ := newUser(t, "Mia")
	ann, _ := newUser(t, "Ann") // has Mia as a contact
	bob, _ := newUser(t, "Bob") // has a private dialog with Mia
	cat, _ := newUser(t, "Cat") // member of Mia's group
	dan, _ := newUser(t, "Dan") // left Mia's group
	eve, _ := newUser(t, "Eve") // member of a group Mia left
	fay, _ := newUser(t, "Fay") // member of Mia's deactivated group
	gus, _ := newUser(t, "Gus") // member of Mia's group, deleted the account
	hal, _ := newUser(t, "Hal") // Mia has Hal as a contact, not Hal Mia
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := itRaw.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_first_name, is_deleted, date2) VALUES (?, ?, 'M.', 0, 1)", ann, me)
	exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_first_name, is_deleted, date2) VALUES (?, ?, 'H.', 0, 1)", me, hal)
	exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, date2) VALUES (?, ?, ?, ?, 'null', 1)",
		bob, mtproto.PEER_USER, me, mtproto.MakePeerDialogId(mtproto.PEER_USER, me))

	group := func(id int64, deactivated bool, members map[int64]int) {
		t.Helper()
		exec("INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title, deactivated, date) VALUES (?, ?, 1, ?, ?, 'G', ?, 1)",
			id, me, id, len(members), deactivated)
		for u, state := range members {
			exec("INSERT INTO chat_participants (chat_id, user_id, state) VALUES (?, ?, ?)", id, u, state)
		}
	}
	base := me * 10
	group(base+1, false, map[int64]int{me: mtproto.ChatMemberStateNormal, cat: mtproto.ChatMemberStateNormal,
		dan: mtproto.ChatMemberStateLeft, gus: mtproto.ChatMemberStateNormal})
	group(base+2, false, map[int64]int{me: mtproto.ChatMemberStateLeft, eve: mtproto.ChatMemberStateNormal})
	group(base+3, true, map[int64]int{me: mtproto.ChatMemberStateNormal, fay: mtproto.ChatMemberStateNormal})
	exec("UPDATE users SET deleted = 1, phone = CONCAT('-', id) WHERE id = ?", gus)

	r, err := c.UserGetStatusWatchers(&user.TLUserGetStatusWatchers{UserId: me})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{ann, bob, cat}
	slices.Sort(want)
	if got := r.GetDatas(); !slices.Equal(got, want) {
		t.Fatalf("watchers %v, want %v: Ann %d, Bob %d, Cat %d; not Dan %d, Eve %d, Fay %d, Gus %d, Hal %d",
			got, want, ann, bob, cat, dan, eve, fay, gus, hal)
	}

	// The support ghost has no watchers: nobody is told when it is online.
	exec("UPDATE users SET ghost = 1 WHERE id = ?", me)
	if r, err = c.UserGetStatusWatchers(&user.TLUserGetStatusWatchers{UserId: me}); err != nil || len(r.GetDatas()) != 0 {
		t.Fatalf("watchers of a ghost: %v, %v", r.GetDatas(), err)
	}
}
