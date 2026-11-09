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
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/voipcalls/internal/svc"
)

type Service struct {
	svcCtx *svc.ServiceContext
}

func New(ctx *svc.ServiceContext) *Service {
	return &Service{svcCtx: ctx}
}

// MessagesDeletePhoneCallHistory
// messages.deletePhoneCallHistory#f9cbe409 flags:# revoke:flags.0?true = messages.AffectedFoundMessages;
func (s *Service) MessagesDeletePhoneCallHistory(ctx context.Context, request *mtproto.TLMessagesDeletePhoneCallHistory) (*mtproto.Messages_AffectedFoundMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.deletePhoneCallHistory - metadata: {%s}", c.MD)
	return c.MessagesDeletePhoneCallHistory(request)
}

// PhoneGetCallConfig
// phone.getCallConfig#55451fa9 = DataJSON;
func (s *Service) PhoneGetCallConfig(ctx context.Context, request *mtproto.TLPhoneGetCallConfig) (*mtproto.DataJSON, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getCallConfig - metadata: {%s}", c.MD)
	return c.PhoneGetCallConfig(request)
}

// PhoneRequestCall
// phone.requestCall#42ff96ed flags:# video:flags.0?true user_id:InputUser random_id:int g_a_hash:bytes protocol:PhoneCallProtocol = phone.PhoneCall;
func (s *Service) PhoneRequestCall(ctx context.Context, request *mtproto.TLPhoneRequestCall) (*mtproto.Phone_PhoneCall, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.requestCall - metadata: {%s}", c.MD)
	return c.PhoneRequestCall(request)
}

// PhoneAcceptCall
// phone.acceptCall#3bd2b4a0 peer:InputPhoneCall g_b:bytes protocol:PhoneCallProtocol = phone.PhoneCall;
func (s *Service) PhoneAcceptCall(ctx context.Context, request *mtproto.TLPhoneAcceptCall) (*mtproto.Phone_PhoneCall, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.acceptCall - metadata: {%s}", c.MD)
	return c.PhoneAcceptCall(request)
}

// PhoneConfirmCall
// phone.confirmCall#2efe1722 peer:InputPhoneCall g_a:bytes key_fingerprint:long protocol:PhoneCallProtocol = phone.PhoneCall;
func (s *Service) PhoneConfirmCall(ctx context.Context, request *mtproto.TLPhoneConfirmCall) (*mtproto.Phone_PhoneCall, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.confirmCall - metadata: {%s}", c.MD)
	return c.PhoneConfirmCall(request)
}

// PhoneReceivedCall
// phone.receivedCall#17d54f61 peer:InputPhoneCall = Bool;
func (s *Service) PhoneReceivedCall(ctx context.Context, request *mtproto.TLPhoneReceivedCall) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.receivedCall - metadata: {%s}", c.MD)
	return c.PhoneReceivedCall(request)
}

// PhoneDiscardCall
// phone.discardCall#b2cbc1c0 flags:# video:flags.0?true peer:InputPhoneCall duration:int reason:PhoneCallDiscardReason connection_id:long = Updates;
func (s *Service) PhoneDiscardCall(ctx context.Context, request *mtproto.TLPhoneDiscardCall) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.discardCall - metadata: {%s}", c.MD)
	return c.PhoneDiscardCall(request)
}

// PhoneSetCallRating
// phone.setCallRating#59ead627 flags:# user_initiative:flags.0?true peer:InputPhoneCall rating:int comment:string = Updates;
func (s *Service) PhoneSetCallRating(ctx context.Context, request *mtproto.TLPhoneSetCallRating) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.setCallRating - metadata: {%s}", c.MD)
	return c.PhoneSetCallRating(request)
}

// PhoneSaveCallDebug
// phone.saveCallDebug#277add7e peer:InputPhoneCall debug:DataJSON = Bool;
func (s *Service) PhoneSaveCallDebug(ctx context.Context, request *mtproto.TLPhoneSaveCallDebug) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.saveCallDebug - metadata: {%s}", c.MD)
	return c.PhoneSaveCallDebug(request)
}

// PhoneSendSignalingData
// phone.sendSignalingData#ff7a9383 peer:InputPhoneCall data:bytes = Bool;
func (s *Service) PhoneSendSignalingData(ctx context.Context, request *mtproto.TLPhoneSendSignalingData) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.sendSignalingData - metadata: {%s}", c.MD)
	return c.PhoneSendSignalingData(request)
}

// PhoneSaveCallLog
// phone.saveCallLog#41248786 peer:InputPhoneCall file:InputFile = Bool;
func (s *Service) PhoneSaveCallLog(ctx context.Context, request *mtproto.TLPhoneSaveCallLog) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.saveCallLog - metadata: {%s}", c.MD)
	return c.PhoneSaveCallLog(request)
}
