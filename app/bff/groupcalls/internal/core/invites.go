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
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"

	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The invitations to a conference (ADR 0075 §4). An invitation is the
// service message messageActionConferenceCall from the inviter in his
// private chat with the invitee, sent through msg as the message of a
// one-to-one call is. The clients ring on it, not on an updatePhoneCall:
// Android on a new incoming one without active or missed
// (NotificationsController.java:1088-1109), iOS while it has neither
// missed nor a duration (CallSessionManager.swift:486-500), tdesktop from
// History::applyServiceChanges (history.cpp:1527-1531), Web K on a new one
// at most 45 s old (conferenceInvitesController.ts:99, 303). An invitee
// with several devices rings on all of them. The ringing ends by an edit
// of the same message in both boxes (updateEditMessage to every device of
// both): active when the invitee joined, missed when he declined, the
// inviter cancelled or no one answered within InviteRingTimeout, the
// conference's duration when it ended after he joined. Android ends the
// ringing on active or missed (NotificationsController.java:1031-1035) and
// the inviter's "calling" with them (MessagesController.java:21022), iOS
// has no timer of its own for the invitee, tdesktop drops the invitation
// when the edit replaces the media (data_media_types.cpp:1839-1849), Web K
// on any edit (conferenceInvitesController.ts:103). The state moves first
// (groupcalls.MoveInvite, a conditional update), then the message is
// edited; an edit that failed is made again by the collector.

var metricInvites = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "groupcalls",
	Name:      "conference_invites_total",
	Help:      "Invitations to conferences: sent (ringing) and refused (privacy, kicked, already, flood); moves of their state (active, missed, ended); edits of their messages that failed (edit_failed).",
	Labels:    []string{"result"},
})

// otherParticipantsMax bounds other_participants of an invitation
// (decision 9 of task 117).
const otherParticipantsMax = 10

// PhoneInviteConferenceCallParticipant
// phone.inviteConferenceCallParticipant#bcf22685 flags:# video:flags.0?true call:InputGroupCall user_id:InputUser = Updates;
//
// Invites a user into a conference by one in it: any participant may
// (tdesktop calls_group_members.cpp:1840-1843), one who is not is
// GROUPCALL_FORBIDDEN, on which tdesktop joins again and repeats
// (calls_group_call.cpp:4052-4080). The invitee is refused as
// phone.requestCall refuses a callee: his privacy of calls or his block of
// the inviter is USER_PRIVACY_RESTRICTED (Android then offers the link,
// VoIPService.java:2656-2700), one in the call USER_ALREADY_PARTICIPANT,
// one the creator kicked USER_WAS_KICKED unless the creator invites him
// (decision 7). An invitation that still rings for him is replaced: the
// old message is missed and the new one rings. The answer is msg's answer
// to the inviter, with updateNewMessage of his message: Android and iOS take
// its id (VoIPService.java:2629-2655, GroupCalls.swift:1010-1014).
func (c *GroupCallsCore) PhoneInviteConferenceCallParticipant(in *mtproto.TLPhoneInviteConferenceCallParticipant) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	if !call.Conference {
		return nil, mtproto.ErrGroupcallForbidden
	}
	me, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) || (err == nil && !me.InCall) {
		return nil, mtproto.ErrGroupcallForbidden
	}
	if err != nil {
		return nil, err
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetUserId())
	if !peer.IsUser() || peer.PeerId == c.MD.UserId {
		return nil, mtproto.ErrUserIdInvalid
	}
	inviteeId := peer.PeerId
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id: []int64{c.MD.UserId, inviteeId}, Privacy: true, HasTo: true, To: []int64{c.MD.UserId, inviteeId},
	})
	if err != nil {
		return nil, err
	}
	inviter, ok := users.GetImmutableUser(c.MD.UserId)
	if !ok {
		return nil, mtproto.ErrInputUserDeactivated
	}
	invitee, ok := users.GetImmutableUser(inviteeId)
	if !ok || invitee.IsBot() {
		return nil, mtproto.ErrUserIdInvalid
	}
	if invitee.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	row, err := c.svcCtx.Dao.Store.ParticipantOf(c.ctx, call.Id, inviteeId)
	kicked := err == nil && row.Kicked
	switch {
	case err == nil && row.InCall:
		metricInvites.Inc("already")
		return nil, mtproto.ErrUserAlreadyParticipant
	case err == nil && row.Kicked && c.MD.UserId != call.CreatorId:
		metricInvites.Inc("kicked")
		return nil, errUserWasKicked
	case err != nil && !errors.Is(err, groupcalls.ErrNotFound):
		return nil, err
	}
	blocked, err := c.svcCtx.Dao.UserClient.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{UserId: inviteeId, PeerUserId: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	if mtproto.FromBool(blocked) || !invitee.CheckPrivacyFor(mtproto.PHONE_CALL, inviter) {
		metricInvites.Inc("privacy")
		return nil, mtproto.ErrUserPrivacyRestricted
	}
	sent, err := c.svcCtx.Dao.Store.InvitesSent(c.ctx, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if sent >= groupcalls.InvitesPerMinute {
		metricInvites.Inc("flood")
		return nil, mtproto.NewErrFloodWaitX(60)
	}
	if kicked {
		// The creator's own invitation lets him back.
		if err = c.svcCtx.Dao.Store.ClearKicked(c.ctx, call.Id, inviteeId); err != nil {
			return nil, err
		}
	}
	if old, err := c.svcCtx.Dao.Store.Ringing(c.ctx, call.Id, inviteeId); err == nil {
		c.moveInvite(old, groupcalls.InviteRinging, groupcalls.InviteMissed, 0)
	} else if !errors.Is(err, groupcalls.ErrNotFound) {
		return nil, err
	}
	others, err := c.otherParticipants(call, inviteeId)
	if err != nil {
		return nil, err
	}
	reply, msgId, err := c.sendInvite(call, inviteeId, in.GetVideo(), others)
	if err != nil {
		return nil, err
	}
	inv := &groupcalls.Invite{CallId: call.Id, InviterId: c.MD.UserId, InviteeId: inviteeId, InviterMsgId: msgId, Video: in.GetVideo()}
	box, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgId})
	if err == nil {
		inv.DialogMessageId = box.GetDialogMessageId()
		err = c.svcCtx.Dao.Store.AddInvite(c.ctx, inv)
	}
	if err != nil {
		// The message rings with no invitation behind it: iOS would ring
		// until an edit. It is made missed at once.
		c.Logger.Errorf("groupcalls: the invitation of %d to conference %d: %v", inviteeId, call.Id, err)
		if eerr := c.editInvite(c.MD.UserId, inviteeId, msgId, groupcalls.InviteMissed, 0, 0); eerr != nil {
			c.Logger.Errorf("groupcalls: a lost invitation to conference %d: %v", call.Id, eerr)
		}
		return nil, err
	}
	metricInvites.Inc("ringing")
	return reply, nil
}

// otherParticipants are those in the call but the inviter and the invitee,
// at most otherParticipantsMax: who the invitee is called to.
func (c *GroupCallsCore) otherParticipants(call *groupcalls.Call, inviteeId int64) ([]*mtproto.Peer, error) {
	in, err := c.svcCtx.Dao.Store.InCall(c.ctx, call.Id)
	if err != nil {
		return nil, err
	}
	out := []*mtproto.Peer{}
	for _, p := range in {
		if p.UserId == c.MD.UserId || p.UserId == inviteeId {
			continue
		}
		if len(out) == otherParticipantsMax {
			break
		}
		out = append(out, mtproto.MakePeerUser(p.UserId))
	}
	return out, nil
}

// sendInvite sends the invitation message from the account to inviteeId
// and returns msg's answer with the id of the message in his box.
func (c *GroupCallsCore) sendInvite(call *groupcalls.Call, inviteeId int64, video bool, others []*mtproto.Peer) (*mtproto.Updates, int32, error) {
	var rb [8]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return nil, 0, err
	}
	randomId := int64(binary.BigEndian.Uint64(rb[:]) >> 1)
	action := &mtproto.MessageAction{CallId: call.Id, Video: video}
	if len(others) > 0 {
		action.OtherParticipants = others
	}
	reply, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msg.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_USER,
		PeerId:    inviteeId,
		Message: []*msg.OutboxMessage{msg.MakeTLOutboxMessage(&msg.OutboxMessage{
			NoWebpage: true,
			RandomId:  randomId,
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: mtproto.MakePeerUser(inviteeId),
				Date:   int32(time.Now().Unix()),
				Action: mtproto.MakeTLMessageActionConferenceCall(action).To_MessageAction(),
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		return nil, 0, err
	}
	for _, u := range reply.GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			if m := u.GetMessage_MESSAGE(); m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionConferenceCall {
				return reply, m.GetId(), nil
			}
		case mtproto.Predicate_updateMessageID:
			if u.GetRandomId() == randomId {
				return reply, u.GetId_INT32(), nil
			}
		}
	}
	return nil, 0, errors.New("groupcalls: msg answered an invitation without its message")
}

// PhoneDeclineConferenceCallInvite
// phone.declineConferenceCallInvite#3c479971 msg_id:int = Updates;
//
// Ends the ringing of an invitation: msg_id is the invitation in the
// account's box, incoming (the invitee declines: Android
// VoIPGroupNotification.java:366-386, iOS CallSessionManager.swift:800-820,
// tdesktop calls_instance.cpp:1099-1120, Web K
// conferenceInvitesController.ts:394) or outgoing (the inviter cancels:
// Android GroupCallActivity.java:3717-3752, iOS CallSessionManager.swift:
// 784-798, tdesktop calls_instance.cpp:1122-1164). A ringing invitation is
// missed, and the edit goes to every device of both; one that rings no
// more (a repeat, a join that came first) changes nothing. The inviter's
// answer carries his edit; the invitee's edit reaches all his devices with
// the box of the inviter's edit, so his answer carries none.
func (c *GroupCallsCore) PhoneDeclineConferenceCallInvite(in *mtproto.TLPhoneDeclineConferenceCallInvite) (*mtproto.Updates, error) {
	inv, err := c.inviteOfMessage(in.GetMsgId())
	if err != nil {
		return nil, err
	}
	after, moved, err := c.svcCtx.Dao.Store.MoveInvite(c.ctx, inv.Id, groupcalls.InviteRinging, groupcalls.InviteMissed, 0)
	if err != nil {
		return nil, err
	}
	empty := c.envelope(c.MD.UserId, []*mtproto.Update{}, nil, nil)
	if !moved {
		return empty, nil
	}
	metricInvites.Inc("missed")
	if c.MD.UserId != after.InviterId {
		c.showInvite(after)
		return empty, nil
	}
	reply, err := c.editInviteReply(after, c.MD.PermAuthKeyId)
	if err != nil {
		c.inviteEditFailed(after, err)
		return empty, nil
	}
	c.markShown(after)
	return reply, nil
}

// conferenceJoined is what a join of a conference changes besides the
// call: it is not empty, and an invitation of the account that rings is
// answered.
func (c *GroupCallsCore) conferenceJoined(call *groupcalls.Call) {
	if err := c.svcCtx.Dao.Store.MarkJoined(c.ctx, call.Id); err != nil {
		c.Logger.Errorf("groupcalls: conference %d: %v", call.Id, err)
	}
	inv, err := c.svcCtx.Dao.Store.Ringing(c.ctx, call.Id, c.MD.UserId)
	if errors.Is(err, groupcalls.ErrNotFound) {
		return
	}
	if err != nil {
		c.Logger.Errorf("groupcalls: the invitation of %d to conference %d: %v", c.MD.UserId, call.Id, err)
		return
	}
	c.moveInvite(inv, groupcalls.InviteRinging, groupcalls.InviteActive, 0)
}

// endInvites moves the invitations of a conference that ended: ringing
// ones are missed, those of the invitees who joined get its duration
// (decision 6 of task 117).
func (c *GroupCallsCore) endInvites(call *groupcalls.Call) {
	for _, move := range []struct{ from, to groupcalls.InviteState }{
		{groupcalls.InviteRinging, groupcalls.InviteMissed},
		{groupcalls.InviteActive, groupcalls.InviteEnded},
	} {
		list, err := c.svcCtx.Dao.Store.InvitesIn(c.ctx, call.Id, move.from)
		if err != nil {
			c.Logger.Errorf("groupcalls: invitations of conference %d: %v", call.Id, err)
			continue
		}
		for _, inv := range list {
			c.moveInvite(inv, move.from, move.to, call.Duration)
		}
	}
}

// moveInvite moves an invitation and edits its message.
func (c *GroupCallsCore) moveInvite(inv *groupcalls.Invite, from, to groupcalls.InviteState, duration int32) {
	after, moved, err := c.svcCtx.Dao.Store.MoveInvite(c.ctx, inv.Id, from, to, duration)
	if err != nil {
		c.Logger.Errorf("groupcalls: invitation %d: %v", inv.Id, err)
		return
	}
	if !moved {
		return
	}
	metricInvites.Inc(inviteResult(to))
	c.showInvite(after)
}

func inviteResult(s groupcalls.InviteState) string {
	switch s {
	case groupcalls.InviteActive:
		return "active"
	case groupcalls.InviteMissed:
		return "missed"
	case groupcalls.InviteEnded:
		return "ended"
	}
	return "ringing"
}

// showInvite edits the message of an invitation to its state, to every
// device of both. A failure is logged and left to the collector.
func (c *GroupCallsCore) showInvite(inv *groupcalls.Invite) {
	if _, err := c.editInviteReply(inv, 0); err != nil {
		c.inviteEditFailed(inv, err)
		return
	}
	c.markShown(inv)
}

func (c *GroupCallsCore) markShown(inv *groupcalls.Invite) {
	if err := c.svcCtx.Dao.Store.MarkShown(c.ctx, inv.Id, inv.State); err != nil {
		c.Logger.Errorf("groupcalls: invitation %d: %v", inv.Id, err)
	}
}

func (c *GroupCallsCore) inviteEditFailed(inv *groupcalls.Invite, err error) {
	if errors.Is(err, errInviteGone) {
		// The inviter deleted it: there is nothing to edit, and the
		// clients stop ringing on the deletion.
		c.markShown(inv)
		return
	}
	metricInvites.Inc("edit_failed")
	c.Logger.Errorf("groupcalls: the message of invitation %d: %v", inv.Id, err)
}

// errInviteGone: the message of an invitation is deleted.
var errInviteGone = errors.New("groupcalls: the invitation message is deleted")

// editInviteReply edits the message of inv to its state from the inviter's
// box; authKeyId is the device the answer goes to (0: every device by the
// updates).
func (c *GroupCallsCore) editInviteReply(inv *groupcalls.Invite, authKeyId int64) (*mtproto.Updates, error) {
	reply, err := c.editInviteMessage(inv.InviterId, inv.InviteeId, inv.InviterMsgId, inv.State, inv.Duration, authKeyId)
	return reply, err
}

// editInvite edits an invitation message whose invitation is not stored.
func (c *GroupCallsCore) editInvite(inviterId, inviteeId int64, msgId int32, state groupcalls.InviteState, duration int32, authKeyId int64) error {
	_, err := c.editInviteMessage(inviterId, inviteeId, msgId, state, duration, authKeyId)
	return err
}

// editInviteMessage is the edit of an invitation message: the action as
// sent with the flags of state. It is the inviter's edit of his message
// (msg.editMessageV2), which msg applies to the invitee's copy; edit_date
// is not set, as the message is a service one.
func (c *GroupCallsCore) editInviteMessage(inviterId, inviteeId int64, msgId int32, state groupcalls.InviteState, duration int32, authKeyId int64) (*mtproto.Updates, error) {
	box, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: inviterId, Id: msgId})
	if err != nil && !isAPIError(err, mtproto.ErrMessageIdInvalid) {
		return nil, err
	}
	if err != nil || box.GetMessage() == nil {
		return nil, errInviteGone
	}
	m := proto.Clone(box.GetMessage()).(*mtproto.Message)
	a := m.GetAction()
	if a.GetPredicateName() != mtproto.Predicate_messageActionConferenceCall {
		return nil, errInviteGone
	}
	a.Active, a.Missed, a.Duration = false, false, nil
	switch state {
	case groupcalls.InviteActive:
		a.Active = true
	case groupcalls.InviteMissed:
		a.Missed = true
	case groupcalls.InviteEnded:
		a.Duration = wrapperspb.Int32(duration)
	}
	m.EditDate = nil
	reply, err := c.svcCtx.Dao.MsgClient.MsgEditMessageV2(c.ctx, &msg.TLMsgEditMessageV2{
		UserId:     inviterId,
		AuthKeyId:  authKeyId,
		PeerType:   mtproto.PEER_USER,
		PeerId:     inviteeId,
		EditType:   msg.EditTypeNormal,
		NewMessage: msg.MakeTLOutboxMessage(&msg.OutboxMessage{NoWebpage: true, Message: m}).To_OutboxMessage(),
		DstMessage: box,
	})
	if isAPIError(err, mtproto.ErrMessageIdInvalid) {
		return nil, errInviteGone
	}
	return reply, err
}

// isAPIError tells whether err, as it came from another service, is the
// error of the API want.
func isAPIError(err, want error) bool {
	if err == nil {
		return false
	}
	got, ok := status.FromError(err)
	w, _ := status.FromError(want)
	return ok && got.Code() == w.Code() && got.Message() == w.Message()
}

// sweepInvites moves the invitations that rang out to missed and edits
// again the messages of those whose edit failed.
func (c *GroupCallsCore) sweepInvites() {
	unanswered, err := c.svcCtx.Dao.Store.Unanswered(c.ctx, collectBatch)
	if err != nil {
		c.Logger.Errorf("groupcalls: unanswered invitations: %v", err)
	}
	for _, inv := range unanswered {
		c.moveInvite(inv, groupcalls.InviteRinging, groupcalls.InviteMissed, 0)
	}
	unshown, err := c.svcCtx.Dao.Store.Unshown(c.ctx, time.Now().Add(-overKeep), collectBatch)
	if err != nil {
		c.Logger.Errorf("groupcalls: unshown invitations: %v", err)
	}
	for _, inv := range unshown {
		c.showInvite(inv)
	}
}
