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
	"container/list"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// channelFold folds the journal rows of one channel difference, in pts
// order, as the difference of an account is folded (ADR 0019; biz updates
// differenceBuilder), for one viewer (ADR 0034, p. 4):
//
//   - new_messages are the messages added in the range that still exist at
//     its end, in their last edit, in ascending id order;
//   - other_updates begin with updateMessageID of the viewer's own new
//     messages (addMessageIds), then the other updates in journal order:
//     an edit of an older message once, in its last state, and none for a
//     message deleted afterwards; the deletions merged into one, where the
//     last one was; pins and web pages as they came;
//   - nothing of a message below the viewer's available_min_id;
//   - every update carries the pts of the end of the range and no
//     pts_count, and every message is as the viewer sees it.
type channelFold struct {
	viewer         int64
	availableMinId int32
	pts            int32 // of the end of the range

	newMessages map[int32]*mtproto.Message
	edits       map[int32]*list.Element
	updates     *list.List
	deletes     *list.Element
	messageIds  []*mtproto.Update
	// own are the messages of the range the viewer wrote (setOwn).
	own map[int32]int64
}

func newChannelFold(viewer int64, availableMinId, pts int32) *channelFold {
	return &channelFold{
		viewer:         viewer,
		availableMinId: availableMinId,
		pts:            pts,
		newMessages:    map[int32]*mtproto.Message{},
		edits:          map[int32]*list.Element{},
		updates:        list.New(),
	}
}

// visible keeps the ids the viewer sees.
func (f *channelFold) visible(ids []int32) []int32 {
	return slices.DeleteFunc(slices.Clone(ids), func(id int32) bool { return id < f.availableMinId })
}

// add folds the next journal update in.
func (f *channelFold) add(u *mtproto.Update) {
	switch u.PredicateName {
	case mtproto.Predicate_updateNewChannelMessage:
		m := u.GetMessage_MESSAGE()
		if m.GetId() >= f.availableMinId {
			f.newMessages[m.GetId()] = m
		}
	case mtproto.Predicate_updateEditChannelMessage:
		m := u.GetMessage_MESSAGE()
		switch {
		case m.GetId() < f.availableMinId:
		case f.newMessages[m.GetId()] != nil:
			f.newMessages[m.GetId()] = m
		default:
			if e, ok := f.edits[m.GetId()]; ok {
				f.updates.Remove(e)
			}
			u.Message_MESSAGE = m
			f.edits[m.GetId()] = f.push(u)
		}
	case mtproto.Predicate_updateDeleteChannelMessages:
		ids := f.visible(u.GetMessages())
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			delete(f.newMessages, id)
			if e, ok := f.edits[id]; ok {
				f.updates.Remove(e)
				delete(f.edits, id)
			}
		}
		if f.deletes != nil {
			ids = append(f.updates.Remove(f.deletes).(*mtproto.Update).Messages, ids...)
		}
		u.Messages = ids
		f.deletes = f.push(u)
	case mtproto.Predicate_updatePinnedChannelMessages, mtproto.Predicate_updateChannelWebPage:
		if u.Messages != nil {
			if u.Messages = f.visible(u.GetMessages()); len(u.Messages) == 0 {
				return
			}
		}
		f.push(u)
	default:
		f.push(u)
	}
}

func (f *channelFold) push(u *mtproto.Update) *list.Element {
	u.Pts_INT32 = f.pts
	u.PtsCount = 0
	return f.updates.PushBack(u)
}

// newMessageIds are the ids of the new messages, ascending.
func (f *channelFold) newMessageIds() []int32 {
	ids := make([]int32, 0, len(f.newMessages))
	for id := range f.newMessages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// viewedIds are the ids of the new and the edited messages, the ones
// whose view setOwn needs.
func (f *channelFold) viewedIds() []int32 {
	ids := f.newMessageIds()
	for id := range f.edits {
		ids = append(ids, id)
	}
	return ids
}

// setOwn tells the fold the messages the viewer wrote: own maps their ids
// to their random_id (0 for none). A message written as a channel is out
// for its author alone (channel.ViewSentMessage); the new ones it sent
// with a random_id get updateMessageID, by which Android binds its pending
// copy to the server's message (MessagesController.java:16630-16667).
func (f *channelFold) setOwn(own map[int32]int64) {
	f.own = own
	for _, id := range f.newMessageIds() {
		if r := own[id]; r != 0 {
			f.messageIds = append(f.messageIds, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: id, RandomId: r}).To_Update())
		}
	}
}

// messages are new_messages as the viewer sees them.
func (f *channelFold) messages() []*mtproto.Message {
	out := make([]*mtproto.Message, 0, len(f.newMessages))
	for _, id := range f.newMessageIds() {
		out = append(out, f.view(f.newMessages[id]))
	}
	return out
}

// view is m as the viewer sees it.
func (f *channelFold) view(m *mtproto.Message) *mtproto.Message {
	_, own := f.own[m.GetId()]
	return channel.ViewSentMessage(m, f.viewer, own)
}

// otherUpdates are other_updates.
func (f *channelFold) otherUpdates() []*mtproto.Update {
	out := make([]*mtproto.Update, 0, len(f.messageIds)+f.updates.Len())
	out = append(out, f.messageIds...)
	for e := f.updates.Front(); e != nil; e = e.Next() {
		u := e.Value.(*mtproto.Update)
		if u.PredicateName == mtproto.Predicate_updateEditChannelMessage {
			u.Message_MESSAGE = f.view(u.GetMessage_MESSAGE())
		}
		out = append(out, u)
	}
	return out
}
