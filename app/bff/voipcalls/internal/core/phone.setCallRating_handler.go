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
)

// PhoneSetCallRating stores one side's rating of the call. Its text goes to
// the operator's database only, never to a log.
func (c *VoipCallsCore) PhoneSetCallRating(in *mtproto.TLPhoneSetCallRating) (*mtproto.Updates, error) {
	call, role, err := c.load(in.GetPeer())
	if err != nil {
		return nil, err
	}
	if in.GetRating() < 1 || in.GetRating() > 5 {
		return nil, mtproto.ErrCallPeerInvalid
	}
	comment := in.GetComment()
	if len(comment) > maxComment {
		comment = comment[:maxComment]
	}
	if err := c.svcCtx.Dao.Store.SetRating(c.ctx, call.Id, role, in.GetRating(), comment); err != nil {
		c.Logger.Errorf("phone.setCallRating - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{}, Users: []*mtproto.User{}, Chats: []*mtproto.Chat{}, Date: int32(time.Now().Unix())}).To_Updates(), nil
}
