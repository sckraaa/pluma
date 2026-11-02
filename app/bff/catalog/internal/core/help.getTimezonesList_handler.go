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

// HelpGetTimezonesList
// help.getTimezonesList#49b30240 hash:int = help.TimezonesList;
//
// The time zones of business hours. From the catalog; nothing while it is not imported.
func (c *CatalogCore) HelpGetTimezonesList(in *mtproto.TLHelpGetTimezonesList) (*mtproto.Help_TimezonesList, error) {
	answer := &mtproto.Help_TimezonesList{}
	hash, found, err := c.stored("help.getTimezonesList", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return mtproto.MakeTLHelpTimezonesList(&mtproto.Help_TimezonesList{Timezones: []*mtproto.Timezone{}}).To_Help_TimezonesList(), nil
	}
	if notModified(int64(in.GetHash()), hash) {
		return mtproto.MakeTLHelpTimezonesListNotModified(nil).To_Help_TimezonesList(), nil
	}
	return answer, nil
}
