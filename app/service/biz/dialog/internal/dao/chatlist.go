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
	"context"
	"errors"
	"slices"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/random2"
	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/env2"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Shared folders (task 064, core.telegram.org/api/folders): a folder link
// with its channels and the folders imported from one. Everything that
// changes a folder or a link is one box transaction of its owner, with the
// channels it shares locked after it in ascending id (rights and public
// status read under the lock), so that the link, the channel invite links
// it makes and the update with seq commit together (ADR 0020). A link makes
// one invite link per channel that cannot be joined by name, which the link
// keeps until it stops sharing the channel.

// The limits of shared folders (appConfig chatlist_invites_limit_* and
// chatlists_joined_limit_*, core.telegram.org/api/config); Premium is the
// user's, granted by the admin (ADR 0016).
const (
	ChatlistInvitesLimit        = 3
	ChatlistInvitesLimitPremium = 100
	ChatlistsJoinedLimit        = 2
	ChatlistsJoinedLimitPremium = 20
)

const (
	// chatlistSlugLen is the length of a link's slug: letters and digits, as
	// the clients take them (tdesktop local_url_handlers.cpp, addlist regex).
	chatlistSlugLen = 22
	// chatlistTitleMax is the longest title of a link, in characters
	// (tdesktop edit_filter_links.cpp, kMaxLinkTitleLength).
	chatlistTitleMax = 32
	// chatlistFolderIdMin and chatlistFolderIdMax bound the id of an
	// imported folder, which the server chooses (0 and 1 are All chats and
	// the archive, messages.updateDialogFilter).
	chatlistFolderIdMin = 2
	chatlistFolderIdMax = 255
)

// ErrChatlistsTooMuch: over the limit of shared folders of a user (the
// clients show their limit box on it: Android FilterCreateActivity.java:2784,
// tdesktop edit_filter_box.cpp:1074).
var ErrChatlistsTooMuch = status.Error(mtproto.ErrBadRequest, "CHATLISTS_TOO_MUCH")

// chatlistSlug is a link's slug.
func chatlistSlug() string { return random2.RandomAlphanumeric(chatlistSlugLen) }

// chatlistURL is the link of slug on the links' host (ADR 0036); the clients
// take the slug after addlist/ (iOS ChatFolders.swift:62-64, and its patch).
func chatlistURL(slug string) string { return "https://" + env2.TDotMe + "/addlist/" + slug }

// channelPeers returns the ids of peers, which are channels: PEER_ID_INVALID
// for another kind.
func channelPeers(peers []*mtproto.InputPeer) ([]int64, error) {
	ids := make([]int64, 0, len(peers))
	for _, p := range peers {
		if p.GetPredicateName() != mtproto.Predicate_inputPeerChannel {
			return nil, mtproto.ErrPeerIdInvalid
		}
		if !slices.Contains(ids, p.GetChannelId()) {
			ids = append(ids, p.GetChannelId())
		}
	}
	return ids, nil
}

// filterRow is a folder's row, locked by lockFilters.
type filterRow struct {
	IsChatlist   bool   `db:"is_chatlist"`
	JoinedBySlug bool   `db:"joined_by_slug"`
	Slug         string `db:"slug"`
	Data         string `db:"dialog_filter"`
}

// lockedFilter returns folder id of b's owner as it is stored, nil when
// there is none.
func lockedFilter(b *userbox.Box, id int32) (*filterRow, *mtproto.DialogFilter, error) {
	var rows []filterRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT is_chatlist, joined_by_slug, slug, dialog_filter FROM dialog_filters WHERE user_id = ? AND dialog_filter_id = ? AND deleted = 0",
		b.UserId(), id); err != nil || len(rows) == 0 {
		return nil, nil, err
	}
	f, err := mtproto.UnmarshalDialogFilter(rows[0].Data)
	if err != nil {
		return nil, nil, err
	}
	return &rows[0], f, nil
}

// writeFilter stores folder f of b's owner at place order, keeping the
// import of row, and pushes updateDialogFilter with seq (users and chats are
// its peers). It returns the folder as it reads back.
func (d *Dao) writeFilter(b *userbox.Box, f *mtproto.DialogFilter, row filterRow, order int64, users []*mtproto.User, chats []*mtproto.Chat) (*mtproto.DialogFilter, error) {
	data, err := jsonx.Marshal(f)
	if err != nil {
		return nil, err
	}
	saved, err := mtproto.UnmarshalDialogFilter(string(data))
	if err != nil {
		return nil, err
	}
	if _, _, err = d.DialogFiltersDAO.InsertOrUpdateTx(b.Tx(), &dataobject.DialogFiltersDO{
		UserId:         b.UserId(),
		DialogFilterId: f.GetId(),
		IsChatlist:     f.GetPredicateName() == mtproto.Predicate_dialogFilterChatlist,
		JoinedBySlug:   row.JoinedBySlug,
		Slug:           row.Slug,
		DialogFilter:   string(data),
		OrderValue:     order,
	}); err != nil {
		return nil, err
	}
	if err = d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogFilter(&mtproto.Update{Id_INT32: f.GetId(), Filter: saved}).To_Update(), users, chats); err != nil {
		return nil, err
	}
	return saved, nil
}

// folderPlace returns the place of folder id among rows.
func folderPlace(rows []filterOrder, id int32) (int64, bool) {
	for _, r := range rows {
		if r.Id == id {
			return r.Order, true
		}
	}
	return 0, false
}

// toChatlist is folder f as a shared one, with hasMyInvites.
func toChatlist(f *mtproto.DialogFilter, hasMyInvites bool) *mtproto.DialogFilter {
	return mtproto.MakeTLDialogFilterChatlist(&mtproto.DialogFilter{
		HasMyInvites:           hasMyInvites,
		TitleNoanimate:         f.GetTitleNoanimate(),
		Id:                     f.GetId(),
		Title_TEXTWITHENTITIES: f.GetTitle_TEXTWITHENTITIES(),
		Emoticon:               f.GetEmoticon(),
		Color:                  f.GetColor(),
		PinnedPeers:            f.GetPinnedPeers(),
		IncludePeers:           f.GetIncludePeers(),
	}).To_DialogFilter()
}

// shareable tells whether f may have shared links: its chats are named, one
// by one (dialogFilterChatlist has only those; tdesktop refuses the rest
// before it asks, GoodForExportFilterLink).
func shareable(f *mtproto.DialogFilter) bool {
	if f.GetPredicateName() == mtproto.Predicate_dialogFilterChatlist {
		return true
	}
	return !f.GetContacts() && !f.GetNonContacts() && !f.GetGroups() && !f.GetBroadcasts() && !f.GetBots() &&
		!f.GetExcludeMuted() && !f.GetExcludeRead() && !f.GetExcludeArchived() && len(f.GetExcludePeers()) == 0
}

// countChatlists is how many shared folders (made shared, or imported) b's
// owner has.
func countChatlists(b *userbox.Box) (int, error) {
	var n []int
	err := b.Tx().QueryRowsPartial(&n, "SELECT COUNT(*) FROM dialog_filters WHERE user_id = ? AND is_chatlist = 1 AND deleted = 0", b.UserId())
	if err != nil {
		return 0, err
	}
	return n[0], nil
}

// chatlistLimits returns the limits of b's owner: of shared folders and of
// links of a folder.
func chatlistLimits(b *userbox.Box) (joined, invites int, err error) {
	premium, err := boxPremium(b)
	if err != nil {
		return 0, 0, err
	}
	if premium {
		return ChatlistsJoinedLimitPremium, ChatlistInvitesLimitPremium, nil
	}
	return ChatlistsJoinedLimit, ChatlistInvitesLimit, nil
}

// inputMigrated replaces, in peers, basic groups moved to supergroups: the
// folder follows them.
func inputMigrated(peers []*mtproto.InputPeer, chats, channels []int64, input map[int64]*mtproto.InputPeer) []*mtproto.InputPeer {
	out := make([]*mtproto.InputPeer, 0, len(peers))
	for _, p := range peers {
		q := p
		if p.GetPredicateName() == mtproto.Predicate_inputPeerChat {
			if i := slices.Index(chats, p.GetChatId()); i >= 0 && i < len(channels) {
				if in, ok := input[channels[i]]; ok {
					q = in
				}
			}
		}
		out = append(out, q)
	}
	return out
}

// folderHasPeers tells whether f holds all of the channels ids.
func folderHasPeers(f *mtproto.DialogFilter, ids []int64) bool {
	in := make(map[int64]bool, len(f.GetIncludePeers())+len(f.GetPinnedPeers()))
	for _, list := range [][]*mtproto.InputPeer{f.GetIncludePeers(), f.GetPinnedPeers()} {
		for _, p := range list {
			if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel {
				in[p.GetChannelId()] = true
			}
		}
	}
	return !slices.ContainsFunc(ids, func(id int64) bool { return !in[id] })
}

type shareChannel struct {
	Broadcast   bool  `db:"broadcast"`
	Deleted     bool  `db:"deleted"`
	JoinRequest bool  `db:"join_request"`
	Default     int32 `db:"default_banned_rights"`
	Public      bool  `db:"public"`
}

type shareParticipant struct {
	Role         int32 `db:"role"`
	State        int32 `db:"state"`
	AdminRights  int32 `db:"admin_rights"`
	BannedRights int32 `db:"banned_rights"`
	UntilDate    int32 `db:"until_date"`
}

// sortedChannels is ids in the order channels lock in (channelbox).
func sortedChannels(ids []int64) []int64 {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

// shareChannelLink checks under the lock that b's owner may share channel
// id (a public channel that takes anyone, or a user that manages its
// invite links: iOS canShareLinkToPeer, Android
// FilterCreateActivity.canAddToFolder, tdesktop ErrorForSharing) and makes
// its invite link: a channel without a name, or one that asks to approve
// the joining, is joined by a link; "" otherwise.
func shareChannelLink(b *userbox.Box, id int64, title string) (string, error) {
	if _, err := channelbox.Open(b, id); err != nil {
		if errors.Is(err, channelbox.ErrNoChannel) {
			return "", mtproto.ErrChannelInvalid
		}
		return "", err
	}
	var chans []shareChannel
	if err := b.Tx().QueryRowsPartial(&chans,
		"SELECT c.broadcast, c.deleted, c.join_request, c.default_banned_rights,"+
			" EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1) AS public"+
			" FROM channels c WHERE c.id = ?", mtproto.PEER_CHANNEL, id); err != nil {
		return "", err
	}
	if len(chans) == 0 || chans[0].Deleted {
		return "", mtproto.ErrChannelInvalid
	}
	c := chans[0]
	var members []shareParticipant
	if err := b.Tx().QueryRowsPartial(&members,
		"SELECT role, state, admin_rights, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ?", id, b.UserId()); err != nil {
		return "", err
	}
	var p channelperm.Participant
	if len(members) > 0 {
		m := members[0]
		p = channelperm.Participant{Role: m.Role, State: m.State, AdminRights: m.AdminRights, BannedRights: m.BannedRights, UntilDate: m.UntilDate}
	} else {
		p = channelperm.Participant{State: channelperm.StateLeft}
	}
	// Managing the links is the right of the creator and of an admin with
	// invite_users, not of a member a supergroup lets add others (tdesktop
	// ChannelData::canHaveInviteLink, iOS canShareLinkToPeer).
	manages := p.State == channelperm.StateIn && p.Role != channelperm.RoleMember &&
		channelperm.Can(channelperm.Channel{Broadcast: c.Broadcast, Default: c.Default}, p, channelperm.InviteUsers, int32(b.Date())) == nil
	switch {
	case manages:
	case c.Public && !c.JoinRequest:
	case p.State != channelperm.StateIn:
		return "", mtproto.ErrChannelPrivate
	default:
		return "", mtproto.ErrChatAdminRequired
	}
	if c.Public && !c.JoinRequest {
		return "", nil
	}
	link := chatpb.GenChannelInviteHash()
	if _, err := b.Tx().Exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, request_needed, title, date2) VALUES (?, ?, ?, 0, ?, ?, ?)",
		id, b.UserId(), link, c.JoinRequest, title, b.Date()); err != nil {
		return "", err
	}
	return link, nil
}

// revokeLinks revokes the invite links links of channels.
func revokeLinks(b *userbox.Box, links []string) error {
	for _, l := range links {
		if l == "" {
			continue
		}
		if _, err := b.Tx().Exec("UPDATE chat_invites SET revoked = 1 WHERE link = ?", l); err != nil {
			return err
		}
	}
	return nil
}

// cutChatlistTitle cuts a link's title to chatlistTitleMax characters.
func cutChatlistTitle(title string) string {
	if utf8.RuneCountInString(title) <= chatlistTitleMax {
		return title
	}
	return string([]rune(title)[:chatlistTitleMax])
}

// ChatlistShare is what a link shares; Chats are the peers of the
// updateDialogFilter its folder change pushes.
type ChatlistShare struct {
	Title            string
	Peers            []*mtproto.InputPeer
	MigratedChats    []int64
	MigratedChannels []int64
	Chats            []*mtproto.Chat
	CheckOnly        bool
}

// checkExport checks folder id for a new or changed link of b's owner: the
// folder, the limits when a new link is made (mkLink).
func checkExport(b *userbox.Box, id int32, mkLink bool) (*filterRow, *mtproto.DialogFilter, int64, error) {
	rows, err := lockFilters(b)
	if err != nil {
		return nil, nil, 0, err
	}
	row, f, err := lockedFilter(b, id)
	if err != nil {
		return nil, nil, 0, err
	}
	if row == nil || (f.GetPredicateName() != mtproto.Predicate_dialogFilter && f.GetPredicateName() != mtproto.Predicate_dialogFilterChatlist) {
		return nil, nil, 0, mtproto.ErrFilterIdInvalid
	}
	if !shareable(f) {
		return nil, nil, 0, mtproto.ErrFilterNotSupported
	}
	order, _ := folderPlace(rows, id)
	if !mkLink {
		return row, f, order, nil
	}
	joined, invites, err := chatlistLimits(b)
	if err != nil {
		return nil, nil, 0, err
	}
	var count []int
	if err = b.Tx().QueryRowsPartial(&count, "SELECT COUNT(*) FROM chatlist_invites WHERE user_id = ? AND dialog_filter_id = ?", b.UserId(), id); err != nil {
		return nil, nil, 0, err
	}
	if count[0] >= invites {
		return nil, nil, 0, mtproto.ErrInvitesTooMuch
	}
	if !row.IsChatlist {
		lists, err := countChatlists(b)
		if err != nil {
			return nil, nil, 0, err
		}
		if lists >= joined {
			return nil, nil, 0, ErrChatlistsTooMuch
		}
	}
	return row, f, order, nil
}

// insertChatlistPeers adds the channels ids to link slug from position on.
func insertChatlistPeers(b *userbox.Box, slug string, ids []int64, links map[int64]string, position int) error {
	for i, id := range ids {
		if _, err := b.Tx().Exec("INSERT INTO chatlist_invite_peers (slug, channel_id, link, position) VALUES (?, ?, ?, ?)",
			slug, id, links[id], position+i); err != nil {
			return err
		}
	}
	return nil
}

// ExportChatlistInvite makes a link of folder id for its channels peers
// (peers of a group moved to a supergroup are in share.Migrated*), turning
// the folder shared on the first link. It returns the folder and the link;
// with CheckOnly nothing is written and both are nil.
func (d *Dao) ExportChatlistInvite(ctx context.Context, userId int64, id int32, share ChatlistShare) (*mtproto.Chatlists_ExportedChatlistInvite, error) {
	ids, err := channelPeers(share.Peers)
	if err != nil {
		return nil, err
	}
	var out *mtproto.Chatlists_ExportedChatlistInvite
	err = d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		row, f, order, err := checkExport(b, id, true)
		if err != nil || share.CheckOnly {
			return err
		}
		f = applyMigrated(f, share)
		if !folderHasPeers(f, ids) {
			return mtproto.ErrPeerIdInvalid
		}
		slug, title := chatlistSlug(), cutChatlistTitle(share.Title)
		links := make(map[int64]string, len(ids))
		for _, cid := range sortedChannels(ids) {
			if links[cid], err = shareChannelLink(b, cid, title); err != nil {
				return err
			}
		}
		if _, err = b.Tx().Exec("INSERT INTO chatlist_invites (slug, user_id, dialog_filter_id, title, date2) VALUES (?, ?, ?, ?, ?)",
			slug, userId, id, title, b.Date()); err != nil {
			return err
		}
		if err = insertChatlistPeers(b, slug, ids, links, 0); err != nil {
			return err
		}
		saved, err := d.writeFilter(b, toChatlist(f, true), *row, order, nil, share.Chats)
		if err != nil {
			return err
		}
		out = mtproto.MakeTLChatlistsExportedChatlistInvite(&mtproto.Chatlists_ExportedChatlistInvite{
			Filter: saved,
			Invite: makeExportedInvite(slug, title, ids),
		}).To_Chatlists_ExportedChatlistInvite()
		return nil
	})
	return out, err
}

// applyMigrated returns f with the basic groups of share moved to
// supergroups, as the channels of share.Peers.
func applyMigrated(f *mtproto.DialogFilter, share ChatlistShare) *mtproto.DialogFilter {
	if len(share.MigratedChats) == 0 {
		return f
	}
	input := make(map[int64]*mtproto.InputPeer, len(share.Peers))
	for _, p := range share.Peers {
		input[p.GetChannelId()] = p
	}
	f = proto.Clone(f).(*mtproto.DialogFilter)
	f.PinnedPeers = inputMigrated(f.GetPinnedPeers(), share.MigratedChats, share.MigratedChannels, input)
	f.IncludePeers = inputMigrated(f.GetIncludePeers(), share.MigratedChats, share.MigratedChannels, input)
	return f
}

// makeExportedInvite is the link slug as the clients read it.
func makeExportedInvite(slug, title string, channels []int64) *mtproto.ExportedChatlistInvite {
	peers := make([]*mtproto.Peer, 0, len(channels))
	for _, id := range channels {
		peers = append(peers, mtproto.MakePeer(mtproto.PEER_CHANNEL, id))
	}
	return mtproto.MakeTLExportedChatlistInvite(&mtproto.ExportedChatlistInvite{
		Title: title,
		Url:   chatlistURL(slug),
		Peers: peers,
	}).To_ExportedChatlistInvite()
}

type chatlistInviteRow struct {
	Slug   string `db:"slug"`
	UserId int64  `db:"user_id"`
	Filter int32  `db:"dialog_filter_id"`
	Title  string `db:"title"`
}

type chatlistPeerRow struct {
	ChannelId int64  `db:"channel_id"`
	Link      string `db:"link"`
	Position  int    `db:"position"`
}

// lockedInvite returns the link slug of b's owner and folder id, locked;
// INVITE_SLUG_EXPIRED when it has no such link.
func lockedInvite(b *userbox.Box, slug string, id int32) (*chatlistInviteRow, []chatlistPeerRow, error) {
	var invites []chatlistInviteRow
	if err := b.Tx().QueryRowsPartial(&invites,
		"SELECT slug, user_id, dialog_filter_id, title FROM chatlist_invites WHERE slug = ? FOR UPDATE", slug); err != nil {
		return nil, nil, err
	}
	if len(invites) == 0 || invites[0].UserId != b.UserId() || invites[0].Filter != id {
		return nil, nil, mtproto.ErrInviteSlugExpired
	}
	var peers []chatlistPeerRow
	if err := b.Tx().QueryRowsPartial(&peers,
		"SELECT channel_id, link, position FROM chatlist_invite_peers WHERE slug = ? ORDER BY position", slug); err != nil {
		return nil, nil, err
	}
	return &invites[0], peers, nil
}

// EditChatlistInvite changes the title of the link slug (setTitle) or its
// channels (setPeers): a channel no longer shared loses its invite link, a
// new one gets its own, and the rest keep theirs (links importers joined
// by stay valid).
func (d *Dao) EditChatlistInvite(ctx context.Context, userId int64, id int32, slug string, setTitle bool, title string, setPeers bool, share ChatlistShare) (*mtproto.ExportedChatlistInvite, error) {
	var ids []int64
	if setPeers {
		var err error
		if ids, err = channelPeers(share.Peers); err != nil {
			return nil, err
		}
	}
	var out *mtproto.ExportedChatlistInvite
	err := d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		row, f, order, err := checkExport(b, id, false)
		if err != nil {
			return err
		}
		invite, peers, err := lockedInvite(b, slug, id)
		if err != nil || share.CheckOnly {
			return err
		}
		newTitle := invite.Title
		if setTitle {
			newTitle = cutChatlistTitle(title)
		}
		have := make([]int64, 0, len(peers))
		for _, p := range peers {
			have = append(have, p.ChannelId)
		}
		final := have
		if setPeers {
			f = applyMigrated(f, share)
			if !folderHasPeers(f, ids) {
				return mtproto.ErrPeerIdInvalid
			}
			var added []int64
			for _, cid := range ids {
				if !slices.Contains(have, cid) {
					added = append(added, cid)
				}
			}
			links := make(map[int64]string, len(added))
			for _, cid := range sortedChannels(added) {
				if links[cid], err = shareChannelLink(b, cid, newTitle); err != nil {
					return err
				}
			}
			var removed []string
			for _, p := range peers {
				if !slices.Contains(ids, p.ChannelId) {
					removed = append(removed, p.Link)
					if _, err = b.Tx().Exec("DELETE FROM chatlist_invite_peers WHERE slug = ? AND channel_id = ?", slug, p.ChannelId); err != nil {
						return err
					}
				}
			}
			if err = revokeLinks(b, removed); err != nil {
				return err
			}
			// New positions follow the list as sent.
			for i, cid := range ids {
				if slices.Contains(have, cid) {
					if _, err = b.Tx().Exec("UPDATE chatlist_invite_peers SET position = ? WHERE slug = ? AND channel_id = ?", i, slug, cid); err != nil {
						return err
					}
				}
			}
			for _, cid := range added {
				if err = insertChatlistPeers(b, slug, []int64{cid}, links, slices.Index(ids, cid)); err != nil {
					return err
				}
			}
			final = ids
			if len(share.MigratedChats) > 0 {
				if _, err = d.writeFilter(b, f, *row, order, nil, share.Chats); err != nil {
					return err
				}
			}
		}
		if setTitle {
			if _, err = b.Tx().Exec("UPDATE chatlist_invites SET title = ? WHERE slug = ?", newTitle, slug); err != nil {
				return err
			}
		}
		out = makeExportedInvite(slug, newTitle, final)
		return nil
	})
	return out, err
}

// DeleteChatlistInvite deletes the link slug of folder id; its channels'
// invite links stop working. The folder of a last link deleted is plain
// again (Android FilterCreateActivity.onDelete), unless it was imported.
func (d *Dao) DeleteChatlistInvite(ctx context.Context, userId int64, id int32, slug string) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		row, f, err := lockedFilter(b, id)
		if err != nil {
			return err
		}
		if row == nil {
			return mtproto.ErrFilterIdInvalid
		}
		if !row.IsChatlist {
			return mtproto.ErrFilterNotSupported
		}
		_, peers, err := lockedInvite(b, slug, id)
		if err != nil {
			return err
		}
		if _, err = b.Tx().Exec("DELETE FROM chatlist_invite_peers WHERE slug = ?", slug); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("DELETE FROM chatlist_invites WHERE slug = ?", slug); err != nil {
			return err
		}
		links := make([]string, 0, len(peers))
		for _, p := range peers {
			links = append(links, p.Link)
		}
		if err = revokeLinks(b, links); err != nil {
			return err
		}
		var left []int
		if err = b.Tx().QueryRowsPartial(&left, "SELECT COUNT(*) FROM chatlist_invites WHERE user_id = ? AND dialog_filter_id = ?", userId, id); err != nil {
			return err
		}
		if left[0] > 0 || !f.GetHasMyInvites() {
			return nil
		}
		next := toChatlist(f, false)
		if !row.JoinedBySlug {
			next = mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
				TitleNoanimate:         f.GetTitleNoanimate(),
				Id:                     f.GetId(),
				Title_TEXTWITHENTITIES: f.GetTitle_TEXTWITHENTITIES(),
				Emoticon:               f.GetEmoticon(),
				Color:                  f.GetColor(),
				PinnedPeers:            f.GetPinnedPeers(),
				IncludePeers:           f.GetIncludePeers(),
				ExcludePeers:           []*mtproto.InputPeer{},
			}).To_DialogFilter()
		}
		order, _ := folderPlace(rows, id)
		_, err = d.writeFilter(b, next, *row, order, nil, nil)
		return err
	})
}

// GetChatlistInvites returns the links of folder id of userId.
func (d *Dao) GetChatlistInvites(ctx context.Context, userId int64, id int32) (*mtproto.Chatlists_ExportedInvites, error) {
	do, err := d.DialogFiltersDAO.Select(ctx, userId, id)
	if err != nil {
		return nil, err
	}
	if do == nil {
		return nil, mtproto.ErrFilterIdInvalid
	}
	var invites []chatlistInviteRow
	if err = d.Mysql.DB.QueryRowsPartial(ctx, &invites,
		"SELECT slug, user_id, dialog_filter_id, title FROM chatlist_invites WHERE user_id = ? AND dialog_filter_id = ? ORDER BY date2", userId, id); err != nil {
		return nil, err
	}
	out := make([]*mtproto.ExportedChatlistInvite, 0, len(invites))
	for _, i := range invites {
		peers, err := d.chatlistPeers(ctx, i.Slug)
		if err != nil {
			return nil, err
		}
		ids := make([]int64, 0, len(peers))
		for _, p := range peers {
			ids = append(ids, p.ChannelId)
		}
		out = append(out, makeExportedInvite(i.Slug, i.Title, ids))
	}
	return mtproto.MakeTLChatlistsExportedInvites(&mtproto.Chatlists_ExportedInvites{
		Invites: out,
		Chats:   []*mtproto.Chat{},
		Users:   []*mtproto.User{},
	}).To_Chatlists_ExportedInvites(), nil
}

func (d *Dao) chatlistPeers(ctx context.Context, slug string) ([]chatlistPeerRow, error) {
	var peers []chatlistPeerRow
	err := d.Mysql.DB.QueryRowsPartial(ctx, &peers,
		"SELECT channel_id, link, position FROM chatlist_invite_peers WHERE slug = ? ORDER BY position", slug)
	return peers, err
}

// GetChatlistInvite returns the link slug as userId may use it; with slug
// empty, for the folder filterId imported by userId, the channels of its
// link that are new to it (neither in the folder nor seen when it was
// joined or dismissed).
func (d *Dao) GetChatlistInvite(ctx context.Context, userId int64, slug string, filterId int32) (*dialog.ChatlistInviteExt, error) {
	if slug == "" {
		return d.chatlistInviteUpdates(ctx, userId, filterId)
	}
	var invites []chatlistInviteRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &invites,
		"SELECT slug, user_id, dialog_filter_id, title FROM chatlist_invites WHERE slug = ?", slug); err != nil {
		return nil, err
	}
	if len(invites) == 0 {
		return nil, mtproto.ErrInviteSlugExpired
	}
	inv := invites[0]
	owner, err := d.DialogFiltersDAO.Select(ctx, inv.UserId, inv.Filter)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, mtproto.ErrInviteSlugExpired
	}
	folder, err := mtproto.UnmarshalDialogFilter(owner.DialogFilter)
	if err != nil {
		return nil, err
	}
	peers, err := d.chatlistPeers(ctx, slug)
	if err != nil {
		return nil, err
	}
	ext := &dialog.ChatlistInviteExt{
		Slug:          slug,
		OwnerId:       inv.UserId,
		OwnerFilterId: inv.Filter,
		LinkTitle:     inv.Title,
		DialogFilter:  folder,
	}
	fillChatlistExtPeers(ext, peers)
	mine, err := d.myChatlistFolder(ctx, userId, inv)
	if err != nil {
		return nil, err
	}
	if mine != nil {
		f, err := mtproto.UnmarshalDialogFilter(mine.DialogFilter)
		if err != nil {
			return nil, err
		}
		ext.MyFilterId = mine.DialogFilterId
		ext.MyPeers = folderChannelPeers(f)
	}
	return ext, nil
}

// myChatlistFolder returns the folder of userId this link is: its own, or
// imported by it from slug.
func (d *Dao) myChatlistFolder(ctx context.Context, userId int64, inv chatlistInviteRow) (*dataobject.DialogFiltersDO, error) {
	if userId == inv.UserId {
		return d.DialogFiltersDAO.Select(ctx, userId, inv.Filter)
	}
	return d.DialogFiltersDAO.SelectBySlug(ctx, userId, inv.Slug)
}

func fillChatlistExtPeers(ext *dialog.ChatlistInviteExt, peers []chatlistPeerRow) {
	ext.Peers = make([]*mtproto.Peer, 0, len(peers))
	ext.Links = make([]string, 0, len(peers))
	for _, p := range peers {
		ext.Peers = append(ext.Peers, mtproto.MakePeer(mtproto.PEER_CHANNEL, p.ChannelId))
		ext.Links = append(ext.Links, p.Link)
	}
}

func folderChannelPeers(f *mtproto.DialogFilter) []*mtproto.Peer {
	out := make([]*mtproto.Peer, 0, len(f.GetIncludePeers())+len(f.GetPinnedPeers()))
	for _, list := range [][]*mtproto.InputPeer{f.GetPinnedPeers(), f.GetIncludePeers()} {
		for _, p := range list {
			if p.GetPredicateName() == mtproto.Predicate_inputPeerChannel {
				out = append(out, mtproto.MakePeer(mtproto.PEER_CHANNEL, p.GetChannelId()))
			}
		}
	}
	return out
}

// chatlistInviteUpdates is GetChatlistInvite for a folder imported by userId.
func (d *Dao) chatlistInviteUpdates(ctx context.Context, userId int64, filterId int32) (*dialog.ChatlistInviteExt, error) {
	do, err := d.DialogFiltersDAO.Select(ctx, userId, filterId)
	if err != nil {
		return nil, err
	}
	if do == nil {
		return nil, mtproto.ErrFilterIdInvalid
	}
	if !do.IsChatlist {
		return nil, mtproto.ErrFilterNotSupported
	}
	folder, err := mtproto.UnmarshalDialogFilter(do.DialogFilter)
	if err != nil {
		return nil, err
	}
	ext := &dialog.ChatlistInviteExt{MyFilterId: filterId, DialogFilter: folder, MyPeers: folderChannelPeers(folder)}
	if !do.JoinedBySlug || do.Slug == "" {
		return ext, nil
	}
	var invites []chatlistInviteRow
	if err = d.Mysql.DB.QueryRowsPartial(ctx, &invites,
		"SELECT slug, user_id, dialog_filter_id, title FROM chatlist_invites WHERE slug = ?", do.Slug); err != nil {
		return nil, err
	}
	if len(invites) == 0 {
		return ext, nil
	}
	ext.Slug = do.Slug
	ext.OwnerId = invites[0].UserId
	ext.OwnerFilterId = invites[0].Filter
	peers, err := d.chatlistPeers(ctx, do.Slug)
	if err != nil {
		return nil, err
	}
	var seen []int64
	if err = d.Mysql.DB.QueryRowsPartial(ctx, &seen,
		"SELECT channel_id FROM chatlist_seen_peers WHERE user_id = ? AND dialog_filter_id = ?", userId, filterId); err != nil {
		return nil, err
	}
	var news []chatlistPeerRow
	for _, p := range peers {
		if folderHasPeers(folder, []int64{p.ChannelId}) || slices.Contains(seen, p.ChannelId) {
			continue
		}
		news = append(news, p)
	}
	fillChatlistExtPeers(ext, news)
	return ext, nil
}

// markChatlistSeen marks the channels ids of folder id as shown to b's
// owner: they are not new any more.
func markChatlistSeen(b *userbox.Box, id int32, ids []int64) error {
	for _, cid := range ids {
		if _, err := b.Tx().Exec("INSERT IGNORE INTO chatlist_seen_peers (user_id, dialog_filter_id, channel_id) VALUES (?, ?, ?)",
			b.UserId(), id, cid); err != nil {
			return err
		}
	}
	return nil
}

// JoinChatlistFolder gives userId's folder of the link slug (or its
// imported folder filterId) the channels peers, which it joined or asked
// to join by now, and returns the folder. A first import makes the folder,
// shared, with the title and icon of the owner's: DIALOG_FILTERS_TOO_MUCH or
// CHATLISTS_TOO_MUCH over the limits. It is safe to repeat. With CheckOnly
// only the limits are checked and nil is returned, before any channel is
// joined for the folder.
func (d *Dao) JoinChatlistFolder(ctx context.Context, userId int64, slug string, filterId int32, peers []*mtproto.InputPeer, chats []*mtproto.Chat, checkOnly bool) (*dialog.DialogFilterExt, error) {
	var ext *dialog.DialogFilterExt
	err := d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		var (
			row   *filterRow
			f     *mtproto.DialogFilter
			owner *mtproto.DialogFilter
		)
		if slug != "" {
			inv, folder, err := linkForJoin(b, slug)
			if err != nil {
				return err
			}
			owner = folder
			mine, err := joinedFolderId(b, inv)
			if err != nil {
				return err
			}
			if mine != 0 {
				filterId = mine
			}
		}
		if filterId != 0 {
			if row, f, err = lockedFilter(b, filterId); err != nil {
				return err
			}
			if row == nil {
				if slug == "" {
					return mtproto.ErrFilterIdInvalid
				}
			} else if !row.IsChatlist {
				return mtproto.ErrFilterNotSupported
			}
		}
		if row == nil {
			if err = checkNewChatlist(b, rows); err != nil {
				return err
			}
			if filterId, err = freeFolderId(rows); err != nil {
				return err
			}
		}
		if checkOnly {
			return nil
		}
		order, found := folderPlace(rows, filterId)
		before := 0
		if f == nil {
			f = mtproto.MakeTLDialogFilterChatlist(&mtproto.DialogFilter{
				Id:                     filterId,
				TitleNoanimate:         owner.GetTitleNoanimate(),
				Title_TEXTWITHENTITIES: owner.GetTitle_TEXTWITHENTITIES(),
				Emoticon:               owner.GetEmoticon(),
				PinnedPeers:            []*mtproto.InputPeer{},
				IncludePeers:           []*mtproto.InputPeer{},
			}).To_DialogFilter()
		} else {
			before = len(f.GetIncludePeers()) + len(f.GetPinnedPeers())
		}
		if !found {
			order = filterOrderTop
			if len(rows) > 0 {
				order = rows[len(rows)-1].Order - filterOrderStep
			}
		}
		next := proto.Clone(f).(*mtproto.DialogFilter)
		for _, p := range peers {
			if p.GetPredicateName() != mtproto.Predicate_inputPeerChannel {
				return mtproto.ErrPeerIdInvalid
			}
			if !folderHasPeers(next, []int64{p.GetChannelId()}) {
				next.IncludePeers = append(next.IncludePeers, p)
			}
		}
		chatsLimit := FilterChatsLimit
		premium, err := boxPremium(b)
		if err != nil {
			return err
		}
		if premium {
			chatsLimit = FilterChatsLimitPremium
		}
		if len(next.GetIncludePeers())+len(next.GetPinnedPeers()) > chatsLimit {
			return ErrFilterChatsTooMuch
		}
		// The link's whole list is seen: what is new to the folder is only
		// what the link gets from now on, also of what was not chosen now
		// (getChatlistUpdates: "recently added").
		linkSlug := slug
		if linkSlug == "" {
			linkSlug = row.Slug
		}
		if linkSlug != "" {
			var ids []int64
			if err = b.Tx().QueryRowsPartial(&ids, "SELECT channel_id FROM chatlist_invite_peers WHERE slug = ?", linkSlug); err != nil {
				return err
			}
			if err = markChatlistSeen(b, filterId, ids); err != nil {
				return err
			}
		}
		saved := next
		if row == nil {
			meta := filterRow{JoinedBySlug: true, Slug: slug}
			if saved, err = d.writeFilter(b, next, meta, order, nil, chats); err != nil {
				return err
			}
		} else if len(next.GetIncludePeers())+len(next.GetPinnedPeers()) != before {
			if saved, err = d.writeFilter(b, next, *row, order, nil, chats); err != nil {
				return err
			}
		}
		ext = dialog.MakeTLDialogFilterExt(&dialog.DialogFilterExt{
			Id:           filterId,
			JoinedBySlug: true,
			Slug:         slug,
			DialogFilter: saved,
			Order:        order,
		}).To_DialogFilterExt()
		return nil
	})
	return ext, err
}

// linkForJoin returns the link slug and the owner's folder.
func linkForJoin(b *userbox.Box, slug string) (*chatlistInviteRow, *mtproto.DialogFilter, error) {
	var invites []chatlistInviteRow
	if err := b.Tx().QueryRowsPartial(&invites,
		"SELECT slug, user_id, dialog_filter_id, title FROM chatlist_invites WHERE slug = ?", slug); err != nil {
		return nil, nil, err
	}
	if len(invites) == 0 {
		return nil, nil, mtproto.ErrInviteSlugExpired
	}
	inv := &invites[0]
	var data []string
	if err := b.Tx().QueryRowsPartial(&data,
		"SELECT dialog_filter FROM dialog_filters WHERE user_id = ? AND dialog_filter_id = ? AND deleted = 0", inv.UserId, inv.Filter); err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, mtproto.ErrInviteSlugExpired
	}
	folder, err := mtproto.UnmarshalDialogFilter(data[0])
	if err != nil {
		return nil, nil, err
	}
	return inv, folder, nil
}

// joinedFolderId returns the folder of b's owner for the link inv: its own,
// or imported from the link.
func joinedFolderId(b *userbox.Box, inv *chatlistInviteRow) (int32, error) {
	if b.UserId() == inv.UserId {
		return inv.Filter, nil
	}
	var ids []int32
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT dialog_filter_id FROM dialog_filters WHERE user_id = ? AND slug = ? AND deleted = 0", b.UserId(), inv.Slug); err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// checkNewChatlist checks the limits for a new imported folder of b's owner.
func checkNewChatlist(b *userbox.Box, rows []filterOrder) error {
	joined, _, err := chatlistLimits(b)
	if err != nil {
		return err
	}
	lists, err := countChatlists(b)
	if err != nil {
		return err
	}
	if lists >= joined {
		return ErrChatlistsTooMuch
	}
	premium, err := boxPremium(b)
	if err != nil {
		return err
	}
	folders := DialogFiltersLimit
	if premium {
		folders = DialogFiltersLimitPremium
	}
	count := 0
	for _, r := range rows {
		if r.Id != allChats {
			count++
		}
	}
	if count >= folders {
		return ErrDialogFiltersTooMuch
	}
	return nil
}

// freeFolderId is the lowest id no folder of rows uses.
func freeFolderId(rows []filterOrder) (int32, error) {
	for id := int32(chatlistFolderIdMin); id <= chatlistFolderIdMax; id++ {
		if !slices.ContainsFunc(rows, func(r filterOrder) bool { return r.Id == id }) {
			return id, nil
		}
	}
	return 0, ErrDialogFiltersTooMuch
}

// HideChatlistUpdates marks what is new in the link of folder id of userId
// as seen.
func (d *Dao) HideChatlistUpdates(ctx context.Context, userId int64, id int32) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		if _, err := lockFilters(b); err != nil {
			return err
		}
		row, _, err := lockedFilter(b, id)
		if err != nil {
			return err
		}
		if row == nil {
			return mtproto.ErrFilterIdInvalid
		}
		if !row.IsChatlist {
			return mtproto.ErrFilterNotSupported
		}
		if row.Slug == "" {
			return nil
		}
		var ids []int64
		if err = b.Tx().QueryRowsPartial(&ids, "SELECT channel_id FROM chatlist_invite_peers WHERE slug = ?", row.Slug); err != nil {
			return err
		}
		return markChatlistSeen(b, id, ids)
	})
}

// clearChatlistState removes the links of folder id of b's owner, the seen
// marks of it and stops the channel invite links its links made (tdesktop
// lng_filters_delete_sure: deleting a folder "will also deactivate all the
// invite links").
func clearChatlistState(b *userbox.Box, id int32) error {
	if _, err := b.Tx().Exec("UPDATE chat_invites i JOIN chatlist_invite_peers p ON p.link = i.link"+
		" JOIN chatlist_invites c ON c.slug = p.slug SET i.revoked = 1 WHERE c.user_id = ? AND c.dialog_filter_id = ? AND p.link != ''", b.UserId(), id); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("DELETE p FROM chatlist_invite_peers p JOIN chatlist_invites c ON c.slug = p.slug WHERE c.user_id = ? AND c.dialog_filter_id = ?", b.UserId(), id); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("DELETE FROM chatlist_invites WHERE user_id = ? AND dialog_filter_id = ?", b.UserId(), id); err != nil {
		return err
	}
	_, err := b.Tx().Exec("DELETE FROM chatlist_seen_peers WHERE user_id = ? AND dialog_filter_id = ?", b.UserId(), id)
	return err
}
