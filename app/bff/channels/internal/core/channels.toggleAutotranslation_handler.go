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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsToggleAutotranslation
// channels.toggleAutotranslation#167fc0a1 channel:InputChannel enabled:Bool = Updates;
//
// A broadcast channel translated for all its subscribers (task 082, ADR
// 0050; core.telegram.org/api/translation), by an admin who may change its
// info; the members load the channel again and see channel.autotranslation.
// The clients show the switch to every channel at boost level 0
// (channel_autotranslation_level_min, ADR 0041); the translation itself is
// the clients' or messages.translateText's (task 027).
func (c *ChannelsCore) ChannelsToggleAutotranslation(in *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	return c.toggle(in.Channel, channelpb.SettingAutotranslation, mtproto.FromBool(in.Enabled))
}
