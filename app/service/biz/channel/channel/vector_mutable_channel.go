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

package channel

import (
	"time"

	"github.com/teamgram/proto/mtproto"
)

// GetChannelListByIdList returns the channels of m as selfId sees them now
// (MutableChannel.ToChat), in the order of m.
func (m *Vector_MutableChannel) GetChannelListByIdList(selfId int64) []*mtproto.Chat {
	now := int32(time.Now().Unix())
	chats := make([]*mtproto.Chat, 0, len(m.GetDatas()))
	for _, c := range m.GetDatas() {
		if c.GetChannel() == nil {
			continue
		}
		chats = append(chats, c.ToChat(selfId, now))
	}
	return chats
}
