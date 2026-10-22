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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxUnpinAllMessages
// inbox.unpinAllMessages user_id:long from_id:long peer_type:int peer_id:long = Void;
//
// from_id unpinned all messages of the dialog: the recipient's pinned
// messages go in portions, one transaction and one push each.
func (c *InboxCore) InboxUnpinAllMessages(in *inbox.TLInboxUnpinAllMessages) error {
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)
	for {
		var left int32
		err := c.run(in.UserId, peer, func(b *userbox.Box) error {
			left = 0
			unpinned, err := c.svcCtx.Dao.UnpinPortion(b, peer.PeerType, peer.PeerId)
			if err != nil || unpinned == nil {
				return err
			}
			left = unpinned.Left
			return c.push(b, unpinned.Update)
		})
		if err != nil || left == 0 {
			return err
		}
	}
}
