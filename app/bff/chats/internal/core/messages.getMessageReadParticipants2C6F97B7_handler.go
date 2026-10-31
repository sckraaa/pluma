// Copyright 2022 Teamgram Authors
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
)

// MessagesGetMessageReadParticipants2C6F97B7
// messages.getMessageReadParticipants#2c6f97b7 peer:InputPeer msg_id:int = Vector<long>;
//
// The layers before the read dates: the readers alone.
func (c *ChatsCore) MessagesGetMessageReadParticipants2C6F97B7(in *mtproto.TLMessagesGetMessageReadParticipants2C6F97B7) (*mtproto.Vector_Long, error) {
	readers, err := c.readParticipants(in.Peer, in.MsgId)
	if err != nil {
		return nil, err
	}

	idList := make([]int64, 0, len(readers))
	for _, r := range readers {
		idList = append(idList, r.GetUserId())
	}

	return &mtproto.Vector_Long{
		Datas: idList,
	}, nil
}
