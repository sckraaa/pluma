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
	"slices"

	"github.com/teamgram/proto/mtproto"
)

// SublistAdmin tells whether the participant p of a monoforum, as the
// service makes it of what the user is in the channel, is an admin of the
// direct messages: it sees every sublist (task 084, ADR 0055).
func SublistAdmin(p *ImmutableChannelParticipant) bool {
	return p != nil && p.Member() && p.GetRole() == RoleAdmin
}

// NamedIds are the messages the updates name by id without carrying them:
// the ones whose sublists a push of a monoforum has to look up
// (channel.getMessageSublists) before SublistView.
func NamedIds(updates []*mtproto.Update) []int32 {
	var ids []int32
	for _, u := range updates {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateDeleteChannelMessages, mtproto.Predicate_updatePinnedChannelMessages:
			ids = append(ids, u.GetMessages()...)
		case mtproto.Predicate_updateMessageReactions:
			ids = append(ids, u.GetMsgId_INT32())
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// SublistView is what of the updates of a monoforum the subscriber viewer
// gets: those of its own sublist. A message is told apart by its
// saved_peer_id, a message named by id by sublists, which maps the ids of
// NamedIds to their sublists. Updates of any other kind are for the admins
// of the direct messages alone. The updates kept are in their order, with
// their pts as they are: the pts of what was left out pass with nothing to
// show, and the client catches up with updates.getChannelDifference, which
// answers by the same rule. The updates themselves are changed in place:
// the caller gives its own copy.
func SublistView(updates []*mtproto.Update, viewer int64, sublists map[int32]int64) []*mtproto.Update {
	kept := updates[:0]
	for _, u := range updates {
		if m := u.GetMessage_MESSAGE(); m != nil {
			if m.GetSavedPeerId().GetUserId() == viewer {
				kept = append(kept, u)
			}
			continue
		}
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateDeleteChannelMessages, mtproto.Predicate_updatePinnedChannelMessages:
			u.Messages = slices.DeleteFunc(slices.Clone(u.GetMessages()), func(id int32) bool { return sublists[id] != viewer })
			if len(u.Messages) > 0 {
				kept = append(kept, u)
			}
		case mtproto.Predicate_updateMessageReactions:
			if sublists[u.GetMsgId_INT32()] == viewer {
				kept = append(kept, u)
			}
		}
	}
	return kept
}
