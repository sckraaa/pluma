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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/scheduledmessages/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ScheduledMessagesCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *ScheduledMessagesCore {
	return &ScheduledMessagesCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// peerOf is the dialog an InputPeer names, Saved Messages as the private
// chat with oneself, as the boxes keep it.
func (c *ScheduledMessagesCore) peerOf(in *mtproto.InputPeer) (int32, int64, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch peer.PeerType {
	case mtproto.PEER_SELF:
		return mtproto.PEER_USER, c.MD.UserId, nil
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		return peer.PeerType, peer.PeerId, nil
	}
	return 0, 0, mtproto.ErrPeerIdInvalid
}
