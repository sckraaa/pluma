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

import "github.com/teamgram/proto/mtproto"

// ringingCalls returns the calls ringing for this authorization that it does
// not hold (task 068): the difference is the only way a device woken by a
// push learns of a call on Android. An offer that phone.receivedCall did not
// confirm comes again later; a second phoneCallRequested makes Android
// decline it as busy (MessagesController.java:20377), so the server does not
// take that for an answer (phonecalls.Delivery.Repeated). A failure keeps
// what was claimed: those calls are not offered again for a while.
func (c *UpdatesCore) ringingCalls() []*mtproto.Update {
	if c.svcCtx.Dao.Calls == nil {
		return nil
	}
	calls, err := c.svcCtx.Dao.Calls.ClaimRinging(c.ctx, c.MD.UserId, c.MD.PermAuthKeyId)
	if err != nil {
		c.Logger.Errorf("updates.getDifference - calls of user %d: %v", c.MD.UserId, err)
	}
	out := make([]*mtproto.Update, 0, len(calls))
	for _, call := range calls {
		out = append(out, call.RequestedUpdate())
	}
	return out
}
