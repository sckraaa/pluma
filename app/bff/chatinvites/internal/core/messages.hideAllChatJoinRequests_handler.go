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

// MessagesHideAllChatJoinRequests
// messages.hideAllChatJoinRequests#e085f4ea flags:# approved:flags.0?true peer:InputPeer link:flags.1?string = Updates;
//
// Every request to join a channel, or those of one link (task 041). A basic
// group answers its requests one by one (biz chat serves no bulk answer).
func (c *ChatInvitesCore) MessagesHideAllChatJoinRequests(in *mtproto.TLMessagesHideAllChatJoinRequests) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if !peer.IsChannel() {
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	return c.hideChannelRequests(peer, in.GetApproved(), true, in.GetLink().GetValue(), 0)
}
