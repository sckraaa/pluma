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
	"context"
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/secretchat"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// maxPayload is the longest data of one message. The documentation names no
// limit but DATA_TOO_LONG; the longest DecryptedMessage the clients produce
// (a caption, entities and a web page, Android SecretChatHelper) is far
// below this.
const maxPayload = 256 << 10

// floodWait is the wait of a request over a limit of the store, seconds.
const floodWait = 3600

// errUserIsBlocked is 403, as core.telegram.org/method/messages.requestEncryption
// lists it (mtproto.ErrUserIsBlocked is 400).
var errUserIsBlocked = status.Error(mtproto.ErrForbidden, "USER_IS_BLOCKED")

// randomIdDuplicate is 400 as the same page lists it (mtproto.ErrRandomIdDuplicate
// is 500, which tgnet would retry).
var randomIdDuplicate = status.Error(mtproto.ErrBadRequest, "RANDOM_ID_DUPLICATE")

// SecretChatsCore handles one request.
type SecretChatsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

// New returns the core of a request.
func New(ctx context.Context, svcCtx *svc.ServiceContext) *SecretChatsCore {
	return &SecretChatsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

func (c *SecretChatsCore) device() secretchat.Device {
	return secretchat.Device{UserId: c.MD.UserId, AuthKey: c.MD.PermAuthKeyId}
}

// checkUser refuses bots: "Only users can use this method".
func (c *SecretChatsCore) checkUser() error {
	if c.MD.IsBot {
		return mtproto.ErrBotMethodInvalid
	}
	return nil
}

// rpcError is the error of the method for an error of the store.
func (c *SecretChatsCore) rpcError(name string, err error) error {
	switch {
	case errors.Is(err, secretchat.ErrChatNotFound):
		return mtproto.ErrChatIdInvalid
	case errors.Is(err, secretchat.ErrWrongDevice):
		return mtproto.ErrEncryptionIdInvalid
	case errors.Is(err, secretchat.ErrDeclined):
		return mtproto.ErrEncryptionDeclined
	case errors.Is(err, secretchat.ErrAlreadyAccepted):
		return mtproto.ErrEncryptionAlreadyAccepted
	case errors.Is(err, secretchat.ErrAlreadyDeclined):
		return mtproto.ErrEncryptionAlreadyDeclined
	case errors.Is(err, secretchat.ErrRandomIdUsed):
		return randomIdDuplicate
	case errors.Is(err, secretchat.ErrRequestFlood), errors.Is(err, secretchat.ErrQueueFull):
		// The clients show a failed request or message (iOS
		// ManagedSecretChatOutgoingOperations.swift:2173, CreateSecretChat.swift:34).
		c.Logger.Errorf("%s - user %d: %v", name, c.MD.UserId, err)
		return mtproto.NewErrFloodWaitX(floodWait)
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	c.Logger.Errorf("%s - user %d: %v", name, c.MD.UserId, err)
	return mtproto.ErrInternalServerError
}

func (c *SecretChatsCore) blockedBy(peerId int64) (bool, error) {
	blocked, err := c.svcCtx.Dao.UserClient.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{
		UserId:     peerId,
		PeerUserId: c.MD.UserId,
	})
	if err != nil {
		return false, err
	}
	return mtproto.FromBool(blocked), nil
}

// push sends updates to one authorization: a row of the outbox, durable
// once written. A failure is logged and left to updates.getDifference.
func push(ctx context.Context, svcCtx *svc.ServiceContext, d secretchat.Device, users []*mtproto.User, list ...*mtproto.Update) {
	if d.AuthKey == 0 {
		return
	}
	if users == nil {
		users = []*mtproto.User{}
	}
	_, err := svcCtx.Dao.SyncClient.SyncUpdatesMe(ctx, &sync.TLSyncUpdatesMe{
		UserId:        d.UserId,
		PermAuthKeyId: d.AuthKey,
		Updates: mtproto.MakeTLUpdates(&mtproto.Updates{
			Updates: list,
			Users:   users,
			Chats:   []*mtproto.Chat{},
			Date:    now(),
			Seq:     0,
		}).To_Updates(),
	})
	if err != nil {
		logx.WithContext(ctx).Errorf("secretchats: push to user %d: %v", d.UserId, err)
	}
}

func (c *SecretChatsCore) push(d secretchat.Device, users []*mtproto.User, list ...*mtproto.Update) {
	push(c.ctx, c.svcCtx, d, users, list...)
}

// usersOf is the two ends of a chat as viewer sees them: a client needs
// them to show a new chat; a failure leaves them for it to fetch.
func (c *SecretChatsCore) usersOf(viewer, adminId, participantId int64) []*mtproto.User {
	ids := []int64{adminId, participantId}
	list, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids})
	if err != nil {
		c.Logger.Errorf("secretchats - users of a chat: %v", err)
		return []*mtproto.User{}
	}
	return list.GetUserListByIdList(viewer, ids...)
}

// now is the time of a push.
func now() int32 { return int32(time.Now().Unix()) }
