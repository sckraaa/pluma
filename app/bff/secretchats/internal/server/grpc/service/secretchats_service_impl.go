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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/secretchats/internal/core"
)

// MessagesGetDhConfig
// messages.getDhConfig#26cf8950 version:int random_length:int = messages.DhConfig;
func (s *Service) MessagesGetDhConfig(ctx context.Context, request *mtproto.TLMessagesGetDhConfig) (*mtproto.Messages_DhConfig, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getDhConfig - metadata: {%s}", c.MD)
	return c.MessagesGetDhConfig(request)
}

// MessagesRequestEncryption
// messages.requestEncryption#f64daf43 user_id:InputUser random_id:int g_a:bytes = EncryptedChat;
func (s *Service) MessagesRequestEncryption(ctx context.Context, request *mtproto.TLMessagesRequestEncryption) (*mtproto.EncryptedChat, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.requestEncryption - metadata: {%s}", c.MD)
	return c.MessagesRequestEncryption(request)
}

// MessagesAcceptEncryption
// messages.acceptEncryption#3dbc0415 peer:InputEncryptedChat g_b:bytes key_fingerprint:long = EncryptedChat;
func (s *Service) MessagesAcceptEncryption(ctx context.Context, request *mtproto.TLMessagesAcceptEncryption) (*mtproto.EncryptedChat, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.acceptEncryption - metadata: {%s}", c.MD)
	return c.MessagesAcceptEncryption(request)
}

// MessagesDiscardEncryption
// messages.discardEncryption#f393aea0 flags:# delete_history:flags.0?true chat_id:int = Bool;
func (s *Service) MessagesDiscardEncryption(ctx context.Context, request *mtproto.TLMessagesDiscardEncryption) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.discardEncryption - metadata: {%s}", c.MD)
	return c.MessagesDiscardEncryption(request)
}

// MessagesSetEncryptedTyping
// messages.setEncryptedTyping#791451ed peer:InputEncryptedChat typing:Bool = Bool;
func (s *Service) MessagesSetEncryptedTyping(ctx context.Context, request *mtproto.TLMessagesSetEncryptedTyping) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setEncryptedTyping - metadata: {%s}", c.MD)
	return c.MessagesSetEncryptedTyping(request)
}

// MessagesReadEncryptedHistory
// messages.readEncryptedHistory#7f4b690a peer:InputEncryptedChat max_date:int = Bool;
func (s *Service) MessagesReadEncryptedHistory(ctx context.Context, request *mtproto.TLMessagesReadEncryptedHistory) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.readEncryptedHistory - metadata: {%s}", c.MD)
	return c.MessagesReadEncryptedHistory(request)
}

// MessagesSendEncrypted
// messages.sendEncrypted#44fa7a15 flags:# silent:flags.0?true peer:InputEncryptedChat random_id:long data:bytes = messages.SentEncryptedMessage;
func (s *Service) MessagesSendEncrypted(ctx context.Context, request *mtproto.TLMessagesSendEncrypted) (*mtproto.Messages_SentEncryptedMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendEncrypted - metadata: {%s}", c.MD)
	return c.MessagesSendEncrypted(request)
}

// MessagesSendEncryptedFile
// messages.sendEncryptedFile#5559481d flags:# silent:flags.0?true peer:InputEncryptedChat random_id:long data:bytes file:InputEncryptedFile = messages.SentEncryptedMessage;
func (s *Service) MessagesSendEncryptedFile(ctx context.Context, request *mtproto.TLMessagesSendEncryptedFile) (*mtproto.Messages_SentEncryptedMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendEncryptedFile - metadata: {%s}", c.MD)
	return c.MessagesSendEncryptedFile(request)
}

// MessagesSendEncryptedService
// messages.sendEncryptedService#32d439a4 peer:InputEncryptedChat random_id:long data:bytes = messages.SentEncryptedMessage;
func (s *Service) MessagesSendEncryptedService(ctx context.Context, request *mtproto.TLMessagesSendEncryptedService) (*mtproto.Messages_SentEncryptedMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendEncryptedService - metadata: {%s}", c.MD)
	return c.MessagesSendEncryptedService(request)
}

// MessagesReceivedQueue
// messages.receivedQueue#55a5bb66 max_qts:int = Vector<long>;
func (s *Service) MessagesReceivedQueue(ctx context.Context, request *mtproto.TLMessagesReceivedQueue) (*mtproto.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.receivedQueue - metadata: {%s}", c.MD)
	return c.MessagesReceivedQueue(request)
}
