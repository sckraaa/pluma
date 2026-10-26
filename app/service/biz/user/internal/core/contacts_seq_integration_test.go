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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Contacts and the peer settings bar with seq (ADR 0020): the change, its
// rows of the seq journal and the pushes to the account's devices are one
// box transaction.

// seqUsers returns the users of userId's push of seq.
func seqUsers(t *testing.T, userId int64, seq int32) []*mtproto.User {
	t.Helper()
	for _, ev := range eventsOf(t, userId) {
		if ev.method != "sync.TL_sync_pushUpdates" {
			continue
		}
		in := &sync.TLSyncPushUpdates{}
		if err := jsonx.Unmarshal(ev.payload, in); err != nil {
			t.Fatal(err)
		}
		if in.GetUpdates().GetSeq() == seq {
			return in.GetUpdates().GetUsers()
		}
	}
	t.Fatalf("user %d: no push of seq %d", userId, seq)
	return nil
}

// describe writes the pushes from the n-th on as seq:update, where the
// update is peer_settings:<peer>/<bar> or privacy:<allowed users>.
func describe(t *testing.T, userId int64, n int) string {
	t.Helper()
	var out []string
	pushes := seqPushes(t, userId)
	if n > len(pushes) {
		return fmt.Sprintf("%d pushes, want more than %d", len(pushes), n)
	}
	for _, p := range pushes[n:] {
		u := p.update
		switch u.GetPredicateName() {
		case mtproto.Predicate_updatePeerSettings:
			peer := u.GetPeer_PEER()
			id := "u" + fmt.Sprint(peer.GetUserId())
			if peer.GetPredicateName() == mtproto.Predicate_peerChat {
				id = "c" + fmt.Sprint(peer.GetChatId())
			}
			bar := ""
			if s := u.GetSettings(); s.GetReportSpam() {
				bar = "spam"
			} else if s.GetAddContact() {
				bar = "add"
			}
			out = append(out, fmt.Sprintf("%d:peer_settings:%s/%s", p.seq, id, bar))
		case mtproto.Predicate_updatePrivacy:
			var allowed []int64
			for _, r := range u.GetRules() {
				if r.GetPredicateName() == mtproto.Predicate_privacyValueAllowUsers {
					allowed = append(allowed, r.GetUsers()...)
				}
			}
			out = append(out, fmt.Sprintf("%d:privacy:%s:%v", p.seq, u.GetKey().GetPredicateName(), allowed))
		default:
			out = append(out, fmt.Sprintf("%d:%s", p.seq, u.GetPredicateName()))
		}
	}
	return fmt.Sprint(out)
}

func plainUser(id int64, first string) *mtproto.User {
	return mtproto.MakeTLUser(&mtproto.User{Id: id, FirstName: mtproto.MakeFlagsString(first)}).To_User()
}

func TestContactsWithSeq(t *testing.T) {
	c := userCore(t)
	ann, _ := newUser(t, "Ann")
	bob, _ := newUser(t, "Bob")
	stranger, _ := newUser(t, "Sam")
	add := func(owner, id int64, first string, exception bool) bool {
		t.Helper()
		r, err := c.UserAddContact(&user.TLUserAddContact{
			UserId: owner, Id: id, FirstName: first, AddPhonePrivacyException: mtproto.ToBool(exception),
			Users: []*mtproto.User{plainUser(id, "Own")},
		})
		if err != nil {
			t.Fatal(err)
		}
		return mtproto.FromBool(r)
	}
	del := func(owner, id int64) {
		t.Helper()
		if _, err := c.UserDeleteContact(&user.TLUserDeleteContact{UserId: owner, Id: id, Users: []*mtproto.User{plainUser(id, "Own")}}); err != nil {
			t.Fatal(err)
		}
	}

	// Ann adds Bob: one update, with Bob as her contact.
	if mutual := add(ann, bob, "Bobby", false); mutual {
		t.Errorf("Bob has not added Ann: mutual")
	}
	if got, want := describe(t, ann, 0), fmt.Sprintf("[2:peer_settings:u%d/]", bob); got != want {
		t.Errorf("pushes %s, want %s", got, want)
	}
	if u := seqUsers(t, ann, 2); len(u) != 1 || u[0].GetId() != bob || !u[0].GetContact() || u[0].GetMutualContact() || u[0].GetFirstName().GetValue() != "Bobby" {
		t.Errorf("users %v: Bob as Ann's contact", u)
	}
	// The same again changes nothing; a new name is an update.
	add(ann, bob, "Bobby", false)
	if s := stateSeq(t, ann); s != userbox.BaseSeq+1 {
		t.Errorf("seq %d after a repeated add, want %d", s, userbox.BaseSeq+1)
	}
	add(ann, bob, "Robert", false)
	if u := seqUsers(t, ann, 3); len(u) != 1 || u[0].GetFirstName().GetValue() != "Robert" {
		t.Errorf("users of the rename %v", u)
	}

	// Bob adds Ann and shares his number: the contact, mutual now, and the
	// rules of his phone number with Ann allowed; Ann's devices get
	// nothing from Bob's transaction.
	if mutual := add(bob, ann, "Ann", true); !mutual {
		t.Errorf("Ann has Bob: not mutual")
	}
	if got, want := describe(t, bob, 0), fmt.Sprintf("[2:peer_settings:u%d/ 3:privacy:privacyKeyPhoneNumber:[%d]]", ann, ann); got != want {
		t.Errorf("pushes %s, want %s", got, want)
	}
	if u := seqUsers(t, bob, 2); len(u) != 1 || !u[0].GetContact() || !u[0].GetMutualContact() {
		t.Errorf("users %v: Ann as Bob's mutual contact", u)
	}
	if r := contactOf(t, ann, bob); r == nil || !r.mutual {
		t.Errorf("Ann's row %+v: mutual", r)
	}
	if s := stateSeq(t, ann); s != userbox.BaseSeq+2 {
		t.Errorf("Ann's seq %d, want %d", s, userbox.BaseSeq+2)
	}
	rules, err := itSvc.Dao.GetUserPrivacyRules(c.ctx, bob, mtproto.PHONE_NUMBER)
	if err != nil {
		t.Fatal(err)
	}
	if !mtproto.CheckPrivacyIsAllow(bob, rules.GetRules(), ann, func(int64, int64) bool { return false }, func(int64, []int64) bool { return false }) {
		t.Errorf("Ann does not see Bob's number: %v", rules.GetRules())
	}
	// Again: the contact and the exception are there, nothing to tell.
	add(bob, ann, "Ann", true)
	if s := stateSeq(t, bob); s != userbox.BaseSeq+2 {
		t.Errorf("Bob's seq %d after a repeated add, want %d", s, userbox.BaseSeq+2)
	}

	// Ann deletes Bob: one update, Bob no contact; his row is no longer
	// mutual. Deleting him again, or someone who is no contact, changes
	// nothing.
	del(ann, bob)
	if got, want := describe(t, ann, 2), fmt.Sprintf("[4:peer_settings:u%d/]", bob); got != want {
		t.Errorf("pushes of the deletion %s, want %s", got, want)
	}
	if u := seqUsers(t, ann, 4); len(u) != 1 || u[0].GetContact() || u[0].GetMutualContact() || u[0].GetFirstName().GetValue() != "Own" {
		t.Errorf("users %v: Bob as no contact", u)
	}
	if r := contactOf(t, ann, bob); r == nil || !r.deleted {
		t.Errorf("Ann's row %+v: deleted", r)
	}
	if r := contactOf(t, bob, ann); r == nil || r.mutual {
		t.Errorf("Bob's row %+v: not mutual", r)
	}
	del(ann, bob)
	del(ann, stranger)
	if s := stateSeq(t, ann); s != userbox.BaseSeq+3 {
		t.Errorf("seq %d after deletions of no contact, want %d", s, userbox.BaseSeq+3)
	}
	if ids, err := c.UserGetContactIdList(&user.TLUserGetContactIdList{UserId: ann}); err != nil || len(ids.GetDatas()) != 0 {
		t.Errorf("Ann's contacts %v, %v: the cache saw the deletion", ids.GetDatas(), err)
	}
}

func TestPeerSettingsBarWithSeq(t *testing.T) {
	c := userCore(t)
	ann, _ := newUser(t, "Ann")
	bob, _ := newUser(t, "Bob")
	bar := mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{ReportSpam: true, BlockContact: true}).To_PeerSettings()
	hide := func(peerType int32, peerId int64) {
		t.Helper()
		if _, err := c.UserDeletePeerSettings(&user.TLUserDeletePeerSettings{UserId: ann, PeerType: peerType, PeerId: peerId, Users: []*mtproto.User{plainUser(bob, "Bob")}}); err != nil {
			t.Fatal(err)
		}
	}
	set := func(peerType int32, peerId int64, s *mtproto.PeerSettings, chats []*mtproto.Chat) {
		t.Helper()
		var users []*mtproto.User
		if peerType == mtproto.PEER_USER {
			users = []*mtproto.User{plainUser(peerId, "Bob")}
		}
		if _, err := c.UserAddPeerSettings(&user.TLUserAddPeerSettings{UserId: ann, PeerType: peerType, PeerId: peerId, Settings: s, Users: users, Chats: chats}); err != nil {
			t.Fatal(err)
		}
	}
	stored := func() *mtproto.PeerSettings {
		t.Helper()
		s, err := c.UserGetPeerSettings(&user.TLUserGetPeerSettings{UserId: ann, PeerType: mtproto.PEER_USER, PeerId: bob})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// No bar: hiding it changes nothing.
	hide(mtproto.PEER_USER, bob)
	if s := stateSeq(t, ann); s != userbox.BaseSeq {
		t.Errorf("seq %d after hiding no bar", s)
	}

	// A bar, the same bar again, the bar hidden, hidden again.
	set(mtproto.PEER_USER, bob, bar, nil)
	if s := stored(); !s.GetReportSpam() || !s.GetBlockContact() {
		t.Errorf("stored %v", s)
	}
	set(mtproto.PEER_USER, bob, bar, nil)
	hide(mtproto.PEER_USER, bob)
	if s := stored(); s.GetReportSpam() || s.GetBlockContact() {
		t.Errorf("stored after hiding %v", s)
	}
	hide(mtproto.PEER_USER, bob)
	if got, want := describe(t, ann, 0), fmt.Sprintf("[2:peer_settings:u%d/spam 3:peer_settings:u%d/]", bob, bob); got != want {
		t.Errorf("pushes %s, want %s", got, want)
	}
	if u := seqUsers(t, ann, 3); len(u) != 1 || u[0].GetId() != bob {
		t.Errorf("users of the hidden bar %v: the peer", u)
	}

	// The bar of a group goes with the group.
	group := mtproto.MakeTLChat(&mtproto.Chat{Id: 1 << 40, Title: "G"}).To_Chat()
	set(mtproto.PEER_CHAT, group.GetId(), bar, []*mtproto.Chat{group})
	pushes := seqPushes(t, ann)
	if p := pushes[len(pushes)-1]; p.update.GetPeer_PEER().GetChatId() != group.GetId() || p.chats != 1 || p.users != 0 {
		t.Errorf("push of the group's bar: peer %v, %d chats, %d users", p.update.GetPeer_PEER(), p.chats, p.users)
	}

	// A contact's update carries the bar as stored.
	set(mtproto.PEER_USER, bob, mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{AddContact: true}).To_PeerSettings(), nil)
	if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: ann, Id: bob, FirstName: "Bob", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if got, want := describe(t, ann, 3), fmt.Sprintf("[5:peer_settings:u%d/add 6:peer_settings:u%d/add]", bob, bob); got != want {
		t.Errorf("pushes %s, want %s", got, want)
	}
}

func TestImportContactsWithSeq(t *testing.T) {
	ann, annPhone := newUser(t, "Ann")
	bob, bobPhone := newUser(t, "Bob")
	carl, carlPhone := newUser(t, "Carl")

	// Ann imports Bob, Carl and a number without an account: an update
	// of each new contact, with the user as her contact.
	importContacts(t, ann,
		phoneContact(1, "+"+bobPhone, "Bobby", ""),
		phoneContact(2, "+"+carlPhone, "Carl", "C"),
		phoneContact(3, "+"+nextPhone(), "Sam", ""))
	pushes := seqPushes(t, ann)
	var ids []int64
	for _, p := range pushes {
		if p.update.GetPredicateName() != mtproto.Predicate_updatePeerSettings || p.users != 1 {
			t.Errorf("push %d: %s with %d users", p.seq, p.update.GetPredicateName(), p.users)
		}
		ids = append(ids, p.update.GetPeer_PEER().GetUserId())
		u := seqUsers(t, ann, p.seq)[0]
		if u.GetId() != p.update.GetPeer_PEER().GetUserId() || !u.GetContact() || u.GetMutualContact() {
			t.Errorf("user of push %d: %v", p.seq, u)
		}
	}
	slices.Sort(ids)
	if want := []int64{bob, carl}; !slices.Equal(ids, want) || stateSeq(t, ann) != userbox.BaseSeq+2 {
		t.Errorf("updates of %v, seq %d; want %v, %d", ids, stateSeq(t, ann), want, userbox.BaseSeq+2)
	}

	// The same phone book changes nothing; Bob renamed is one update.
	importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Bobby", ""), phoneContact(2, "+"+carlPhone, "Carl", "C"))
	importContacts(t, ann, phoneContact(1, "+"+bobPhone, "Robert", ""), phoneContact(2, "+"+carlPhone, "Carl", "C"))
	if got, want := describe(t, ann, 2), fmt.Sprintf("[4:peer_settings:u%d/]", bob); got != want {
		t.Errorf("pushes %s, want %s", got, want)
	}
	if u := seqUsers(t, ann, 4); len(u) != 1 || u[0].GetFirstName().GetValue() != "Robert" {
		t.Errorf("users of the rename %v", u)
	}

	// Bob imports Ann, who has him: mutual on his side.
	importContacts(t, bob, phoneContact(1, "+"+annPhone, "Ann", ""))
	last := seqPushes(t, bob)
	if u := seqUsers(t, bob, last[len(last)-1].seq); len(u) != 1 || u[0].GetId() != ann || !u[0].GetMutualContact() {
		t.Errorf("users %v: Ann as Bob's mutual contact", u)
	}
}
