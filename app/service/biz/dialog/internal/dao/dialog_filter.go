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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The folders of a user (task 013). A change is one box transaction of the
// user: the box lock serializes the changes of one user, so two devices
// editing folders at once cannot exceed the limit or tangle the order. The
// same transaction pushes the update with seq that tells every device of
// the account about the change (ADR 0020); a request that changes nothing
// pushes nothing.

// The limits of folders: Telegram's with Premium, which everybody has
// (ADR 0041; appConfig dialog_filters_limit_premium and
// dialog_filters_chats_limit_premium, which the clients enforce too).
const (
	DialogFiltersLimit = 30
	FilterChatsLimit   = 200
)

var (
	// ErrDialogFiltersTooMuch: a new folder over the limit of folders.
	ErrDialogFiltersTooMuch = status.Error(mtproto.ErrBadRequest, "DIALOG_FILTERS_TOO_MUCH")
	// ErrFilterChatsTooMuch: more pinned and included chats, or more
	// excluded ones, than a folder may have.
	ErrFilterChatsTooMuch = status.Error(mtproto.ErrBadRequest, "FILTER_INCLUDE_TOO_MUCH")
)

// allChats is the id of "All chats" (dialogFilterDefault). A user may put
// it after other folders: its place is kept as a row with this id.
const allChats = 0

// dialogFilterTagsKey is the user_settings key of folder tags: "1" — on.
const dialogFilterTagsKey = "dialogFilterTags"

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
// folder keeps its place, a new one goes last. Every device of the account
// gets updateDialogFilter with the folder as stored; users and chats are
// its peers as userId sees them. A folder saved as it is changes nothing.
//
// A shared folder (task 064) stays shared, in whichever form it comes:
// tdesktop and iOS send dialogFilterChatlist (ChatFilter::tl,
// ChatListFiltering.swift:475), Android dialogFilter for every folder
// (FilterCreateActivity.saveFilterToServer). It keeps the owner's invites and
// the import of the row. Only the server makes one, by a link, and it has no
// rules or exclusions: FILTER_NOT_SUPPORTED otherwise.
func (d *Dao) SaveDialogFilter(ctx context.Context, userId int64, id int32, filter *mtproto.DialogFilter, users []*mtproto.User, chats []*mtproto.Chat) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		if len(filter.GetPinnedPeers())+len(filter.GetIncludePeers()) > FilterChatsLimit || len(filter.GetExcludePeers()) > FilterChatsLimit {
			return ErrFilterChatsTooMuch
		}
		order, found, count := int64(0), false, 0
		for _, r := range rows {
			if r.Id == id {
				order, found = r.Order, true
			}
			if r.Id != allChats {
				count++
			}
		}
		if !found {
			if count >= DialogFiltersLimit {
				return ErrDialogFiltersTooMuch
			}
			order = filterOrderTop
			if len(rows) > 0 {
				order = rows[len(rows)-1].Order - filterOrderStep
			}
		}
		var meta filterRow
		var old *mtproto.DialogFilter
		if found {
			row, f, err := lockedFilter(b, id)
			if err != nil {
				return err
			}
			if row != nil {
				meta, old = *row, f
			}
		}
		if filter.GetPredicateName() == mtproto.Predicate_dialogFilterChatlist && !meta.IsChatlist {
			return mtproto.ErrFilterNotSupported
		}
		f := filter
		if meta.IsChatlist {
			if !shareable(filter) {
				return mtproto.ErrFilterNotSupported
			}
			f = toChatlist(filter, old.GetHasMyInvites())
		}
		data, err := jsonx.Marshal(f)
		if err != nil {
			return err
		}
		// The folder as it reads back from its JSON, to compare with the one
		// stored.
		saved, err := mtproto.UnmarshalDialogFilter(string(data))
		if err != nil {
			return err
		}
		if found && old != nil && proto.Equal(old, saved) {
			return nil
		}
		if _, _, err = d.DialogFiltersDAO.InsertOrUpdateTx(b.Tx(), &dataobject.DialogFiltersDO{
			UserId:         userId,
			DialogFilterId: id,
			IsChatlist:     meta.IsChatlist,
			JoinedBySlug:   meta.JoinedBySlug,
			Slug:           meta.Slug,
			DialogFilter:   string(data),
			OrderValue:     order,
		}); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogFilter(&mtproto.Update{
			Id_INT32: id,
			Filter:   saved,
		}).To_Update(), users, chats)
	})
}

// DeleteDialogFilter removes folder id of userId; every device of the
// account gets updateDialogFilter without a folder. A folder that is not
// there is removed already.
func (d *Dao) DeleteDialogFilter(ctx context.Context, userId int64, id int32) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(rows, func(r filterOrder) bool { return r.Id == id }) {
			return nil
		}
		if _, err = d.DialogFiltersDAO.ClearTx(b.Tx(), userId, id); err != nil {
			return err
		}
		// Its links stop working with it (task 064).
		if err = clearChatlistState(b, id); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogFilter(&mtproto.Update{
			Id_INT32: id,
		}).To_Update(), nil, nil)
	})
}

// OrderDialogFilters puts the folders of userId in the order of ids. Ids of
// folders the user does not have are ignored (a device may reorder a list
// another one just changed); folders missing from ids keep their relative
// order after the others. "All chats" (0) takes its place like the others.
// It returns the order now stored. Every device of the account gets
// updateDialogFilterOrder with it as the clients see it (clientOrder); an
// order as it is changes nothing.
func (d *Dao) OrderDialogFilters(ctx context.Context, userId int64, ids []int32) ([]int32, error) {
	var stored []int32
	err := d.RunBox(ctx, userId, []string{dialog.GetDialogFilterCacheKey(userId)}, func(b *userbox.Box) error {
		rows, err := lockFilters(b)
		if err != nil {
			return err
		}
		have := make(map[int32]bool, len(rows))
		current := make([]int32, 0, len(rows))
		for _, r := range rows {
			have[r.Id] = true
			current = append(current, r.Id)
		}
		// "All chats" gets a row to keep its place.
		placeAllChats := false
		if !have[allChats] && slices.Contains(ids, allChats) {
			placeAllChats = true
			have[allChats] = true
			rows = append(rows, filterOrder{Id: allChats})
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
			if have[r.Id] && !placed[r.Id] {
				stored = append(stored, r.Id)
			}
		}
		if !placeAllChats && slices.Equal(stored, current) {
			return nil
		}
		if placeAllChats {
			data, err := jsonx.Marshal(mtproto.MakeTLDialogFilterDefault(nil).To_DialogFilter())
			if err != nil {
				return err
			}
			if _, _, err = d.DialogFiltersDAO.InsertOrUpdateTx(b.Tx(), &dataobject.DialogFiltersDO{
				UserId:       userId,
				DialogFilter: string(data),
			}); err != nil {
				return err
			}
		}
		for i, id := range stored {
			if _, err = d.DialogFiltersDAO.UpdateOrderTx(b.Tx(), filterOrderTop-int64(i)*filterOrderStep, userId, id); err != nil {
				return err
			}
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogFilterOrder(&mtproto.Update{
			Order_VECTORINT32: clientOrder(stored),
		}).To_Update(), nil, nil)
	})
	return stored, err
}

// clientOrder is the stored order of folders as the clients get it from
// messages.getDialogFilters: "All chats" (0) is in every list, first unless
// its place is stored.
func clientOrder(stored []int32) []int32 {
	if slices.Contains(stored, allChats) {
		return stored
	}
	return append([]int32{allChats}, stored...)
}

// DialogFilterTags returns whether userId turned folder tags on.
func (d *Dao) DialogFilterTags(ctx context.Context, userId int64) (bool, error) {
	var values []string
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &values,
		"SELECT value FROM user_settings WHERE user_id = ? AND key2 = ? AND deleted = 0", userId, dialogFilterTagsKey); err != nil {
		return false, err
	}
	return len(values) == 1 && values[0] == "1", nil
}

// SetDialogFilterTags turns folder tags of userId on or off. Every device
// of the account gets updateDialogFilters and asks for the folders again;
// tags already as asked change nothing.
func (d *Dao) SetDialogFilterTags(ctx context.Context, userId int64, on bool) error {
	value := "0"
	if on {
		value = "1"
	}
	return d.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
		// The key is written only here, under the box lock.
		var values []string
		if err := b.Tx().QueryRowsPartial(&values,
			"SELECT value FROM user_settings WHERE user_id = ? AND key2 = ? AND deleted = 0", userId, dialogFilterTagsKey); err != nil {
			return err
		}
		if (len(values) == 1 && values[0] == "1") == on {
			return nil
		}
		if _, err := b.Tx().Exec("INSERT INTO user_settings (user_id, key2, value) VALUES (?, ?, ?) AS new"+
			" ON DUPLICATE KEY UPDATE value = new.value, deleted = 0",
			userId, dialogFilterTagsKey, value); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogFilters(nil).To_Update(), nil, nil)
	})
}
