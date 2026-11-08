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

package dao

import (
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestShareable(t *testing.T) {
	plain := mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{Id: 2}).To_DialogFilter()
	if !shareable(plain) {
		t.Fatal("a folder of named chats is not shareable")
	}
	for _, f := range []*mtproto.DialogFilter{
		mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{Id: 2, Contacts: true}).To_DialogFilter(),
		mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{Id: 2, ExcludeMuted: true}).To_DialogFilter(),
		mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{Id: 2, ExcludePeers: []*mtproto.InputPeer{mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()}}).To_DialogFilter(),
	} {
		if shareable(f) {
			t.Fatalf("%v is shareable", f)
		}
	}
	if !shareable(mtproto.MakeTLDialogFilterChatlist(&mtproto.DialogFilter{Id: 2}).To_DialogFilter()) {
		t.Fatal("a shared folder is not shareable")
	}
}

func TestToChatlistKeepsTheFolder(t *testing.T) {
	f := mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id:                     3,
		TitleNoanimate:         true,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "A"}).To_TextWithEntities(),
		IncludePeers:           []*mtproto.InputPeer{mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 9}).To_InputPeer()},
	}).To_DialogFilter()
	got := toChatlist(f, true)
	if got.GetPredicateName() != mtproto.Predicate_dialogFilterChatlist || !got.GetHasMyInvites() || !got.GetTitleNoanimate() ||
		got.GetId() != 3 || len(got.GetIncludePeers()) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestFreeFolderId(t *testing.T) {
	id, err := freeFolderId([]filterOrder{{Id: 0}, {Id: 2}, {Id: 3}})
	if err != nil || id != 4 {
		t.Fatalf("got %d, %v", id, err)
	}
	var full []filterOrder
	for i := int32(chatlistFolderIdMin); i <= chatlistFolderIdMax; i++ {
		full = append(full, filterOrder{Id: i})
	}
	if _, err = freeFolderId(full); err != ErrDialogFiltersTooMuch {
		t.Fatalf("no free id: %v", err)
	}
}

func TestCutChatlistTitle(t *testing.T) {
	long := strings.Repeat("е", chatlistTitleMax+5)
	if got := cutChatlistTitle(long); len([]rune(got)) != chatlistTitleMax {
		t.Fatalf("cut to %d", len([]rune(got)))
	}
	if cutChatlistTitle("Short") != "Short" {
		t.Fatal("a short title changed")
	}
}

func TestSortedChannelsAscendingUnique(t *testing.T) {
	if got := sortedChannels([]int64{5, 2, 5, 9}); !slices.Equal(got, []int64{2, 5, 9}) {
		t.Fatalf("got %v", got)
	}
}

func TestInputMigratedReplacesTheGroup(t *testing.T) {
	channel := mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 77, AccessHash: 3}).To_InputPeer()
	peers := inputMigrated([]*mtproto.InputPeer{
		mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 5}).To_InputPeer(),
		mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: 6}).To_InputPeer(),
	}, []int64{5}, []int64{77}, map[int64]*mtproto.InputPeer{77: channel})
	if peers[0] != channel || peers[1].GetChatId() != 6 {
		t.Fatalf("got %v", peers)
	}
}

func TestFolderHasPeers(t *testing.T) {
	f := mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		IncludePeers: []*mtproto.InputPeer{mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 1}).To_InputPeer()},
		PinnedPeers:  []*mtproto.InputPeer{mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 2}).To_InputPeer()},
	}).To_DialogFilter()
	if !folderHasPeers(f, []int64{1, 2}) || folderHasPeers(f, []int64{1, 3}) {
		t.Fatal("folderHasPeers")
	}
}
