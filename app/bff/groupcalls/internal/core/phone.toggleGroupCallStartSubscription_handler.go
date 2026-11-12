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

// PhoneToggleGroupCallStartSubscription
// phone.toggleGroupCallStartSubscription#219c34e6 call:InputGroupCall subscribed:Bool = Updates;
//
// The account waits (or stops waiting) for a scheduled call to start; its
// call shows schedule_start_subscribed.
func (c *GroupCallsCore) PhoneToggleGroupCallStartSubscription(in *mtproto.TLPhoneToggleGroupCallStartSubscription) (*mtproto.Updates, error) {
	call, err := c.loadLive(in.GetCall())
	if err != nil {
		return nil, err
	}
	if !call.Scheduled() {
		return nil, mtproto.ErrGroupcallAlreadyStarted
	}
	a, err := c.viewable(call)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Dao.Store.Subscribe(c.ctx, call.Id, c.MD.UserId, mtproto.FromBool(in.GetSubscribed())); err != nil {
		return nil, err
	}
	return c.answer(a, call, nil, nil, true), nil
}
