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
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/env2"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The invite links of a channel (task 041), kept in chat_invites with those
// of basic groups under the channel's id (the hash tells them apart, its
// first character is a letter): writers hold the channel's lock, which the
// joins take too, so a link is never revoked halfway through a join.

const (
	// InviteUsageMax is the largest usage_limit (tdesktop's kMaxLimit of
	// the editor of a link).
	InviteUsageMax = 99999
	// InviteTitleMax is the longest title of a link, in characters
	// (tdesktop's kMaxLabelLength, Android's filter of LinkEditActivity).
	InviteTitleMax = 32
	// InvitesMax is the most links of one page.
	InvitesMax = 100
)

// linkAdmin checks that a may make and manage links: an admin or the creator
// with invite_users. channelperm allows inviting to members of a supergroup
// too, but the links are the admins' (CHAT_ADMIN_REQUIRED,
// core.telegram.org/method/messages.exportChatInvite; Android's
// ACTION_INVITE and tdesktop's canHaveInviteLink are of admins).
func (a *actor) linkAdmin(now int64) error {
	if a.p.Role == channelperm.RoleMember {
		return mtproto.ErrChatAdminRequired
	}
	return a.can(channelperm.InviteUsers, now)
}

// linksOf checks that a, a link admin, may act on the links of adminId:
// its own, or any for the creator (tdesktop shows the links of others only
// to the creator: AdminsController::prepare, edit_peer_invite_links.cpp).
func (a *actor) linksOf(selfId, adminId int64) error {
	if adminId == selfId || a.p.Role == channelperm.RoleCreator {
		return nil
	}
	return mtproto.ErrChatAdminRequired
}

// linkActor reads b's owner's rights in the locked channel for link
// management.
func linkActor(b *userbox.Box, channelId int64) (*actor, error) {
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	return a, a.linkAdmin(b.Date())
}

// InviteRow is a link of chat_invites with its counts: who joined by it and
// who asks to.
type InviteRow struct {
	AdminId       int64  `db:"admin_id"`
	Link          string `db:"link"`
	Permanent     bool   `db:"permanent"`
	Revoked       bool   `db:"revoked"`
	RequestNeeded bool   `db:"request_needed"`
	StartDate     int64  `db:"start_date"`
	ExpireDate    int64  `db:"expire_date"`
	UsageLimit    int32  `db:"usage_limit"`
	Title         string `db:"title"`
	Date          int64  `db:"date2"`
	Joined        int32  `db:"joined"`
	Asked         int32  `db:"asked"`
}

const inviteRowColumns = "i.admin_id, i.link, i.permanent, i.revoked, i.request_needed, i.start_date, i.expire_date, i.usage_limit, i.title, i.date2," +
	" (SELECT COUNT(*) FROM chat_invite_participants p WHERE p.link = i.link AND p.requested = 0) AS joined," +
	" (SELECT COUNT(*) FROM channel_join_requests r WHERE r.channel_id = i.chat_id AND r.link = i.link) AS asked"

// Exported is the link as the clients get it.
func (r *InviteRow) Exported() *mtproto.ExportedChatInvite {
	return mtproto.MakeTLChatInviteExported(&mtproto.ExportedChatInvite{
		Revoked:       r.Revoked,
		Permanent:     r.Permanent,
		RequestNeeded: r.RequestNeeded,
		Link:          "https://" + env2.TDotMe + "/+" + r.Link,
		AdminId:       r.AdminId,
		Date:          int32(r.Date),
		StartDate:     mtproto.MakeFlagsInt32(int32(r.StartDate)),
		ExpireDate:    mtproto.MakeFlagsInt32(int32(r.ExpireDate)),
		UsageLimit:    mtproto.MakeFlagsInt32(r.UsageLimit),
		Usage:         mtproto.MakeFlagsInt32(r.Joined),
		Requested:     mtproto.MakeFlagsInt32(r.Asked),
		Title:         mtproto.MakeFlagsString(r.Title),
	}).To_ExportedChatInvite()
}

// lockedInvite reads the link of the channel by b's transaction (the
// channel locked); nil when it has none.
func lockedInvite(b *userbox.Box, channelId int64, link string) (*InviteRow, error) {
	var rows []InviteRow
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT "+inviteRowColumns+" FROM chat_invites i WHERE i.link = ? AND i.chat_id = ?", link, channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// InviteOf is the link of the channel; nil when it has none.
func (d *Dao) InviteOf(ctx context.Context, channelId int64, link string) (*InviteRow, error) {
	var rows []InviteRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+inviteRowColumns+" FROM chat_invites i WHERE i.link = ? AND i.chat_id = ?", link, channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// checkLimits checks a new expire date and usage limit: an expire date is
// in the future (EXPIRE_DATE_INVALID, 0 is none), a limit is 0 or up to
// InviteUsageMax (USAGE_LIMIT_INVALID; core.telegram.org/method/
// messages.exportChatInvite).
func checkLimits(expire, limit int32, now int64) error {
	if expire != 0 && int64(expire) <= now {
		return mtproto.ErrExpireDateInvalid
	}
	if limit < 0 || limit > InviteUsageMax {
		return mtproto.ErrUsageLimitInvalid
	}
	return nil
}

// cutTitle cuts a title to the InviteTitleMax characters the clients let
// one enter (the documentation names no error for a longer one).
func cutTitle(title string) string {
	if utf8.RuneCountInString(title) <= InviteTitleMax {
		return title
	}
	return string([]rune(title)[:InviteTitleMax])
}

func insertInvite(b *userbox.Box, channelId int64, r *InviteRow) error {
	_, err := b.Tx().Exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, revoked, request_needed, start_date, expire_date, usage_limit, usage2, requested, title, date2) VALUES (?, ?, ?, ?, 0, ?, 0, ?, ?, 0, 0, ?, ?)",
		channelId, r.AdminId, r.Link, r.Permanent, r.RequestNeeded, r.ExpireDate, r.UsageLimit, r.Title, r.Date)
	return err
}

// ExportInvite makes a link of the channel for b's owner, who must be the
// creator or an admin with invite_users. A link that needs approval takes
// no usage limit: Android sends none with it (LinkEditActivity.java:749-751).
// With legacy, the account's permanent link is replaced by a new one (the
// clients make the permanent link so when it is missing, or to change it:
// tdesktop api_invite_links.cpp:92-101, ManageLinksActivity.java:987-989).
func (d *Dao) ExportInvite(b *userbox.Box, channelId int64, legacy, requestNeeded bool, expire, limit int32, title string) (*InviteRow, error) {
	if err := checkLimits(expire, limit, b.Date()); err != nil {
		return nil, err
	}
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	if _, err := linkActor(b, channelId); err != nil {
		return nil, err
	}
	r := &InviteRow{
		AdminId:       b.UserId(),
		Link:          chatpb.GenChannelInviteHash(),
		Permanent:     legacy,
		RequestNeeded: requestNeeded && !legacy,
		ExpireDate:    int64(expire),
		UsageLimit:    limit,
		Title:         cutTitle(title),
		Date:          b.Date(),
	}
	if r.RequestNeeded {
		r.UsageLimit = 0
	}
	if legacy {
		r.ExpireDate, r.UsageLimit, r.Title = 0, 0, ""
		if _, err := b.Tx().Exec("UPDATE chat_invites SET revoked = 1 WHERE chat_id = ? AND admin_id = ? AND permanent = 1 AND revoked = 0", channelId, b.UserId()); err != nil {
			return nil, err
		}
	}
	return r, insertInvite(b, channelId, r)
}

// InviteEdit is what messages.editExportedChatInvite changes: the fields of
// channel.InviteField* in Fields are given.
type InviteEdit struct {
	Revoked       bool
	Fields        int32
	ExpireDate    int32
	UsageLimit    int32
	RequestNeeded bool
	Title         string
}

// EditInvite changes or revokes a link of the channel, by its owner or the
// creator. Revoked (idempotent) a permanent link is replaced by a new one,
// returned second (messages.exportedChatInviteReplaced; clients:
// ManageLinksActivity.java:1472). A permanent link takes no expire date,
// limit or approval (CHAT_INVITE_PERMANENT); an edit of a revoked or unknown
// link is INVITE_HASH_EXPIRED.
func (d *Dao) EditInvite(b *userbox.Box, channelId int64, link string, e InviteEdit) (*InviteRow, *InviteRow, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, nil, err
	}
	a, err := linkActor(b, channelId)
	if err != nil {
		return nil, nil, err
	}
	row, err := lockedInvite(b, channelId, link)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, mtproto.ErrInviteHashExpired
	}
	if err = a.linksOf(b.UserId(), row.AdminId); err != nil {
		return nil, nil, err
	}
	if e.Revoked {
		if row.Revoked {
			return row, nil, nil
		}
		if _, err = b.Tx().Exec("UPDATE chat_invites SET revoked = 1 WHERE link = ?", link); err != nil {
			return nil, nil, err
		}
		row.Revoked = true
		if !row.Permanent {
			return row, nil, nil
		}
		next := &InviteRow{AdminId: row.AdminId, Link: chatpb.GenChannelInviteHash(), Permanent: true, Date: b.Date()}
		return row, next, insertInvite(b, channelId, next)
	}
	if row.Revoked {
		return nil, nil, mtproto.ErrInviteHashExpired
	}

	expire, limit := int32(row.ExpireDate), row.UsageLimit
	request, title := row.RequestNeeded, row.Title
	if e.Fields&channel.InviteFieldExpireDate != 0 {
		expire = e.ExpireDate
	}
	if e.Fields&channel.InviteFieldUsageLimit != 0 {
		limit = e.UsageLimit
	}
	if e.Fields&channel.InviteFieldRequestNeeded != 0 {
		request = e.RequestNeeded
	}
	if e.Fields&channel.InviteFieldTitle != 0 {
		title = cutTitle(e.Title)
	}
	if row.Permanent && (expire != 0 || limit != 0 || request) {
		return nil, nil, mtproto.ErrChatInvitePermanent
	}
	// checkLimits judges what is new: an expire date kept as it is may
	// already be past.
	checkExpire := int32(0)
	if e.Fields&channel.InviteFieldExpireDate != 0 {
		checkExpire = expire
	}
	if err = checkLimits(checkExpire, limit, b.Date()); err != nil {
		return nil, nil, err
	}
	if request {
		limit = 0
	}
	if _, err = b.Tx().Exec("UPDATE chat_invites SET expire_date = ?, usage_limit = ?, request_needed = ?, title = ?, start_date = ? WHERE link = ?",
		expire, limit, request, title, b.Date(), link); err != nil {
		return nil, nil, err
	}
	row.ExpireDate, row.UsageLimit, row.RequestNeeded, row.Title, row.StartDate = int64(expire), limit, request, title, b.Date()
	return row, nil, nil
}

// deleteLinks removes the links and their importers; a request asked
// through one stands, without its link.
func deleteLinks(b *userbox.Box, channelId int64, links []string) error {
	for _, link := range links {
		if _, err := b.Tx().Exec("UPDATE channel_join_requests SET link = '' WHERE channel_id = ? AND link = ?", channelId, link); err != nil {
			return err
		}
		if _, err := b.Tx().Exec("DELETE FROM chat_invite_participants WHERE link = ?", link); err != nil {
			return err
		}
		if _, err := b.Tx().Exec("DELETE FROM chat_invites WHERE link = ? AND chat_id = ?", link, channelId); err != nil {
			return err
		}
	}
	return nil
}

// DeleteInvite deletes a revoked link: INVITE_HASH_EXPIRED for one that is
// not there, INVITE_REVOKED_MISSING for one not revoked
// (core.telegram.org/method/messages.deleteExportedChatInvite).
func (d *Dao) DeleteInvite(b *userbox.Box, channelId int64, link string) error {
	if _, err := openChannel(b, channelId); err != nil {
		return err
	}
	a, err := linkActor(b, channelId)
	if err != nil {
		return err
	}
	row, err := lockedInvite(b, channelId, link)
	if err != nil {
		return err
	}
	switch {
	case row == nil:
		return mtproto.ErrInviteHashExpired
	case !row.Revoked:
		return mtproto.ErrInviteRevokedMissing
	}
	if err = a.linksOf(b.UserId(), row.AdminId); err != nil {
		return err
	}
	return deleteLinks(b, channelId, []string{link})
}

// DeleteRevokedInvites deletes the revoked links of adminId.
func (d *Dao) DeleteRevokedInvites(b *userbox.Box, channelId, adminId int64) error {
	if _, err := openChannel(b, channelId); err != nil {
		return err
	}
	a, err := linkActor(b, channelId)
	if err != nil {
		return err
	}
	if err = a.linksOf(b.UserId(), adminId); err != nil {
		return err
	}
	var links []string
	if err = b.Tx().QueryRowsPartial(&links, "SELECT link FROM chat_invites WHERE chat_id = ? AND admin_id = ? AND revoked = 1", channelId, adminId); err != nil {
		return err
	}
	return deleteLinks(b, channelId, links)
}

// applyJoinRequest sets the approval on every working link of the channel
// (channels.toggleJoinRequest with apply_to_invites): enabling it, those
// with a usage limit lose it, as approval and a limit do not go together.
func applyJoinRequest(b *userbox.Box, channelId int64, enabled bool) error {
	_, err := b.Tx().Exec("UPDATE chat_invites SET request_needed = ?, usage_limit = IF(?, 0, usage_limit) WHERE chat_id = ? AND revoked = 0 AND permanent = 0", enabled, enabled, channelId)
	return err
}

// ToggleJoinRequest turns the approval of those who join on or off, by one
// who may ban or invite (Android gates the switch by ban_users,
// JoinToSendSettingsView.java:58). CHAT_NOT_MODIFIED for the value it has;
// with applyToInvites the working links take the setting as well
// (Android's ApproveNewMembersApplyToLinks dialog, ChatEditTypeActivity.java:594-613),
// so that a channel with links can go over to approval at once. The
// flag also applies to a private channel: the clients offer it there, unlike
// the older note of the method's page on public groups.
func (d *Dao) ToggleJoinRequest(b *userbox.Box, channelId int64, enabled, applyToInvites bool) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if a.p.Role == channelperm.RoleMember {
		return nil, mtproto.ErrChatAdminRequired
	}
	if err = a.can(channelperm.BanUsers, b.Date()); err != nil {
		if err = a.can(channelperm.InviteUsers, b.Date()); err != nil {
			return nil, err
		}
	}
	var current []bool
	if err = b.Tx().QueryRowsPartial(&current, "SELECT join_request FROM channels WHERE id = ?", channelId); err != nil {
		return nil, err
	}
	if len(current) == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	if current[0] == enabled && !applyToInvites {
		return nil, mtproto.ErrChatNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET join_request = ?, version = version + 1 WHERE id = ?", enabled, channelId); err != nil {
		return nil, err
	}
	if applyToInvites {
		if err = applyJoinRequest(b, channelId, enabled); err != nil {
			return nil, err
		}
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// Invites is a page of the links of adminId in the channel, those not
// revoked or the revoked ones, the permanent link first and then the newest
// (tdesktop takes the first of a page for the permanent link,
// api_invite_links.cpp:131), with the count of all. The offset is the last
// link of the page before.
func (d *Dao) Invites(ctx context.Context, channelId, adminId int64, revoked bool, offsetLink string, limit int32) ([]InviteRow, int32, error) {
	const from = " FROM chat_invites i WHERE i.chat_id = ? AND i.admin_id = ? AND i.revoked = ?"
	var count []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*)"+from, channelId, adminId, revoked); err != nil {
		return nil, 0, err
	}
	where, args := from, []any{channelId, adminId, revoked}
	if offsetLink != "" {
		where += " AND (i.permanent, i.date2, i.link) < (SELECT o.permanent, o.date2, o.link FROM chat_invites o WHERE o.link = ?)"
		args = append(args, offsetLink)
	}
	if limit <= 0 || limit > InvitesMax {
		limit = InvitesMax
	}
	var rows []InviteRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+inviteRowColumns+where+" ORDER BY i.permanent DESC, i.date2 DESC, i.link DESC LIMIT ?", append(args, limit)...); err != nil {
		return nil, 0, err
	}
	return rows, first(count), nil
}

// AdminInvites is an admin's count of links, those working and those
// revoked.
type AdminInvites struct {
	AdminId int64 `db:"admin_id"`
	Active  int32 `db:"active"`
	Revoked int32 `db:"revoked"`
}

// AdminsWithInvites is the admins of the channel with links.
func (d *Dao) AdminsWithInvites(ctx context.Context, channelId int64) ([]AdminInvites, error) {
	var rows []AdminInvites
	err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT admin_id, CAST(SUM(revoked = 0) AS SIGNED) AS active, CAST(SUM(revoked = 1) AS SIGNED) AS revoked FROM chat_invites WHERE chat_id = ? GROUP BY admin_id ORDER BY admin_id", channelId)
	return rows, err
}
