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
)

func notify(peer int64, muteUntil int32) *mtproto.Update {
	return mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
		Peer_NOTIFYPEER: mtproto.MakeTLNotifyPeer(&mtproto.NotifyPeer{Peer: mtproto.MakePeerUser(peer)}).To_NotifyPeer(),
		NotifySettings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
			MuteUntil: mtproto.MakeFlagsInt32(muteUntil),
		}).To_PeerNotifySettings(),
	}).To_Update()
}

func dialogPeer(peer int64) *mtproto.DialogPeer {
	return mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{Peer: mtproto.MakePeerUser(peer)}).To_DialogPeer()
}

func pinDialog(peer int64, pin bool) *mtproto.Update {
	return mtproto.MakeTLUpdateDialogPinned(&mtproto.Update{Pinned: pin, Peer_DIALOGPEER: dialogPeer(peer)}).To_Update()
}

func pinnedDialogs(order ...int64) *mtproto.Update {
	var peers []*mtproto.DialogPeer
	for _, p := range order {
		peers = append(peers, dialogPeer(p))
	}
	return mtproto.MakeTLUpdatePinnedDialogs(&mtproto.Update{Order_FLAGVECTORDIALOGPEER: peers}).To_Update()
}

func filter(id int32, title string) *mtproto.Update {
	return mtproto.MakeTLUpdateDialogFilter(&mtproto.Update{
		Id_INT32: id,
		Filter:   mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{Id: id, Title_STRING: title}).To_DialogFilter(),
	}).To_Update()
}

func filterOrder(ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDialogFilterOrder(&mtproto.Update{Order_VECTORINT32: ids}).To_Update()
}

// describeSeq writes other_updates of updates with seq.
func describeSeq(b *differenceBuilder) string {
	var us []string
	for _, u := range b.otherUpdates() {
		s := strings.TrimPrefix(u.GetPredicateName(), "update")
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNotifySettings:
			s += fmt.Sprintf("(%d,%d)", u.GetPeer_NOTIFYPEER().GetPeer().GetUserId(), u.GetNotifySettings().GetMuteUntil().GetValue())
		case mtproto.Predicate_updateDialogPinned:
			s += fmt.Sprintf("(%d,%v)", u.GetPeer_DIALOGPEER().GetPeer().GetUserId(), u.GetPinned())
		case mtproto.Predicate_updatePinnedDialogs:
			var ids []int64
			for _, p := range u.GetOrder_FLAGVECTORDIALOGPEER() {
				ids = append(ids, p.GetPeer().GetUserId())
			}
			s += fmt.Sprint(ids)
		case mtproto.Predicate_updateDialogFilter:
			s += fmt.Sprintf("(%d:%s)", u.GetId_INT32(), u.GetFilter().GetTitle_STRING())
		case mtproto.Predicate_updateDialogFilterOrder:
			s += fmt.Sprint(u.GetOrder_VECTORINT32())
		case mtproto.Predicate_updateDeleteMessages:
			s += fmt.Sprint(u.GetMessages())
		}
		us = append(us, s)
	}
	return strings.Join(us, " ")
}

func foldSeq(updates ...*mtproto.Update) *differenceBuilder {
	b := newDifferenceBuilder(10)
	for _, u := range updates {
		b.addSeq(u)
	}
	return b
}

func TestSeqFold(t *testing.T) {
	for _, c := range []struct {
		name    string
		updates []*mtproto.Update
		want    string
	}{
		{"settings of a peer: the last, where it was",
			[]*mtproto.Update{notify(1, 10), notify(2, 20), notify(1, 0)},
			"NotifySettings(2,20) NotifySettings(1,0)"},
		{"pins of one dialog are not folded",
			[]*mtproto.Update{pinDialog(4, true), pinDialog(3, false), pinDialog(3, true)},
			"DialogPinned(4,true) DialogPinned(3,false) DialogPinned(3,true)"},
		{"the order of the folder drops the pins before it",
			[]*mtproto.Update{pinDialog(4, true), pinnedDialogs(2, 1), pinDialog(1, false), pinDialog(1, true), pinnedDialogs(1, 2)},
			"PinnedDialogs[1 2]"},
		{"pins after the order stay",
			[]*mtproto.Update{pinnedDialogs(2, 1), pinDialog(1, false), pinDialog(1, true)},
			"PinnedDialogs[2 1] DialogPinned(1,false) DialogPinned(1,true)"},
		{"a filter keeps the place of its first update",
			[]*mtproto.Update{filter(5, "work"), filterOrder(5, 1), filter(6, "home"), filter(5, "job"), filterOrder(6, 5, 1)},
			"DialogFilter(5:job) DialogFilter(6:home) DialogFilterOrder[6 5 1]"},
		{"reload of the filters drops the filter updates before it",
			[]*mtproto.Update{filter(5, "work"), filterOrder(5), mtproto.MakeTLUpdateDialogFilters(nil).To_Update(), filter(6, "home")},
			"DialogFilters DialogFilter(6:home)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := describeSeq(foldSeq(c.updates...)); got != c.want {
				t.Errorf("updates %q, want %q", got, c.want)
			}
		})
	}
}

// pinModel is the pinned dialogs of a client, top first. Android ignores a
// pin of a dialog already pinned; Telegram Desktop moves it to the top.
type pinModel struct {
	moveToTop bool
	pinned    []int64
}

func (m *pinModel) apply(u *mtproto.Update) {
	switch u.GetPredicateName() {
	case mtproto.Predicate_updateDialogPinned:
		p := u.GetPeer_DIALOGPEER().GetPeer().GetUserId()
		i := slices.Index(m.pinned, p)
		switch {
		case !u.GetPinned():
			if i >= 0 {
				m.pinned = slices.Delete(m.pinned, i, i+1)
			}
		case i >= 0 && !m.moveToTop:
		default:
			if i >= 0 {
				m.pinned = slices.Delete(m.pinned, i, i+1)
			}
			m.pinned = append([]int64{p}, m.pinned...)
		}
	case mtproto.Predicate_updatePinnedDialogs:
		m.pinned = m.pinned[:0]
		for _, p := range u.GetOrder_FLAGVECTORDIALOGPEER() {
			m.pinned = append(m.pinned, p.GetPeer().GetUserId())
		}
	}
}

// The folded pins give each client what the whole journal gives it, from
// the state the client had before the range.
func TestSeqFoldPinsAsClients(t *testing.T) {
	journals := [][]*mtproto.Update{
		{pinDialog(4, true), pinDialog(3, false), pinDialog(3, true)},
		{pinnedDialogs(2, 1), pinDialog(1, false), pinDialog(1, true)},
		{pinDialog(1, true), pinnedDialogs(1, 2), pinDialog(3, true), pinDialog(2, false)},
	}
	for i, journal := range journals {
		for _, moveToTop := range []bool{false, true} {
			full := &pinModel{moveToTop: moveToTop, pinned: []int64{3, 2}}
			for _, u := range journal {
				full.apply(u)
			}
			folded := &pinModel{moveToTop: moveToTop, pinned: []int64{3, 2}}
			for _, u := range foldSeq(journal...).otherUpdates() {
				folded.apply(u)
			}
			if !slices.Equal(full.pinned, folded.pinned) {
				t.Errorf("journal %d, move to top %v: folded %v, whole %v", i, moveToTop, folded.pinned, full.pinned)
			}
		}
	}
}

// Updates with seq stay where they happened among the journal updates.
func TestSeqAmongPtsUpdates(t *testing.T) {
	b := newDifferenceBuilder(4)
	b.add(deleteMessages(2, 1))
	b.addSeq(notify(1, 10))
	b.add(deleteMessages(3, 2))
	b.addSeq(notify(2, 20))
	if got, want := describeSeq(b), "NotifySettings(1,10) DeleteMessages[1 2] NotifySettings(2,20)"; got != want {
		t.Errorf("updates %q, want %q", got, want)
	}
	for _, u := range b.otherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNotifySettings && u.GetPts_INT32() != 0 {
			t.Errorf("an update with seq got pts %d", u.GetPts_INT32())
		}
	}
}
