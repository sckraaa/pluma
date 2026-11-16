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
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// AccountUpdateBusinessWorkHours
// account.updateBusinessWorkHours#4b00e066 flags:# business_work_hours:flags.0?BusinessWorkHours = Bool;
//
// Sets the work hours of the account; without business_work_hours it
// removes them, as tdesktop does (data/business/data_business_info.cpp:83-87).
// The intervals are kept normalized (business.NormalizeIntervals); the
// open_now of the client is ignored, the server sets it when it gives the
// hours (core.telegram.org/api/business). Errors, as
// core.telegram.org/method/account.updateBusinessWorkHours lists them:
// BUSINESS_WORK_HOURS_EMPTY, BUSINESS_WORK_HOURS_PERIOD_INVALID,
// TIMEZONE_INVALID.
func (c *BusinessCore) AccountUpdateBusinessWorkHours(in *mtproto.TLAccountUpdateBusinessWorkHours) (*mtproto.Bool, error) {
	const method = "account.updateBusinessWorkHours"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	var hours *business.Hours
	if wh := in.GetBusinessWorkHours(); wh != nil {
		intervals, err := business.NormalizeIntervals(business.IntervalsFromTL(wh.GetWeeklyOpen()))
		if err != nil {
			c.Logger.Errorf("%s - error: %v", method, err)
			return nil, err
		}
		if err := c.checkTimezone(method, wh.GetTimezoneId()); err != nil {
			return nil, err
		}
		hours = &business.Hours{TimezoneID: wh.GetTimezoneId(), Intervals: intervals}
	}
	if err := c.saveProfile(method, func(tx *sqlx.Tx, now int64) error {
		return business.SetHoursTx(tx, c.MD.UserId, hours, now)
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
