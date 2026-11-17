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

package cdn

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemStore(t *testing.T) { runStoreSuite(t, NewMemStore()) }

func TestMemStoreErr(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	f := ensure(t, m, FileKey{Kind: KindDocument, Id: 1}, 2<<20)
	down := errors.New("mysql down")
	m.SetErr(down)
	if _, err := m.Get(ctx, f.Key); !errors.Is(err, down) {
		t.Errorf("Get: %v", err)
	}
	if err := m.Enqueue(ctx, f.RowId, TargetHomeOrigin, 0, 1); !errors.Is(err, down) {
		t.Errorf("Enqueue: %v", err)
	}
	if _, err := m.Claim(ctx, TargetHomeOrigin, time.Minute, 1); !errors.Is(err, down) {
		t.Errorf("Claim: %v", err)
	}
	if _, err := m.DeliveryHealth(ctx, 201); !errors.Is(err, down) {
		t.Errorf("DeliveryHealth: %v", err)
	}
	if err := m.AddUsage(ctx, 201, "2026-11", 1, 1); !errors.Is(err, down) {
		t.Errorf("AddUsage: %v", err)
	}
	m.SetErr(nil)
	if got, err := m.Get(ctx, f.Key); err != nil || got == nil {
		t.Errorf("Get after recovery: %v, %v", got, err)
	}
}

// TestMemStoreCopies checks that what the MemStore returns is a copy: a
// caller changing it does not change the store.
func TestMemStoreCopies(t *testing.T) {
	ctx := context.Background()
	m := NewMemStore()
	f := ensure(t, m, FileKey{Kind: KindDocument, Id: 1}, 2<<20)
	got, err := m.SetKey(ctx, f.RowId, mustSecrets(t))
	if err != nil {
		t.Fatal(err)
	}
	tok0 := got.Secrets.Token[0]
	got.Secrets.Token[0] ^= 0xff
	again, _ := m.Get(ctx, f.Key)
	if again.Secrets.Token[0] != tok0 {
		t.Error("a returned file shares its token with the store")
	}
}
