// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package svc

import (
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/files/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/files/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/files/plugin"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/catalog/attached"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/webfile"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

type ServiceContext struct {
	Config config.Config
	*dao.Dao
	Plugin1 plugin.ThumbFilesPlugin
	Plugin2 plugin.StreamFilesPlugin
	// Attached keeps the stickers put on uploaded photos and videos (task
	// 084); nil: none are kept.
	Attached *attached.Store
	// WebFiles serves upload.getWebFile (ADR 0062, p. 5), the maps among
	// them when Maps is set.
	WebFiles *webfile.Server
	// Maps renders the maps of inputWebFileGeoPointLocation (task 100, ADR
	// 0063); nil: maps are off, and such a location is LOCATION_INVALID.
	Maps *staticmap.Service
	// CDN decides the redirects of upload.getFile and serves the CDN
	// methods (task 102, ADR 0065); nil: the CDN is off.
	CDN *cdn.Home
	// cdn runs the pusher and the prober of the CDN; nil when it is off.
	cdn *cdn.Runtime
	// Business checks the uploads of a connected bot of Telegram Business
	// for a chat of its account (task 105).
	Business *business.Store
}

func NewServiceContext(c config.Config, plugin1 plugin.ThumbFilesPlugin, plugin2 plugin.StreamFilesPlugin) *ServiceContext {
	webFiles, err := webfile.New(c.WebFile, c.WebFetch)
	logx.Must(err)
	maps, err := newMaps(c)
	logx.Must(err)
	if maps != nil {
		webFiles.Register(mtproto.Predicate_inputWebFileGeoPointLocation, NewGeoSource(maps))
	}
	d := dao.New(c)
	runtime, err := startCDN(c, d)
	logx.Must(err)
	db := sqlx.NewMySQL(&c.Mysql)
	s := &ServiceContext{
		Config:   c,
		Dao:      d,
		Plugin1:  plugin1,
		Plugin2:  plugin2,
		Attached: attached.NewStore(db),
		Business: business.NewStore(db),
		WebFiles: webFiles,
		Maps:     maps,
		cdn:      runtime,
	}
	if runtime != nil {
		s.CDN = runtime.Home
	}
	return s
}

// Stop stops the work beside the methods: the pusher and the prober of
// the CDN.
func (s *ServiceContext) Stop() {
	if s.cdn != nil {
		s.cdn.Stop()
	}
}

// startCDN starts the CDN of c, nil when it is off. A wrong Cdn block
// stops the start instead of leaving the CDN silently off.
func startCDN(c config.Config, d *dao.Dao) (*cdn.Runtime, error) {
	set, err := c.Cdn.Parse()
	if err != nil || set == nil {
		return nil, err
	}
	files := &cdnFiles{media: d.MediaClient, encrypted: d.SecretChats, dfs: d.DfsClient}
	r, err := cdn.Start(cdn.NewMySQLStore(sqlx.NewMySQL(&c.Mysql)), set, files, files)
	if err != nil {
		return nil, err
	}
	logx.Infof("files: CDN on, DC %d", set.DcId)
	return r, nil
}

// newMaps returns the map service of c, nil when maps are off. A wrong
// Maps block stops the start instead of leaving the maps silently off.
func newMaps(c config.Config) (*staticmap.Service, error) {
	if !c.Maps.Enabled() {
		return nil, nil
	}
	if len(c.KV) == 0 {
		return nil, errors.New("files: Maps needs KV, the Redis the maps are kept in")
	}
	return staticmap.New(c.Maps, kv.NewStore(c.KV))
}
