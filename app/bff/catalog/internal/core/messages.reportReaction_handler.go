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
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// reactionReportReason is the reason of a reaction report: the method names
// none.
const reactionReportReason = "reaction"

// MessagesReportReaction
// messages.reportReaction#3f64c076 peer:InputPeer id:int reaction_peer:InputPeer = Bool;
//
// A member reports the reaction of a user to a message of a supergroup
// (core.telegram.org/method/messages.reportReaction); the report goes to the
// moderation queue (ADR 0040) with the message, the people and the reaction
// as they are now. Android sends the older #61422a48 with an InputUser,
// decoded into this request (third_party/teamgram-proto).
//
// Who may: Desktop offers it in a supergroup only, to anyone in a public
// one, never about oneself (api/api_report.cpp:185-194,
// GetReactionReportCapabilities); Android to an admin deleting a member's
// messages (DeleteMessagesBottomSheet.java:1171-1176) and from the
// profile opened from a reaction (ProfileActivity.java:4396-4402); iOS
// from that profile too (PeerInfoProfileItems.swift:451-458); Web to an
// admin deleting a member's messages (deleteMegagroupMessagesActions.ts:
// 76-80). So: an admin who may ban (or the creator) in any supergroup,
// anyone in a public one; Telegram's error for the rest is not
// documented. Android and iOS ignore the answer, Desktop shows "thanks"
// on success only.
func (c *CatalogCore) MessagesReportReaction(in *mtproto.TLMessagesReportReaction) (*mtproto.Bool, error) {
	if err := c.reportReaction(in); err != nil {
		c.Logger.Errorf("messages.reportReaction - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

func (c *CatalogCore) reportReaction(in *mtproto.TLMessagesReportReaction) error {
	if c.MD.IsBot {
		return mtproto.ErrBotMethodInvalid
	}
	if in.GetPeer() == nil {
		return mtproto.ErrPeerIdInvalid
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if !peer.IsChannel() {
		return mtproto.ErrPeerIdInvalid
	}
	if in.GetId() <= 0 {
		return mtproto.ErrMsgIdInvalid
	}
	now := int32(time.Now().Unix())
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return err
	}
	ch := mc.GetChannel()
	if ch.GetAccessHash() != peer.AccessHash || !ch.GetMegagroup() || ch.GetMonoforum() {
		return mtproto.ErrPeerIdInvalid
	}
	if mc.ToChat(c.MD.UserId, now).GetPredicateName() == mtproto.Predicate_channelForbidden {
		return mtproto.ErrChannelPrivate
	}
	me, _ := mc.GetParticipant(c.MD.UserId)
	if err = canReportReaction(ch, me, now); err != nil {
		return err
	}
	messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{in.GetId()}})
	if err != nil {
		return err
	}
	if len(messages.GetDatas()) == 0 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
		return mtproto.ErrMsgIdInvalid
	}
	message := messages.GetDatas()[0]
	byMessage, err := reactions.LoadChannel(c.ctx, c.svcCtx.Reactions, peer.PeerId, []int32{in.GetId()})
	if err != nil {
		return err
	}
	target, keys, err := reactionOf(in.GetReactionPeer(), c.MD.UserId, byMessage[in.GetId()])
	if err != nil {
		return err
	}

	snapshot := &moderation.Snapshot{
		PeerTitle: ch.GetTitle(),
		Messages: []moderation.SnapshotMessage{{
			Id:     message.GetId(),
			Date:   message.GetDate(),
			FromId: message.GetFromId().GetUserId(),
			Text:   message.GetMessage(),
			Media:  moderation.MediaOf(message.GetMedia()),
		}},
		Reaction: strings.Join(keys, ","),
	}
	people := []int64{target}
	if author := message.GetFromId().GetUserId(); author != 0 && author != target {
		people = append(people, author)
	}
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: people})
	if err != nil {
		return err
	}
	for _, id := range people {
		u, ok := users.GetImmutableUser(id)
		if !ok {
			continue
		}
		snapshot.Users = append(snapshot.Users, moderation.SnapshotUser{
			Id:        u.Id(),
			FirstName: u.FirstName(),
			LastName:  u.LastName(),
			Username:  u.Username(),
			About:     u.About(),
			Bot:       u.IsBot(),
		})
	}

	n, err := c.svcCtx.Reports.ReportsSince(c.ctx, c.MD.UserId, time.Now().Add(-time.Hour).Unix())
	if err != nil {
		return err
	}
	if n >= moderation.ReportsPerHour {
		return mtproto.NewErrFloodWaitX(3600)
	}
	return c.svcCtx.Reports.AddReport(c.ctx, &moderation.Report{
		ReporterUserId: c.MD.UserId,
		Kind:           moderation.KindReaction,
		PeerType:       mtproto.PEER_CHANNEL,
		PeerId:         peer.PeerId,
		TargetUserId:   target,
		Reason:         reactionReportReason,
	}, []int32{in.GetId()}, snapshot)
}

// canReportReaction: the creator and an admin who may ban report in any
// supergroup, anyone else in a public one.
func canReportReaction(ch *channelpb.ImmutableChannel, me *channelpb.ImmutableChannelParticipant, now int32) error {
	if ch.Can(me, channelperm.BanUsers, now) == nil || ch.Public() {
		return nil
	}
	return mtproto.ErrChatAdminRequired
}

// reactionOf is the user of reactionPeer, someone other than the reporter
// whose reaction to the message is among rows, and the keys of that user's
// reactions in the order chosen; anyone else is USER_ID_INVALID.
func reactionOf(reactionPeer *mtproto.InputPeer, self int64, rows []reactions.Row) (int64, []string, error) {
	if reactionPeer == nil {
		return 0, nil, mtproto.ErrUserIdInvalid
	}
	p := mtproto.FromInputPeer2(self, reactionPeer)
	if p.PeerType != mtproto.PEER_USER || p.PeerId == self {
		return 0, nil, mtproto.ErrUserIdInvalid
	}
	var mine []reactions.Row
	for _, r := range rows {
		if r.UserId == p.PeerId {
			mine = append(mine, r)
		}
	}
	slices.SortStableFunc(mine, func(a, b reactions.Row) int { return cmp.Compare(a.ChosenOrder, b.ChosenOrder) })
	keys := make([]string, 0, len(mine))
	for _, r := range mine {
		keys = append(keys, r.Reaction)
	}
	if len(keys) == 0 {
		return 0, nil, mtproto.ErrUserIdInvalid
	}
	return p.PeerId, keys, nil
}
