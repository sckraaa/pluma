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
	"slices"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// ContactBirthdays returns the contacts of userId whose birthday falls
// within one day of now and whose birthday privacy shows it to userId:
// contacts.getBirthdays fetches "all users with birthdays that fall within
// +1/-1 days, relative to the current day" (core.telegram.org), so that a
// client anywhere on Earth finds today's birthdays of its own day among
// them. Rules about members of chats do not let anybody see the birthday
// here, as in users.getFullUser.
func (d *Dao) ContactBirthdays(ctx context.Context, userId int64, now time.Time) ([]*mtproto.ContactBirthday, error) {
	var rows []struct {
		Id       int64  `db:"contact_user_id"`
		Birthday string `db:"birthday"`
	}
	if err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT c.contact_user_id, u.birthday FROM user_contacts c JOIN users u ON u.id = c.contact_user_id"+
			" WHERE c.owner_user_id = ? AND c.is_deleted = 0 AND u.deleted = 0 AND u.birthday <> '' ORDER BY c.contact_user_id",
		userId); err != nil {
		return nil, err
	}
	var near []*mtproto.ContactBirthday
	for _, r := range rows {
		b := mtproto.FromBirthdayString(r.Birthday)
		if b != nil && birthdayNear(b, now) {
			near = append(near, mtproto.MakeTLContactBirthday(&mtproto.ContactBirthday{ContactId: r.Id, Birthday: b}).To_ContactBirthday())
		}
	}
	if len(near) == 0 {
		return nil, nil
	}

	// The contacts who have userId in their contacts, for the rules
	// "my contacts".
	args := []any{userId}
	for _, c := range near {
		args = append(args, c.ContactId)
	}
	var mutual []int64
	if err := d.DB.QueryRowsPartial(ctx, &mutual,
		"SELECT owner_user_id FROM user_contacts WHERE contact_user_id = ? AND is_deleted = 0 AND owner_user_id IN (?"+strings.Repeat(", ?", len(near)-1)+")",
		args...); err != nil {
		return nil, err
	}
	var out []*mtproto.ContactBirthday
	for _, c := range near {
		rules, err := d.GetUserPrivacyRules(ctx, c.ContactId, mtproto.BIRTHDAY)
		if err != nil {
			return nil, err
		}
		if rules == nil {
			rules = mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{Key: mtproto.BIRTHDAY, Rules: birthdayRules}).To_PrivacyKeyRules()
		}
		if mtproto.CheckPrivacyIsAllow(c.ContactId, rules.GetRules(), userId,
			func(_, _ int64) bool { return slices.Contains(mutual, c.ContactId) },
			func(_ int64, _ []int64) bool { return false }) {
			out = append(out, c)
		}
	}
	return out, nil
}

// birthdayNear tells whether the day and month of b fall on the day before
// now, its day or the day after (UTC).
func birthdayNear(b *mtproto.Birthday, now time.Time) bool {
	for _, d := range []int{-1, 0, 1} {
		t := now.UTC().AddDate(0, 0, d)
		if int32(t.Month()) == b.GetMonth() && int32(t.Day()) == b.GetDay() {
			return true
		}
	}
	return false
}
