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

// Package core is the phone.* methods of one-to-one calls (task 068, ADR
// 0004). What the clients expect of each step is cited at the step.
package core

import (
	"context"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	statuspb "github.com/teamgram/teamgram-server/app/service/status/status"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Errors of core.telegram.org/method/phone.* that the mtproto package does
// not define.
var (
	errCallNotActive     = status.Error(mtproto.ErrBadRequest, "CALL_NOT_ACTIVE")
	errProtocolLayer     = status.Error(mtproto.ErrBadRequest, "CALL_PROTOCOL_LAYER_INVALID")
	errBlocked           = status.Error(mtproto.ErrForbidden, "USER_IS_BLOCKED")
	errCallsNotAvailable = status.Error(mtproto.ErrBadRequest, "CALLS_NOT_CONFIGURED")
)

type VoipCallsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *VoipCallsCore {
	return &VoipCallsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// load returns the call peer names, as it is to the caller, CALL_PEER_INVALID
// for anything else.
func (c *VoipCallsCore) load(peer *mtproto.InputPhoneCall) (*phonecalls.Call, phonecalls.Role, error) {
	call, err := c.svcCtx.Dao.Store.Get(c.ctx, peer.GetId())
	if errors.Is(err, phonecalls.ErrNotFound) {
		return nil, 0, mtproto.ErrCallPeerInvalid
	}
	if err != nil {
		return nil, 0, err
	}
	role := call.RoleOf(c.MD.UserId)
	if role == 0 || call.AccessHash != peer.GetAccessHash() {
		return nil, 0, mtproto.ErrCallPeerInvalid
	}
	return call, role, nil
}

// usersOf returns ids as viewer sees them. A missing peer list only leaves
// the client to fetch it: it never fails the step.
func (c *VoipCallsCore) usersOf(viewer int64, ids ...int64) []*mtproto.User {
	list := append([]int64{viewer}, ids...)
	r, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: list, To: []int64{viewer}})
	if err != nil {
		c.Logger.Errorf("calls: the users of a call: %v", err)
		return []*mtproto.User{}
	}
	return r.GetUserListByIdList(viewer, ids...)
}

// updatePhoneCall wraps the update of call for a viewer with its peers.
func (c *VoipCallsCore) phoneCallUpdates(viewer int64, phoneCall *mtproto.PhoneCall, ids ...int64) *mtproto.Updates {
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdatePhoneCall(&mtproto.Update{PhoneCall: phoneCall}).To_Update()},
		Users:   c.usersOf(viewer, ids...),
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
		Seq:     0,
	}).To_Updates()
}

// toAuth pushes updates to one device of userId: the one bound to the call.
// A device that is offline right now loses them, as a lost connection would
// lose any other update; the clients have timers for that.
func (c *VoipCallsCore) toAuth(userId, authKeyId int64, updates *mtproto.Updates) {
	list, err := c.svcCtx.Dao.StatusClient.StatusGetUserOnlineSessions(c.ctx, &statuspb.TLStatusGetUserOnlineSessions{UserId: userId})
	if err != nil {
		c.Logger.Errorf("calls: online sessions of user %d: %v", userId, err)
		return
	}
	for _, sess := range list.GetUserSessions() {
		if sess.GetAuthKeyId() != authKeyId && sess.GetPermAuthKeyId() != authKeyId {
			continue
		}
		_, err := c.svcCtx.Dao.SyncClient.SyncUpdatesMe(c.ctx, &syncpb.TLSyncUpdatesMe{
			UserId:        userId,
			PermAuthKeyId: authKeyId,
			ServerId:      &wrapperspb.StringValue{Value: sess.GetGateway()},
			Updates:       updates,
		})
		if err != nil {
			c.Logger.Errorf("calls: push to user %d: %v", userId, err)
		}
	}
}

// otherAuth is the device of the other side of call for role.
func otherAuth(call *phonecalls.Call, role phonecalls.Role) (int64, int64) {
	if role == phonecalls.Admin {
		return call.ParticipantId, call.ParticipantAuthKeyId
	}
	return call.AdminId, call.AdminAuthKeyId
}

// finish tells both sides that a call ended, excluding the device of the
// discard call itself, and writes the history. The discard that ran it holds
// state: a duplicate discard does not reach here.
func (c *VoipCallsCore) finish(call *phonecalls.Call, wasRequested bool, by phonecalls.Role, byAuth int64) {
	discarded := func(role phonecalls.Role) *mtproto.Updates {
		viewer, other := call.AdminId, call.ParticipantId
		if role == phonecalls.Participant {
			viewer, other = other, viewer
		}
		return c.phoneCallUpdates(viewer, call.TL(role, nil), other)
	}
	// The admin's device: the one bound to the call.
	if by != phonecalls.Admin {
		c.toAuth(call.AdminId, call.AdminAuthKeyId, discarded(phonecalls.Admin))
	}
	switch {
	case wasRequested:
		// Every device of the participant is ringing: all of them hear, or
		// all but the one that declined.
		if by == phonecalls.Participant {
			c.syncNotMe(call.ParticipantId, byAuth, discarded(phonecalls.Participant))
		} else {
			c.syncAll(call.ParticipantId, discarded(phonecalls.Participant))
		}
	case by != phonecalls.Participant:
		c.toAuth(call.ParticipantId, call.ParticipantAuthKeyId, discarded(phonecalls.Participant))
	}
	c.postMessage(call)
}

// syncNotMe and syncAll log the error: losing one fan-out must not lose the
// other side or the history.
func (c *VoipCallsCore) syncNotMe(userId, permAuthKeyId int64, updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &syncpb.TLSyncUpdatesNotMe{
		UserId:        userId,
		PermAuthKeyId: permAuthKeyId,
		Updates:       updates,
	}); err != nil {
		c.Logger.Errorf("calls: push to user %d: %v", userId, err)
	}
}

func (c *VoipCallsCore) syncAll(userId int64, updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &syncpb.TLSyncPushUpdates{
		UserId:  userId,
		Updates: updates,
	}); err != nil {
		c.Logger.Errorf("calls: push to user %d: %v", userId, err)
	}
}

// postMessage writes messageActionPhoneCall to the history of both users,
// from the admin. Its random_id is the call id: when the sweep retries a
// send that was only lost on the way, msg answers with the first message
// (dao.SentBefore). The two updates go to every device of both users, the
// device of the discard among them.
func (c *VoipCallsCore) postMessage(call *phonecalls.Call) {
	reason := phonecalls.TLReason(call.Reason)
	out := msg.MakeTLOutboxMessage(&msg.OutboxMessage{
		NoWebpage: true,
		RandomId:  call.Id,
		Message: mtproto.MakeTLMessageService(&mtproto.Message{
			Out:    true,
			FromId: mtproto.MakePeerUser(call.AdminId),
			PeerId: mtproto.MakePeerUser(call.ParticipantId),
			Date:   int32(time.Now().Unix()),
			Action: mtproto.MakeMessageActionPhoneCall(call.Video, call.Id, reason, call.Duration),
		}).To_Message(),
	}).To_OutboxMessage()
	if _, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msg.TLMsgSendMessageV2{
		UserId:   call.AdminId,
		PeerType: mtproto.PEER_USER,
		PeerId:   call.ParticipantId,
		Message:  []*msg.OutboxMessage{out},
	}); err != nil {
		// The sweep sends it again: the message of a call must not depend on
		// the moment the call ended.
		c.Logger.Errorf("calls: the history message of call %d: %v", call.Id, err)
		return
	}
	if err := c.svcCtx.Dao.Store.MarkPosted(c.ctx, call.Id); err != nil {
		c.Logger.Errorf("calls: posting of call %d: %v", call.Id, err)
	}
}
