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

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/inline"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// TestBuiltinAnswerOnRedis: the answers of the built-in bots on the
// production Redis, with the limiter of the server. An answer is kept for
// its cache time and past it (inlineKeepOf), shared by the next user who
// asks the same, and sent from by anyone who got it: a built-in bot owns
// no query. hide_via drops via_bot_id of a built-in bot, a GIF of @gif sent
// is counted. A provider that fails leaves nothing cached. The 61st query
// of a user in a minute is FLOOD_WAIT_X.
func TestBuiltinAnswerOnRedis(t *testing.T) {
	ctx := context.Background()
	redis, err := testinfra.StartRedis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redis.Terminate(context.Background()) })
	store := kv.NewStore(redis.KvConf())

	f := newBuiltinFixture(t)
	sent := &sentMessages{}
	counted := &gifCounter{}
	f.svc.Dao.KV = store
	f.svc.Dao.MsgClient = sent
	f.svc.Dao.SendAs = sendAsChannels{}
	f.svc.GifSends = counted
	f.svc.Limiter = logincode.NewLimiter(store)

	src := picSource(1)
	f.imports.media[src.Key()] = &inlinemedia.Media{Photo: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 60}).To_Photo()}
	f.pic.page = inline.Page{Items: []inline.Item{{Source: src, Next: "p1.1"}}, Next: "p1.1", CacheTime: 3600}
	const ann, ben = int64(10), int64(11)

	first, err := f.query(ann, inlinebots.Pic, "cat", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Results) != 1 || first.Results[0].Photo.GetId() != 60 || first.CacheTime != 3600 || first.NextOffset.GetValue() != "p1.1" {
		t.Fatalf("the answer: %v", first)
	}
	ttl, err := store.TtlCtx(ctx, "bot_inline_answer:"+fmt.Sprint(first.QueryId))
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 3600 || ttl > 3600+3600 {
		t.Errorf("the answer is kept %d s", ttl)
	}
	second, err := f.query(ben, inlinebots.Pic, "cat", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.QueryId != first.QueryId || f.pic.calls != 1 {
		t.Fatalf("the same query of another user: query %d (first %d), provider called %d times", second.QueryId, first.QueryId, f.pic.calls)
	}

	send := func(user, query int64, id string, hide bool) *mtproto.Message {
		t.Helper()
		sent.got = nil
		if _, err := f.core(user).MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{
			Peer:     mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 30}).To_InputPeer(),
			RandomId: time.Now().UnixNano(), QueryId: query, Id: id, HideVia: hide,
		}); err != nil {
			t.Fatal(err)
		}
		return sent.got.GetMessage()[0].GetMessage()
	}
	if m := send(ben, first.QueryId, first.Results[0].Id, true); m.ViaBotId != nil || m.Media.GetPhoto_FLAGPHOTO().GetId() != 60 {
		t.Fatalf("@pic sent by another user, hidden: via %v, media %v", m.ViaBotId, m.Media)
	}
	if m := send(ann, first.QueryId, first.Results[0].Id, false); m.ViaBotId.GetValue() != inlinebots.Pic.Id {
		t.Fatalf("@pic sent: via %v", m.ViaBotId)
	}

	f.gif.page = inline.Page{Items: []inline.Item{{Document: gifDocument(50)}}, CacheTime: 600}
	gifs, err := f.query(ann, inlinebots.Gif, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if m := send(ann, gifs.QueryId, gifs.Results[0].Id, true); m.ViaBotId != nil || m.Media.GetDocument().GetId() != 50 {
		t.Fatalf("@gif from the tab: via %v, media %v", m.ViaBotId, m.Media)
	}
	if len(counted.counted) != 1 || counted.counted[0] != 50 {
		t.Errorf("GIF sends counted %v", counted.counted)
	}

	f.pic.err = errors.New("search engine down")
	down, err := f.query(ann, inlinebots.Pic, "dog", "")
	if err != nil || len(down.Results) != 0 || down.CacheTime != 0 {
		t.Fatalf("a provider that fails: %v, %v", down, err)
	}
	if q, err := f.svc.CachedInlineQuery(ctx, dao.InlineCacheKey(inlinebots.Pic.Id, 0, builtinCacheKind, nil, "dog", "")); err != nil || q != 0 {
		t.Errorf("the failed answer is cached: query %d, %v", q, err)
	}
	f.pic.err = nil

	// Three queries of Ann that reached the provider so far.
	for i := int64(4); i <= builtinRate.Limit; i++ {
		if _, err := f.query(ann, inlinebots.Pic, fmt.Sprintf("q%d", i), ""); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}
	if _, err := f.query(ann, inlinebots.Pic, "over", ""); err == nil || !strings.Contains(err.Error(), "FLOOD_WAIT_") {
		t.Fatalf("query %d in a minute: %v", builtinRate.Limit+1, err)
	}
	if _, err := f.query(ben, inlinebots.Pic, "ben", ""); err != nil {
		t.Errorf("another user after Ann's limit: %v", err)
	}
	if _, err := f.query(ann, inlinebots.Pic, "cat", ""); err != nil {
		t.Errorf("a cached answer after the limit: %v", err)
	}
}
