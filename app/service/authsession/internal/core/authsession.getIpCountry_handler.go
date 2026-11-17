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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// AuthsessionGetIpCountry
// authsession.getIpCountry ip:string = String;
//
// The country of an address by the GeoIP database of authsession, as an
// ISO 3166-1 alpha-2 code in upper case, "" when it is not known: the route
// of a client by its country, to the edges of pluma_edge and the CDN
// deliveries (task 109, ADR 0072). No other service keeps a copy of the
// database; its callers cache the answers (pkg/ipcountry).
func (c *AuthsessionCore) AuthsessionGetIpCountry(in *authsession.TLAuthsessionGetIpCountry) (*mtproto.String, error) {
	return mtproto.MakeTLString(&mtproto.String{V: c.svcCtx.Dao.IpCountry(in.GetIp())}).To_String(), nil
}
