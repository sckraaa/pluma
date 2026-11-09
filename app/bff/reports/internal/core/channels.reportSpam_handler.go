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

// ChannelsReportSpam
// channels.reportSpam#f44a8315 channel:InputChannel participant:InputPeer id:Vector<int> = Bool;
//
// A group's admin reports a member's messages as spam (core.telegram.org/
// method/channels.reportSpam). The accused is the participant.
func (c *ReportsCore) ChannelsReportSpam(in *mtproto.TLChannelsReportSpam) (*mtproto.Bool, error) {
	if in.GetChannel().GetPredicateName() != mtproto.Predicate_inputChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	peer := &mtproto.PeerUtil{SelfId: c.MD.UserId, PeerType: mtproto.PEER_CHANNEL, PeerId: in.GetChannel().GetChannelId()}
	s := &moderation.Snapshot{}
	if len(in.Id) != 0 {
		messages, err := c.reportedMessages(peer, in.Id)
		if err != nil {
			c.Logger.Errorf("channels.reportSpam - user %d: %v", c.MD.UserId, err)
			return nil, err
		}
		if len(messages) == 0 {
			return nil, mtproto.ErrMessageIdInvalid
		}
		s.Messages = messages
	}
	participant, err := peerOf(c.MD.UserId, in.Participant)
	if err != nil {
		return nil, err
	}
	var target int64
	if participant.PeerType == mtproto.PEER_USER {
		target = participant.PeerId
	}
	if err = c.fillSnapshot(s, peer, target); err == nil {
		err = c.file(&moderation.Report{
			Kind:         moderation.KindChannelSpam,
			PeerType:     peer.PeerType,
			PeerId:       peer.PeerId,
			TargetUserId: target,
			Reason:       "spam",
		}, in.Id, s)
	}
	if err != nil {
		c.Logger.Errorf("channels.reportSpam - user %d: %v", c.MD.UserId, err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
