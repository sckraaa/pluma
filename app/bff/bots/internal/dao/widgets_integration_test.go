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

package dao

import (
	"context"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// A widget's request in Redis: only its bot answers it, the waiting request
// gets the answer, a user sends a bot WidgetQueriesPer requests a window
// and then waits, and an object takes one edit a second.
func TestWidgetQueriesOnRedis(t *testing.T) {
	ctx := context.Background()
	r, err := testinfra.StartRedis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Terminate(context.Background()) })
	d := &Dao{KV: kv.NewStore(r.KvConf())}

	const user, bot, other = int64(1000001), int64(2000001), int64(2000002)
	q, err := NewQueryId()
	if err != nil {
		t.Fatal(err)
	}
	if err = d.BeginWidgetQuery(ctx, q, bot); err != nil {
		t.Fatal(err)
	}
	result := "ok"
	if ok, err := d.SetWidgetAnswer(ctx, q, other, &WidgetAnswer{Result: &result}); err != nil || ok {
		t.Fatalf("another bot answered: %v, %v", ok, err)
	}
	if ok, err := d.SetWidgetAnswer(ctx, q+1, bot, &WidgetAnswer{}); err != nil || ok {
		t.Fatalf("an unknown request answered: %v, %v", ok, err)
	}
	if ok, err := d.SetWidgetAnswer(ctx, q, bot, &WidgetAnswer{Result: &result}); err != nil || !ok {
		t.Fatalf("the bot's answer: %v, %v", ok, err)
	}
	a, err := d.WaitWidgetAnswer(ctx, q)
	if err != nil || a == nil || a.Result == nil || *a.Result != "ok" {
		t.Fatalf("answer %+v, %v", a, err)
	}

	for i := range WidgetQueriesPer {
		if wait, err := d.AllowWidgetQuery(ctx, user, bot); err != nil || wait != 0 {
			t.Fatalf("request %d: wait %d, %v", i+1, wait, err)
		}
	}
	if wait, err := d.AllowWidgetQuery(ctx, user, bot); err != nil || wait < 1 || wait > widgetQueryWindow {
		t.Fatalf("one request too many: wait %d, %v", wait, err)
	}
	if wait, err := d.AllowWidgetQuery(ctx, user, other); err != nil || wait != 0 {
		t.Fatalf("another bot: wait %d, %v", wait, err)
	}

	if ok, err := d.AllowWidgetEdit(ctx, 7); err != nil || !ok {
		t.Fatalf("first edit: %v, %v", ok, err)
	}
	if ok, err := d.AllowWidgetEdit(ctx, 7); err != nil || ok {
		t.Fatalf("second edit in the second: %v, %v", ok, err)
	}
	if ok, err := d.AllowWidgetEdit(ctx, 8); err != nil || !ok {
		t.Fatalf("another object: %v, %v", ok, err)
	}
}
