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
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Pins of dialogs and of Saved Messages with seq (ADR 0020): the pins, the
// row of the seq journal and the push are one box transaction, the update
// carries what is stored, and a request that changes nothing writes none.

func pinAddDialog(t *testing.T, userId int64, peerType int32, peerId int64, folderId int32) {
	t.Helper()
	if _, err := itDao.BoxDB.Exec(
		"INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, folder_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, 'null', 1)",
		userId, peerType, peerId, mtproto.MakePeerDialogId(peerType, peerId), folderId); err != nil {
		t.Fatal(err)
	}
}

func pinAddSavedDialog(t *testing.T, userId int64, peerType int32, peerId int64) {
	t.Helper()
	if _, err := itDao.BoxDB.Exec(
		"INSERT INTO saved_dialogs (user_id, peer_type, peer_id, top_message) VALUES (?, ?, ?, 1)",
		userId, peerType, peerId); err != nil {
		t.Fatal(err)
	}
}

// pinStored returns userId's pinned dialogs of a folder from the top, as
// "u<id>" and "c<id>".
func pinStored(t *testing.T, userId int64, folderId int32) string {
	t.Helper()
	column, folder := pinColumn(folderId)
	return pinRows(t, "SELECT peer_type, peer_id FROM dialogs WHERE user_id = ? AND "+folder+" AND "+column+" > 0 ORDER BY "+column+" DESC", userId)
}

func pinStoredSaved(t *testing.T, userId int64) string {
	t.Helper()
	return pinRows(t, "SELECT peer_type, peer_id FROM saved_dialogs WHERE user_id = ? AND pinned > 0 ORDER BY pinned DESC", userId)
}

func pinRows(t *testing.T, query string, userId int64) string {
	t.Helper()
	rows, err := itDao.BoxDB.Query(query, userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			peerType int32
			peerId   int64
		)
		if err = rows.Scan(&peerType, &peerId); err != nil {
			t.Fatal(err)
		}
		out = append(out, pinPeerName(mtproto.MakePeer(peerType, peerId)))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(out)
}

func pinPeerName(p *mtproto.Peer) string {
	switch p.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		return fmt.Sprint("u", p.GetUserId())
	case mtproto.Predicate_peerChat:
		return fmt.Sprint("c", p.GetChatId())
	}
	return p.GetPredicateName()
}

// pinUpdate describes an update of pins: "pin u5/0" (pinned, peer, folder),
// "unpin u5/0", "order/0 [u5 c7]", and the same without the folder for
// Saved Messages.
func pinUpdate(u *mtproto.Update) string {
	order := func() string {
		var peers []string
		for _, p := range u.GetOrder_FLAGVECTORDIALOGPEER() {
			peers = append(peers, pinPeerName(p.GetPeer()))
		}
		return fmt.Sprint(peers)
	}
	pin := func() string {
		if u.GetPinned() {
			return "pin " + pinPeerName(u.GetPeer_DIALOGPEER().GetPeer())
		}
		return "unpin " + pinPeerName(u.GetPeer_DIALOGPEER().GetPeer())
	}
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateDialogPinned:
		return fmt.Sprintf("%s/%d", pin(), u.GetFolderId().GetValue())
	case mtproto.Predicate_updatePinnedDialogs:
		return fmt.Sprintf("order/%d %s", u.GetFolderId().GetValue(), order())
	case mtproto.Predicate_updateSavedDialogPinned:
		return "saved " + pin()
	case mtproto.Predicate_updatePinnedSavedDialogs:
		return "saved order " + order()
	}
	return u.GetPredicateName()
}

// pinCheck checks userId's pushes with seq after the first `from`: one
// per change, in the order of the changes, with seq after seq, each the
// update of its journal row, and the committed seq at the last of them.
func pinCheck(t *testing.T, userId int64, from int, want ...string) []seqPush {
	t.Helper()
	pushes := seqPushes(t, userId)
	if len(pushes) < from {
		t.Fatalf("%d pushes, want at least %d", len(pushes), from)
	}
	var got []string
	for i, p := range pushes {
		if p.seq != userbox.BaseSeq+int32(i)+1 {
			t.Errorf("push %d has seq %d, want %d", i, p.seq, userbox.BaseSeq+int32(i)+1)
		}
		var data string
		if err := itDao.BoxDB.QueryRow("SELECT update_data FROM user_seq_updates WHERE user_id = ? AND seq = ?", userId, p.seq).Scan(&data); err != nil {
			t.Fatal(err)
		}
		row := &mtproto.Update{}
		if err := jsonx.UnmarshalFromString(data, row); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(row, p.update) {
			t.Errorf("seq %d: pushed %v, journaled %v", p.seq, p.update, row)
		}
		if i >= from {
			got = append(got, pinUpdate(p.update))
		}
	}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("pushes\n %s\nwant\n %s", strings.Join(got, "; "), strings.Join(want, "; "))
	}
	if s := stateSeq(t, userId); s != userbox.BaseSeq+int32(len(pushes)) {
		t.Errorf("seq %d, want %d", s, userbox.BaseSeq+int32(len(pushes)))
	}
	return pushes[from:]
}

func pinUser(id int64) *mtproto.User {
	return mtproto.MakeTLUser(&mtproto.User{Id: id}).To_User()
}

func pinChat(id int64) *mtproto.Chat {
	return mtproto.MakeTLChat(&mtproto.Chat{Id: id}).To_Chat()
}

func TestToggleDialogPinWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	pinAddDialog(t, u, mtproto.PEER_USER, 101, 0)
	pinAddDialog(t, u, mtproto.PEER_USER, 102, 0)
	pinAddDialog(t, u, mtproto.PEER_CHAT, 7, 0)
	pinAddDialog(t, u, mtproto.PEER_USER, 103, 1)
	toggle := func(peerType int32, peerId int64, pinned bool, users []*mtproto.User, chats []*mtproto.Chat) int32 {
		t.Helper()
		folderId, err := itDao.ToggleDialogPin(ctx, u, peerType, peerId, pinned, users, chats)
		if err != nil {
			t.Fatalf("toggle %d:%d %v: %v", peerType, peerId, pinned, err)
		}
		return folderId
	}

	// The cached dialog follows its pin.
	if d, err := itDao.GetDialog(ctx, u, mtproto.PEER_USER, 101); err != nil || d.GetDialog().GetPinned() {
		t.Fatalf("dialog %v, %v", d, err)
	}
	toggle(mtproto.PEER_USER, 101, true, []*mtproto.User{pinUser(101)}, nil)
	if d, err := itDao.GetDialog(ctx, u, mtproto.PEER_USER, 101); err != nil || !d.GetDialog().GetPinned() {
		t.Fatalf("dialog after the pin %v, %v", d, err)
	}
	// A pin goes on top, also in the same second.
	toggle(mtproto.PEER_CHAT, 7, true, nil, []*mtproto.Chat{pinChat(7)})
	if got := pinStored(t, u, 0); got != "[c7 u101]" {
		t.Fatalf("pins %s, want [c7 u101]", got)
	}

	// As asked already: nothing.
	toggle(mtproto.PEER_USER, 101, true, nil, nil)
	toggle(mtproto.PEER_USER, 102, false, nil, nil)
	if _, err := itDao.ToggleDialogPin(ctx, u, mtproto.PEER_USER, 199, true, nil, nil); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("a pin of no dialog: %v, want PEER_ID_INVALID", err)
	}
	pushes := pinCheck(t, u, 0, "pin u101/0", "pin c7/0")
	if pushes[0].users != 1 || pushes[0].chats != 0 || pushes[1].users != 0 || pushes[1].chats != 1 {
		t.Errorf("peers of the pushes %+v", pushes)
	}

	toggle(mtproto.PEER_USER, 101, false, nil, nil)
	if f := toggle(mtproto.PEER_USER, 103, true, nil, nil); f != 1 {
		t.Errorf("folder of an archived dialog %d, want 1", f)
	}
	if got := pinStored(t, u, 0); got != "[c7]" {
		t.Errorf("pins %s, want [c7]", got)
	}
	if got := pinStored(t, u, 1); got != "[u103]" {
		t.Errorf("pins of the archive %s, want [u103]", got)
	}
	pinCheck(t, u, 2, "unpin u101/0", "pin u103/1")
}

func TestReorderPinnedDialogsWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	for _, id := range []int64{201, 202, 203} {
		pinAddDialog(t, u, mtproto.PEER_USER, id, 0)
	}
	pinAddDialog(t, u, mtproto.PEER_CHAT, 9, 0)
	pinAddDialog(t, u, mtproto.PEER_USER, 204, 1)
	reorder := func(force bool, folderId int32, order ...int64) {
		t.Helper()
		var users []*mtproto.User
		for _, id := range order {
			if id > 0 {
				users = append(users, pinUser(id))
			}
		}
		if err := itDao.ReorderPinnedDialogs(ctx, u, force, folderId, order, users, nil); err != nil {
			t.Fatal(err)
		}
	}
	c9 := mtproto.MakePeerDialogId(mtproto.PEER_CHAT, 9)

	reorder(true, 0, 202, c9, 201)
	if got := pinStored(t, u, 0); got != "[u202 c9 u201]" {
		t.Fatalf("pins %s", got)
	}
	reorder(true, 0, 202, c9, 201) // the same order: nothing
	// A dialog that is not there is left out, the others are unpinned.
	reorder(true, 0, 201, 299)
	// Without force the other pins stay, below.
	reorder(false, 0, 203)
	if got := pinStored(t, u, 0); got != "[u203 u201]" {
		t.Fatalf("pins %s, want [u203 u201]", got)
	}
	reorder(true, 1, 204)
	reorder(true, 0)
	if got := pinStored(t, u, 0); got != "[]" {
		t.Errorf("pins %s, want none", got)
	}
	if got := pinStored(t, u, 1); got != "[u204]" {
		t.Errorf("pins of the archive %s", got)
	}
	reorder(true, 0) // none, and none asked: nothing
	pushes := pinCheck(t, u, 0,
		"order/0 [u202 c9 u201]", "order/0 [u201]", "order/0 [u203 u201]", "order/1 [u204]", "order/0 []")
	if pushes[0].users != 2 {
		t.Errorf("users of the first push %d, want the two of the order", pushes[0].users)
	}
	// No pins: the order is not sent, the client asks for the pinned dialogs.
	if pushes[4].update.GetOrder_FLAGVECTORDIALOGPEER() != nil {
		t.Errorf("an empty order is sent: %v", pushes[4].update)
	}
}

func TestToggleSavedDialogPinWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	pinAddSavedDialog(t, u, mtproto.PEER_USER, 301)
	pinAddSavedDialog(t, u, mtproto.PEER_CHAT, 11)
	pinAddSavedDialog(t, u, mtproto.PEER_USER, u) // the owner's own notes
	toggle := func(peerType int32, peerId int64, pinned bool, users []*mtproto.User) {
		t.Helper()
		if err := itDao.ToggleSavedDialogPin(ctx, u, mtproto.MakePeerUtil(peerType, peerId), pinned, users, nil); err != nil {
			t.Fatal(err)
		}
	}

	toggle(mtproto.PEER_USER, 301, true, []*mtproto.User{pinUser(301)})
	toggle(mtproto.PEER_SELF, u, true, []*mtproto.User{pinUser(u)})
	if got, want := pinStoredSaved(t, u), fmt.Sprintf("[u%d u301]", u); got != want {
		t.Fatalf("pins %s, want %s", got, want)
	}
	// As asked already, or no such dialog: nothing.
	toggle(mtproto.PEER_USER, 301, true, nil)
	toggle(mtproto.PEER_CHAT, 11, false, nil)
	toggle(mtproto.PEER_USER, 399, true, nil)
	toggle(mtproto.PEER_USER, 301, false, nil)
	if got, want := pinStoredSaved(t, u), fmt.Sprintf("[u%d]", u); got != want {
		t.Errorf("pins %s, want %s", got, want)
	}
	pushes := pinCheck(t, u, 0, "saved pin u301", fmt.Sprintf("saved pin u%d", u), "saved unpin u301")
	if pushes[0].users != 1 || pushes[2].users != 0 {
		t.Errorf("peers of the pushes %+v", pushes)
	}
}

func TestReorderPinnedSavedDialogsWithSeq(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	pinAddSavedDialog(t, u, mtproto.PEER_USER, 401)
	pinAddSavedDialog(t, u, mtproto.PEER_USER, 402)
	pinAddSavedDialog(t, u, mtproto.PEER_CHAT, 12)
	reorder := func(force bool, order ...*mtproto.PeerUtil) {
		t.Helper()
		if err := itDao.ReorderPinnedSavedDialogs(ctx, u, force, order, nil, []*mtproto.Chat{pinChat(12)}); err != nil {
			t.Fatal(err)
		}
	}
	p := mtproto.MakePeerUtil

	reorder(true, p(mtproto.PEER_CHAT, 12), p(mtproto.PEER_USER, 401))
	reorder(true, p(mtproto.PEER_CHAT, 12), p(mtproto.PEER_USER, 401)) // the same: nothing
	reorder(false, p(mtproto.PEER_USER, 402))
	if got := pinStoredSaved(t, u); got != "[u402 c12 u401]" {
		t.Fatalf("pins %s, want [u402 c12 u401]", got)
	}
	reorder(true, p(mtproto.PEER_USER, 401), p(mtproto.PEER_USER, 499))
	if got := pinStoredSaved(t, u); got != "[u401]" {
		t.Errorf("pins %s, want [u401]", got)
	}
	pushes := pinCheck(t, u, 0, "saved order [c12 u401]", "saved order [u402 c12 u401]", "saved order [u401]")
	if pushes[0].chats != 1 {
		t.Errorf("chats of the first push %d", pushes[0].chats)
	}
}
