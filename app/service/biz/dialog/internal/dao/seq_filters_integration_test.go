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

package dao

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Folders and the unread mark with seq (ADR 0020): each change is one box
// transaction with its row of the seq journal and its push to every device
// of the account; a change of nothing writes neither.

// seqDesc describes the pushes with seq of userId from the from-th on, one
// "seq:update" each.
func seqDesc(t *testing.T, userId int64, from int) []string {
	t.Helper()
	var out []string
	for _, p := range seqPushes(t, userId)[from:] {
		u := p.update
		var s string
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateDialogFilter:
			s = fmt.Sprintf("filter %d", u.GetId_INT32())
			if f := u.GetFilter(); f != nil {
				s += " " + f.GetTitle_TEXTWITHENTITIES().GetText()
			}
		case mtproto.Predicate_updateDialogFilterOrder:
			s = fmt.Sprintf("order %v", u.GetOrder_VECTORINT32())
		case mtproto.Predicate_updateDialogFilters:
			s = "filters"
		case mtproto.Predicate_updateDialogUnreadMark:
			p := u.GetPeer_DIALOGPEER().GetPeer()
			s = fmt.Sprintf("unread %v u%d c%d", u.GetUnread(), p.GetUserId(), p.GetChatId())
			if u.GetSavedPeerId() != nil {
				s += " saved"
			}
		default:
			s = u.GetPredicateName()
		}
		out = append(out, fmt.Sprintf("%d:%s u%d c%d", p.seq, s, p.users, p.chats))
	}
	return out
}

// checkSeq checks that userId's pushes with seq from the from-th on are
// want, and that the committed seq is the last of them.
func checkSeq(t *testing.T, userId int64, from int, want ...string) {
	t.Helper()
	got := seqDesc(t, userId, from)
	if !slices.Equal(got, want) {
		t.Fatalf("pushes with seq:\n got %q\nwant %q", got, want)
	}
	if s, n := stateSeq(t, userId), len(seqPushes(t, userId)); s != userbox.BaseSeq+int32(n) {
		t.Fatalf("seq %d after %d pushes, want %d", s, n, userbox.BaseSeq+int32(n))
	}
}

func seqPeers() ([]*mtproto.User, []*mtproto.Chat) {
	return []*mtproto.User{mtproto.MakeTLUser(&mtproto.User{Id: 1, FirstName: mtproto.MakeFlagsString("Bob")}).To_User()},
		[]*mtproto.Chat{mtproto.MakeTLChat(&mtproto.Chat{Id: 1, Title: "Team"}).To_Chat()}
}

func TestDialogFilterWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	users, chats := seqPeers()
	saveSeq := func(id int32, name string) {
		t.Helper()
		f := withChats(id, 1, 0)
		f.Title_TEXTWITHENTITIES.Text = name
		if err := itDao.SaveDialogFilter(ctx, u, id, f, users, chats); err != nil {
			t.Fatalf("save folder %d: %v", id, err)
		}
	}

	saveSeq(2, "Work")
	saveSeq(2, "Work") // as it is: nothing
	saveSeq(2, "Job")
	saveSeq(3, "Family")
	if ids, titles := stored(t, u); !slices.Equal(ids, []int32{2, 3}) || titles[2] != "Job" {
		t.Fatalf("folders %v %v", ids, titles)
	}
	checkSeq(t, u, 0,
		"2:filter 2 Work u1 c1",
		"3:filter 2 Job u1 c1",
		"4:filter 3 Family u1 c1")

	// A folder over the limit is refused with nothing written.
	for id := int32(4); id < 2+DialogFiltersLimit; id++ {
		save(t, u, id, "More")
	}
	n := len(seqPushes(t, u))
	if err := itDao.SaveDialogFilter(ctx, u, 99, folder(99, "Over"), nil, nil); !errors.Is(err, ErrDialogFiltersTooMuch) {
		t.Fatalf("a folder over the limit: %v", err)
	}
	checkSeq(t, u, n)
	for id := int32(4); id < 2+DialogFiltersLimit; id++ {
		if err := itDao.DeleteDialogFilter(ctx, u, id); err != nil {
			t.Fatal(err)
		}
	}

	n = len(seqPushes(t, u))
	if err := itDao.DeleteDialogFilter(ctx, u, 3); err != nil {
		t.Fatal(err)
	}
	if err := itDao.DeleteDialogFilter(ctx, u, 3); err != nil { // not there: nothing
		t.Fatal(err)
	}
	if ids, _ := stored(t, u); !slices.Equal(ids, []int32{2}) {
		t.Fatalf("folders after the deletion %v", ids)
	}
	checkSeq(t, u, n, fmt.Sprintf("%d:filter 3 u0 c0", int(userbox.BaseSeq)+n+1))
	if f := seqPushes(t, u)[n].update; f.GetFilter() != nil {
		t.Fatalf("the deletion carries a folder: %v", f.GetFilter())
	}
}

func TestDialogFilterOrderWithSeq(t *testing.T) {
	ctx := context.Background()
	order := func(u int64, ids ...int32) []int32 {
		t.Helper()
		got, err := itDao.OrderDialogFilters(ctx, u, ids)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Without Premium "All chats" is first in the order the devices get.
	u := newUser()
	save(t, u, 2, "Work")
	save(t, u, 3, "Family")
	n := len(seqPushes(t, u))
	if got := order(u, 3, 0, 2); !slices.Equal(got, []int32{3, 2}) {
		t.Fatalf("stored order %v", got)
	}
	order(u, 3, 2)    // as it is: nothing
	order(u, 3, 0, 2) // "All chats" is not placed without Premium: nothing
	checkSeq(t, u, n, fmt.Sprintf("%d:order [0 3 2] u0 c0", int(userbox.BaseSeq)+n+1))

	// With Premium it takes its place, and a row keeps it.
	p := newPremiumUser(t)
	save(t, p, 2, "Work")
	save(t, p, 3, "Family")
	n = len(seqPushes(t, p))
	order(p, 3, 0, 2)
	order(p, 3, 0, 2) // as it is: nothing
	if ids, _ := stored(t, p); !slices.Equal(ids, []int32{3, 0, 2}) {
		t.Fatalf("stored folders %v", ids)
	}
	// After Premium its place goes back to the top.
	setPremium(t, p, false, 0)
	order(p, 3, 0, 2)
	checkSeq(t, p, n,
		fmt.Sprintf("%d:order [3 0 2] u0 c0", int(userbox.BaseSeq)+n+1),
		fmt.Sprintf("%d:order [0 3 2] u0 c0", int(userbox.BaseSeq)+n+2))
}

func TestDialogFilterTagsWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	for _, on := range []bool{false, true, true, false, false} {
		if err := itDao.SetDialogFilterTags(ctx, u, on); err != nil {
			t.Fatal(err)
		}
		if got, err := itDao.DialogFilterTags(ctx, u); err != nil || got != on {
			t.Fatalf("tags %v, %v; want %v", got, err, on)
		}
	}
	checkSeq(t, u, 0, "2:filters u0 c0", "3:filters u0 c0")
}

// newDialogRow gives userId a dialog with a peer.
func newDialogRow(t *testing.T, userId int64, peerType int32, peerId int64) {
	t.Helper()
	if _, _, err := itDao.DialogsDAO.InsertIgnore(context.Background(), &dataobject.DialogsDO{
		UserId:           userId,
		PeerType:         peerType,
		PeerId:           peerId,
		PeerDialogId:     mtproto.MakePeerDialogId(peerType, peerId),
		DraftMessageData: "null",
		Date2:            time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDialogUnreadMarkWithSeq(t *testing.T) {
	ctx := context.Background()
	u, peer := newUser(), newUser()
	const chat = 77
	newDialogRow(t, u, mtproto.PEER_USER, peer)
	newDialogRow(t, u, mtproto.PEER_USER, u) // Saved Messages
	newDialogRow(t, u, mtproto.PEER_CHAT, chat)
	users, chats := seqPeers()

	mark := func(peerType int32, peerId int64, unread bool) error {
		t.Helper()
		switch peerType {
		case mtproto.PEER_CHAT:
			return itDao.MarkDialogUnread(ctx, u, peerType, peerId, unread, nil, chats)
		default:
			return itDao.MarkDialogUnread(ctx, u, peerType, peerId, unread, users, nil)
		}
	}
	unreadOf := func(peerType int32, peerId int64) bool {
		t.Helper()
		d, err := itDao.GetDialog(ctx, u, peerType, peerId)
		if err != nil {
			t.Fatal(err)
		}
		return d.GetDialog().GetUnreadMark()
	}

	if unreadOf(mtproto.PEER_USER, peer) {
		t.Fatal("a new dialog is marked unread")
	}
	for _, step := range []struct {
		peerType int32
		peerId   int64
		unread   bool
	}{
		{mtproto.PEER_USER, peer, true},
		{mtproto.PEER_USER, peer, true}, // as it is: nothing
		{mtproto.PEER_SELF, u, true},
		{mtproto.PEER_USER, u, true}, // the same dialog: nothing
		{mtproto.PEER_CHAT, chat, true},
		{mtproto.PEER_USER, peer, false},
		{mtproto.PEER_CHAT, chat, false},
		{mtproto.PEER_CHAT, chat, false}, // as it is: nothing
	} {
		if err := mark(step.peerType, step.peerId, step.unread); err != nil {
			t.Fatalf("mark (%d, %d) %v: %v", step.peerType, step.peerId, step.unread, err)
		}
		// The cached dialog follows.
		if got := unreadOf(step.peerType, step.peerId); got != step.unread {
			t.Fatalf("dialog (%d, %d) unread %v, want %v", step.peerType, step.peerId, got, step.unread)
		}
	}
	n := len(seqPushes(t, u))
	if err := mark(mtproto.PEER_USER, peer+1000, true); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("a peer without a dialog: %v, want PEER_ID_INVALID", err)
	}
	checkSeq(t, u, 0,
		fmt.Sprintf("2:unread true u%d c0 u1 c0", peer),
		fmt.Sprintf("3:unread true u%d c0 u1 c0", u),
		fmt.Sprintf("4:unread true u0 c%d u0 c1", chat),
		fmt.Sprintf("5:unread false u%d c0 u1 c0", peer),
		fmt.Sprintf("6:unread false u0 c%d u0 c1", chat))
	if n != 5 {
		t.Fatalf("%d pushes before the peer without a dialog", n)
	}
	if desc := strings.Join(seqDesc(t, u, 0), " "); strings.Contains(desc, "saved") {
		t.Fatalf("saved_peer_id set: %s", desc)
	}
}
