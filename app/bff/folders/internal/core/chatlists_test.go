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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

func TestChatlistSlugOf(t *testing.T) {
	for in, want := range map[string]string{
		"abc":                            "abc",
		"https://pluma.chat/addlist/abc": "abc",
		"pluma.chat/addlist/abc":         "abc",
		"https://pluma.chat/addlist/":    "",
		"":                               "",
	} {
		if got := chatlistSlugOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestChatlistFolderId(t *testing.T) {
	make := func(id int32) *mtproto.InputChatlist {
		return mtproto.MakeTLInputChatlistDialogFilter(&mtproto.InputChatlist{FilterId: id}).To_InputChatlist()
	}
	if id, err := chatlistFolderId(make(2)); err != nil || id != 2 {
		t.Fatalf("got %d, %v", id, err)
	}
	if _, err := chatlistFolderId(make(0)); !errors.Is(err, mtproto.ErrFilterIdInvalid) {
		t.Fatalf("id 0: %v", err)
	}
	if _, err := chatlistFolderId(nil); !errors.Is(err, mtproto.ErrInputChatlistInvalid) {
		t.Fatalf("nil: %v", err)
	}
}

func TestResolveShareRejects(t *testing.T) {
	c := testCore()
	never := func() error { t.Fatal("check called"); return nil }
	if _, err := c.resolveShare(nil, never); !errors.Is(err, mtproto.ErrPeersListEmpty) {
		t.Fatalf("no peers: %v", err)
	}
	user := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 9}).To_InputPeer()
	if _, err := c.resolveShare([]*mtproto.InputPeer{user}, never); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("a user: %v", err)
	}
	tooMany := make([]*mtproto.InputPeer, filterChatsMax+1)
	for i := range tooMany {
		tooMany[i] = user
	}
	if _, err := c.resolveShare(tooMany, never); !errors.Is(err, errFilterChatsTooMuch) {
		t.Fatalf("too many: %v", err)
	}
}

func TestChosenJoinPeers(t *testing.T) {
	ext := &dialog.ChatlistInviteExt{
		Peers: []*mtproto.Peer{mtproto.MakePeer(mtproto.PEER_CHANNEL, 1), mtproto.MakePeer(mtproto.PEER_CHANNEL, 2), mtproto.MakePeer(mtproto.PEER_CHANNEL, 3)},
		Links: []string{"a", "", "c"},
	}
	in := []*mtproto.InputPeer{
		mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 3}).To_InputPeer(),
		mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 3}).To_InputPeer(),
		mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 2}).To_InputPeer(),
		mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: 99}).To_InputPeer(),
		mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 7}).To_InputPeer(),
	}
	got := chosenJoinPeers(ext, in)
	if len(got) != 2 || got[0].id != 2 || got[0].link != "" || got[1].id != 3 || got[1].link != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestMergeChats(t *testing.T) {
	a := mtproto.MakeTLChannel(&mtproto.Chat{Id: 1}).To_Chat()
	b := mtproto.MakeTLChannel(&mtproto.Chat{Id: 2}).To_Chat()
	got := mergeChats([]*mtproto.Chat{a}, []*mtproto.Chat{b, a})
	if len(got) != 2 {
		t.Fatalf("got %d chats", len(got))
	}
}

func TestMigratedChannel(t *testing.T) {
	r := &mtproto.Updates{Chats: []*mtproto.Chat{
		mtproto.MakeTLChat(&mtproto.Chat{Id: 5}).To_Chat(),
		mtproto.MakeTLChannel(&mtproto.Chat{Id: 7}).To_Chat(),
	}}
	if migratedChannel(r) != 7 {
		t.Fatalf("got %d", migratedChannel(r))
	}
}
