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
	"slices"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/proto"
)

// saveProfile runs write, a change of the Business profile of the caller,
// in a box transaction of the caller, with updateUser about the caller
// with seq to its other devices (ADR 0020): "changes will be emitted to
// all other logged-in sessions using updateUser"
// (core.telegram.org/api/business). tdesktop reloads the full user on it
// (api/api_updates.cpp:2178-2185). The caller's device keeps what it sent.
// Other users get nothing: they read userFull again when they open the
// profile, as for the bio.
func (c *BusinessCore) saveProfile(method string, write func(tx *sqlx.Tx, now int64) error) error {
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	self := me.ToSelfUser()
	res, err := userbox.Run(c.ctx, c.svcCtx.Dao.BoxDB, c.MD.UserId, func(b *userbox.Box) error {
		if err := write(b.Tx(), time.Now().UnixMilli()); err != nil {
			return err
		}
		return sync_client.EmitSeqUpdateExcept(b, c.svcCtx.Dao.SyncTopic,
			mtproto.MakeTLUpdateUser(&mtproto.Update{UserId: c.MD.UserId}).To_Update(),
			[]*mtproto.User{self}, nil, c.MD.PermAuthKeyId)
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return nil
}

// checkTimezone checks that id is a time zone of help.getTimezonesList,
// the list the clients choose from (Android loads it on entering Business,
// ui/PremiumPreviewFragment.java:1487-1489), and one the server can tell
// the time in, for open_now. Without a list in the catalog every time zone
// the server knows is taken: the list of the catalog is empty until it is
// imported (help.getTimezonesList_handler.go of the catalog).
func (c *BusinessCore) checkTimezone(method, id string) error {
	if _, err := business.LoadTimezone(id); err != nil {
		c.Logger.Errorf("%s - time zone: %v", method, err)
		return mtproto.ErrTimezoneInvalid
	}
	a, err := c.svcCtx.Dao.Catalog.Answer(c.ctx, "help.getTimezonesList", "")
	if err != nil {
		c.Logger.Errorf("%s - the catalog: %v", method, err)
		return err
	}
	if a == nil {
		return nil
	}
	list := &mtproto.Help_TimezonesList{}
	if err := proto.Unmarshal(a.Data, list); err != nil {
		c.Logger.Errorf("%s - the time zones of the catalog: %v", method, err)
		return err
	}
	if len(list.GetTimezones()) == 0 || slices.ContainsFunc(list.GetTimezones(), func(t *mtproto.Timezone) bool { return t.GetId() == id }) {
		return nil
	}
	c.Logger.Errorf("%s - time zone %q is not in the list", method, id)
	return mtproto.ErrTimezoneInvalid
}
