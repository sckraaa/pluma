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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/autodownload"
)

// AccountSaveAutoDownloadSettings
// account.saveAutoDownloadSettings#76f36233 flags:# low:flags.0?true high:flags.1?true settings:AutoDownloadSettings = Bool;
//
// What the account uses for auto-download under one of the three names:
// low, high, or medium without a flag. It is kept for the account
// (pkg/autodownload says why it is not given back as a preset); low and
// high together name nothing and are refused.
func (c *AutoDownloadCore) AccountSaveAutoDownloadSettings(in *mtproto.TLAccountSaveAutoDownloadSettings) (*mtproto.Bool, error) {
	if in.GetSettings() == nil || (in.GetLow() && in.GetHigh()) {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("account.saveAutoDownloadSettings - error: %v", err)
		return nil, err
	}
	preset := autodownload.Medium
	switch {
	case in.GetLow():
		preset = autodownload.Low
	case in.GetHigh():
		preset = autodownload.High
	}
	if err := c.svcCtx.Saved.Save(c.ctx, c.MD.UserId, preset, autodownload.FromTL(in.GetSettings()), time.Now().Unix()); err != nil {
		c.Logger.Errorf("account.saveAutoDownloadSettings - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
