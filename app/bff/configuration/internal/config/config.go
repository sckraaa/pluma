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

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql             sqlx.Config // dismissed suggestions
	UserClient        zrpc.RpcClientConf
	AuthsessionClient zrpc.RpcClientConf // the client's time zone, its app (help.getAppUpdate)
	// MediaClient gives the APK of an Android release (help.getAppUpdate).
	MediaClient zrpc.RpcClientConf
	// ReleasesURL is where the releases are, the site's /dl/ (task 054):
	// the update prefix of the desktop app (help.getConfig) and the install
	// page of the iPhone app.
	ReleasesURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// IOSUpdate lets an Ad Hoc build of the iPhone app update from the app
	// (task 085); off without its secret.
	IOSUpdate iosupdate.Settings `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Translate is the translation engine of bff (task 101, ADR 0064);
	// without its URL the app config hides the translation from the
	// clients.
	Translate translate.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Dc1Addresses are the addresses of DC 1 in dc_options of
	// help.getConfig (PLUMA_DC1_ADDRESSES, task 102, ADR 0065 p. 7): ip:port
	// separated by commas, the port built into the client apps first.
	// Empty: no DC 1 in dc_options.
	Dc1Addresses string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Cdn is the CDN-DC block of bff, the one files reads too: when the CDN
	// is on, its DC goes into dc_options (DcOptions).
	Cdn cdn.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}
