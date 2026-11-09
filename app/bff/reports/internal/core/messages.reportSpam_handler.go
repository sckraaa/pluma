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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// MessagesReportSpam
// messages.reportSpam#cf1592db peer:InputPeer = Bool;
//
// "This is spam" of a chat: no message or text comes with it, so a report of
// a private chat keeps the last messages as the evidence.
func (c *ReportsCore) MessagesReportSpam(in *mtproto.TLMessagesReportSpam) (*mtproto.Bool, error) {
	peer, err := peerOf(c.MD.UserId, in.Peer)
	if err != nil {
		return nil, err
	}
	s := &moderation.Snapshot{}
	if peer.PeerType == mtproto.PEER_USER {
		if s.Messages, err = c.historyOf(peer); err != nil {
			c.Logger.Errorf("messages.reportSpam - user %d: %v", c.MD.UserId, err)
			return nil, err
		}
	}
	target := targetOf(s, peer, c.MD.UserId)
	if err = c.fillSnapshot(s, peer, target); err == nil {
		err = c.file(&moderation.Report{
			Kind:         moderation.KindSpam,
			PeerType:     peer.PeerType,
			PeerId:       peer.PeerId,
			TargetUserId: target,
			Reason:       "spam",
		}, nil, s)
	}
	if err != nil {
		c.Logger.Errorf("messages.reportSpam - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
