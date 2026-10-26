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
	"container/list"
	"slices"

	"github.com/teamgram/proto/mtproto"
)

func equalPeer(p, o *mtproto.Peer) bool {
	if p.GetPredicateName() != o.GetPredicateName() {
		return false
	}
	switch p.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		return p.GetUserId() == o.GetUserId()
	case mtproto.Predicate_peerChat:
		return p.GetChatId() == o.GetChatId()
	case mtproto.Predicate_peerChannel:
		return p.GetChannelId() == o.GetChannelId()
	default:
		return false
	}
}

// differenceBuilder folds the journal rows of one difference, in pts order,
// into the answer Telegram gives (conformance fixture saved_messages,
// task 010). The journal keeps every update as it was written, so the fold
// happens here, when it is read:
//
//   - new_messages are the messages added in the range that still exist at
//     its end, in their state at its end: a later edit of such a message
//     replaces it and is not an update of its own; a later deletion drops
//     it. They are in ascending id order, as clients expect.
//   - other_updates begin with updateMessageID of the account's own new
//     messages (the random_id is not in the journal; see addMessageIds),
//     then the other updates in journal order: an edit of an older message
//     once, in its last state, and none for a message deleted afterwards;
//     the deletions merged into one, where the last one was. The read
//     history of each dialog comes last, once, with its last max_id: it is
//     the dialog's read state at the end of the range, and so is its
//     still_unread_count (set by the caller, see inboxReads).
//   - every kept update carries the pts of the end of the range and no
//     pts_count: a client applies a difference as a whole.
type differenceBuilder struct {
	pts int32 // of the end of the range

	newMessages map[int32]*mtproto.Message
	edits       map[int32]*list.Element // edits of older messages
	updates     *list.List              // in journal order
	deletes     *list.Element
	contents    *list.Element // updateReadMessagesContents, merged
	reads       []*mtproto.Update
	phoneCall   *list.Element
	messageIds  []*mtproto.Update
}

func newDifferenceBuilder(pts int32) *differenceBuilder {
	return &differenceBuilder{
		pts:         pts,
		newMessages: map[int32]*mtproto.Message{},
		edits:       map[int32]*list.Element{},
		updates:     list.New(),
	}
}

// add folds the next journal update in.
func (b *differenceBuilder) add(update *mtproto.Update) {
	switch update.PredicateName {
	case mtproto.Predicate_updateNewMessage:
		// updateNewMessage#1f2b0afd message:Message pts:int pts_count:int = Update;
		m := update.GetMessage_MESSAGE()
		b.newMessages[m.GetId()] = m
	case mtproto.Predicate_updateEditMessage:
		// updateEditMessage#e40370a3 message:Message pts:int pts_count:int = Update;
		m := update.GetMessage_MESSAGE()
		if _, ok := b.newMessages[m.GetId()]; ok {
			b.newMessages[m.GetId()] = m
			return
		}
		if e, ok := b.edits[m.GetId()]; ok {
			b.updates.Remove(e)
		}
		b.edits[m.GetId()] = b.push(update)
	case mtproto.Predicate_updateDeleteMessages:
		// updateDeleteMessages#a20db0e5 messages:Vector<int> pts:int pts_count:int = Update;
		for _, id := range update.GetMessages() {
			delete(b.newMessages, id)
			if e, ok := b.edits[id]; ok {
				b.updates.Remove(e)
				delete(b.edits, id)
			}
		}
		if b.deletes != nil {
			merged := b.updates.Remove(b.deletes).(*mtproto.Update)
			merged.Messages = append(merged.Messages, update.GetMessages()...)
			update = merged
		}
		b.deletes = b.push(update)
	case mtproto.Predicate_updateReadMessagesContents:
		// updateReadMessagesContents#68c13933 messages:Vector<int> pts:int pts_count:int = Update;
		if b.contents != nil {
			merged := b.updates.Remove(b.contents).(*mtproto.Update)
			merged.Messages = append(merged.Messages, update.GetMessages()...)
			update = merged
		}
		b.contents = b.push(update)
	case mtproto.Predicate_updateReadHistoryInbox:
		// updateReadHistoryInbox#9c974fdf flags:# folder_id:flags.0?int peer:Peer max_id:int still_unread_count:int pts:int pts_count:int = Update;
		for _, r := range b.reads {
			if r.PredicateName == update.PredicateName && equalPeer(r.GetPeer_PEER(), update.GetPeer_PEER()) {
				r.MaxId = max(r.GetMaxId(), update.GetMaxId())
				r.StillUnreadCount = update.GetStillUnreadCount()
				r.FolderId = update.GetFolderId()
				return
			}
		}
		b.reads = append(b.reads, b.final(update))
	case mtproto.Predicate_updateReadHistoryOutbox:
		// updateReadHistoryOutbox#2f2f21bf peer:Peer max_id:int pts:int pts_count:int = Update;
		for _, r := range b.reads {
			if r.PredicateName == update.PredicateName && equalPeer(r.GetPeer_PEER(), update.GetPeer_PEER()) {
				r.MaxId = max(r.GetMaxId(), update.GetMaxId())
				return
			}
		}
		b.reads = append(b.reads, b.final(update))
	case mtproto.Predicate_updateNewChannelMessage,
		mtproto.Predicate_updateDeleteChannelMessages,
		mtproto.Predicate_updateEditChannelMessage,
		mtproto.Predicate_updateChannelWebPage,
		mtproto.Predicate_updatePinnedChannelMessages:
		// Channels have their own pts (updates.getChannelDifference).
	case mtproto.Predicate_updatePhoneCall:
		switch update.GetPhoneCall().GetPredicateName() {
		case mtproto.Predicate_phoneCallRequested:
			if b.phoneCall != nil {
				b.updates.Remove(b.phoneCall)
			}
			b.phoneCall = b.updates.PushBack(update)
		case mtproto.Predicate_phoneCallDiscarded:
			if b.phoneCall != nil {
				b.updates.Remove(b.phoneCall)
				b.phoneCall = nil
			}
		}
	default:
		// updateWebPage, updatePinnedMessages, updateFolderPeers and the
		// rest: each as it came, since a later one of the same kind may be
		// about another dialog or other messages.
		b.push(update)
	}
}

// final gives a journal update the pts of the end of the range.
func (b *differenceBuilder) final(update *mtproto.Update) *mtproto.Update {
	update.Pts_INT32 = b.pts
	update.PtsCount = 0
	return update
}

func (b *differenceBuilder) push(update *mtproto.Update) *list.Element {
	return b.updates.PushBack(b.final(update))
}

// inboxReads returns the updateReadHistoryInbox kept: their
// still_unread_count is the one of the read and must become the dialog's
// count at the end of the range, which messages after the read and
// deletions change. Clients take it as the unread counter.
func (b *differenceBuilder) inboxReads() []*mtproto.Update {
	var out []*mtproto.Update
	for _, r := range b.reads {
		if r.PredicateName == mtproto.Predicate_updateReadHistoryInbox {
			out = append(out, r)
		}
	}
	return out
}

// newMessageIds returns the ids of the new messages, ascending.
func (b *differenceBuilder) newMessageIds() []int32 {
	ids := make([]int32, 0, len(b.newMessages))
	for id := range b.newMessages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// addMessageIds adds updateMessageID of the new messages the account sent
// itself: randomIds maps their ids to their random_id (the messages
// table). A client binds its pending copy of a message to the server id by
// it, before it adds new_messages.
func (b *differenceBuilder) addMessageIds(randomIds map[int32]int64) {
	for _, id := range b.newMessageIds() {
		if randomId := randomIds[id]; randomId != 0 {
			b.messageIds = append(b.messageIds, mtproto.MakeTLUpdateMessageID(&mtproto.Update{
				Id_INT32: id,
				RandomId: randomId,
			}).To_Update())
		}
	}
}

// messages returns new_messages.
func (b *differenceBuilder) messages() []*mtproto.Message {
	out := make([]*mtproto.Message, 0, len(b.newMessages))
	for _, id := range b.newMessageIds() {
		out = append(out, b.newMessages[id])
	}
	return out
}

// otherUpdates returns other_updates.
func (b *differenceBuilder) otherUpdates() []*mtproto.Update {
	out := make([]*mtproto.Update, 0, len(b.messageIds)+b.updates.Len()+len(b.reads))
	out = append(out, b.messageIds...)
	for e := b.updates.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(*mtproto.Update))
	}
	return append(out, b.reads...)
}
