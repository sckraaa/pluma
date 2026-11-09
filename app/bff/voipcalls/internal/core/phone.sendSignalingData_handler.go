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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/phonecalls"
)

// PhoneSendSignalingData relays bytes between the two bound devices; the
// clients keep the order themselves (updatePhoneCallSignalingData).
func (c *VoipCallsCore) PhoneSendSignalingData(in *mtproto.TLPhoneSendSignalingData) (*mtproto.Bool, error) {
	if len(in.GetData()) == 0 || len(in.GetData()) > maxSignaling {
		return nil, mtproto.ErrCallPeerInvalid
	}
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	bound := call.AdminAuthKeyId
	if role == phonecalls.Participant {
		bound = call.ParticipantAuthKeyId
	}
	if bound != c.MD.PermAuthKeyId || call.State != phonecalls.StateActive {
		return nil, errCallNotActive
	}
	userId, auth := otherAuth(call, role)
	c.toAuthIfOnline(userId, auth, mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdatePhoneCallSignalingData(&mtproto.Update{
			PhoneCallId:    call.Id,
			Data_FLAGBYTES: in.GetData(),
		}).To_Update()},
		Users: []*mtproto.User{},
		Chats: []*mtproto.Chat{},
		Date:  int32(time.Now().Unix()),
	}).To_Updates())
	return mtproto.BoolTrue, nil
}
