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

package translate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func testRedis(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

func TestRedisLimits(t *testing.T) {
	mr, store := testRedis(t)
	ctx := context.Background()
	l := NewRedisLimits(store, Config{}.WithDefaults())

	if err := l.Reserve(ctx, 7, DefaultItemsPerMinute); err != nil {
		t.Fatalf("a whole minute: %v", err)
	}
	err := l.Reserve(ctx, 7, 1)
	if x := floodSeconds(err); x < 3 || x > 60 {
		t.Fatalf("over the minute: %v, want FLOOD_WAIT_X", err)
	}
	if got, _ := mr.Get("translate_items_m_7"); got != "300" {
		t.Fatalf("the minute holds %q after a refusal, want 300", got)
	}
	if err := l.Reserve(ctx, 8, 1); err != nil {
		t.Fatalf("another user: %v", err)
	}

	// The minute ends, the hour does not.
	mr.FastForward(61 * time.Second)
	l = &RedisLimits{kv: store, perMinute: 10, perHr: 15}
	if err := l.Reserve(ctx, 9, 10); err != nil {
		t.Fatal(err)
	}
	mr.FastForward(61 * time.Second)
	err = l.Reserve(ctx, 9, 10)
	if x := floodSeconds(err); x != int(MaxFloodWait/time.Second) {
		t.Fatalf("over the hour: %v, want FLOOD_WAIT_30", err)
	}
	if mr.Exists("translate_items_m_9") {
		t.Fatal("the minute keeps the items the hour refused")
	}
	if got, _ := mr.Get("translate_items_h_9"); got != "10" {
		t.Fatalf("the hour holds %q, want 10", got)
	}
	if err := l.Reserve(ctx, 9, 5); err != nil {
		t.Fatalf("what is left of the hour: %v", err)
	}
	if ttl := mr.TTL("translate_items_h_9"); ttl <= 0 || ttl > time.Hour {
		t.Fatalf("the hour expires in %v", ttl)
	}

	// Redis is gone: the items go through.
	mr.Close()
	if err := l.Reserve(ctx, 9, 100); err != nil {
		t.Fatalf("without Redis: %v", err)
	}
}

func TestCheckItems(t *testing.T) {
	c := Config{}.WithDefaults()
	emoji := "😀" // two UTF-16 code units
	for _, tc := range []struct {
		name  string
		items []*mtproto.TextWithEntities
		want  error
	}{
		{"none", nil, mtproto.ErrInputTextEmpty},
		{"empty items are items", plain("", ""), nil},
		{"longest item", plain(strings.Repeat("a", DefaultMaxItemLength)), nil},
		{"item too long", plain(strings.Repeat("a", DefaultMaxItemLength+1)), ErrInputTextTooLong},
		{"longest item in pairs", plain(strings.Repeat(emoji, DefaultMaxItemLength/2)), nil},
		{"item too long in pairs", plain(strings.Repeat(emoji, DefaultMaxItemLength/2) + "a"), ErrInputTextTooLong},
		{"most items", plain(make([]string, DefaultMaxItems)...), nil},
		{"too many items", plain(make([]string, DefaultMaxItems+1)...), ErrInputTextTooLong},
		{"longest request", plain(repeatItems(strings.Repeat("a", DefaultMaxItemLength), DefaultMaxRequestLength/DefaultMaxItemLength)...), nil},
		{"request too long", plain(append(repeatItems(strings.Repeat("a", DefaultMaxItemLength), DefaultMaxRequestLength/DefaultMaxItemLength), "a")...), ErrInputTextTooLong},
	} {
		if err := CheckItems(c, tc.items); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func repeatItems(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
