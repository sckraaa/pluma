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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// draftReplyTo is the reply of a request of any layer as the schema of the
// server has it: older layers name the message and the topic with
// reply_to_msg_id and top_msg_id.
func draftReplyTo(in *mtproto.TLMessagesSaveDraft) *mtproto.InputReplyTo {
	if r := in.GetReplyTo(); r != nil {
		return r
	}
	if in.GetReplyToMsgId() == nil && in.GetTopMsgId() == nil {
		return nil
	}
	return mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: in.GetReplyToMsgId().GetValue(),
		TopMsgId:     in.GetTopMsgId(),
	}).To_InputReplyTo()
}

// isEmptyDraft tells a draft that clears the chat's: no text, no entities,
// no media and no message or story replied to. A reply_to that only names
// the topic is the place of the draft, not its content (Android
// MediaDataController.saveDraft treats reply_to_msg_id 0 as no reply).
func isEmptyDraft(in *mtproto.TLMessagesSaveDraft, reply *mtproto.InputReplyTo) bool {
	return in.GetMessage() == "" && len(in.GetEntities()) == 0 && in.GetMedia() == nil &&
		reply.GetReplyToMsgId() == 0 && reply.GetStoryId() == 0 && reply.GetMonoforumPeerId() == nil
}

// draftEntities are entities as the other devices read them: a mention of a
// user the client typed as an input entity comes back as a mention by id.
func draftEntities(selfId int64, entities []*mtproto.MessageEntity) []*mtproto.MessageEntity {
	if entities == nil {
		return nil
	}
	out := make([]*mtproto.MessageEntity, 0, len(entities))
	for _, e := range entities {
		if e.GetPredicateName() != mtproto.Predicate_inputMessageEntityMentionName {
			out = append(out, e)
			continue
		}
		userId := e.GetUserId_INPUTUSER().GetUserId()
		if e.GetUserId_INPUTUSER().GetPredicateName() == mtproto.Predicate_inputUserSelf {
			userId = selfId
		}
		if userId == 0 {
			continue
		}
		out = append(out, mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{
			Offset:       e.GetOffset(),
			Length:       e.GetLength(),
			UserId_INT64: userId,
		}).To_MessageEntity())
	}
	return out
}

// MessagesSaveDraft
// messages.saveDraft#ad0fa15c flags:# no_webpage:flags.1?true invert_media:flags.6?true reply_to:flags.4?InputReplyTo peer:InputPeer message:string entities:flags.3?Vector<MessageEntity> media:flags.5?InputMedia effect:flags.7?long suggested_post:flags.8?SuggestedPost rich_message:flags.9?InputRichMessage = Bool;
//
// The draft of a chat or of one of its topics (reply_to.top_msg_id), saved
// as sent and told to the user's other devices (updateDraftMessage, without
// seq: ADR 0020, p. 2). An empty one clears it. rich_message is not served:
// nothing else here makes rich messages either.
func (c *DraftsCore) MessagesSaveDraft(in *mtproto.TLMessagesSaveDraft) (*mtproto.Bool, error) {
	var (
		peer  = mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
		reply = draftReplyTo(in)
		top   = reply.GetTopMsgId().GetValue()
		date  = int32(time.Now().Unix())
		draft *mtproto.DraftMessage
		err   error
	)
	switch peer.PeerType {
	case mtproto.PEER_SELF:
		peer = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
	case mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}

	if isEmptyDraft(in, reply) {
		draft = mtproto.MakeTLDraftMessageEmpty(&mtproto.DraftMessage{
			Date_FLAGINT32: mtproto.MakeFlagsInt32(date),
		}).To_DraftMessage()
		if top != 0 {
			_, err = c.svcCtx.Dao.DialogClient.DialogSaveThreadDraft(c.ctx, &dialog.TLDialogSaveThreadDraft{
				UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, TopMsgId: top, Message: draft,
			})
		} else {
			_, err = c.svcCtx.Dao.DialogClient.DialogClearDraftMessage(c.ctx, &dialog.TLDialogClearDraftMessage{
				UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId,
			})
		}
	} else {
		draft = mtproto.MakeTLDraftMessage(&mtproto.DraftMessage{
			NoWebpage:     in.GetNoWebpage(),
			InvertMedia:   in.GetInvertMedia(),
			ReplyTo:       reply,
			Message:       in.GetMessage(),
			Entities:      draftEntities(c.MD.UserId, in.GetEntities()),
			Media:         in.GetMedia(),
			Date_INT32:    date,
			Effect:        in.GetEffect(),
			SuggestedPost: in.GetSuggestedPost(),
		}).To_DraftMessage()
		if top != 0 {
			_, err = c.svcCtx.Dao.DialogClient.DialogSaveThreadDraft(c.ctx, &dialog.TLDialogSaveThreadDraft{
				UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, TopMsgId: top, Message: draft,
			})
		} else {
			_, err = c.svcCtx.Dao.DialogClient.DialogSaveDraftMessage(c.ctx, &dialog.TLDialogSaveDraftMessage{
				UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, Message: draft,
			})
		}
	}
	if err != nil {
		c.Logger.Errorf("messages.saveDraft - error: %v", err)
		return nil, err
	}

	syncUpdates := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{
		Peer_PEER:          peer.ToPeer(),
		TopMsgId_FLAGINT32: mtproto.MakeFlagsInt32(top),
		Draft:              draft,
	}).To_Update())
	if err = c.addDraftPeer(syncUpdates, peer.PeerType, peer.PeerId); err != nil {
		c.Logger.Errorf("messages.saveDraft - error: %v", err)
		return nil, err
	}

	// sync
	if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       syncUpdates,
	}); err != nil {
		c.Logger.Errorf("messages.saveDraft - error: %v", err)
	}

	return mtproto.BoolTrue, nil
}

// addDraftPeer adds the peer of a draft to its push.
func (c *DraftsCore) addDraftPeer(updates *mtproto.Updates, peerType int32, peerId int64) error {
	switch peerType {
	case mtproto.PEER_USER:
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{c.MD.UserId, peerId},
		})
		if err != nil {
			return err
		}
		user, _ := users.GetUnsafeUser(c.MD.UserId, peerId)
		updates.AddSafeUser(user)
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peerId})
		if err != nil {
			return err
		}
		updates.AddSafeChat(chat.ToUnsafeChat(c.MD.UserId))
	case mtproto.PEER_CHANNEL:
		chats, err := c.channelChats(peerId)
		if err != nil {
			return err
		}
		updates.PushChat(chats...)
	}
	return nil
}

// channelChats are the channels id as the account sees them.
func (c *DraftsCore) channelChats(id ...int64) ([]*mtproto.Chat, error) {
	list, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: id})
	if err != nil {
		return nil, err
	}
	return list.GetChannelListByIdList(c.MD.UserId), nil
}
