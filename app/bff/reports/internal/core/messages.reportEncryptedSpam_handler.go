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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// MessagesReportEncryptedSpam
// messages.reportEncryptedSpam#4b0c8c0f peer:InputEncryptedChat = Bool;
//
// The report names the user on the other end. The server cannot read the
// chat, so nothing of it goes with the report.
func (c *ReportsCore) MessagesReportEncryptedSpam(in *mtproto.TLMessagesReportEncryptedSpam) (*mtproto.Bool, error) {
	me := secretchat.Device{UserId: c.MD.UserId, AuthKey: c.MD.PermAuthKeyId}
	chat, err := c.svcCtx.Dao.SecretChats.Check(c.ctx, me, in.GetPeer().GetChatId(), in.GetPeer().GetAccessHash())
	if err != nil {
		if errors.Is(err, secretchat.ErrChatNotFound) || errors.Is(err, secretchat.ErrWrongDevice) {
			return nil, mtproto.ErrChatIdInvalid
		}
		c.Logger.Errorf("messages.reportEncryptedSpam - user %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	peerId := chat.Peer(me).UserId
	peer := mtproto.MakeUserPeerUtil(peerId)
	s := &moderation.Snapshot{}
	if err = c.fillSnapshot(s, peer, peerId); err == nil {
		err = c.file(&moderation.Report{
			Kind:         moderation.KindSpam,
			PeerType:     peer.PeerType,
			PeerId:       peerId,
			TargetUserId: peerId,
			Reason:       "spam",
		}, nil, s)
	}
	if err != nil {
		c.Logger.Errorf("messages.reportEncryptedSpam - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
