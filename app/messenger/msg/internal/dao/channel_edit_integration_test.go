//go:build integration

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

package dao_test

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// newChannelOf makes a supergroup with owner as its creator.
func (w *world) newChannelOf(owner int64) int64 {
	w.t.Helper()
	id := newChat()
	if _, err := w.d.RunBox(context.Background(), owner, nil, func(b *userbox.Box) error {
		if _, err := channelbox.Create(b, channelbox.NewChannel{Id: id, AccessHash: id, CreatorUserId: owner, Title: "t", Megagroup: true, Date: b.Date()}); err != nil {
			return err
		}
		_, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, ?)", id, owner, channelperm.RoleCreator, b.Date())
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) exec(query string, args ...any) {
	w.t.Helper()
	if _, err := w.check.Exec(query, args...); err != nil {
		w.t.Fatal(err)
	}
}

// sendChannel sends text from owner, replying to replyTo when not 0.
func (w *world) sendChannel(owner, channelId int64, text string, replyTo int32) (dao.ChannelSent, *dao.Discussion) {
	w.t.Helper()
	m := mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message()
	if replyTo != 0 {
		m.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
			ReplyToMsgId:           replyTo,
			ReplyToMsgId_INT32:     replyTo,
			ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(replyTo),
		}).To_MessageReplyHeader()
	}
	var (
		sent []dao.ChannelSent
		disc *dao.Discussion
	)
	if _, err := w.d.RunBox(context.Background(), owner, nil, func(b *userbox.Box) error {
		var err error
		sent, _, disc, err = w.d.SendChannel(b, channelId, []dao.Outgoing{{RandomId: time.Now().UnixNano(), Message: m}}, "")
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return sent[0], disc
}

func (w *world) editChannel(owner, channelId int64, id int32, text string) *dao.ChannelEdited {
	w.t.Helper()
	edit := mtproto.MakeTLMessage(&mtproto.Message{Message: text, EditDate: wrapperspb.Int32(int32(time.Now().Unix()))}).To_Message()
	var edited *dao.ChannelEdited
	if _, err := w.d.RunBox(context.Background(), owner, nil, func(b *userbox.Box) error {
		var err error
		edited, err = w.d.EditChannel(b, channelId, id, edit)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return edited
}

// channelMessage is the stored text and thread of a message.
func (w *world) channelMessage(channelId int64, id int32) (text string, top int32, m *mtproto.Message) {
	w.t.Helper()
	var data string
	if err := w.check.QueryRow("SELECT message, reply_top_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, id).Scan(&text, &top, &data); err != nil {
		w.t.Fatal(err)
	}
	if err := jsonx.UnmarshalFromString(data, &m); err != nil {
		w.t.Fatal(err)
	}
	return text, top, m
}

// linkedPost is a post of a broadcast channel of owner with its copy in the
// discussion group.
func (w *world) linkedPost(owner, channelId, groupId int64) (post, copyId int32) {
	w.t.Helper()
	w.exec("UPDATE channels SET broadcast = 1, megagroup = 0, linked_chat_id = ? WHERE id = ?", groupId, channelId)
	w.exec("UPDATE channels SET linked_chat_id = ? WHERE id = ?", channelId, groupId)
	sent, disc := w.sendChannel(owner, channelId, "as posted", 0)
	if disc == nil || len(disc.Copies) != 1 {
		w.t.Fatalf("the copy: %v", disc)
	}
	return sent.Message.GetId(), disc.Copies[0].Message.GetId()
}

// TestEditChannelPost: an edit of a post is an edit of its copy, with a pts
// of the group's journal, whichever of the two has the lower id, and it
// carries the comments as they are now.
func TestEditChannelPost(t *testing.T) {
	for name, groupFirst := range map[string]bool{"channel first": false, "group first": true} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, boxDSN(""), 0)
			owner := newUser()
			channelId, groupId := w.newChannelOf(owner), w.newChannelOf(owner)
			if groupFirst {
				channelId, groupId = groupId, channelId
			}
			post, copyId := w.linkedPost(owner, channelId, groupId)
			comment, _ := w.sendChannel(owner, groupId, "a comment", copyId)

			edited := w.editChannel(owner, channelId, post, "as edited")
			if edited.Copy == nil || edited.GroupId != groupId {
				t.Fatalf("the copy: %+v", edited)
			}
			if text, _, m := w.channelMessage(groupId, copyId); text != "as edited" || m.GetEditDate() == nil || m.GetFwdFrom() == nil {
				t.Errorf("the stored copy: %q, %v", text, m)
			}
			var (
				pts, updateType int32
				data            string
			)
			if err := w.check.QueryRow("SELECT pts, update_type, update_data FROM channel_pts_updates WHERE channel_id = ? ORDER BY pts DESC LIMIT 1", groupId).Scan(&pts, &updateType, &data); err != nil {
				t.Fatal(err)
			}
			var u *mtproto.Update
			if err := jsonx.UnmarshalFromString(data, &u); err != nil {
				t.Fatal(err)
			}
			if pts != edited.Copy.Pts || u.GetPredicateName() != mtproto.Predicate_updateEditChannelMessage || u.GetMessage_MESSAGE().GetId() != copyId {
				t.Errorf("the group's journal: pts %d of %d, %v", pts, edited.Copy.Pts, u)
			}
			r := edited.Message.GetReplies()
			if r.GetReplies() != 1 || r.GetMaxId().GetValue() != comment.Message.GetId() || len(r.GetRecentRepliers()) != 1 || r.GetRepliesPts() != edited.Pts {
				t.Errorf("the comments of the edited post: %v", r)
			}

			w.exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id = ?", groupId, copyId)
			if again := w.editChannel(owner, channelId, post, "once more"); again.Copy != nil {
				t.Errorf("an edit of a deleted copy: %v", again.Copy)
			}
		})
	}
}

// TestReplyToDeletedThreadRoot: a reply to the deleted start of a thread
// that has replies stays in the thread; a reply to a deleted message
// without a thread replies to nothing.
func TestReplyToDeletedThreadRoot(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner := newUser()
	groupId := w.newChannelOf(owner)

	root, _ := w.sendChannel(owner, groupId, "root", 0)
	rootId := root.Message.GetId()
	w.sendChannel(owner, groupId, "first reply", rootId)
	w.exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id = ?", groupId, rootId)
	inThread, _ := w.sendChannel(owner, groupId, "in the thread", rootId)
	if inThread.Message.GetReplyTo().GetReplyToMsgId() != rootId {
		t.Errorf("the reply's header: %v", inThread.Message.GetReplyTo())
	}
	if _, top, _ := w.channelMessage(groupId, inThread.Message.GetId()); top != rootId {
		t.Errorf("the reply's thread %d, want %d", top, rootId)
	}

	plain, _ := w.sendChannel(owner, groupId, "plain", 0)
	w.exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id = ?", groupId, plain.Message.GetId())
	outside, _ := w.sendChannel(owner, groupId, "to nothing", plain.Message.GetId())
	if outside.Message.GetReplyTo() != nil {
		t.Errorf("a reply to a deleted message: %v", outside.Message.GetReplyTo())
	}
	if _, top, _ := w.channelMessage(groupId, outside.Message.GetId()); top != 0 {
		t.Errorf("the thread of a reply to a deleted message: %d", top)
	}
}
