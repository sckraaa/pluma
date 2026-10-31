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
