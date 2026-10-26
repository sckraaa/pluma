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

//go:build integration

package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// Privacy rules and blocks with seq (ADR 0020): the change, its row of the
// seq journal and its push to every device of the account are one box
// transaction of biz user; a change that changes nothing writes none.

func seqRowCount(t *testing.T, userId int64) int {
	t.Helper()
	return count(t, "SELECT COUNT(*) FROM user_seq_updates WHERE user_id = ?", userId)
}

func blockedPeerUser(id int64, first string) *mtproto.User {
	return mtproto.MakeTLUser(&mtproto.User{Id: id, FirstName: mtproto.MakeFlagsString(first)}).To_User()
}

func samePrivacyRules(a, b []*mtproto.PrivacyRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestPrivacyWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Alice")
	peer, _ := newUser(t, "Bob")
	bob := blockedPeerUser(peer, "Bob")

	// The cached rules of the key are dropped with the change.
	if got, err := c.svcCtx.Dao.GetUserPrivacyRules(c.ctx, u, mtproto.STATUS_TIMESTAMP); err != nil || len(got.GetRules()) != 1 ||
		got.GetRules()[0].GetPredicateName() != mtproto.Predicate_privacyValueAllowAll {
		t.Fatalf("default rules %v, %v", got, err)
	}
	rules := []*mtproto.PrivacyRule{
		mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule(),
		mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: []int64{peer}}).To_PrivacyRule(),
	}
	set := func(key int32, rules []*mtproto.PrivacyRule, users ...*mtproto.User) error {
		_, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: u, KeyType: key, Rules: rules, Users: users})
		return err
	}
	if err := set(mtproto.STATUS_TIMESTAMP, rules, bob); err != nil {
		t.Fatal(err)
	}
	got, err := c.svcCtx.Dao.GetUserPrivacyRules(c.ctx, u, mtproto.STATUS_TIMESTAMP)
	if err != nil || !samePrivacyRules(got.GetRules(), rules) {
		t.Fatalf("stored rules %v, %v; want %v", got, err, rules)
	}
	pushes := seqPushes(t, u)
	if len(pushes) != 1 {
		t.Fatalf("%d pushes, want 1", len(pushes))
	}
	p := pushes[0]
	if p.seq != userbox.BaseSeq+1 || p.update.GetPredicateName() != mtproto.Predicate_updatePrivacy ||
		p.update.GetKey().GetPredicateName() != mtproto.Predicate_privacyKeyStatusTimestamp || !samePrivacyRules(p.update.GetRules(), rules) {
		t.Errorf("push %d: %v", p.seq, p.update)
	}
	if p.users != 1 || p.chats != 0 {
		t.Errorf("push with %d users, %d chats; want the user the rules name", p.users, p.chats)
	}

	// The same rules again, and the default rules of a key nobody set:
	// nothing changes, nothing is written or pushed.
	if err = set(mtproto.STATUS_TIMESTAMP, rules, bob); err != nil {
		t.Fatal(err)
	}
	if err = set(mtproto.PROFILE_PHOTO, []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}); err != nil {
		t.Fatal(err)
	}
	if err = set(mtproto.PHONE_NUMBER, []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "SELECT COUNT(*) FROM user_privacies WHERE user_id = ? AND key_type IN (?, ?)", u, mtproto.PROFILE_PHOTO, mtproto.PHONE_NUMBER); n != 0 {
		t.Errorf("%d rows of default rules", n)
	}
	if n := len(seqPushes(t, u)); n != 1 || seqRowCount(t, u) != 1 || stateSeq(t, u) != userbox.BaseSeq+1 {
		t.Errorf("no-ops: %d pushes, %d rows, seq %d", n, seqRowCount(t, u), stateSeq(t, u))
	}

	// A key that is not one.
	if err = set(1000, rules); !errors.Is(err, mtproto.ErrPrivacyKeyInvalid) {
		t.Errorf("invalid key: %v", err)
	}

	// The next change of the key, and another key.
	everybody := []*mtproto.PrivacyRule{
		mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule(),
		mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: []int64{peer}}).To_PrivacyRule(),
	}
	if err = set(mtproto.STATUS_TIMESTAMP, everybody, bob); err != nil {
		t.Fatal(err)
	}
	if err = set(mtproto.PHONE_NUMBER, []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}); err != nil {
		t.Fatal(err)
	}
	var desc []string
	for _, p := range seqPushes(t, u) {
		desc = append(desc, fmt.Sprintf("%d:%s/%d/%d", p.seq, p.update.GetKey().GetPredicateName(), len(p.update.GetRules()), p.users))
	}
	if want := "[2:privacyKeyStatusTimestamp/2/1 3:privacyKeyStatusTimestamp/2/1 4:privacyKeyPhoneNumber/1/0]"; fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
	if got, err = c.svcCtx.Dao.GetUserPrivacyRules(c.ctx, u, mtproto.STATUS_TIMESTAMP); err != nil || !samePrivacyRules(got.GetRules(), everybody) {
		t.Errorf("stored rules %v, %v; want %v", got, err, everybody)
	}
	if s := stateSeq(t, u); s != userbox.BaseSeq+3 || seqRowCount(t, u) != 3 {
		t.Errorf("seq %d with %d rows, want %d", s, seqRowCount(t, u), userbox.BaseSeq+3)
	}
}

func TestBlocksWithSeq(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Alice")
	peer, _ := newUser(t, "Bob")
	other, _ := newUser(t, "Carl")
	bob := blockedPeerUser(peer, "Bob")

	block := func(id int64, users ...*mtproto.User) error {
		_, err := c.UserBlockPeer(&user.TLUserBlockPeer{UserId: u, PeerType: mtproto.PEER_USER, PeerId: id, Users: users})
		return err
	}
	unblock := func(id int64, users ...*mtproto.User) error {
		_, err := c.UserUnBlockPeer(&user.TLUserUnBlockPeer{UserId: u, PeerType: mtproto.PEER_USER, PeerId: id, Users: users})
		return err
	}
	blocked := func(id int64) bool {
		return count(t, "SELECT COUNT(*) FROM user_peer_blocks WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0", u, mtproto.PEER_USER, id) == 1
	}

	// The cached answer of CheckBlocked is dropped with each change.
	if c.svcCtx.Dao.CheckBlocked(c.ctx, u, peer) {
		t.Fatal("blocked before the block")
	}
	if err := block(peer, bob); err != nil {
		t.Fatal(err)
	}
	if !blocked(peer) || !c.svcCtx.Dao.CheckBlocked(c.ctx, u, peer) {
		t.Errorf("not blocked after the block")
	}
	// Blocked already: a no-op.
	if err := block(peer, bob); err != nil {
		t.Fatal(err)
	}
	if err := unblock(peer, bob); err != nil {
		t.Fatal(err)
	}
	if blocked(peer) || c.svcCtx.Dao.CheckBlocked(c.ctx, u, peer) {
		t.Errorf("blocked after the unblock")
	}
	// Not blocked: no-ops.
	if err := unblock(peer, bob); err != nil {
		t.Fatal(err)
	}
	if err := unblock(other); err != nil {
		t.Fatal(err)
	}
	// Blocked again: the row of the first block comes back.
	if err := block(peer, bob); err != nil {
		t.Fatal(err)
	}
	if !blocked(peer) {
		t.Errorf("not blocked after the second block")
	}

	pushes := seqPushes(t, u)
	var desc []string
	for _, p := range pushes {
		desc = append(desc, fmt.Sprintf("%d:u%d/%t/%s/%d", p.seq, p.update.GetPeerId().GetUserId(),
			p.update.GetBlocked_FLAGBOOLEAN(), p.update.GetBlocked_BOOL().GetPredicateName(), p.users))
		if p.update.GetPredicateName() != mtproto.Predicate_updatePeerBlocked || p.update.GetBlockedMyStoriesFrom() {
			t.Errorf("push %d: %v", p.seq, p.update)
		}
	}
	want := fmt.Sprintf("[2:u%d/true/boolTrue/1 3:u%d/false/boolFalse/1 4:u%d/true/boolTrue/1]", peer, peer, peer)
	if fmt.Sprint(desc) != want {
		t.Errorf("pushes %v, want %s", desc, want)
	}
	if s := stateSeq(t, u); s != userbox.BaseSeq+3 || seqRowCount(t, u) != 3 {
		t.Errorf("seq %d with %d rows, want %d", s, seqRowCount(t, u), userbox.BaseSeq+3)
	}

	// Only another user is blocked.
	for _, in := range []*user.TLUserBlockPeer{
		{UserId: u, PeerType: mtproto.PEER_USER, PeerId: u},
		{UserId: u, PeerType: mtproto.PEER_CHAT, PeerId: other},
		{UserId: u, PeerType: mtproto.PEER_USER},
	} {
		if _, err := c.UserBlockPeer(in); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
			t.Errorf("block of %d/%d: %v", in.PeerType, in.PeerId, err)
		}
		if _, err := c.UserUnBlockPeer(&user.TLUserUnBlockPeer{UserId: in.UserId, PeerType: in.PeerType, PeerId: in.PeerId}); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
			t.Errorf("unblock of %d/%d: %v", in.PeerType, in.PeerId, err)
		}
	}
	if n := len(seqPushes(t, u)); n != 3 || blocked(u) || blocked(other) {
		t.Errorf("invalid peers: %d pushes", n)
	}
}
