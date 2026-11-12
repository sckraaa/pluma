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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesActivateStealthMode
// stories.activateStealthMode#57bbd166 flags:# past:flags.0?true future:flags.1?true = Updates;
//
// Turns the stealth mode on: past erases the caller's views of the last
// five minutes from the stories of users, future hides its views for the
// next twenty-five, and the mode cannot be used again for an hour (the
// periods the clients show, Android MessagesController.java:1691-1699).
// The answer and the caller's other devices get updateStoriesStealthMode.
// While it cools down the answer is FLOOD_WAIT with the seconds left: the
// clients do not send the request then (Android StealthModeAlert.java:163
// checks cooldown_until_date first), so the error is for one that does
// anyway; the error Telegram gives was not read.
func (c *StoriesCore) StoriesActivateStealthMode(in *mtproto.TLStoriesActivateStealthMode) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	at := now()
	st, err := c.svcCtx.Dao.Store.ActivateStealth(c.ctx, c.MD.UserId, in.Past, in.Future, at)
	if err != nil {
		if errors.Is(err, stories.ErrCooldown) {
			cur, err := c.svcCtx.Dao.Store.Stealth(c.ctx, c.MD.UserId)
			if err != nil {
				c.Logger.Errorf("stories.activateStealthMode - error: %v", err)
				return nil, err
			}
			return nil, mtproto.NewErrFloodWaitX(max(cur.CooldownUntil-at, 1))
		}
		c.Logger.Errorf("stories.activateStealthMode - error: %v", err)
		return nil, err
	}
	updates := updatesOf([]*mtproto.Update{mtproto.MakeTLUpdateStoriesStealthMode(&mtproto.Update{
		StealthMode: stealthMode(st, at),
	}).To_Update()}, nil, nil)
	c.pushOthers(updates)
	return updates, nil
}
