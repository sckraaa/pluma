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

package premium

// The limits Premium raises, as in the app config of Telegram
// (core.telegram.org/api/config) and of Pluma (teamgramd/bin/app_config.json,
// task 025).
const (
	ChannelsLimitFree    = 500  // channels_limit_default
	ChannelsLimitPremium = 1000 // channels_limit_premium
	PublicLimitFree      = 10   // channels_public_limit_default
	PublicLimitPremium   = 20   // channels_public_limit_premium
)

// Querier is the part of a transaction a limit is read through, so that it
// is read under the same lock as the count it is compared with.
type Querier interface {
	QueryRowsPartial(v any, query string, args ...any) error
}

// Active tells whether a Premium with this end (0: none) runs at now.
func Active(flag bool, until, now int64) bool {
	return flag && (until == 0 || now < until)
}

func hasPremium(q Querier, userId, now int64) (bool, error) {
	var rows []struct {
		Premium bool  `db:"premium"`
		Until   int64 `db:"premium_expire_date"`
	}
	if err := q.QueryRowsPartial(&rows, "SELECT premium, premium_expire_date FROM users WHERE id = ?", userId); err != nil {
		return false, err
	}
	return len(rows) == 1 && Active(rows[0].Premium, rows[0].Until, now), nil
}

// ChannelsLimit is the most channels and supergroups userId may be in.
func ChannelsLimit(q Querier, userId, now int64) (int, error) {
	p, err := hasPremium(q, userId, now)
	if err != nil {
		return 0, err
	}
	if p {
		return ChannelsLimitPremium, nil
	}
	return ChannelsLimitFree, nil
}

// PublicChannelsLimit is the most public channels userId may own.
func PublicChannelsLimit(q Querier, userId, now int64) (int, error) {
	p, err := hasPremium(q, userId, now)
	if err != nil {
		return 0, err
	}
	if p {
		return PublicLimitPremium, nil
	}
	return PublicLimitFree, nil
}
