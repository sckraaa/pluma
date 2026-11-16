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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/metric"
)

// The updates of connected bots queued, by kind (the predicate).
var metricBotUpdates = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "business",
	Name:      "bot_updates_total",
	Help:      "Updates of Telegram Business queued for connected bots, by kind.",
	Labels:    []string{"kind"},
})

// InboxBotBusinessUpdateToInbox
// inbox.botBusinessUpdateToInbox user_id:long from_id:long update:Update = Void;
//
// The bot user_id is told update of its connection to the account from_id
// (task 105, ADR 0067, p. 6): each of its authorizations gets it with its
// next qts, queued until the authorization confirms it with
// updates.getDifference (pkg/qtsqueue), and pushed. The users and chats it
// names are read before the transaction, as the bot sees them; a failed
// read leaves the event to be tried again, since a bot cannot ask for a
// user of a business chat by itself.
func (c *InboxCore) InboxBotBusinessUpdateToInbox(in *inbox.TLInboxBotBusinessUpdateToInbox) error {
	u := in.GetUpdate()
	if !inbox.BotBusinessUpdate(u.GetPredicateName()) {
		return fmt.Errorf("%w: %q in a business update to bot %d", userbox.ErrMisuse, u.GetPredicateName(), in.UserId)
	}
	h := mtproto.NewIDListHelper()
	inbox.PickBotBusinessUpdate(h, u)
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, in.UserId, h)
	if err != nil {
		return err
	}
	queued := 0
	if err = c.apply(in.UserId, nil, func(b *userbox.Box) (bool, error) {
		n, err := c.svcCtx.Dao.QueueBotBusinessUpdate(b, u, h.UserIdList, peers.Users, peers.Chats)
		queued = n
		return true, err
	}); err != nil {
		return err
	}
	if queued > 0 {
		metricBotUpdates.Inc(u.GetPredicateName())
	}
	return nil
}
