//go:build integration

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

package urlauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

var itRedis *testinfra.Redis

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itRedis, err = testinfra.StartRedis(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

func itStore() *Store {
	return NewStore(kv.NewStore(itRedis.KvConf()))
}

// The scripts behave on a real Redis as on miniredis.
func TestStoreRedis(t *testing.T) {
	storeSuite(t, itStore())
}

// Users accepting one request at once: one of them logs the site in.
func TestStoreRedisRaceAccept(t *testing.T) {
	s := itStore()
	ctx := context.Background()
	e, err := s.Create(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []int64
	)
	for u := int64(1); u <= 16; u++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Accept(ctx, e.Token, Acceptance{UserId: u, Code: e.Code})
			if errors.Is(err, ErrExpired) {
				return
			}
			if err != nil {
				t.Errorf("user %d: %v", u, err)
				return
			}
			if r == CodeRight {
				mu.Lock()
				winners = append(winners, u)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("winners %v", winners)
	}
	got, err := s.Get(ctx, e.Token)
	if err != nil || got.UserId != winners[0] {
		t.Fatalf("%+v, %v", got, err)
	}
}

// Wrong codes sent at once: the request takes MaxWrongCodes of them and
// ends exactly once.
func TestStoreRedisRaceWrongCodes(t *testing.T) {
	s := itStore()
	ctx := context.Background()
	e, err := s.Create(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	wrong := wrongOf(e)
	var (
		wg                    sync.WaitGroup
		mu                    sync.Mutex
		wrongs, ends, expired int
	)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.CheckCode(ctx, e.Token, wrong)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrExpired):
				expired++
			case err != nil:
				t.Error(err)
			case r == CodeWrong:
				wrongs++
			case r == CodeEnded:
				ends++
			}
		}()
	}
	wg.Wait()
	if wrongs != MaxWrongCodes-1 || ends != 1 || expired != 12-MaxWrongCodes {
		t.Fatalf("wrong %d, ended %d, expired %d", wrongs, ends, expired)
	}
}
