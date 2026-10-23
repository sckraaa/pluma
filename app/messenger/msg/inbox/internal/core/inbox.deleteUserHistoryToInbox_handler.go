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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
)

// InboxDeleteUserHistoryToInbox
// inbox.deleteUserHistoryToInbox user_id:long from_id:long = Void;
//
// from_id deleted the private chat for both: the recipient's dialog with
// from_id goes in portions, its row with the last one.
func (c *InboxCore) InboxDeleteUserHistoryToInbox(in *inbox.TLInboxDeleteUserHistoryToInbox) error {
	return c.deleteDialog(in.UserId, mtproto.MakePeerUtil(mtproto.PEER_USER, in.FromId), true)
}
