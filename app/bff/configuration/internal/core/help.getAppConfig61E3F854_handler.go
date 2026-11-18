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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/appconfig"
)

// HelpGetAppConfig61E3F854
// help.getAppConfig#61e3f854 hash:int = help.AppConfig;
func (c *ConfigurationCore) HelpGetAppConfig61E3F854(in *mtproto.TLHelpGetAppConfig61E3F854) (*mtproto.Help_AppConfig, error) {
	return c.appConfig().Reply(in.GetHash()), nil
}

// appConfig is the app config of this client: the live config with
// pluma_edge of the country of the client's address (task 109, ADR 0072).
// The country is looked up only when a record of EDGE_ROUTES names
// countries; an unknown one (no address, no GeoIP answer) gets the record
// "*".
func (c *ConfigurationCore) appConfig() *appconfig.AppConfig {
	cfg := c.svcCtx.AppConfig.Current(c.ctx)
	edges := c.svcCtx.Edges
	country := ""
	if edges.ByCountry() {
		country = c.svcCtx.Countries.Country(c.ctx, c.MD.GetClientAddr())
	}
	return edges.Config(cfg, country)
}
