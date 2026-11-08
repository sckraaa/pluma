/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2025 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/core"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MsgPushUserMessage
// msg.pushUserMessage user_id:long auth_key_id:long peer_type:int peer_id:long push_type:int message:OutboxMessage = Bool;
func (s *Service) MsgPushUserMessage(ctx context.Context, request *msg.TLMsgPushUserMessage) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.pushUserMessage - metadata: {%s}", c.MD)

	r, err := c.MsgPushUserMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgReadMessageContents
// msg.readMessageContents user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<ContentMessage> = messages.AffectedMessages;
func (s *Service) MsgReadMessageContents(ctx context.Context, request *msg.TLMsgReadMessageContents) (*mtproto.Messages_AffectedMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.readMessageContents - metadata: {%s}", c.MD)

	r, err := c.MsgReadMessageContents(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgSendMessageV2
// msg.sendMessageV2 user_id:long auth_key_id:long peer_type:int peer_id:long message:Vector<OutboxMessage> = Updates;
func (s *Service) MsgSendMessageV2(ctx context.Context, request *msg.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.sendMessageV2 - metadata: {%s}", c.MD)

	r, err := c.MsgSendMessageV2(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgEditMessageV2
// msg.editMessageV2 user_id:long auth_key_id:long peer_type:int peer_id:long edit_type:int new_message:OutboxMessage dst_message:MessageBox = Updates;
func (s *Service) MsgEditMessageV2(ctx context.Context, request *msg.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.editMessageV2 - metadata: {%s}", c.MD)

	r, err := c.MsgEditMessageV2(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeleteMessages
// msg.deleteMessages flags:# user_id:long auth_key_id:long peer_type:int peer_id:long revoke:flags.1?true id:Vector<int> = messages.AffectedMessages;
func (s *Service) MsgDeleteMessages(ctx context.Context, request *msg.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deleteMessages - metadata: {%s}", c.MD)

	r, err := c.MsgDeleteMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeleteHistory
// msg.deleteHistory flags:# user_id:long auth_key_id:long peer_type:int peer_id:long just_clear:flags.0?true revoke:flags.1?true max_id:int = messages.AffectedHistory;
func (s *Service) MsgDeleteHistory(ctx context.Context, request *msg.TLMsgDeleteHistory) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deleteHistory - metadata: {%s}", c.MD)

	r, err := c.MsgDeleteHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeletePhoneCallHistory
// msg.deletePhoneCallHistory flags:# user_id:long auth_key_id:long revoke:flags.1?true = messages.AffectedFoundMessages;
func (s *Service) MsgDeletePhoneCallHistory(ctx context.Context, request *msg.TLMsgDeletePhoneCallHistory) (*mtproto.Messages_AffectedFoundMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deletePhoneCallHistory - metadata: {%s}", c.MD)

	r, err := c.MsgDeletePhoneCallHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeleteChatHistory
// msg.deleteChatHistory chat_id:long delete_user_id:long = Bool;
func (s *Service) MsgDeleteChatHistory(ctx context.Context, request *msg.TLMsgDeleteChatHistory) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deleteChatHistory - metadata: {%s}", c.MD)

	r, err := c.MsgDeleteChatHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgReadHistoryV2
// msg.readHistoryV2 user_id:long auth_key_id:long peer_type:int peer_id:long max_id:int = messages.AffectedMessages;
func (s *Service) MsgReadHistoryV2(ctx context.Context, request *msg.TLMsgReadHistoryV2) (*mtproto.Messages_AffectedMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.readHistoryV2 - metadata: {%s}", c.MD)

	r, err := c.MsgReadHistoryV2(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgUpdatePinnedMessage
// msg.updatePinnedMessage flags:# user_id:long auth_key_id:long silent:flags.0?true unpin:flags.1?true pm_oneside:flags.2?true peer_type:int peer_id:long id:int = Updates;
func (s *Service) MsgUpdatePinnedMessage(ctx context.Context, request *msg.TLMsgUpdatePinnedMessage) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.updatePinnedMessage - metadata: {%s}", c.MD)

	r, err := c.MsgUpdatePinnedMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgUnpinAllMessages
// msg.unpinAllMessages user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
func (s *Service) MsgUnpinAllMessages(ctx context.Context, request *msg.TLMsgUnpinAllMessages) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.unpinAllMessages - metadata: {%s}", c.MD)

	r, err := c.MsgUnpinAllMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgReadMentions
// msg.readMentions user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
func (s *Service) MsgReadMentions(ctx context.Context, request *msg.TLMsgReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.readMentions - metadata: {%s}", c.MD)

	r, err := c.MsgReadMentions(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeleteHistoryRange
// msg.deleteHistoryRange user_id:long auth_key_id:long peer_type:int peer_id:long saved_peer_type:int saved_peer_id:long max_id:int min_date:int max_date:int revoke:Bool = messages.AffectedHistory;
func (s *Service) MsgDeleteHistoryRange(ctx context.Context, request *msg.TLMsgDeleteHistoryRange) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deleteHistoryRange - metadata: {%s}", c.MD)

	r, err := c.MsgDeleteHistoryRange(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgEditPeerFolders
// msg.editPeerFolders user_id:long auth_key_id:long peers:Vector<long> folder_id:int = Updates;
func (s *Service) MsgEditPeerFolders(ctx context.Context, request *msg.TLMsgEditPeerFolders) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.editPeerFolders - metadata: {%s}", c.MD)

	r, err := c.MsgEditPeerFolders(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgSendReaction
// msg.sendReaction user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int big:Bool add_to_recent:Bool reaction:Vector<Reaction> = Updates;
func (s *Service) MsgSendReaction(ctx context.Context, request *msg.TLMsgSendReaction) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.sendReaction - metadata: {%s}", c.MD)

	r, err := c.MsgSendReaction(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgReadReactions
// msg.readReactions user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
func (s *Service) MsgReadReactions(ctx context.Context, request *msg.TLMsgReadReactions) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.readReactions - metadata: {%s}", c.MD)

	r, err := c.MsgReadReactions(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgUpdateSavedReactionTag
// msg.updateSavedReactionTag user_id:long auth_key_id:long reaction:Reaction title:string = Bool;
func (s *Service) MsgUpdateSavedReactionTag(ctx context.Context, request *msg.TLMsgUpdateSavedReactionTag) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.updateSavedReactionTag - metadata: {%s}", c.MD)

	r, err := c.MsgUpdateSavedReactionTag(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgMigrateChat
// msg.migrateChat user_id:long auth_key_id:long chat_id:long = Updates;
func (s *Service) MsgMigrateChat(ctx context.Context, request *msg.TLMsgMigrateChat) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.migrateChat - metadata: {%s}", c.MD)

	r, err := c.MsgMigrateChat(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgGetScheduledHistory
// msg.getScheduledHistory user_id:long peer_type:int peer_id:long hash:long = messages.Messages;
func (s *Service) MsgGetScheduledHistory(ctx context.Context, request *msg.TLMsgGetScheduledHistory) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.getScheduledHistory - metadata: {%s}", c.MD)

	r, err := c.MsgGetScheduledHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgGetScheduledMessages
// msg.getScheduledMessages user_id:long peer_type:int peer_id:long id:Vector<int> = messages.Messages;
func (s *Service) MsgGetScheduledMessages(ctx context.Context, request *msg.TLMsgGetScheduledMessages) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.getScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MsgGetScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgSendScheduledMessages
// msg.sendScheduledMessages user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<int> = Updates;
func (s *Service) MsgSendScheduledMessages(ctx context.Context, request *msg.TLMsgSendScheduledMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.sendScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MsgSendScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgDeleteScheduledMessages
// msg.deleteScheduledMessages user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<int> = Updates;
func (s *Service) MsgDeleteScheduledMessages(ctx context.Context, request *msg.TLMsgDeleteScheduledMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.deleteScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MsgDeleteScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MsgHasScheduled
// msg.hasScheduled user_id:long peer_type:int peer_id:long = Bool;
func (s *Service) MsgHasScheduled(ctx context.Context, request *msg.TLMsgHasScheduled) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("msg.hasScheduled - metadata: {%s}", c.MD)

	r, err := c.MsgHasScheduled(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
