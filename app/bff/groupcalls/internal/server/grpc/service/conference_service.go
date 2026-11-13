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
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/core"
)

// The methods of mtproto.RPCConferenceCallsServer: the chain of the
// end-to-end encryption every group call has (task 094, ADR 0060) by core,
// the conferences as a product and the encrypted messages with
// ErrEnterpriseIsBlocked (core/not_served.go).

var _ mtproto.RPCConferenceCallsServer = (*Service)(nil)

// PhoneGetGroupCallChainBlocks
// phone.getGroupCallChainBlocks#ee9f88a6 call:InputGroupCall sub_chain_id:int offset:int limit:int = Updates;
func (s *Service) PhoneGetGroupCallChainBlocks(ctx context.Context, request *mtproto.TLPhoneGetGroupCallChainBlocks) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.getGroupCallChainBlocks - metadata: {%s}", c.MD)
	r, err := c.PhoneGetGroupCallChainBlocks(request)
	core.CountError("phone.getGroupCallChainBlocks", err)
	return r, err
}

// PhoneSendConferenceCallBroadcast
// phone.sendConferenceCallBroadcast#c6701900 call:InputGroupCall block:bytes = Updates;
func (s *Service) PhoneSendConferenceCallBroadcast(ctx context.Context, request *mtproto.TLPhoneSendConferenceCallBroadcast) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.sendConferenceCallBroadcast - metadata: {%s}", c.MD)
	r, err := c.PhoneSendConferenceCallBroadcast(request)
	core.CountError("phone.sendConferenceCallBroadcast", err)
	return r, err
}

// PhoneDeleteConferenceCallParticipants
// phone.deleteConferenceCallParticipants#8ca60525 flags:# only_left:flags.0?true kick:flags.1?true call:InputGroupCall ids:Vector<long> block:bytes = Updates;
func (s *Service) PhoneDeleteConferenceCallParticipants(ctx context.Context, request *mtproto.TLPhoneDeleteConferenceCallParticipants) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("phone.deleteConferenceCallParticipants - metadata: {%s}", c.MD)
	r, err := c.PhoneDeleteConferenceCallParticipants(request)
	core.CountError("phone.deleteConferenceCallParticipants", err)
	return r, err
}

// PhoneCreateConferenceCall7D0444BB
// phone.createConferenceCall#7d0444bb flags:# muted:flags.0?true video_stopped:flags.2?true join:flags.3?true random_id:int public_key:flags.3?int256 block:flags.3?bytes params:flags.3?DataJSON = Updates;
func (s *Service) PhoneCreateConferenceCall7D0444BB(ctx context.Context, request *mtproto.TLPhoneCreateConferenceCall7D0444BB) (*mtproto.Updates, error) {
	r, err := core.New(ctx, s.svcCtx).PhoneCreateConferenceCall7D0444BB(request)
	core.CountError("phone.createConferenceCall", err)
	return r, err
}

// PhoneCreateConferenceCallDFC909AB
// phone.createConferenceCall of an older layer, answered with a phone call.
func (s *Service) PhoneCreateConferenceCallDFC909AB(ctx context.Context, request *mtproto.TLPhoneCreateConferenceCallDFC909AB) (*mtproto.Phone_PhoneCall, error) {
	r, err := core.New(ctx, s.svcCtx).PhoneCreateConferenceCallDFC909AB(request)
	core.CountError("phone.createConferenceCall", err)
	return r, err
}

// PhoneInviteConferenceCallParticipant
// phone.inviteConferenceCallParticipant#bcf22685 flags:# video:flags.0?true call:InputGroupCall user_id:InputUser = Updates;
func (s *Service) PhoneInviteConferenceCallParticipant(ctx context.Context, request *mtproto.TLPhoneInviteConferenceCallParticipant) (*mtproto.Updates, error) {
	r, err := core.New(ctx, s.svcCtx).PhoneInviteConferenceCallParticipant(request)
	core.CountError("phone.inviteConferenceCallParticipant", err)
	return r, err
}

// PhoneDeclineConferenceCallInvite
// phone.declineConferenceCallInvite#3c479971 msg_id:int = Updates;
func (s *Service) PhoneDeclineConferenceCallInvite(ctx context.Context, request *mtproto.TLPhoneDeclineConferenceCallInvite) (*mtproto.Updates, error) {
	r, err := core.New(ctx, s.svcCtx).PhoneDeclineConferenceCallInvite(request)
	core.CountError("phone.declineConferenceCallInvite", err)
	return r, err
}

// PhoneSendGroupCallEncryptedMessage
// phone.sendGroupCallEncryptedMessage#e5afa56d call:InputGroupCall encrypted_message:bytes = Bool;
func (s *Service) PhoneSendGroupCallEncryptedMessage(ctx context.Context, request *mtproto.TLPhoneSendGroupCallEncryptedMessage) (*mtproto.Bool, error) {
	r, err := core.New(ctx, s.svcCtx).PhoneSendGroupCallEncryptedMessage(request)
	core.CountError("phone.sendGroupCallEncryptedMessage", err)
	return r, err
}
