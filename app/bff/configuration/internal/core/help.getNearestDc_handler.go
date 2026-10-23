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
)

// HelpGetNearestDc
// help.getNearestDc#1fb33026 = NearestDc;
//
// Pluma is a single datacenter. The country comes from the client's IP in
// Telegram; the server has no IP geolocation database, so it answers that
// the country is unknown and the login screen takes the SIM card's country.
func (c *ConfigurationCore) HelpGetNearestDc(in *mtproto.TLHelpGetNearestDc) (*mtproto.NearestDc, error) {
	_ = in

	return mtproto.MakeTLNearestDc(&mtproto.NearestDc{
		Country:   "",
		ThisDc:    1,
		NearestDc: 1,
	}).To_NearestDc(), nil
}
