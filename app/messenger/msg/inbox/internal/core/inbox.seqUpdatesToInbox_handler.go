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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// seqPeersWait is how long the users and chats of an event that biz did
// not return are waited for, from the event's creation. biz user and biz
// chat leave out an id whose load failed and report no error, and the cache
// keeps such a miss as not found for a minute (the placeholder of sqlc): a
// look-up retried for longer than that reads the database again. An event
// older than this goes with the peers found, so that an id that never
// resolves does not hold the recipient's partition for good.
const seqPeersWait = 3 * time.Minute

// InboxSeqUpdatesToInbox
// inbox.seqUpdatesToInbox user_id:long from_id:long updates:Vector<Update> = Void;
//
// from_id changed states of the recipient's account (ADR 0021): each
// update is journaled with the next seq of the recipient and pushed alone,
// with that seq, to all its devices (sync_client.EmitSeqUpdate), all in one
// transaction that takes the event.
func (c *InboxCore) InboxSeqUpdatesToInbox(in *inbox.TLInboxSeqUpdatesToInbox) error {
	// EmitSeqInbox let only updates with seq in; a writer that bypassed it
	// gets its event dead before anything is looked up.
	for _, u := range in.Updates {
		if !userbox.SeqUpdate(u.GetPredicateName()) {
			return fmt.Errorf("%w: %q to user %d is not an update with seq", userbox.ErrMisuse, u.GetPredicateName(), in.UserId)
		}
	}

	// The users and chats the updates refer to, as the recipient sees them
	// now: looked up before the box transaction, which calls no service.
	h := mtproto.NewIDListHelper(in.UserId)
	seqpeers.Pick(h, in.UserId, in.Updates...)
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, in.UserId, h)
	if err != nil {
		return err
	}
	if peers.Viewer == nil {
		// biz user answers an account it cannot read with nothing: the
		// look-up is tried again, as any failed one.
		return fmt.Errorf("inbox: user %d, the recipient of updates with seq, not loaded", in.UserId)
	}
	if users, chats := peers.Missing(h); len(users) > 0 || len(chats) > 0 {
		// A peer left out is a failed load as often as an id that does
		// not exist: a client drops an update about a peer it cannot
		// resolve (iOS), so the look-up is tried again while it can still
		// succeed.
		if age := time.Since(c.createdAt); age < seqPeersWait {
			return fmt.Errorf("inbox: event %d to user %d: users %v and chats %v not loaded yet", c.eventId, in.UserId, users, chats)
		}
		c.Logger.Errorf("inbox: event %d to user %d goes without users %v and chats %v, not loaded %v after it was written",
			c.eventId, in.UserId, users, chats, time.Since(c.createdAt).Round(time.Second))
	}
	bot := peers.Viewer.IsBot()

	return c.apply(in.UserId, nil, func(b *userbox.Box) (bool, error) {
		if bot {
			// Bots are not served (sync.pushBotUpdates is a stub, msg has no
			// BotSync topic configured) and the Bot API has none of these
			// updates: a bot's state of a chat comes from its requests.
			// The event goes; nothing is journaled.
			return true, nil
		}
		for _, u := range in.Updates {
			users, chats := peers.Of(u)
			if err := sync_client.EmitSeqUpdate(b, c.svcCtx.Dao.Topics.Sync, u, users, chats); err != nil {
				return true, err
			}
		}
		return true, nil
	})
}
