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
	"math/rand/v2"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgUpdatePinnedMessage
// msg.updatePinnedMessage flags:# user_id:long auth_key_id:long silent:flags.0?true unpin:flags.1?true pm_oneside:flags.2?true peer_type:int peer_id:long id:int = Updates;
//
// One box transaction pins the message and, for a pin others are told
// about, writes the service message messageActionPinMessage. The reply
// holds updatePinnedMessages, updateMessageID and updateNewMessage; each
// other member gets the pin, then the service message, as inbox events of
// one key in that order.
func (c *MsgCore) MsgUpdatePinnedMessage(in *msg.TLMsgUpdatePinnedMessage) (*mtproto.Updates, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.updatePinnedMessage - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	r, err := c.updatePinnedMessage(in, peerType, peerId)
	if err != nil {
		c.Logger.Errorf("msg.updatePinnedMessage - error: %v", err)
		return nil, err
	}
	return r, nil
}

// pinTarget is who learns of a pin and what they need to show it.
type pinTarget struct {
	recipients []int64
	notify     bool // a service message goes with the pin
	toService  func(userId int64) bool
	users      func(userId int64) []*mtproto.User
	chats      func(userId int64) []*mtproto.Chat
}

func (c *MsgCore) pinTarget(in *msg.TLMsgUpdatePinnedMessage, peerType int32, peerId int64) (*pinTarget, error) {
	self := peerType == mtproto.PEER_USER && peerId == in.UserId
	t := &pinTarget{
		notify:    !in.Unpin && !in.PmOneside && !self,
		toService: func(int64) bool { return true },
	}
	switch peerType {
	case mtproto.PEER_USER:
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{in.UserId, peerId},
			To: []int64{in.UserId, peerId},
		})
		if err != nil {
			return nil, err
		}
		t.users = func(userId int64) []*mtproto.User { return users.GetUserListByIdList(userId, in.UserId, peerId) }
		t.chats = func(int64) []*mtproto.Chat { return []*mtproto.Chat{} }
		if in.PmOneside || self {
			return t, nil
		}
		t.recipients = []int64{peerId}
		if t.notify {
			blocked, err := c.isBlockedBy(in.UserId, peerId)
			if err != nil {
				return nil, err
			}
			t.toService = func(int64) bool { return !blocked }
		}
	case mtproto.PEER_CHAT:
		chat, users, err := c.chatAndUsers(peerId, mtproto.NewIDListHelper(in.UserId))
		if err != nil {
			return nil, err
		}
		if _, ok := chat.GetImmutableChatParticipant(in.UserId); !ok {
			return nil, mtproto.ErrChatWriteForbidden
		}
		t.users = func(userId int64) []*mtproto.User { return usersFor(users, userId) }
		t.chats = func(userId int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(userId)} }
		if !in.PmOneside {
			t.recipients = chatRecipients(chat, in.UserId, false)
		}
	}
	return t, nil
}

func (c *MsgCore) updatePinnedMessage(in *msg.TLMsgUpdatePinnedMessage, peerType int32, peerId int64) (*mtproto.Updates, error) {
	t, err := c.pinTarget(in, peerType, peerId)
	if err != nil {
		return nil, err
	}
	var service []dao.Outgoing
	if t.notify {
		dialogMessageId, err := c.svcCtx.Dao.NextId(c.ctx)
		if err != nil {
			return nil, err
		}
		service = []dao.Outgoing{{
			RandomId:        rand.Int64(), //nolint:gosec // random_id of a service message, not a secret
			DialogMessageId: dialogMessageId,
			Message:         mtproto.MakePinnedMessageService(in.Silent, in.UserId, mtproto.MakePeerUtil(peerType, peerId), in.Id),
		}}
	}

	var rUpdates *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			rUpdates = mtproto.MakeUpdatesByUpdates()
			pinned, err := c.svcCtx.Dao.Pin(b, peerType, peerId, in.Id, in.Unpin)
			if err != nil || pinned == nil {
				return err
			}
			updates := []*mtproto.Update{pinned.Update}
			var boxes []*mtproto.MessageBox
			if len(service) > 0 {
				if boxes, err = c.svcCtx.Dao.SendOutgoing(b, peerType, peerId, service); err != nil {
					return err
				}
				updates = append(updates, newMessageUpdates(boxes)...)
			}

			reply := []*mtproto.Update{pinned.Update}
			for _, box := range boxes {
				reply = append(reply,
					mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: box.MessageId, RandomId: box.RandomId}).To_Update(),
					mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
						Message_MESSAGE: box.Message,
						Pts_INT32:       box.Pts,
						PtsCount:        box.PtsCount,
					}).To_Update())
			}
			rUpdates = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(in.UserId), t.chats(in.UserId), reply...)
			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId,
				mtproto.MakeUpdatesByUpdatesUsersChats(t.users(in.UserId), t.chats(in.UserId), updates...)); err != nil {
				return err
			}
			for _, to := range t.recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdatePinnedMessage{
					UserId:          to,
					FromId:          in.UserId,
					Unpin:           in.Unpin,
					PeerType:        peerType,
					PeerId:          peerId,
					DialogMessageId: pinned.Row.DialogMessageId,
				}); err != nil {
					return err
				}
				if len(boxes) == 0 || !t.toService(to) {
					continue
				}
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
					UserId:   to,
					FromId:   in.UserId,
					PeerType: peerType,
					PeerId:   peerId,
					BoxList:  boxes,
					Users:    t.users(to),
					Chats:    t.chats(to),
				}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	return rUpdates, nil
}
