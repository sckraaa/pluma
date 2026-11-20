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
	"time"

	"github.com/teamgram/proto/mtproto"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/env2"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

// A conference (task 117, ADR 0075) is a group call without a chat: the
// same join, leave, chain, SFU and collector as a call of a chat, with its
// own policy. Its participants, its creator, whoever comes with the slug of
// its link and whoever it invited see it and may join; any participant
// invites; only the creator manages it (groupCall.creator: he ends it for
// everyone, kicks, resets the link). One the creator kicked does not come
// back until the creator invites him. Every conference is end-to-end
// encrypted as every group call is (ADR 0060 §2): a join without a key and
// a block is refused, and the invitations and the links write no blocks.

var (
	// errConferenceLimit: the creator has groupcalls.ConferencesPerCreator
	// conferences that are not over (decision 10 of task 117; the code
	// Telegram uses is not known, the clients show a general error).
	errConferenceLimit = status.Error(mtproto.ErrBadRequest, "CONFERENCE_LIMIT_EXCEEDED")
	// errUserWasKicked: the creator kicked the invitee, and another
	// participant invites him (tdesktop calls_group_call.cpp:4052-4080).
	errUserWasKicked = status.Error(mtproto.ErrBadRequest, "USER_WAS_KICKED")
)

var (
	metricConferences = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "conferences_total",
		Help:      "Conferences created (created: link, joined) and ended (ended: discard, empty).",
		Labels:    []string{"event"},
	})
	metricConferencesLive = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "groupcalls",
		Name:      "conferences_live",
		Help:      "Conferences that are not over.",
	})
)

// inviteLink is the link of a conference, "" for a call of a chat:
// https://<t.me>/call/<slug>, which all four clients open in the app
// (Android LaunchActivity.java:2127-2128, iOS UrlHandling.swift:522-527,
// tdesktop local_url_handlers.cpp:2220-2222, Web K
// internalLinkProcessor.ts:1565-1576), the clients of Pluma with pluma.chat
// for t.me (ci/patch.py of each).
func inviteLink(call *groupcalls.Call) string {
	if !call.Conference || call.Slug == "" {
		return ""
	}
	return "https://" + env2.TDotMe + "/call/" + call.Slug
}

// conferenceAccess is the policy of a conference for the accounts a step
// loaded.
type conferenceAccess struct {
	creatorId int64
	viewers   map[int64]bool
}

// conferenceAccess loads who of users may see the conference call.
func (c *GroupCallsCore) conferenceAccess(call *groupcalls.Call, users ...int64) (*access, error) {
	ca := &conferenceAccess{creatorId: call.CreatorId, viewers: map[int64]bool{}}
	for _, userId := range uniq(users) {
		ok, err := c.mayView(call, userId)
		if err != nil {
			return nil, err
		}
		ca.viewers[userId] = ok
	}
	return &access{conf: ca}, nil
}

// mayView: userId was not kicked and is the creator, has been in the call,
// came with a reference that grants it (the slug, his invitation) or was
// invited to it. Android joins by a tap on an old invitation
// (ChatActivity.java:40439-40470), so an invitation in any state counts
// while the conference lasts.
func (c *GroupCallsCore) mayView(call *groupcalls.Call, userId int64) (bool, error) {
	p, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, userId)
	switch {
	case err == nil && p.Kicked:
		return false, nil
	case err == nil, userId == call.CreatorId:
		return true, nil
	case !errors.Is(err, groupcalls.ErrNotFound):
		return false, err
	case userId == c.MD.UserId && c.granted == call.Id:
		return true, nil
	}
	return c.svcCtx.Dao.Store.Invited(c.ctx, call.Id, userId)
}

// loadBySlug is the conference of a slug. A slug that was reset, or of a
// conference over for longer than ConferenceSlugKeep, is GROUPCALL_INVALID:
// Android shows "the link is not active" (GroupCallSheet.java:59-120),
// tdesktop lng_confcall_not_accessible (calls_group_call.cpp:1835-1858).
func (c *GroupCallsCore) loadBySlug(slug string) (*groupcalls.Call, error) {
	call, err := c.svcCtx.Dao.Store.BySlug(c.ctx, slug)
	if errors.Is(err, groupcalls.ErrNotFound) {
		return nil, mtproto.ErrGroupcallInvalid
	}
	if err != nil {
		return nil, err
	}
	if call.Discarded && time.Since(time.Unix(int64(call.EndDate), 0)) > groupcalls.ConferenceSlugKeep {
		return nil, mtproto.ErrGroupcallInvalid
	}
	c.granted = call.Id
	return call, nil
}

// loadByInvite is the conference of an invitation message msgId in the
// account's box, incoming or outgoing (iOS takes .message for a user's
// cloud message, GroupCalls.swift:209-212).
func (c *GroupCallsCore) loadByInvite(msgId int32) (*groupcalls.Call, error) {
	inv, err := c.inviteOfMessage(msgId)
	if err != nil {
		if errors.Is(err, mtproto.ErrMessageIdInvalid) {
			return nil, mtproto.ErrGroupcallInvalid
		}
		return nil, err
	}
	call, err := c.svcCtx.Dao.Store.Get(c.ctx, inv.CallId)
	if errors.Is(err, groupcalls.ErrNotFound) {
		return nil, mtproto.ErrGroupcallInvalid
	}
	if err != nil {
		return nil, err
	}
	c.granted = call.Id
	return call, nil
}

// inviteOfMessage is the invitation of the message msgId in the account's
// box; MESSAGE_ID_INVALID for a message that is none of his invitations.
func (c *GroupCallsCore) inviteOfMessage(msgId int32) (*groupcalls.Invite, error) {
	box, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgId})
	if err != nil && !isAPIError(err, mtproto.ErrMessageIdInvalid) {
		return nil, err
	}
	if err != nil || box.GetMessage() == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	if box.GetPeerType() != mtproto.PEER_USER || box.GetMessage().GetAction().GetPredicateName() != mtproto.Predicate_messageActionConferenceCall {
		return nil, mtproto.ErrMessageIdInvalid
	}
	inv, err := c.svcCtx.Dao.Store.InviteOf(c.ctx, box.GetDialogMessageId(), c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return inv, err
}

// endConference finishes what the end of a conference leaves: its room,
// its invitations (ringing: missed; active: the conference's duration),
// and the updates to those who were in it. after is the call over, left
// those who were in it.
func (c *GroupCallsCore) endConference(a *access, after *groupcalls.Call, left []*groupcalls.Participant, reason string) *access {
	metricConferences.Inc(reason)
	c.svcCtx.Members.Forget(after.Id)
	var gone []int64
	for _, p := range left {
		metricLeaves.Inc("discard")
		gone = append(gone, p.UserId)
	}
	c.closeRoom(after)
	c.endInvites(after)
	return c.deliver(a, after, fanOut{call: true, left: gone})
}

// PhoneCreateConferenceCall7D0444BB
// phone.createConferenceCall#7d0444bb flags:# muted:flags.0?true video_stopped:flags.2?true join:flags.3?true random_id:int public_key:flags.3?int256 block:flags.3?bytes params:flags.3?DataJSON = Updates;
//
// Creates a conference of the account. Without join (a link made ahead:
// Android CreateGroupCallSheet.java:181-226, tdesktop
// calls_group_common.cpp:498-503, iOS GroupCalls.swift:3348, Web K
// appGroupCallsManager.ts:1563-1580) the chain is empty and the answer is
// one updateGroupCall with conference, creator and invite_link: all four
// look for it (tdesktop data_session.cpp:1472-1500 "Call not found!", iOS
// GroupCalls.swift:3352-3354, Web K groupCallUpdates.ts). With join (tdesktop
// calls_group_call.cpp:1732-1777, Web K groupCallsController.ts:803-812)
// the creator's zero block and his row go in the transaction of the call,
// and the answer is that of phone.joinGroupCall (tdesktop takes it as one,
// calls_group_call.cpp:1782-1832): the call at version 2, the connection,
// his row and both sub-chains, with the SFU's addresses of his country
// (routes, ADR 0074). A repeat of the request (random_id) gets the same
// conference. An SFU that refuses the creator ends it: there is no one to
// keep it for, and Web K discards it itself then (appGroupCallsManager.ts).
func (c *GroupCallsCore) PhoneCreateConferenceCall7D0444BB(in *mtproto.TLPhoneCreateConferenceCall7D0444BB) (*mtproto.Updates, error) {
	if in.GetJoin() {
		return c.createJoined(in)
	}
	slug, err := groupcalls.NewSlug()
	if err != nil {
		return nil, err
	}
	id, hash, err := newCallIds()
	if err != nil {
		return nil, err
	}
	call, created, _, err := c.svcCtx.Dao.Store.CreateConference(c.ctx, groupcalls.ConferenceParams{
		Id: id, AccessHash: hash, CreatorId: c.MD.UserId, RandomId: in.GetRandomId(), Slug: slug,
	})
	if err != nil {
		return nil, storeError(err)
	}
	if created {
		metricConferences.Inc("link")
	}
	if call.Discarded {
		return nil, mtproto.ErrGroupcallAlreadyDiscarded
	}
	a, err := c.accessOf(call, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	return c.answer(a, call, nil, nil, true), nil
}

// createJoined is phone.createConferenceCall with join.
func (c *GroupCallsCore) createJoined(in *mtproto.TLPhoneCreateConferenceCall7D0444BB) (*mtproto.Updates, error) {
	if len(in.GetPublicKey()) == 0 || len(in.GetBlock()) == 0 {
		return nil, mtproto.ErrGroupcallForbidden
	}
	if len(in.GetPublicKey()) != 32 {
		return nil, errBlockInvalid
	}
	if !c.svcCtx.SFU.Enabled() {
		return nil, errNotConfigured
	}
	params := []byte(in.GetParams().GetData())
	req, err := tgcallsJoin(params)
	if err != nil {
		return nil, err
	}
	endpoint, err := newEndpoint()
	if err != nil {
		return nil, err
	}
	slug, err := groupcalls.NewSlug()
	if err != nil {
		return nil, err
	}
	id, hash, err := newCallIds()
	if err != nil {
		return nil, err
	}
	routes := c.routes()
	join := &groupcalls.JoinParams{
		UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId,
		Source: req.SSRC, Video: groupcalls.MediaOf(req, endpoint), VideoStopped: in.GetVideoStopped() || !req.HasVideo(),
		State: func(call *groupcalls.Call, prev *groupcalls.Participant) (bool, bool) {
			return groupcalls.JoinState(call, prev, true, false, in.GetMuted())
		},
		VideoLimit: c.svcCtx.VideoLimit,
		Chain:      &groupcalls.ChainJoin{PublicKey: [32]byte(in.GetPublicKey()), Block: in.GetBlock()},
	}
	call, created, res, err := c.svcCtx.Dao.Store.CreateConference(c.ctx, groupcalls.ConferenceParams{
		Id: id, AccessHash: hash, CreatorId: c.MD.UserId, RandomId: in.GetRandomId(), Slug: slug, Join: join,
	})
	if err != nil {
		return nil, chainError(err)
	}
	if created {
		metricConferences.Inc("joined")
	}
	a, err := c.accessOf(call, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	// The call as it was before the join, for what the join changed.
	before := *res.Call
	if res.Changes[0].JustJoined {
		before.ParticipantsCount--
	}
	answer, err := c.finishJoin(a, &before, res, joined{
		params: params, source: req.SSRC, endpoint: endpoint, routes: routes,
		// The creator was in no chain of it: the block is his first.
		wasMember: false,
	})
	if err != nil {
		if created {
			// The SFU refused the one the conference was made for.
			if after, left, won, derr := c.svcCtx.Dao.Store.Discard(c.ctx, call.Id); derr != nil {
				c.Logger.Errorf("groupcalls: end conference %d the SFU refused: %v", call.Id, derr)
			} else if won {
				c.endConference(a, after, left, "refused")
			}
		}
		return nil, err
	}
	return answer, nil
}

// newCallIds are the id and the access_hash of a new call.
func newCallIds() (int64, int64, error) {
	id, err := positiveId()
	if err != nil {
		return 0, 0, err
	}
	hash, err := positiveId()
	return id, hash, err
}

// conferenceOnly refuses a method that has no sense for a conference
// (decision 11 of task 117): the title, the invitations of a chat, the
// schedule. The clients do not offer them for one.
func conferenceOnly(call *groupcalls.Call) error {
	if call.Conference {
		return mtproto.ErrGroupcallForbidden
	}
	return nil
}

// countConferences sets the gauge of the conferences that are not over.
func (c *GroupCallsCore) countConferences() {
	n, err := c.svcCtx.Dao.Store.ConferencesLive(c.ctx)
	if err != nil {
		c.Logger.Errorf("groupcalls: live conferences: %v", err)
		return
	}
	metricConferencesLive.Set(float64(n))
}

// sweepConferences ends the conferences no one is in for long enough
// (groupcalls.EmptyEnds): ConferenceEmptyEnd after the last one left,
// ConferenceUnusedEnd after one no one ever joined was created.
func (c *GroupCallsCore) sweepConferences() {
	now := time.Now()
	for after := int64(0); c.ctx.Err() == nil; {
		list, err := c.svcCtx.Dao.Store.LiveConferences(c.ctx, after, collectBatch)
		if err != nil {
			c.Logger.Errorf("groupcalls: live conferences: %v", err)
			return
		}
		for _, lc := range list {
			after = lc.Call.Id
			c.sweepConference(lc, now)
		}
		if len(list) < collectBatch {
			break
		}
	}
	c.countConferences()
}

func (c *GroupCallsCore) sweepConference(lc *groupcalls.LiveConference, now time.Time) {
	call := lc.Call
	switch {
	case call.ParticipantsCount > 0:
		if lc.EmptySince != 0 {
			if err := c.svcCtx.Dao.Store.MarkJoined(c.ctx, call.Id); err != nil {
				c.Logger.Errorf("groupcalls: conference %d: %v", call.Id, err)
			}
		}
		return
	case lc.EmptySince == 0:
		if err := c.svcCtx.Dao.Store.MarkEmpty(c.ctx, call.Id, now); err != nil {
			c.Logger.Errorf("groupcalls: conference %d: %v", call.Id, err)
		}
		return
	case !groupcalls.EmptyEnds(lc, now):
		return
	}
	after, won, err := c.svcCtx.Dao.Store.DiscardEmpty(c.ctx, call.Id)
	if err != nil {
		c.Logger.Errorf("groupcalls: end the empty conference %d: %v", call.Id, err)
		return
	}
	if !won {
		return
	}
	a, err := c.accessOf(after)
	if err != nil {
		c.Logger.Errorf("groupcalls: conference %d: %v", call.Id, err)
		return
	}
	c.endConference(a, after, nil, "empty")
}
