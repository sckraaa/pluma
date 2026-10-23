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

// InboxUpdatePinnedMessage
// inbox.updatePinnedMessage flags:# user_id:long from_id:long unpin:flags.1?true peer_type:int peer_id:long dialog_message_id:long = Void;
//
// from_id pinned (or unpinned) a message: the recipient's copy follows.
func (c *InboxCore) InboxUpdatePinnedMessage(in *inbox.TLInboxUpdatePinnedMessage) error {
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		pinned, err := c.svcCtx.Dao.PinCopy(b, peer.PeerType, peer.PeerId, in.DialogMessageId, in.Unpin)
		if err != nil || pinned == nil {
			return true, err
		}
		return true, c.push(b, pinned.Update)
	})
}
