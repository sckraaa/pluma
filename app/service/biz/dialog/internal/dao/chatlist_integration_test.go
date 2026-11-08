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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// Shared folders (task 064) on MySQL: the rule of what a link may share and
// the channel invite link it makes, the limits, joining, what is new in a
// link and what stops a link.

var itChannelId atomic.Int64

// newShareChannel makes a channel owner created, with a name if public.
func newShareChannel(t *testing.T, owner int64, public bool) int64 {
	t.Helper()
	id := itChannelId.Add(1) + time.Now().UnixNano()%1_000_000_000*1000
	ctx := context.Background()
	if _, err := itDao.Mysql.DB.Exec(ctx,
		"INSERT INTO channels (id, access_hash, creator_user_id, title, about, broadcast, megagroup, date, date2) VALUES (?, 1, ?, 'Ch', '', 1, 0, 0, 0)",
		id, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := itDao.Mysql.DB.Exec(ctx,
		"INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, 0)",
		id, owner, channelperm.RoleCreator); err != nil {
		t.Fatal(err)
	}
	if public {
		if _, err := itDao.Mysql.DB.Exec(ctx,
			"INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
			fmt.Sprintf("pub%d", id), mtproto.PEER_CHANNEL, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func addShareMember(t *testing.T, channelId, userId int64, role, rights int32) {
	t.Helper()
	if _, err := itDao.Mysql.DB.Exec(context.Background(),
		"INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 0)",
		channelId, userId, role, rights); err != nil {
		t.Fatal(err)
	}
}

func channelInput(ids ...int64) []*mtproto.InputPeer {
	out := make([]*mtproto.InputPeer, 0, len(ids))
	for _, id := range ids {
		out = append(out, mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: id, AccessHash: 1}).To_InputPeer())
	}
	return out
}

// shareFolder saves a plain folder id of owner holding channels ids.
func shareFolder(t *testing.T, owner int64, id int32, ids ...int64) {
	t.Helper()
	f := mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id: id,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{
			Text: "Shared",
		}).To_TextWithEntities(),
		IncludePeers: channelInput(ids...),
	}).To_DialogFilter()
	if err := itDao.SaveDialogFilter(context.Background(), owner, id, f, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func slugOf(t *testing.T, invite *mtproto.ExportedChatlistInvite) string {
	t.Helper()
	url := invite.GetUrl()
	prefix := "https://" + env2.TDotMe + "/addlist/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("link %q", url)
	}
	return url[len(prefix):]
}

func exported(t *testing.T, owner int64, id int32, ids ...int64) string {
	t.Helper()
	r, err := itDao.ExportChatlistInvite(context.Background(), owner, id, ChatlistShare{Peers: channelInput(ids...)})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return slugOf(t, r.GetInvite())
}

func inviteRevoked(t *testing.T, channelId int64) bool {
	t.Helper()
	var revoked []bool
	if err := itDao.Mysql.DB.QueryRowsPartial(context.Background(), &revoked,
		"SELECT revoked FROM chat_invites WHERE chat_id = ?", channelId); err != nil || len(revoked) == 0 {
		t.Fatalf("invite of channel %d: %v %v", channelId, revoked, err)
	}
	return revoked[0]
}

func TestChatlistExportRules(t *testing.T) {
	ctx := context.Background()
	owner := newUser()
	mine := newShareChannel(t, owner, false)
	public := newShareChannel(t, newUser(), true)
	shareFolder(t, owner, 2, mine, public)

	// A folder of rules or exclusions cannot have links.
	save(t, owner, 3, "Contacts")
	if _, err := itDao.ExportChatlistInvite(ctx, owner, 3, ChatlistShare{Peers: channelInput(mine)}); !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("a folder of rules: %v", err)
	}
	if _, err := itDao.ExportChatlistInvite(ctx, owner, 9, ChatlistShare{Peers: channelInput(mine)}); !errors.Is(err, mtproto.ErrFilterIdInvalid) {
		t.Fatalf("no folder: %v", err)
	}

	// A channel outside the folder cannot be shared.
	other := newShareChannel(t, owner, false)
	if _, err := itDao.ExportChatlistInvite(ctx, owner, 2, ChatlistShare{Peers: channelInput(other)}); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Fatalf("a channel out of the folder: %v", err)
	}

	r, err := itDao.ExportChatlistInvite(ctx, owner, 2, ChatlistShare{Title: "Friends", Peers: channelInput(mine, public)})
	if err != nil {
		t.Fatal(err)
	}
	if f := r.GetFilter(); f.GetPredicateName() != mtproto.Predicate_dialogFilterChatlist || !f.GetHasMyInvites() {
		t.Fatalf("the folder after the link: %v", f)
	}
	if r.GetInvite().GetTitle() != "Friends" || len(r.GetInvite().GetPeers()) != 2 {
		t.Fatalf("the link: %v", r.GetInvite())
	}
	// A private channel gets an invite link of its own, a public one none.
	var private, open []string
	if err = itDao.Mysql.DB.QueryRowsPartial(ctx, &private, "SELECT link FROM chat_invites WHERE chat_id = ? AND admin_id = ?", mine, owner); err != nil || len(private) != 1 {
		t.Fatalf("invite of the private channel: %v %v", private, err)
	}
	if err = itDao.Mysql.DB.QueryRowsPartial(ctx, &open, "SELECT link FROM chat_invites WHERE chat_id = ?", public); err != nil || len(open) != 0 {
		t.Fatalf("invite of the public channel: %v %v", open, err)
	}

	// A member cannot share a private channel; an admin with the right can.
	stranger := newUser()
	shareFolder(t, stranger, 2, mine)
	if _, err = itDao.ExportChatlistInvite(ctx, stranger, 2, ChatlistShare{Peers: channelInput(mine)}); !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Fatalf("one not in the channel: %v", err)
	}
	addShareMember(t, mine, stranger, channelperm.RoleMember, 0)
	if _, err = itDao.ExportChatlistInvite(ctx, stranger, 2, ChatlistShare{Peers: channelInput(mine)}); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Fatalf("a member: %v", err)
	}
	admin := newUser()
	addShareMember(t, mine, admin, channelperm.RoleAdmin, mtproto.ADMIN_INVITE_USERS)
	shareFolder(t, admin, 2, mine)
	if _, err = itDao.ExportChatlistInvite(ctx, admin, 2, ChatlistShare{Peers: channelInput(mine)}); err != nil {
		t.Fatalf("an admin with the right: %v", err)
	}
}

func TestChatlistLimits(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	ch := newShareChannel(t, u, false)
	shareFolder(t, u, 2, ch)
	for i := range ChatlistInvitesLimit {
		if _, err := itDao.ExportChatlistInvite(ctx, u, 2, ChatlistShare{Peers: channelInput(ch), Title: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := itDao.ExportChatlistInvite(ctx, u, 2, ChatlistShare{Peers: channelInput(ch)}); !errors.Is(err, mtproto.ErrInvitesTooMuch) {
		t.Fatalf("over the links of a folder: %v", err)
	}

	// A second shared folder is the last without Premium; a third is refused,
	// made by a link or by an import.
	for id := int32(3); id < 3+ChatlistsJoinedLimit-1; id++ {
		shareFolder(t, u, id, ch)
		exported(t, u, id, ch)
	}
	over := int32(3 + ChatlistsJoinedLimit - 1)
	shareFolder(t, u, over, ch)
	if _, err := itDao.ExportChatlistInvite(ctx, u, over, ChatlistShare{Peers: channelInput(ch)}); !errors.Is(err, ErrChatlistsTooMuch) {
		t.Fatalf("over the shared folders: %v", err)
	}
	other := newUser()
	otherCh := newShareChannel(t, other, true)
	shareFolder(t, other, 2, otherCh)
	slug := exported(t, other, 2, otherCh)
	if _, err := itDao.JoinChatlistFolder(ctx, u, slug, 0, channelInput(otherCh), nil, false); !errors.Is(err, ErrChatlistsTooMuch) {
		t.Fatalf("an import over the shared folders: %v", err)
	}
	if _, err := itDao.JoinChatlistFolder(ctx, u, slug, 0, nil, nil, true); !errors.Is(err, ErrChatlistsTooMuch) {
		t.Fatalf("the check before the join: %v", err)
	}
}

// Two devices making links at once do not pass the limit of links.
func TestChatlistLinksConcurrent(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	ch := newShareChannel(t, u, false)
	shareFolder(t, u, 2, ch)
	var wg sync.WaitGroup
	var ok, tooMuch atomic.Int32
	for range ChatlistInvitesLimit + 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch _, err := itDao.ExportChatlistInvite(ctx, u, 2, ChatlistShare{Peers: channelInput(ch)}); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, mtproto.ErrInvitesTooMuch):
				tooMuch.Add(1)
			default:
				t.Errorf("export: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != ChatlistInvitesLimit || tooMuch.Load() != 5 {
		t.Fatalf("%d links made and %d refused, want %d and 5", ok.Load(), tooMuch.Load(), ChatlistInvitesLimit)
	}
}

func folderIds(f *mtproto.DialogFilter) []int64 {
	var out []int64
	for _, p := range f.GetIncludePeers() {
		out = append(out, p.GetChannelId())
	}
	return out
}

func TestChatlistJoinAndUpdates(t *testing.T) {
	ctx := context.Background()
	owner := newUser()
	a := newShareChannel(t, owner, false)
	b := newShareChannel(t, owner, true)
	c := newShareChannel(t, owner, false)
	shareFolder(t, owner, 2, a, b, c)
	slug := exported(t, owner, 2, a, b)

	ext, err := itDao.GetChatlistInvite(ctx, newUser(), slug, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ext.GetMyFilterId() != 0 || ext.GetDialogFilter().GetTitle_TEXTWITHENTITIES().GetText() != "Shared" || len(ext.GetLinks()) != 2 || ext.GetLinks()[1] != "" || ext.GetLinks()[0] == "" {
		t.Fatalf("the link: %v", ext)
	}
	if _, err = itDao.GetChatlistInvite(ctx, newUser(), "nosuchslug", 0); !errors.Is(err, mtproto.ErrInviteSlugExpired) {
		t.Fatalf("no link: %v", err)
	}

	u := newUser()
	check, err := itDao.JoinChatlistFolder(ctx, u, slug, 0, nil, nil, true)
	if err != nil || check != nil {
		t.Fatalf("check: %v %v", check, err)
	}
	// Only a is chosen: b is in the link and seen.
	join, err := itDao.JoinChatlistFolder(ctx, u, slug, 0, channelInput(a), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if join.GetDialogFilter().GetPredicateName() != mtproto.Predicate_dialogFilterChatlist || join.GetId() < chatlistFolderIdMin || !slices.Equal(folderIds(join.GetDialogFilter()), []int64{a}) {
		t.Fatalf("the folder: %v", join)
	}
	// Joining again is the same folder.
	again, err := itDao.JoinChatlistFolder(ctx, u, slug, 0, channelInput(a), nil, false)
	if err != nil || again.GetId() != join.GetId() {
		t.Fatalf("again: %v %v", again, err)
	}

	mine, err := itDao.GetChatlistInvite(ctx, u, slug, 0)
	if err != nil || mine.GetMyFilterId() != join.GetId() {
		t.Fatalf("already: %v %v", mine, err)
	}

	// Nothing new yet: b was seen on the join.
	updates, err := itDao.GetChatlistInvite(ctx, u, "", join.GetId())
	if err != nil || len(updates.GetPeers()) != 0 {
		t.Fatalf("updates at first: %v %v", updates.GetPeers(), err)
	}

	// The owner adds c to the link: it is new.
	if _, err = itDao.EditChatlistInvite(ctx, owner, 2, slug, false, "", true, ChatlistShare{Peers: channelInput(a, b, c)}); err != nil {
		t.Fatal(err)
	}
	updates, err = itDao.GetChatlistInvite(ctx, u, "", join.GetId())
	if err != nil || len(updates.GetPeers()) != 1 || updates.GetPeers()[0].GetChannelId() != c {
		t.Fatalf("updates after the edit: %v %v", updates.GetPeers(), err)
	}
	// Joining it adds it to the folder, and it is new no more.
	join2, err := itDao.JoinChatlistFolder(ctx, u, "", join.GetId(), channelInput(c), nil, false)
	if err != nil || !slices.Equal(folderIds(join2.GetDialogFilter()), []int64{a, c}) {
		t.Fatalf("join of the new channel: %v %v", join2, err)
	}
	updates, err = itDao.GetChatlistInvite(ctx, u, "", join.GetId())
	if err != nil || len(updates.GetPeers()) != 0 {
		t.Fatalf("updates after joining: %v %v", updates.GetPeers(), err)
	}

	// Another channel added, dismissed: not suggested again.
	d := newShareChannel(t, owner, false)
	do, err := itDao.DialogFiltersDAO.Select(ctx, owner, 2)
	if err != nil {
		t.Fatal(err)
	}
	ownerFolder, err := mtproto.UnmarshalDialogFilter(do.DialogFilter)
	if err != nil {
		t.Fatal(err)
	}
	ownerFolder.IncludePeers = append(ownerFolder.IncludePeers, channelInput(d)...)
	if err = itDao.SaveDialogFilter(ctx, owner, 2, ownerFolder, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = itDao.EditChatlistInvite(ctx, owner, 2, slug, false, "", true, ChatlistShare{Peers: channelInput(a, b, c, d)}); err != nil {
		t.Fatal(err)
	}
	if updates, err = itDao.GetChatlistInvite(ctx, u, "", join.GetId()); err != nil || len(updates.GetPeers()) != 1 || updates.GetPeers()[0].GetChannelId() != d {
		t.Fatalf("updates with d: %v %v", updates.GetPeers(), err)
	}
	if err = itDao.HideChatlistUpdates(ctx, u, join.GetId()); err != nil {
		t.Fatal(err)
	}
	updates, err = itDao.GetChatlistInvite(ctx, u, "", join.GetId())
	if err != nil || len(updates.GetPeers()) != 0 {
		t.Fatalf("updates after dismissing: %v %v", updates.GetPeers(), err)
	}

	// The folder's owner sees no updates; a folder not shared has none.
	if updates, err = itDao.GetChatlistInvite(ctx, owner, "", 2); err != nil || len(updates.GetPeers()) != 0 {
		t.Fatalf("the owner's updates: %v %v", updates.GetPeers(), err)
	}
	save(t, u, 7, "Plain")
	if _, err = itDao.GetChatlistInvite(ctx, u, "", 7); !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("a plain folder: %v", err)
	}
	if _, err = itDao.GetChatlistInvite(ctx, u, "", 200); !errors.Is(err, mtproto.ErrFilterIdInvalid) {
		t.Fatalf("no folder: %v", err)
	}
}

func TestChatlistEditKeepsChannelLinks(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	a := newShareChannel(t, u, false)
	b := newShareChannel(t, u, false)
	shareFolder(t, u, 2, a, b)
	slug := exported(t, u, 2, a)
	r, err := itDao.EditChatlistInvite(ctx, u, 2, slug, true, "New title", true, ChatlistShare{Peers: channelInput(b)})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetTitle() != "New title" || len(r.GetPeers()) != 1 || r.GetPeers()[0].GetChannelId() != b {
		t.Fatalf("the link: %v", r)
	}
	if !inviteRevoked(t, a) {
		t.Fatal("the removed channel's invite is not revoked")
	}
	if _, err = itDao.EditChatlistInvite(ctx, u, 2, "nosuchslug", false, "", false, ChatlistShare{}); !errors.Is(err, mtproto.ErrInviteSlugExpired) {
		t.Fatalf("no link: %v", err)
	}
}

// A last link deleted makes the folder plain again; deleting the folder
// stops its links and frees the shared-folder slot.
func TestChatlistDelete(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	a := newShareChannel(t, u, false)
	shareFolder(t, u, 2, a)
	slug := exported(t, u, 2, a)
	if err := itDao.DeleteChatlistInvite(ctx, u, 2, slug); err != nil {
		t.Fatal(err)
	}
	if !inviteRevoked(t, a) {
		t.Fatal("the invite of the channel is not revoked")
	}
	f, err := itDao.DialogFiltersDAO.Select(ctx, u, 2)
	if err != nil || f.IsChatlist {
		t.Fatalf("the folder after its last link: %+v %v", f, err)
	}
	if _, err = itDao.GetChatlistInvite(ctx, newUser(), slug, 0); !errors.Is(err, mtproto.ErrInviteSlugExpired) {
		t.Fatalf("the deleted link: %v", err)
	}
	if err = itDao.DeleteChatlistInvite(ctx, u, 2, slug); !errors.Is(err, mtproto.ErrInviteSlugExpired) && !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("deleted again: %v", err)
	}

	second := newShareChannel(t, u, false)
	shareFolder(t, u, 2, a, second)
	slug = exported(t, u, 2, a)
	joined := newUser()
	if _, err = itDao.JoinChatlistFolder(ctx, joined, slug, 0, channelInput(a), nil, false); err != nil {
		t.Fatal(err)
	}
	if err = itDao.DeleteDialogFilter(ctx, u, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = itDao.GetChatlistInvite(ctx, newUser(), slug, 0); !errors.Is(err, mtproto.ErrInviteSlugExpired) {
		t.Fatalf("the link of a deleted folder: %v", err)
	}
}

// A shared folder stays shared; a plain folder cannot be made shared by
// messages.updateDialogFilter.
func TestSaveChatlistFilter(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	a := newShareChannel(t, u, false)
	shareFolder(t, u, 2, a)
	exported(t, u, 2, a)
	do, err := itDao.DialogFiltersDAO.Select(ctx, u, 2)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := mtproto.UnmarshalDialogFilter(do.DialogFilter)
	if err != nil {
		t.Fatal(err)
	}
	renamed := toChatlist(stored, false)
	renamed.Title_TEXTWITHENTITIES = mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "Other"}).To_TextWithEntities()
	renamed.HasMyInvites = false
	if err = itDao.SaveDialogFilter(ctx, u, 2, renamed, nil, nil); err != nil {
		t.Fatal(err)
	}
	do, _ = itDao.DialogFiltersDAO.Select(ctx, u, 2)
	after, err := mtproto.UnmarshalDialogFilter(do.DialogFilter)
	if err != nil || after.GetTitle_TEXTWITHENTITIES().GetText() != "Other" || !after.GetHasMyInvites() || !do.IsChatlist {
		t.Fatalf("the shared folder after a rename: %v %v", after, err)
	}
	// Android saves a shared folder as dialogFilter: it stays shared.
	plain := mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id:                     2,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: "Plain"}).To_TextWithEntities(),
		IncludePeers:           channelInput(a),
	}).To_DialogFilter()
	if err = itDao.SaveDialogFilter(ctx, u, 2, plain, nil, nil); err != nil {
		t.Fatalf("a shared folder in the plain form: %v", err)
	}
	do, _ = itDao.DialogFiltersDAO.Select(ctx, u, 2)
	after, err = mtproto.UnmarshalDialogFilter(do.DialogFilter)
	if err != nil || after.GetPredicateName() != mtproto.Predicate_dialogFilterChatlist || after.GetTitle_TEXTWITHENTITIES().GetText() != "Plain" || !after.GetHasMyInvites() || !do.IsChatlist {
		t.Fatalf("the shared folder after a save in the plain form: %v %v", after, err)
	}
	// Rules cannot be written over a shared folder, nor the shared form over
	// a plain one.
	if err = itDao.SaveDialogFilter(ctx, u, 2, folder(2, "Rules"), nil, nil); !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("rules over a shared folder: %v", err)
	}
	save(t, u, 3, "Plain")
	shared := toChatlist(folder(3, "Plain"), false)
	if err = itDao.SaveDialogFilter(ctx, u, 3, shared, nil, nil); !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("a shared folder over a plain one: %v", err)
	}
	if err = itDao.SaveDialogFilter(ctx, u, 4, shared, nil, nil); !errors.Is(err, mtproto.ErrFilterNotSupported) {
		t.Fatalf("a new shared folder: %v", err)
	}
}
