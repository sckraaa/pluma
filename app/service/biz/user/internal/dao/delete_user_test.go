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
	"errors"
	"slices"
	"testing"
)

// userRowsStore records the deletions asked of it, or fails them.
type userRowsStore struct {
	name    string
	deleted *[]string
	err     error
}

func (s userRowsStore) DeleteUser(_ context.Context, userId int64) error {
	if s.err != nil {
		return s.err
	}
	*s.deleted = append(*s.deleted, s.name)
	return nil
}

// Every store of UserRows deletes the account's rows, in order; the first
// failure stops the deletion, so that the account, not yet deleted, is
// deleted again whole.
func TestDeleteUserRows(t *testing.T) {
	var deleted []string
	d := &Dao{UserRows: []UserRows{
		userRowsStore{name: "a", deleted: &deleted},
		userRowsStore{name: "b", deleted: &deleted},
	}}
	if err := d.deleteUserRows(context.Background(), 1); err != nil || !slices.Equal(deleted, []string{"a", "b"}) {
		t.Fatalf("deleted %q: %v", deleted, err)
	}

	deleted = nil
	failure := errors.New("mysql down")
	d.UserRows = []UserRows{
		userRowsStore{name: "a", deleted: &deleted},
		userRowsStore{name: "b", deleted: &deleted, err: failure},
		userRowsStore{name: "c", deleted: &deleted},
	}
	if err := d.deleteUserRows(context.Background(), 1); !errors.Is(err, failure) || !slices.Equal(deleted, []string{"a"}) {
		t.Fatalf("deleted %q: %v", deleted, err)
	}
	if d.DeleteUser(context.Background(), 1, "", "") {
		t.Fatal("the account was deleted though its rows were not")
	}
}
