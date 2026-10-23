/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgEditMessageV2
// msg.editMessageV2 user_id:long auth_key_id:long peer_type:int peer_id:long edit_type:int new_message:OutboxMessage dst_message:MessageBox = Updates;
//
// dst_message names the edited message; the message itself is read in the
// box transaction, the edit is applied to what is stored.
func (c *MsgCore) MsgEditMessageV2(in *msg.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	if in.DstMessage == nil || in.NewMessage.GetMessage() == nil {
		c.Logger.Errorf("msg.editMessage - error: request{user_id: %d, peer: %d/%d} without the message", in.GetUserId(), in.GetPeerType(), in.GetPeerId())
		return nil, mtproto.ErrInputRequestInvalid
	}

	var (
		rUpdates *mtproto.Updates
		err      error
	)
	switch in.PeerType {
	case mtproto.PEER_USER:
		rUpdates, err = c.editUserMessage(in.UserId, in.AuthKeyId, in.PeerId, in.NewMessage, in.DstMessage.MessageId)
	case mtproto.PEER_CHAT:
		rUpdates, err = c.editChatMessage(in.UserId, in.AuthKeyId, in.PeerId, in.NewMessage, in.DstMessage.MessageId)
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("msg.editMessageV2 blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		err = mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		c.Logger.Errorf("msg.editMessage - error: %v", err)
		return nil, err
	}
	return rUpdates, nil
}

func (c *MsgCore) editUserMessage(fromUserId, fromAuthKeyId, toUserId int64, editBox *msg.OutboxMessage, id int32) (*mtproto.Updates, error) {
	idHelper := mtproto.NewIDListHelper(fromUserId, toUserId)
	idHelper.PickByMessage(editBox.GetMessage())
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: idHelper.UserIdList,
		To: []int64{fromUserId, toUserId},
	})
	if err != nil {
		return nil, err
	}
	if sender, _ := users.GetImmutableUser(fromUserId); sender == nil || sender.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	if peerUser, _ := users.GetImmutableUser(toUserId); peerUser == nil || peerUser.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	blocked, err := c.isBlockedBy(fromUserId, toUserId)
	if err != nil {
		return nil, err
	}
	editBox.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, editBox.Message, fromUserId, editBox.NoWebpage, func() bool {
		hasBot := false
		users.Visit(func(it *mtproto.ImmutableUser) {
			if it.IsBot() {
				hasBot = true
			}
		})
		return hasBot
	})

	var update *mtproto.Update
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		box, edited, err := c.svcCtx.Dao.EditOutgoing(b, mtproto.PEER_USER, toUserId, id, editBox.Message)
		if err != nil {
			return err
		}
		update = edited
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, fromUserId, fromAuthKeyId,
			mtproto.MakeUpdatesByUpdatesUsersChats(users.GetUserListByIdList(fromUserId, idHelper.UserIdList...), nil, update)); err != nil {
			return err
		}
		if toUserId == fromUserId || blocked {
			return nil
		}
		return c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxEditMessageToInboxV2{
			UserId:     toUserId,
			FromId:     fromUserId,
			PeerType:   mtproto.PEER_USER,
			PeerId:     toUserId,
			NewMessage: box,
			Users:      users.GetUserListByIdList(toUserId, idHelper.UserIdList...),
		})
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeReplyUpdates(
		func(idList []int64) []*mtproto.User { return users.GetUserListByIdList(fromUserId, idList...) },
		func([]int64) []*mtproto.Chat { return []*mtproto.Chat{} },
		func([]int64) []*mtproto.Chat { return []*mtproto.Chat{} },
		update), nil
}

func (c *MsgCore) editChatMessage(fromUserId, fromAuthKeyId, peerChatId int64, editBox *msg.OutboxMessage, id int32) (*mtproto.Updates, error) {
	idHelper := mtproto.NewIDListHelper(fromUserId)
	idHelper.PickByMessage(editBox.GetMessage())
	chat, users, err := c.chatAndUsers(peerChatId, idHelper)
	if err != nil {
		return nil, err
	}
	if _, ok := chat.GetImmutableChatParticipant(fromUserId); !ok {
		return nil, mtproto.ErrChatWriteForbidden
	}
	editBox.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, editBox.Message, fromUserId, editBox.NoWebpage, func() bool {
		hasBot := false
		chat.Walk(func(userId int64, participant *mtproto.ImmutableChatParticipant) error {
			if participant.IsBot {
				hasBot = true
			}
			return nil
		})
		return hasBot
	})
	recipients := chatRecipients(chat, fromUserId, false)

	var update *mtproto.Update
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		box, edited, err := c.svcCtx.Dao.EditOutgoing(b, mtproto.PEER_CHAT, peerChatId, id, editBox.Message)
		if err != nil {
			return err
		}
		update = edited
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, fromUserId, fromAuthKeyId,
			mtproto.MakeUpdatesByUpdatesUsersChats(usersFor(users, fromUserId), []*mtproto.Chat{chat.ToUnsafeChat(fromUserId)}, update)); err != nil {
			return err
		}
		for _, userId := range recipients {
			if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxEditMessageToInboxV2{
				UserId:     userId,
				FromId:     fromUserId,
				PeerType:   mtproto.PEER_CHAT,
				PeerId:     peerChatId,
				NewMessage: box,
				Users:      usersFor(users, userId),
				Chats:      []*mtproto.Chat{chat.ToUnsafeChat(userId)},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeReplyUpdates(
		func(idList []int64) []*mtproto.User { return users.GetUserListByIdList(fromUserId, idList...) },
		func([]int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(fromUserId)} },
		func([]int64) []*mtproto.Chat { return nil },
		update), nil
}
