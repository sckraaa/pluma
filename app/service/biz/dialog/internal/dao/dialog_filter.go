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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
)

// The folders of a user (task 013). A change is one box transaction of the
// user: the box lock serializes the changes of one user, so two devices
// editing folders at once cannot exceed the limit or tangle the order.

// DialogFiltersLimit is the most folders a user has: Telegram's limit for a
// user without Premium (appConfig dialog_filters_limit_default; the
// clients enforce it too). Pluma has no Premium.
const DialogFiltersLimit = 10

// ErrDialogFiltersTooMuch: a new folder over DialogFiltersLimit. The
// official clients never ask for it; the error is for other clients.
var ErrDialogFiltersTooMuch = status.Error(mtproto.ErrBadRequest, "DIALOG_FILTERS_TOO_MUCH")

// order_value of the folders: the list is read in descending order. The
// first folder gets filterOrderTop; a new folder goes after the last one,
// filterOrderStep below it; a reorder numbers the folders from the top.
const (
	filterOrderTop  = int64(1) << 40
	filterOrderStep = int64(1) << 20
)

type filterOrder struct {
	Id    int32 `db:"dialog_filter_id"`
	Order int64 `db:"order_value"`
}

// lockFilters returns the user's folders in their order and locks the rows.
func lockFilters(b *userbox.Box) ([]filterOrder, error) {
	var rows []filterOrder
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT dialog_filter_id, order_value FROM dialog_filters WHERE user_id = ? AND deleted = 0 ORDER BY order_value DESC FOR UPDATE",
		b.UserId())
	return rows, err
}

// SaveDialogFilter creates folder id of userId or changes it: a changed
// folder keeps its place, a new one goes last.
func (d *Dao) SaveDialogFilter(ctx context.Context, userId int64, id int32, filter *mtproto.DialogFilter) error {
	data, err := jsonx.Marshal(filter)
	if err != nil {
		return err
	}
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		order, found := int64(0), false
		for _, r := range rows {
			if r.Id == id {
				order, found = r.Order, true
				break
			}
		}
		if !found {
			if len(rows) >= DialogFiltersLimit {
				return ErrDialogFiltersTooMuch
			}
			order = filterOrderTop
			if len(rows) > 0 {
				order = rows[len(rows)-1].Order - filterOrderStep
			}
		}
		_, _, err = d.DialogFiltersDAO.InsertOrUpdateTx(b.Tx(), &dataobject.DialogFiltersDO{
			UserId:         userId,
			DialogFilterId: id,
			IsChatlist:     filter.GetPredicateName() == mtproto.Predicate_dialogFilterChatlist,
			DialogFilter:   string(data),
			OrderValue:     order,
		})
		return err
	})
}

// DeleteDialogFilter removes folder id of userId; a folder that is not
// there is removed already.
func (d *Dao) DeleteDialogFilter(ctx context.Context, userId int64, id int32) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		_, err := d.DialogFiltersDAO.ClearTx(b.Tx(), userId, id)
		return err
	})
}

// OrderDialogFilters puts the folders of userId in the order of ids. Ids of
// folders the user does not have are ignored (a device may reorder a list
// another one just changed); folders missing from ids keep their relative
// order after the others. It returns the order now stored.
func (d *Dao) OrderDialogFilters(ctx context.Context, userId int64, ids []int32) ([]int32, error) {
	var stored []int32
	err := d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		have := make(map[int32]bool, len(rows))
		for _, r := range rows {
			have[r.Id] = true
		}
		stored = make([]int32, 0, len(rows))
		placed := make(map[int32]bool, len(rows))
		for _, id := range ids {
			if have[id] && !placed[id] {
				stored = append(stored, id)
				placed[id] = true
			}
		}
		for _, r := range rows {
			if !placed[r.Id] {
				stored = append(stored, r.Id)
			}
		}
		for i, id := range stored {
			if _, err = d.DialogFiltersDAO.UpdateOrderTx(b.Tx(), filterOrderTop-int64(i)*filterOrderStep, userId, id); err != nil {
				return err
			}
		}
		return nil
	})
	return stored, err
}
