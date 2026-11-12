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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxChannelAntispamToInbox
// inbox.channelAntispamToInbox user_id:long from_id:long channel_id:long ids:Vector<int> rule:int = Void;
//
// The antispam judged the messages ids of from_id in the supergroup spam
// (task 082, ADR 0050): biz channel deletes them in from_id's box as the
// antispam account (channel.antispamDelete), which a repeat of the event
// does not change, and then the recipient, the antispam account, takes the
// event. An error leaves the event to be tried again, so that no spam
// stays for a failure.
func (c *InboxCore) InboxChannelAntispamToInbox(in *inbox.TLInboxChannelAntispamToInbox) error {
	if _, err := c.svcCtx.Dao.ChannelClient.ChannelAntispamDelete(c.ctx, &channelpb.TLChannelAntispamDelete{
		UserId:    in.FromId,
		ChannelId: in.ChannelId,
		Id:        in.Ids,
		Rule:      in.Rule,
	}); err != nil {
		return err
	}
	return c.apply(in.UserId, nil, func(*userbox.Box) (bool, error) { return true, nil })
}
