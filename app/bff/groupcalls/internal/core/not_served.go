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
	"github.com/teamgram/proto/mtproto"
)

// The methods of group calls that are not served: the recording, which an
// end-to-end encrypted call cannot have on the server, and the RTMP
// streams, never (ADR 0060 §4); the messages of a call and their stars
// (a later stage); the conferences as a product (createConferenceCall,
// their invitations) and the encrypted messages of a call (ADR 0060, «Что
// не делается»). They answer ErrEnterpriseIsBlocked, a 400 the clients do
// not retry, rather than gRPC Unimplemented, a 500 tgnet retries.

func (c *GroupCallsCore) PhoneToggleGroupCallRecord(*mtproto.TLPhoneToggleGroupCallRecord) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneGetGroupCallStreamChannels(*mtproto.TLPhoneGetGroupCallStreamChannels) (*mtproto.Phone_GroupCallStreamChannels, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneGetGroupCallStreamRtmpUrl(*mtproto.TLPhoneGetGroupCallStreamRtmpUrl) (*mtproto.Phone_GroupCallStreamRtmpUrl, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneSendGroupCallMessageB1D11410(*mtproto.TLPhoneSendGroupCallMessageB1D11410) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneSendGroupCallMessage87893014(*mtproto.TLPhoneSendGroupCallMessage87893014) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneDeleteGroupCallMessages(*mtproto.TLPhoneDeleteGroupCallMessages) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneDeleteGroupCallParticipantMessages(*mtproto.TLPhoneDeleteGroupCallParticipantMessages) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneGetGroupCallStars(*mtproto.TLPhoneGetGroupCallStars) (*mtproto.Phone_GroupCallStars, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneSaveDefaultSendAs(*mtproto.TLPhoneSaveDefaultSendAs) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneCreateConferenceCall7D0444BB(*mtproto.TLPhoneCreateConferenceCall7D0444BB) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneCreateConferenceCallDFC909AB(*mtproto.TLPhoneCreateConferenceCallDFC909AB) (*mtproto.Phone_PhoneCall, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneInviteConferenceCallParticipant(*mtproto.TLPhoneInviteConferenceCallParticipant) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneDeclineConferenceCallInvite(*mtproto.TLPhoneDeclineConferenceCallInvite) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

func (c *GroupCallsCore) PhoneSendGroupCallEncryptedMessage(*mtproto.TLPhoneSendGroupCallEncryptedMessage) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
