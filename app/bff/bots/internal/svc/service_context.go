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

package svc

import (
	"context"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/inline"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"github.com/teamgram/teamgram-server/pkg/webappdata"
	"github.com/teamgram/teamgram-server/pkg/webfetch"
	"github.com/teamgram/teamgram-server/pkg/webfile"

	"github.com/zeromicro/go-zero/core/logx"
)

// InlineHealth tells which built-in inline bots may be served
// (*inlinebots.Health).
type InlineHealth interface {
	Usable(id int64) bool
}

// InlineImporter has the media a provider found as photos and documents of
// the server (*inlinemedia.Importer).
type InlineImporter interface {
	Ensure(ctx context.Context, owner int64, sources []inlinemedia.Source) (inlinemedia.Imported, error)
}

// GifSends counts the GIFs sent from @gif's results (*inlinemedia.Store).
type GifSends interface {
	CountGifSend(ctx context.Context, documentId int64) error
}

// Limiter bounds how often a user may do something (*logincode.Limiter).
type Limiter interface {
	Hit(ctx context.Context, rule logincode.Rule, subject string) error
}

// ServiceContext is the state of the bots methods.
type ServiceContext struct {
	Config config.Config
	*dao.Dao

	// InlineBots tells which built-in inline bots (pkg/inlinebots) have
	// their accounts in order and may be served (ADR 0062, p. 1).
	InlineBots InlineHealth
	// InlineProviders answer the queries of the built-in bots, by the kind
	// of the bot. A kind with no provider answers with empty lists.
	InlineProviders map[inlinebots.Kind]inline.Provider
	// InlineMedia imports what the providers find into dfs.
	InlineMedia InlineImporter
	// GifSends counts the sends of @gif's GIFs, the popular GIFs of its
	// empty query.
	GifSends GifSends
	// Limiter bounds the queries of a user to the built-in bots.
	Limiter Limiter

	// WebFileKey signs the addresses of inline results' webDocument
	// (webfile.Sign); nil: they go out unsigned, as webDocumentNoProxy.
	WebFileKey []byte

	// BotKeys are the keys of bots (pkg/botkeys, task 106): the login key
	// signs the data a login button gives a site. Without
	// BOT_SECRET_ENCRYPTION_KEY it finds none and login buttons open their
	// links as they are.
	BotKeys BotKeys
	// WebAppSigner signs the initData of mini apps with the server's
	// Ed25519 key (WEBAPP_SIGNING_KEY, ADR 0068 p. 3); nil without it, and
	// then no mini app opens (BOT_WEBVIEW_DISABLED).
	WebAppSigner *webappdata.Signer
	// OAuth keeps the login requests of sites through oauth.pluma.chat
	// (pkg/urlauth, task 106 part I) in the KV, where the provider in links
	// makes them and reads their outcome.
	OAuth *urlauth.Store
}

// BotKeys finds the keys of a bot's current token (*botkeys.Store).
type BotKeys interface {
	Get(ctx context.Context, botId int64) (keys botkeys.Keys, ok bool, err error)
}

// NewServiceContext connects the service. It checks the accounts of the
// built-in inline bots before it returns and keeps checking them in the
// background: a bot whose account does not match the registry is not served
// until it does, the rest work.
func NewServiceContext(c config.Config) *ServiceContext {
	key, err := webfile.Key(c.WebFileKey)
	logx.Must(err)
	sealer, err := c.BotSecrets.Sealer()
	logx.Must(err)
	signer, err := c.WebApp.Signer()
	logx.Must(err)
	dailyBytes, err := inlinemedia.ParseDailyBytes(c.InlineBots.ImportDailyBytes)
	logx.Must(err)
	db := sqlx.NewMySQL(&c.Mysql)
	health := inlinebots.NewHealth()
	health.Watch(context.Background(), db)

	d := dao.New(c)
	purgePreparedEvery(context.Background(), d.Bots, preparedPurgeEvery)
	media := inlinemedia.NewStore(db)
	importer := inlinemedia.NewImporter(
		media,
		inlinemedia.NewBudget(d.KV, dailyBytes),
		inlinemedia.NewFetcher(c.WebFetch),
		webfetch.RPCPhotos{
			Dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
			Media: d.MediaClient,
		},
		d.MediaClient,
	)
	// The providers of the built-in bots by kind (task 099): @gif searches
	// KLIPY (part G) when KlipyAPIKey is set, and has the GIFs popular
	// within the server (part D) otherwise, never both in one list (ADR
	// 0062, p. 4.1); @pic searches pictures with SearXNG (part E) when
	// SearxngURL names one, and answers with empty lists otherwise.
	var gifs inline.Provider = inline.NewLocal(inline.NewGifCounts(db), d.MediaClient, d.KV)
	if c.InlineBots.GifSearch() {
		klipy, err := inline.NewKlipy(inline.KlipyURL, c.InlineBots.KlipyAPIKey, d.KV)
		logx.Must(err)
		gifs = klipy
	}
	providers := map[inlinebots.Kind]inline.Provider{
		inlinebots.KindGif: gifs,
	}
	if c.InlineBots.SearxngURL != "" {
		pictures, err := inline.NewSearxng(c.InlineBots.SearxngURL, d.KV)
		logx.Must(err)
		providers[inlinebots.KindPhoto] = pictures
	}
	return &ServiceContext{
		Config:          c,
		Dao:             d,
		InlineBots:      health,
		InlineProviders: providers,
		InlineMedia:     importer,
		GifSends:        gifSends{media},
		Limiter:         logincode.NewLimiter(d.KV),
		WebFileKey:      key,
		BotKeys:         botkeys.NewStore(db, sealer),
		WebAppSigner:    signer,
		OAuth:           urlauth.NewStore(d.KV),
	}
}

// gifSends counts sends on the UTC day of the moment.
type gifSends struct {
	store *inlinemedia.Store
}

func (g gifSends) CountGifSend(ctx context.Context, documentId int64) error {
	return g.store.CountGifSend(ctx, documentId, time.Now())
}
