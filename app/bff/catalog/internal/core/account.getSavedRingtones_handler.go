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
)

// AccountGetSavedRingtones
// account.getSavedRingtones#e1902288 hash:long = account.SavedRingtones;
//
// The notification sounds the account saved. The clients keep the hash and
// ask again with it (Android RingtoneDataStore.java:60-75, tdesktop
// api_ringtones.cpp:130).
func (c *CatalogCore) AccountGetSavedRingtones(in *mtproto.TLAccountGetSavedRingtones) (*mtproto.Account_SavedRingtones, error) {
	saved, err := c.savedRingtones()
	if err != nil {
		c.Logger.Errorf("account.getSavedRingtones - error: %v", err)
		return nil, err
	}
	if notModified(in.GetHash(), saved.GetHash()) {
		return mtproto.MakeTLAccountSavedRingtonesNotModified(nil).To_Account_SavedRingtones(), nil
	}
	return saved, nil
}
