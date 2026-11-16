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
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// HelpGetCdnConfig
// help.getCdnConfig#52029342 = CdnConfig;
//
// The keys of the MTProto CDN DCs, which the clients check the key a CDN
// DC offers in resPQ against: none, with the CDN on or off, since the
// deliveries of Pluma are HTTPS and no client connects to their dc_id
// (task 107, ADR 0071 §9).
func (c *FilesCore) HelpGetCdnConfig(in *mtproto.TLHelpGetCdnConfig) (*mtproto.CdnConfig, error) {
	return cdn.EmptyCdnConfig(), nil
}
