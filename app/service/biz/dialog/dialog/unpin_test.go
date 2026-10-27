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

package dialog

import (
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestUnpinOfDeleted(t *testing.T) {
	describe := func(u *mtproto.Update) string {
		if u == nil {
			return "nothing"
		}
		p := u.GetPeer_DIALOGPEER().GetPeer()
		return fmt.Sprintf("%s pinned %v folder %v peer %s/%d/%d", u.GetPredicateName(), u.GetPinned(), u.GetFolderId().GetValue(),
			p.GetPredicateName(), p.GetUserId(), p.GetChatId())
	}
	for _, c := range []struct {
		name string
		row  PinOfDeleted
		peer [2]int64 // peer type, peer id
		want string
	}{
		{"not pinned", PinOfDeleted{}, [2]int64{mtproto.PEER_USER, 7}, "nothing"},
		{"pinned in the main list", PinOfDeleted{Pinned: 5 << 32}, [2]int64{mtproto.PEER_USER, 7},
			"updateDialogPinned pinned false folder 0 peer peerUser/7/0"},
		{"pinned in the archive", PinOfDeleted{FolderId: 1, FolderPinned: 3}, [2]int64{mtproto.PEER_CHAT, 9},
			"updateDialogPinned pinned false folder 1 peer peerChat/0/9"},
		// The pin of the other folder's column is not shown.
		{"in the archive, the main list's pin kept", PinOfDeleted{FolderId: 1, Pinned: 5}, [2]int64{mtproto.PEER_USER, 7}, "nothing"},
		{"in the main list, an archive pin kept", PinOfDeleted{FolderPinned: 5}, [2]int64{mtproto.PEER_USER, 7}, "nothing"},
		{"a row marked deleted", PinOfDeleted{Pinned: 5, Deleted: true}, [2]int64{mtproto.PEER_USER, 7}, "nothing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := describe(c.row.Unpin(int32(c.peer[0]), c.peer[1])); got != c.want {
				t.Errorf("%s, want %s", got, c.want)
			}
		})
	}
}
