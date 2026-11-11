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
	"math/rand/v2"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgToggleTodoCompleted
// msg.toggleTodoCompleted user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int completed:Vector<int> incompleted:Vector<int> = Updates;
func (c *MsgCore) MsgToggleTodoCompleted(in *msg.TLMsgToggleTodoCompleted) (*mtproto.Updates, error) {
	r, err := c.todoOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, func(b *userbox.Box, ref *dao.TodoRef) (*dao.TodoChange, error) {
		return c.svcCtx.Dao.ToggleTodo(b, ref, in.Completed, in.Incompleted)
	})
	if err != nil {
		c.Logger.Errorf("msg.toggleTodoCompleted - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgAppendTodoList
// msg.appendTodoList user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int list:Vector<TodoItem> = Updates;
func (c *MsgCore) MsgAppendTodoList(in *msg.TLMsgAppendTodoList) (*mtproto.Updates, error) {
	r, err := c.todoOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, func(b *userbox.Box, ref *dao.TodoRef) (*dao.TodoChange, error) {
		return c.svcCtx.Dao.AppendTodo(b, ref, in.List)
	})
	if err != nil {
		c.Logger.Errorf("msg.appendTodoList - error: %v", err)
		return nil, err
	}
	return r, nil
}

// MsgEditTodoList
// msg.editTodoList user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int todo:TodoList = Updates;
func (c *MsgCore) MsgEditTodoList(in *msg.TLMsgEditTodoList) (*mtproto.Updates, error) {
	r, err := c.todoOp(in.UserId, in.AuthKeyId, in.PeerType, in.PeerId, in.MsgId, func(b *userbox.Box, ref *dao.TodoRef) (*dao.TodoChange, error) {
		return c.svcCtx.Dao.EditTodo(b, ref, in.Todo)
	})
	if err != nil {
		c.Logger.Errorf("msg.editTodoList - error: %v", err)
		return nil, err
	}
	return r, nil
}

type todoOpFn func(b *userbox.Box, ref *dao.TodoRef) (*dao.TodoChange, error)

// todoOp runs op on the checklist of a message in the user's box
// transaction: the state changes under its lock, the user's copy is
// rewritten and journaled, the other members' copies by inbox events; the
// change's service message is sent with it.
func (c *MsgCore) todoOp(userId, authKeyId int64, peerType int32, peerId int64, msgId int32, op todoOpFn) (*mtproto.Updates, error) {
	peerType, peerId = selfPeer(userId, peerType, peerId)
	if peerType == mtproto.PEER_CHANNEL {
		return c.channelTodoOp(userId, authKeyId, peerId, msgId, op)
	}
	t, err := c.reactionPeers(userId, peerType, peerId, nil)
	if err != nil {
		return nil, err
	}
	serviceId, err := c.svcCtx.Dao.NextId(c.ctx)
	if err != nil {
		return nil, err
	}
	var reply *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, userId,
		[]string{dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId)},
		func(b *userbox.Box) error {
			ref, err := c.svcCtx.Dao.TodoRefOf(b, peerType, peerId, msgId)
			if err != nil {
				return err
			}
			ch, err := op(b, ref)
			if err != nil {
				return err
			}
			if ch == nil {
				reply = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId))
				return nil
			}
			updates := []*mtproto.Update{ch.Update}
			notMe := []*mtproto.Update{ch.Update}
			var boxes []*mtproto.MessageBox
			if ch.Service != nil {
				service := dao.ServiceMessageOf(userId, mtproto.MakePeer(peerType, peerId), ref.MessageId, ch.Service, b.Date())
				out := []dao.Outgoing{{RandomId: rand.Int64(), DialogMessageId: serviceId, Message: service}} //nolint:gosec // random_id of a service message
				if boxes, err = c.svcCtx.Dao.SendOutgoing(b, peerType, peerId, out); err != nil {
					return err
				}
				updates = append(updates, serviceReply(boxes)...)
				notMe = append(notMe, newMessageUpdates(boxes)...)
			}
			reply = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId), updates...)
			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, userId, authKeyId, mtproto.MakeUpdatesByUpdatesUsersChats(t.users(userId), t.chats(userId), notMe...)); err != nil {
				return err
			}
			for _, to := range t.recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdateTodoToInbox{
					UserId:          to,
					FromId:          userId,
					PeerType:        peerType,
					PeerId:          peerId,
					DialogMessageId: ref.DialogMessageId,
				}); err != nil {
					return err
				}
				if len(boxes) > 0 {
					if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
						UserId:   to,
						FromId:   userId,
						PeerType: peerType,
						PeerId:   peerId,
						BoxList:  boxes,
						Users:    t.users(to),
						Chats:    t.chats(to),
					}); err != nil {
						return err
					}
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

func (c *MsgCore) channelTodoOp(userId, authKeyId, channelId int64, msgId int32, op todoOpFn) (*mtproto.Updates, error) {
	var (
		change   *dao.TodoChange
		service  *dao.ChannelSent
		authorId int64
		randId   = rand.Int64() //nolint:gosec // random_id of a service message
	)
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, userId, nil, func(b *userbox.Box) error {
		ref, err := c.svcCtx.Dao.ChannelTodoRefOf(b, channelId, msgId)
		if err != nil {
			return err
		}
		authorId = ref.AuthorId
		if change, err = op(b, ref); err != nil || change == nil {
			return err
		}
		updates := []*mtproto.Update{mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: change.Message, Pts_INT32: change.Pts, PtsCount: 1}).To_Update()}
		if change.Service != nil {
			if service, err = c.svcCtx.Dao.PostChannelService(b, ref.Channel(), channelId, ref.Broadcast(), msgId, change.Service, randId); err != nil {
				return err
			}
			updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: service.Message, Pts_INT32: service.Pts, PtsCount: 1}).To_Update())
		}
		// The checklist stays the author's: one written as a channel is out
		// for the author's devices, not for those of who ticked it.
		return c.svcCtx.Dao.EmitChannelAuthoredPush(b, channelId, authKeyId, ref.AuthorId, updates...)
	}); err != nil {
		return nil, err
	}
	var update *mtproto.Update
	if change != nil {
		shown := channelpb.ViewSentMessage(change.Message, userId, authorId == userId)
		update = mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update()
	}
	return c.channelTodoReply(userId, channelId, update, service, randId)
}

// channelTodoReply answers with what changed, or an empty Updates.
func (c *MsgCore) channelTodoReply(userId, channelId int64, update *mtproto.Update, service *dao.ChannelSent, randId int64) (*mtproto.Updates, error) {
	if update == nil {
		return mtproto.MakeUpdatesByUpdates(), nil
	}
	return c.channelReply(userId, channelId, update, service, randId)
}
