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

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Folders of chats (task 013): what one device of a user changes, the
// other gets as an update and reads back.

func folderOf(id int32, title string) *mtproto.DialogFilter {
	return mtproto.MakeTLDialogFilter(&mtproto.DialogFilter{
		Id: id,
		Title_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{
			Text: title,
		}).To_TextWithEntities(),
		Contacts:    true,
		NonContacts: true,
	}).To_DialogFilter()
}

// dialogFilters returns the ids and titles of u's folders by the current
// constructor, checking the older one answers the same.
func dialogFilters(ctx context.Context, t *testing.T, u *user) ([]int32, map[int32]string) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDialogFiltersEFD48C89{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDialogFiltersEFD48C89),
	})
	if err != nil {
		t.Fatalf("messages.getDialogFilters: %v", err)
	}
	r, ok := o.(*mtproto.TLMessagesDialogFilters)
	if !ok {
		t.Fatalf("messages.getDialogFilters returned %T", o)
	}
	if r.GetTagsEnabled() {
		t.Error("folder tags enabled without Premium")
	}
	ids := make([]int32, 0, len(r.GetFilters()))
	titles := make(map[int32]string)
	for _, f := range r.GetFilters() {
		ids = append(ids, f.GetId())
		titles[f.GetId()] = f.GetTitle_TEXTWITHENTITIES().GetText()
	}
	if ids[0] != 0 || r.GetFilters()[0].GetPredicateName() != mtproto.Predicate_dialogFilterDefault {
		t.Errorf("the folders do not start with All chats: %v", r.GetFilters()[0])
	}

	// The constructor of the layers before 228 (with the older answer); the
	// server takes it from older clients.
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLMessagesGetDialogFiltersF19ED96D{
		Constructor: mtproto.CRC32_messages_getDialogFiltersF19ED96D,
	})
	if err != nil {
		t.Fatalf("messages.getDialogFilters (Vector): %v", err)
	}
	d := mtproto.NewDecodeBuf(raw)
	if crc := d.Int(); mtproto.TLConstructor(crc) != mtproto.CRC32_vector {
		t.Fatalf("messages.getDialogFilters (Vector) returned constructor %#x", uint32(crc))
	}
	old := make([]int32, d.Int())
	for i := range old {
		switch f := d.Object().(type) {
		case *mtproto.TLDialogFilterDefault:
			old[i] = 0
		case *mtproto.TLDialogFilter:
			old[i] = f.GetId()
		default:
			t.Fatalf("messages.getDialogFilters (Vector) item %d is %T", i, f)
		}
	}
	if err := d.GetError(); err != nil || !slices.Equal(old, ids) {
		t.Errorf("the older constructor answers %v (%v), the current one %v", old, err, ids)
	}
	return ids, titles
}

func updateDialogFilter(ctx context.Context, t *testing.T, u *user, id int32, f *mtproto.DialogFilter) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFilter{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_updateDialogFilter),
		Id:          id,
		Filter:      f,
	}); err != nil {
		t.Fatalf("messages.updateDialogFilter %d: %v", id, err)
	}
}

// waitUpdate waits on u for an update of predicate for which match holds.
func waitUpdate(ctx context.Context, t *testing.T, u *user, predicate string, match func(*mtproto.Update) bool) {
	t.Helper()
	_, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() == predicate && match(x) {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("no %s: %v", predicate, err)
	}
}

func TestDialogFilters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	phoneA := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	if ids, _ := dialogFilters(ctx, t, phoneA); !slices.Equal(ids, []int32{0}) {
		t.Fatalf("folders of a new user: %v, want only All chats", ids)
	}

	// A folder made on the phone reaches the laptop.
	updateDialogFilter(ctx, t, phoneA, 2, folderOf(2, "Work"))
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDialogFilter, func(u *mtproto.Update) bool {
		return u.GetId_INT32() == 2 && u.GetFilter().GetTitle_TEXTWITHENTITIES().GetText() == "Work"
	})
	updateDialogFilter(ctx, t, phoneA, 3, folderOf(3, "Family"))
	if ids, titles := dialogFilters(ctx, t, laptop); !slices.Equal(ids, []int32{0, 2, 3}) || titles[2] != "Work" {
		t.Fatalf("the laptop reads %v %v, want [0 2 3]", ids, titles)
	}

	// A reorder reaches the laptop with the order stored.
	if _, err := phoneA.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFiltersOrder{
		Constructor: phoneA.c.Constructor(mtproto.Predicate_messages_updateDialogFiltersOrder),
		Order:       []int32{3, 2},
	}); err != nil {
		t.Fatalf("messages.updateDialogFiltersOrder: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDialogFilterOrder, func(u *mtproto.Update) bool {
		return slices.Equal(u.GetOrder_VECTORINT32(), []int32{0, 3, 2})
	})

	// A change keeps the place; a deletion reaches the laptop as an update
	// without the folder.
	updateDialogFilter(ctx, t, laptop, 2, folderOf(2, "Job"))
	updateDialogFilter(ctx, t, phoneA, 3, nil)
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDialogFilter, func(u *mtproto.Update) bool {
		return u.GetId_INT32() == 3 && u.GetFilter() == nil
	})
	if ids, titles := dialogFilters(ctx, t, phoneA); !slices.Equal(ids, []int32{0, 2}) || titles[2] != "Job" {
		t.Fatalf("after the change and the deletion: %v %v, want [0 2] with Job", ids, titles)
	}

	// Errors as Telegram answers them.
	for _, tc := range []struct {
		id     int32
		filter *mtproto.DialogFilter
		code   string
	}{
		{1, folderOf(1, "Archive"), "FILTER_ID_INVALID"},
		{4, folderOf(4, ""), "FILTER_TITLE_EMPTY"},
		{4, folderOf(5, "Other"), "FILTER_ID_INVALID"},
	} {
		_, err := phoneA.c.Invoke(ctx, &mtproto.TLMessagesUpdateDialogFilter{
			Constructor: phoneA.c.Constructor(mtproto.Predicate_messages_updateDialogFilter),
			Id:          tc.id,
			Filter:      tc.filter,
		})
		var rpcErr *mtclient.RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Message != tc.code {
			t.Errorf("folder %d: %v, want %s", tc.id, err, tc.code)
		}
	}
}
