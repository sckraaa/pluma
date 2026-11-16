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
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"

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
	// domains of bots, from the database (task 106), and the translation
	// switched off without an engine (task 101).
	AppConfig *appconfig.Live
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
