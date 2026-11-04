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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) title(self, channelId int64, title string) (*mtproto.Updates, error) {
	return f.c.ChannelEditTitle(&channel.TLChannelEditTitle{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Title: title})
}

func (f *diffFixture) about(self, channelId int64, about string) error {
	_, err := f.c.ChannelEditAbout(&channel.TLChannelEditAbout{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, About: about})
	return err
}

// TestEditInfo: the creator renames a supergroup with a service message in
// the journal; a member renames it only while the default rights let
// members change the info; a channel's subscriber never does; the same
// title or description again is not modified.
func TestEditInfo(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	r, err := f.title(creator, id, "new")
	if err != nil {
		t.Fatal(err)
	}
	m := r.GetUpdates()[0].GetMessage_MESSAGE()
	if m.GetAction().GetTitle_STRING() != "new" || m.GetFromId().GetUserId() != creator || r.GetChats()[0].GetTitle() != "new" {
		t.Errorf("rename: %v, chats %v", m, r.GetChats())
	}
	d := f.diff(bob, id, r.GetUpdates()[0].GetPts_INT32()-1, 10)
	if len(d.GetNewMessages()) != 1 || d.GetNewMessages()[0].GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatEditTitle {
		t.Errorf("the journal: %v", d.GetNewMessages())
	}
	_, err = f.title(creator, id, "new")
	wantErr(t, "the same title", err, mtproto.ErrChatNotModified)
	if _, err = f.title(bob, id, "bob's"); err != nil {
		t.Errorf("a member where members may change the info: %v", err)
	}
	f.exec("UPDATE channels SET default_banned_rights = ? WHERE id = ?", mtproto.BANNED_CHANGE_INFO, id)
	_, err = f.title(bob, id, "again")
	wantErr(t, "a member where members may not", err, mtproto.ErrChatAdminRequired)
	wantErr(t, "a member's description", f.about(bob, id, "x"), mtproto.ErrChatAdminRequired)

	if err = f.about(creator, id, "about it"); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "the same description", f.about(creator, id, "about it"), mtproto.ErrChatAboutNotModified)
	var about string
	if err = f.box.QueryRow("SELECT about FROM channels WHERE id = ?", id).Scan(&about); err != nil || about != "about it" {
		t.Errorf("about %q, %v", about, err)
	}

	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0, default_banned_rights = 0 WHERE id = ?", id)
	_, err = f.title(bob, id, "sub")
	wantErr(t, "a channel's subscriber", err, mtproto.ErrChatAdminRequired)
	if r, err = f.title(creator, id, "post"); err != nil {
		t.Fatal(err)
	}
	if m = r.GetUpdates()[0].GetMessage_MESSAGE(); !m.GetPost() || m.GetFromId() != nil {
		t.Errorf("a channel's rename is a post without an author: %v", m)
	}
}
