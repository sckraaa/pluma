// Copyright 2024 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessageGetOutboxReadDate
// message.getOutboxReadDate user_id:long peer_type:int peer_id:long msg_id:int = Vector<ReadParticipantDate>;
//
// The users who read user_id's message msg_id in the dialog with the peer,
// with the dates they read it (0: not known), the latest first: the peer
// of a private chat, the members who read it in a basic group.
func (c *MessageCore) MessageGetOutboxReadDate(in *message.TLMessageGetOutboxReadDate) (*message.Vector_ReadParticipantDate, error) {
	readers, err := c.svcCtx.Dao.Readers(c.ctx, in.UserId, in.PeerType, in.PeerId, in.MsgId)
	if err != nil {
		c.Logger.Errorf("message.getOutboxReadDate - error: %v", err)
		return nil, err
	}

	dateList := make([]*mtproto.ReadParticipantDate, 0, len(readers))
	for _, v := range readers {
		dateList = append(dateList, mtproto.MakeTLReadParticipantDate(&mtproto.ReadParticipantDate{
			UserId: v.UserId,
			Date:   int32(v.Date),
		}).To_ReadParticipantDate())
	}

	return &message.Vector_ReadParticipantDate{
		Datas: dateList,
	}, nil
}
