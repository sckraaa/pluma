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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"

	"google.golang.org/grpc/status"
)

// errMegagroupGeoRequired is Telegram's answer of channels.editLocation for
// a supergroup that is no geo group, "The method works only on geogroups"
// (core.telegram.org/method/channels.editLocation); the vendored schema has
// no variable for it.
var errMegagroupGeoRequired = status.Error(mtproto.ErrBadRequest, "MEGAGROUP_GEO_REQUIRED")

// ChannelsEditLocation
// channels.editLocation#58e63f6d channel:InputChannel geo_point:InputGeoPoint address:string = Bool;
//
// The address of a geo group. Pluma has no geo groups (task 082, ADR 0050):
// none of the pinned clients creates one or lists people nearby any more
// (Android dropped PeopleNearbyActivity in 11.1.1), and channelFull never
// carries can_set_location, without which Android, the one client with the
// item, does not show it (ChatEditActivity.java:875-899). The answer is
// Telegram's for a group that is no geo group, after its checks of the
// channel and the right; the point is neither kept nor logged.
func (c *ChannelsCore) ChannelsEditLocation(in *mtproto.TLChannelsEditLocation) (*mtproto.Bool, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	if !ch.GetMegagroup() {
		return nil, mtproto.ErrMegagroupRequired
	}
	me, _ := mc.GetParticipant(c.MD.UserId)
	if err = ch.Can(me, channelperm.ChangeInfo, int32(time.Now().Unix())); err != nil {
		return nil, err
	}
	return nil, errMegagroupGeoRequired
}
