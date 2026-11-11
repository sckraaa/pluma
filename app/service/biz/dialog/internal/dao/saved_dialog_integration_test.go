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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The sub-chats of Saved Messages by their peers (messages.getSavedDialogsByID,
// task 079): in the order asked, once each, a deleted one and a peer with
// none left out.
func TestSavedDialogsByIdList(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	pinAddSavedDialog(t, u, mtproto.PEER_CHANNEL, 501)
	pinAddSavedDialog(t, u, mtproto.PEER_USER, u)
	pinAddSavedDialog(t, u, mtproto.PEER_USER, 302)
	if _, err := itDao.BoxDB.Exec("UPDATE saved_dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?", u, mtproto.PEER_USER, 302); err != nil {
		t.Fatal(err)
	}

	dialogs, err := itDao.SavedDialogsByIdList(ctx, u, []*mtproto.PeerUtil{
		mtproto.MakePeerUtil(mtproto.PEER_USER, u),
		mtproto.MakePeerUtil(mtproto.PEER_USER, 302), // deleted
		mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, 501),
		mtproto.MakePeerUtil(mtproto.PEER_USER, u),  // asked again
		mtproto.MakePeerUtil(mtproto.PEER_CHAT, 77), // never there
		mtproto.MakePeerUtil(mtproto.PEER_EMPTY, 0), // no peer
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []mtproto.PeerUtil
	for _, d := range dialogs {
		p := mtproto.FromPeer(d.GetPeer())
		got = append(got, mtproto.PeerUtil{PeerType: p.PeerType, PeerId: p.PeerId})
	}
	if len(got) != 2 ||
		got[0].PeerType != mtproto.PEER_USER || got[0].PeerId != u ||
		got[1].PeerType != mtproto.PEER_CHANNEL || got[1].PeerId != 501 {
		t.Errorf("dialogs %v", dialogs)
	}
	for _, d := range dialogs {
		if d.GetTopMessage() != 1 || d.GetPinned() {
			t.Errorf("dialog %v", d)
		}
	}
}
