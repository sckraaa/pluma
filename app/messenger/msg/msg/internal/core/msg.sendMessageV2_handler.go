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
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/premium"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgSendMessageV2
// msg.sendMessageV2 user_id:long auth_key_id:long peer_type:int peer_id:long message:Vector<OutboxMessage> = Updates;
func (c *MsgCore) MsgSendMessageV2(in *msg.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	outBoxList := in.GetMessage()
	if len(outBoxList) == 0 {
		c.Logger.Errorf("msg.sendMessageV2: {user_id: %d, peer: %d/%d} - error: no messages", in.GetUserId(), in.GetPeerType(), in.GetPeerId())
		return nil, mtproto.ErrInputRequestInvalid
	}
	for _, outBox := range outBoxList {
		if outBox.GetScheduleDate().GetValue() != 0 {
			c.Logger.Errorf("msg.sendMessageV2 blocked, License key from https://teamgram.net required to unlock enterprise features.")
			return nil, mtproto.ErrEnterpriseIsBlocked
		}
	}

	var (
		rUpdates *mtproto.Updates
		err      error
	)
	switch in.PeerType {
	case mtproto.PEER_USER:
		rUpdates, err = c.sendUserMessages(in.UserId, in.AuthKeyId, in.PeerId, outBoxList)
	case mtproto.PEER_CHAT:
		rUpdates, err = c.sendChatMessages(in.UserId, in.AuthKeyId, in.PeerId, outBoxList)
	case mtproto.PEER_CHANNEL:
		rUpdates, err = c.sendChannelMessages(in.UserId, in.AuthKeyId, in.PeerId, outBoxList)
	default:
		c.Logger.Errorf("msg.sendMessageV2 - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		c.Logger.Errorf("msg.sendMessageV2 - error: %v", err)
		return nil, err
	}
	return rUpdates, nil
}

// randomIds are the random ids of a send that a repeat is recognized by
// (dao.SentBefore): none when a message has no random id.
func randomIds(outBoxList []*msg.OutboxMessage) ([]int64, error) {
	ids := make([]int64, len(outBoxList))
	for i, outBox := range outBoxList {
		if outBox.RandomId == 0 {
			return nil, nil
		}
		ids[i] = outBox.RandomId
	}
	if dao.RepeatedRandomIds(ids) {
		return nil, mtproto.ErrRandomIdDuplicate
	}
	return ids, nil
}

// outgoing issues the dialog_message_id of each message.
func (c *MsgCore) outgoing(outBoxList []*msg.OutboxMessage) ([]dao.Outgoing, error) {
	ids, err := c.svcCtx.Dao.NextIds(c.ctx, len(outBoxList))
	if err != nil {
		return nil, err
	}
	out := make([]dao.Outgoing, len(outBoxList))
	for i, outBox := range outBoxList {
		out[i] = dao.Outgoing{RandomId: outBox.RandomId, DialogMessageId: ids[i], Message: outBox.Message}
	}
	return out, nil
}

// sendReply is the reply to the sender: updateMessageID and updateNewMessage
// of each message.
func sendReply(boxes []*mtproto.MessageBox, users func([]int64) []*mtproto.User, chats func([]int64) []*mtproto.Chat) *mtproto.Updates {
	updates := make([]*mtproto.Update, len(boxes))
	for i, box := range boxes {
		updates[i] = mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
			Pts_INT32:       box.Pts,
			PtsCount:        box.PtsCount,
			RandomId:        box.RandomId,
			Message_MESSAGE: box.Message,
		}).To_Update()
	}
	return mtproto.MakeReplyUpdates(users, chats, func([]int64) []*mtproto.Chat { return []*mtproto.Chat{} }, updates...)
}

// sendUserMessages sends messages from fromUserId to the private chat with
// toUserId: the sender's box, the push to the sender's other devices and
// one inbox event with all the messages to the recipient (unless it
// blocked the sender), in one transaction. A send repeated with the same
// random ids gets the first reply (dao.SentBefore).
func (c *MsgCore) sendUserMessages(fromUserId, fromAuthKeyId, toUserId int64, outBoxList []*msg.OutboxMessage) (*mtproto.Updates, error) {
	idHelper := mtproto.NewIDListHelper(fromUserId, toUserId)
	for _, outBox := range outBoxList {
		idHelper.PickByMessage(outBox.GetMessage())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: idHelper.UserIdList,
		To: []int64{fromUserId, toUserId},
	})
	if err != nil {
		return nil, err
	}
	sender, _ := users.GetImmutableUser(fromUserId)
	if sender == nil || sender.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	peerUser, _ := users.GetImmutableUser(toUserId)
	if peerUser == nil || peerUser.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	// The recipient may let only its contacts, chats there already and
	// Premium users write (task 014).
	required, err := premium.RequiredToContact(sender, peerUser, func() (bool, error) {
		return c.svcCtx.Dao.HasPrivateChat(c.ctx, toUserId, fromUserId)
	})
	if err != nil {
		return nil, err
	}
	if required {
		return nil, premium.ErrPrivacyPremiumRequired
	}
	// "Who can send me voice and video messages" of the recipient (task
	// 034): the server refuses them, not only the client's greyed button.
	if !peerUser.CheckPrivacyFor(mtproto.VOICE_MESSAGES, sender) {
		for _, outBox := range outBoxList {
			if isVoiceOrRound(outBox.GetMessage()) {
				return nil, mtproto.ErrVoiceMessagesForbidden
			}
		}
	}
	blocked, err := c.isBlockedBy(fromUserId, toUserId)
	if err != nil {
		return nil, err
	}

	hasBot := func() bool {
		found := false
		users.Visit(func(it *mtproto.ImmutableUser) {
			if it.IsBot() {
				found = true
			}
		})
		return found
	}
	for _, outBox := range outBoxList {
		outBox.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, outBox.Message, fromUserId, outBox.NoWebpage, hasBot)
	}
	repeat, err := randomIds(outBoxList)
	if err != nil {
		return nil, err
	}
	msgs, err := c.outgoing(outBoxList)
	if err != nil {
		return nil, err
	}

	var rUpdates *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId,
		[]string{dialog.GetDialogCacheKeyByPeer(fromUserId, mtproto.PEER_USER, toUserId)},
		func(b *userbox.Box) error {
			reply := func(boxes []*mtproto.MessageBox) {
				rUpdates = sendReply(boxes,
					func(idList []int64) []*mtproto.User { return users.GetUserListByIdList(fromUserId, idList...) },
					func([]int64) []*mtproto.Chat { return []*mtproto.Chat{} })
			}
			sent, err := c.svcCtx.Dao.SentBefore(b, mtproto.PEER_USER, toUserId, repeat)
			if err != nil || sent != nil {
				reply(sent)
				return err
			}
			boxes, err := c.svcCtx.Dao.SendOutgoing(b, mtproto.PEER_USER, toUserId, msgs)
			if err != nil {
				return err
			}
			reply(boxes)
			updates := newMessageUpdates(boxes)
			// Saved Messages hold nothing unread: Telegram reads them up to
			// the new message at once.
			if toUserId == fromUserId && len(boxes) > 0 {
				read, err := c.svcCtx.Dao.ReadSaved(b, boxes[len(boxes)-1].MessageId)
				if err != nil {
					return err
				}
				if read != nil {
					rUpdates.Updates = append(rUpdates.Updates, read)
					updates = append(updates, read)
				}
			}

			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, fromUserId, fromAuthKeyId,
				mtproto.MakeUpdatesByUpdatesUsersChats(users.GetUserListByIdList(fromUserId, idHelper.UserIdList...), nil, updates...)); err != nil {
				return err
			}
			if toUserId == fromUserId || blocked {
				return nil
			}
			return c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
				UserId:   toUserId,
				FromId:   fromUserId,
				PeerType: mtproto.PEER_USER,
				PeerId:   toUserId,
				BoxList:  boxes,
				Users:    users.GetUserListByIdList(toUserId, idHelper.UserIdList...),
			})
		})
	if err != nil {
		return nil, err
	}
	return rUpdates, nil
}

// newMessageUpdates are the journaled updateNewMessage of boxes.
func newMessageUpdates(boxes []*mtproto.MessageBox) []*mtproto.Update {
	updates := make([]*mtproto.Update, len(boxes))
	for i, box := range boxes {
		updates[i] = mtproto.MakeTLUpdateNewMessage(&mtproto.Update{
			Pts_INT32:       box.Pts,
			PtsCount:        box.PtsCount,
			Message_MESSAGE: box.Message,
		}).To_Update()
	}
	return updates
}

// chatUsers are the users the messages of a basic group mention, loaded
// with the members who will see them: the privacy of each user is decided
// per member, the member's Premium and own last seen rule included
// (task 034).
type chatUsers struct {
	users *mtproto.MutableUsers
	shown []int64
}

// For returns the users the messages mention as viewerId sees them.
func (u chatUsers) For(viewerId int64) []*mtproto.User {
	viewer, known := u.users.GetImmutableUser(viewerId)
	out := make([]*mtproto.User, 0, len(u.shown))
	for _, id := range u.shown {
		it, ok := u.users.GetImmutableUser(id)
		if !ok {
			continue
		}
		if known {
			out = append(out, it.ToUserFor(viewer))
		} else {
			out = append(out, it.ToUser(viewerId))
		}
	}
	return out
}

// chatAndUsers loads the chat and the users the messages mention, with
// every member (chatUsers).
func (c *MsgCore) chatAndUsers(chatId int64, idHelper *mtproto.IDListHelper) (*mtproto.MutableChat, chatUsers, error) {
	chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: chatId})
	if err != nil {
		return nil, chatUsers{}, err
	}
	members := chat.ParticipantIdList()
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      append(append([]int64{}, idHelper.UserIdList...), members...),
		Privacy: true,
		HasTo:   true,
		To:      members,
	})
	if err != nil {
		return nil, chatUsers{}, err
	}
	return chat, chatUsers{users: users, shown: idHelper.UserIdList}, nil
}

// sendChatMessages sends messages from fromUserId to the basic group
// peerChatId: the sender's box, the push to the sender's other devices and
// one inbox event with all the messages to each member, in one
// transaction. A send repeated with the same random ids gets the first
// reply (dao.SentBefore).
func (c *MsgCore) sendChatMessages(fromUserId, fromAuthKeyId, peerChatId int64, outBoxList []*msg.OutboxMessage) (*mtproto.Updates, error) {
	idHelper := mtproto.NewIDListHelper(fromUserId)
	for _, outBox := range outBoxList {
		idHelper.PickByMessage(outBox.GetMessage())
	}
	chat, users, err := c.chatAndUsers(peerChatId, idHelper)
	if err != nil {
		return nil, err
	}
	// A group moved to a supergroup takes no more messages (task 047): its
	// members are moved. One who leaves still sends the service message of
	// its leave, after its row changed.
	if p, ok := chat.GetImmutableChatParticipant(fromUserId); !ok || p.IsChatMemberStateMigrated() || chat.GetChat().GetMigratedTo() != nil {
		return nil, mtproto.ErrChatWriteForbidden
	}

	hasBot := func() bool {
		found := false
		chat.Walk(func(userId int64, participant *mtproto.ImmutableChatParticipant) error {
			if participant.IsBot {
				found = true
			}
			return nil
		})
		return found
	}
	for _, outBox := range outBoxList {
		outBox.Message = plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, outBox.Message, fromUserId, outBox.NoWebpage, hasBot)
	}
	repeat, err := randomIds(outBoxList)
	if err != nil {
		return nil, err
	}
	msgs, err := c.outgoing(outBoxList)
	if err != nil {
		return nil, err
	}
	recipients := withRemoved(chatRecipients(chat, fromUserId, true), chat, fromUserId, outBoxList)

	var rUpdates *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId,
		[]string{dialog.GetDialogCacheKeyByPeer(fromUserId, mtproto.PEER_CHAT, peerChatId)},
		func(b *userbox.Box) error {
			reply := func(boxes []*mtproto.MessageBox) {
				rUpdates = sendReply(boxes,
					func(idList []int64) []*mtproto.User { return users.users.GetUserListByIdList(fromUserId, idList...) },
					func([]int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(fromUserId)} })
			}
			sent, err := c.svcCtx.Dao.SentBefore(b, mtproto.PEER_CHAT, peerChatId, repeat)
			if err != nil || sent != nil {
				reply(sent)
				return err
			}
			boxes, err := c.svcCtx.Dao.SendOutgoing(b, mtproto.PEER_CHAT, peerChatId, msgs)
			if err != nil {
				return err
			}
			reply(boxes)

			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, fromUserId, fromAuthKeyId,
				mtproto.MakeUpdatesByUpdatesUsersChats(users.For(fromUserId), []*mtproto.Chat{chat.ToUnsafeChat(fromUserId)}, newMessageUpdates(boxes)...)); err != nil {
				return err
			}
			for _, userId := range recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
					UserId:   userId,
					FromId:   fromUserId,
					PeerType: mtproto.PEER_CHAT,
					PeerId:   peerChatId,
					BoxList:  boxes,
					Users:    users.For(userId),
					Chats:    []*mtproto.Chat{chat.ToUnsafeChat(userId)},
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

// isVoiceOrRound tells a voice message or a round video message: the media
// the voice messages privacy guards (core.telegram.org/constructor/
// inputPrivacyKeyVoiceMessages).
func isVoiceOrRound(m *mtproto.Message) bool {
	for _, a := range m.GetMedia().GetDocument().GetAttributes() {
		switch a.GetPredicateName() {
		case mtproto.Predicate_documentAttributeAudio:
			if a.GetVoice() {
				return true
			}
		case mtproto.Predicate_documentAttributeVideo:
			if a.GetRoundMessage() {
				return true
			}
		}
	}
	return false
}
