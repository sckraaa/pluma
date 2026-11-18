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

package langpack

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
)

type fakeReaders struct {
	readers []Reader
	err     error
}

func (f *fakeReaders) Readers(context.Context, string, string) ([]Reader, error) {
	return f.readers, f.err
}

// fakeUpdatesMe records the pushes and fails from the failAt-th one (1-based;
// 0 never).
type fakeUpdatesMe struct {
	pushed []string
	failAt int
}

func (f *fakeUpdatesMe) SyncUpdatesMe(_ context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error) {
	if f.failAt > 0 && len(f.pushed)+1 == f.failAt {
		return nil, errors.New("outbox is down")
	}
	ups := in.GetUpdates()
	if ups.GetPredicateName() != mtproto.Predicate_updates || len(ups.GetUpdates()) != 1 {
		return nil, fmt.Errorf("pushed %v", ups)
	}
	u := ups.GetUpdates()[0]
	f.pushed = append(f.pushed, fmt.Sprintf("%d/%d %s %s", in.GetUserId(), in.GetPermAuthKeyId(), u.GetPredicateName(), u.GetLangCode()))
	return mtproto.EmptyVoid, nil
}

func TestNotifyChanged(t *testing.T) {
	store := &fakeReaders{readers: []Reader{{UserId: 1, PermAuthKeyId: 10}, {UserId: 1, PermAuthKeyId: 11}, {UserId: 2, PermAuthKeyId: 20}}}
	push := &fakeUpdatesMe{}
	n, err := NotifyChanged(context.Background(), store, push, "ios", "pt-br")
	if err != nil || n != 3 {
		t.Fatalf("NotifyChanged = %d, %v", n, err)
	}
	want := "[1/10 updateLangPackTooLong pt-br 1/11 updateLangPackTooLong pt-br 2/20 updateLangPackTooLong pt-br]"
	if fmt.Sprint(push.pushed) != want {
		t.Fatalf("pushed %v", push.pushed)
	}

	push = &fakeUpdatesMe{failAt: 2}
	if n, err = NotifyChanged(context.Background(), store, push, "ios", "pt-br"); err == nil || n != 1 {
		t.Fatalf("failed push: %d, %v", n, err)
	}
	if n, err = NotifyChanged(context.Background(), &fakeReaders{err: errors.New("mysql is down")}, &fakeUpdatesMe{}, "ios", "ru"); err == nil || n != 0 {
		t.Fatalf("failed read: %d, %v", n, err)
	}
}

func TestImportResultRaised(t *testing.T) {
	for _, c := range []struct {
		old, v int32
		want   bool
	}{{0, 5, false}, {5, 5, false}, {5, 6, true}} {
		if got := (&ImportResult{OldVersion: c.old, Version: c.v}).Raised(); got != c.want {
			t.Errorf("%d -> %d: %v", c.old, c.v, got)
		}
	}
}
