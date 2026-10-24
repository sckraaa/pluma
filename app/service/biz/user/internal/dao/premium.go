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

package dao

import (
	"context"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Premium of a user (task 014, ADR 0016): nobody has it unless the admin
// grants it, for good or for some months. users.premium with
// users.premium_expire_date: 0 is no end, otherwise Premium lasts until
// that moment.

// PremiumChange is what a change does to a user's Premium.
type PremiumChange struct {
	// Grant gives Premium; false takes it away.
	Grant bool
	// Months of a grant: 0 is for good; otherwise the months add to the
	// end of a Premium that is still running, or start now.
	Months int32
	// OnlyExpired takes Premium away only if its end has passed (the
	// expiry of a grant for some months, after the user's devices may have
	// seen it running).
	OnlyExpired bool
}

// PremiumState is a user's Premium: Until 0 is no end.
type PremiumState struct {
	Premium bool
	Until   int64
}

// Active is whether the Premium runs at now.
func (s PremiumState) Active(now int64) bool {
	return s.Premium && (s.Until == 0 || now < s.Until)
}

// apply returns the state after the change, at now.
func (c PremiumChange) apply(cur PremiumState, now int64) PremiumState {
	switch {
	case c.OnlyExpired:
		if cur.Premium && cur.Until != 0 && cur.Until <= now {
			return PremiumState{}
		}
		return cur
	case !c.Grant:
		return PremiumState{}
	case c.Months <= 0:
		return PremiumState{Premium: true}
	}
	start := time.Unix(now, 0)
	if cur.Active(now) && cur.Until != 0 {
		start = time.Unix(cur.Until, 0)
	}
	return PremiumState{Premium: true, Until: start.AddDate(0, int(c.Months), 0).Unix()}
}

type premiumRow struct {
	Premium bool  `db:"premium"`
	Until   int64 `db:"premium_expire_date"`
}

// ChangePremium changes userId's Premium in one box transaction of the
// user. When the state changes, the user's devices get the updates
// pushed(state) builds (the user as they see themselves), as an event of
// the transaction. It returns the state now stored and whether it changed.
func (d *Dao) ChangePremium(ctx context.Context, userId int64, change PremiumChange, pushed func(PremiumState) (*mtproto.Updates, error)) (PremiumState, bool, error) {
	var (
		state   PremiumState
		changed bool
	)
	_, err := d.RunBox(ctx, userId, []string{GenCacheUserDataCacheKey(userId)}, func(b *userbox.Box) error {
		var rows []premiumRow
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT premium, premium_expire_date FROM users WHERE id = ? AND deleted = 0 FOR UPDATE", userId); err != nil {
			return err
		}
		if len(rows) == 0 {
			return mtproto.ErrUserIdInvalid
		}
		cur := PremiumState{Premium: rows[0].Premium, Until: rows[0].Until}
		state = change.apply(cur, b.Date())
		changed = state != cur
		if !changed {
			return nil
		}
		if _, err := b.Tx().Exec("UPDATE users SET premium = ?, premium_expire_date = ? WHERE id = ?",
			state.Premium, state.Until, userId); err != nil {
			return err
		}
		updates, err := pushed(state)
		if err != nil {
			return err
		}
		ev, err := d.PushUpdates(userId, updates)
		if err != nil {
			return err
		}
		b.Emit(ev)
		return nil
	})
	return state, changed, err
}

// ExpiredPremium returns up to limit users whose Premium ended at now but
// is still marked: the expiry worker takes it away and tells them.
func (d *Dao) ExpiredPremium(ctx context.Context, now int64, limit int) ([]int64, error) {
	var ids []int64
	err := d.DB.QueryRowsPartial(ctx, &ids,
		"SELECT id FROM users WHERE premium = 1 AND premium_expire_date > 0 AND premium_expire_date <= ? AND deleted = 0 ORDER BY premium_expire_date LIMIT ?",
		now, limit)
	return ids, err
}
