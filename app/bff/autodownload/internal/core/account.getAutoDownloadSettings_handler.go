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
	"github.com/teamgram/teamgram-server/pkg/autodownload"
)

// AccountGetAutoDownloadSettings
// account.getAutoDownloadSettings#56da0b3f = account.AutoDownloadSettings;
//
// The three presets every account chooses from (pkg/autodownload): the
// clients replace their presets with the answer, so it is the same for
// everyone and does not follow account.saveAutoDownloadSettings.
func (c *AutoDownloadCore) AccountGetAutoDownloadSettings(in *mtproto.TLAccountGetAutoDownloadSettings) (*mtproto.Account_AutoDownloadSettings, error) {
	presets := autodownload.Presets()
	return mtproto.MakeTLAccountAutoDownloadSettings(&mtproto.Account_AutoDownloadSettings{
		Low:    presets[autodownload.Low].TL(),
		Medium: presets[autodownload.Medium].TL(),
		High:   presets[autodownload.High].TL(),
	}).To_Account_AutoDownloadSettings(), nil
}
