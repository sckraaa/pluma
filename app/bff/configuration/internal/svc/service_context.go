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
	"fmt"
	"os"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/appconfig"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/releasesite"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/ipcountry"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// clientConfigFile is the config of help.getConfig.
	clientConfigFile = "./config.json"
	// appConfigFile is the client configuration of help.getAppConfig, next to
	// config.json of help.getConfig.
	appConfigFile = "./app_config.json"
	// loginDomainsEvery is how often the login domains of bots, the
	// url_auth_domains of the app config, are read again (task 106).
	loginDomainsEvery = 60 * time.Second
)

type ServiceContext struct {
	Config config.Config
	// ClientConfig is config.json, the base of every help.getConfig answer;
	// handlers clone it before filling the per-request fields.
	ClientConfig *mtproto.TLConfig
	// AppConfig is app_config.json with url_auth_domains, the login
	// domains of bots, from the database (task 106), the translation
	// switched off without an engine (task 101), the emoji categories of
	// the GIF tab while @gif searches by text (task 099), and the public
	// map tiles of the clients' maps (task 112).
	AppConfig *appconfig.Live
	// Edges is pluma_edge of the app config by the country of the client
	// (task 109, ADR 0072); nil without edges.
	Edges *appconfig.Edges
	// Probe is pluma_probe of the app config by the country of the client
	// (task 113, ADR 0073 §7).
	Probe *appconfig.Probe
	// ReleaseSites is the site of the releases of the client apps and its
	// mirrors by the country of the client (tasks 054, 113); nil without
	// PLUMA_RELEASES_URL.
	ReleaseSites *releasesite.Sites
	// ReleaseFiles is where the clients download the files of /dl/ from,
	// the caches of the site by the country of the client (task 115); nil
	// without PLUMA_RELEASES_URL.
	ReleaseFiles *releasesite.Sites
	// Countries tells the country of a client's address
	// (authsession.getIpCountry, cached); used only when Edges has
	// records of countries, for Probe, and when ReleaseSites or
	// ReleaseFiles has records of countries.
	Countries *ipcountry.Locator
	// IOSUpdate asks the install service what an Ad Hoc build of the iPhone
	// app can update to from the app (task 085); nil when that is off.
	IOSUpdate *iosupdate.Checker
	*dao.Dao
}

func NewServiceContext(c config.Config) *ServiceContext {
	clientConfig, err := LoadClientConfig(clientConfigFile)
	logx.Must(err)
	appConfig, err := appconfig.Load(appConfigFile)
	logx.Must(err)
	if !c.Translate.Enabled() {
		// No engine: the clients must not offer what would be refused
		// (ADR 0064, section 9).
		appConfig = appConfig.WithoutTranslation()
	}
	if c.GifSearch {
		// @gif searches by text: each emoji category of the GIF tab is a
		// query it answers (ADR 0062, p. 2).
		appConfig = appConfig.WithGifSearchEmojies(inlinebots.GifSearchEmojies)
	}
	// The public map tiles (task 112, ADR 0070): a wrong MAPS_PUBLIC_URL
	// stops the start instead of leaving the clients' maps silently off.
	mapsPublic, err := c.Maps.Public()
	logx.Must(err)
	if mapsPublic.Enabled() {
		appConfig = appConfig.WithMaps(mapsPublic.URL, mapsPublic.Styles)
	}
	// The edges of the patched clients (task 109): a wrong value stops the
	// start instead of handing the clients a pluma_edge they drop.
	edges, err := appconfig.ParseEdges(c.Edge.WSURLs, c.Edge.Routes, c.Edge.StickyS)
	logx.Must(err)
	// The settings of the path probes (task 113) and the mirrors of the
	// releases' site: a wrong value stops the start, as above.
	probe, err := appconfig.ParseProbe(c.Probe.Variables())
	logx.Must(err)
	releaseSites, err := releasesite.Parse(c.ReleasesURL, c.ReleasesRoutes)
	logx.Must(err)
	releaseFiles, err := releasesite.ParseFiles(releaseSites, c.ReleasesFilesURL, c.ReleasesFilesRoutes)
	logx.Must(err)
	logx.Must(c.IOSUpdate.Check())
	var iosUpdate *iosupdate.Checker
	if c.IOSUpdate.Enabled() {
		iosUpdate = iosupdate.NewChecker(c.IOSUpdate.StatusURL)
	}
	d := dao.New(c)
	return &ServiceContext{
		Config:       c,
		ClientConfig: clientConfig,
		AppConfig:    appconfig.NewLive(appConfig, botstore.New(d.DB()).LoginDomains, loginDomainsEvery),
		Edges:        edges,
		Probe:        probe,
		ReleaseSites: releaseSites,
		ReleaseFiles: releaseFiles,
		Countries:    ipcountry.New(d.AuthsessionClient),
		IOSUpdate:    iosUpdate,
		Dao:          d,
	}
}

// LoadClientConfig reads the config of help.getConfig from path.
func LoadClientConfig(path string) (*mtproto.TLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := new(mtproto.TLConfig)
	if err = jsonx.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}
