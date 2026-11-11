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

package dao

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// SetMainTab makes tab (mtproto.ProfileTabType*) the tab the channel's
// profile opens on (channels.setMainProfileTab, channelFull.main_tab), for
// those who may change its info, as tdesktop offers it to them
// (TabsHost::canSetMainTab, info_profile_tabs_host.cpp:283-288:
// canEditInformation). The members load the full channel again. A tab the
// channel has already changes nothing; an unknown one is
// INPUT_CONSTRUCTOR_INVALID.
func (d *Dao) SetMainTab(b *userbox.Box, channelId int64, tab int32) (*ChannelChange, error) {
	if tab == mtproto.ProfileTabTypeUnknown {
		return nil, mtproto.ErrInputConstructorInvalid
	}
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	if _, err := infoEditor(b, channelId); err != nil {
		return nil, err
	}
	res, err := b.Tx().Exec("UPDATE channels SET main_tab = ?, version = version + 1 WHERE id = ? AND main_tab <> ?", tab, channelId, tab)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}
