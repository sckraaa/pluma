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

// editedLiveLocation is the media of the live location m after the edit
// in: a new point, heading or proximity radius, a new period, or the stop.
// The clients send only what changes:
//   - Android moves the point without the period (messenger/
//     LocationController.java:379-397) and sends the period only to
//     extend it, as the whole period from the date (ui/LocationActivity.
//     java:1849); iOS always sends the period it has (TelegramCore
//     PendingMessages/RequestEditMessage.swift:450-461). A period left out
//     keeps the one of the message.
//   - The stop is stopped with no period: Android with inputGeoPointEmpty
//     (messenger/LocationController.java:725-731), iOS with the last point
//     (RequestEditMessage.swift:463). An empty point keeps the last one:
//     tdesktop drops a location without a point from the message
//     (history/history_item.cpp:407-415, Data::LocationPoint::FromMTP).
//     The stopped location ends now: the clients read a live location as
//     live while date + period is ahead (Android ui/LocationActivity.java:
//     2483, iOS PeerLiveLocationsContext.swift:22), and tell no stop
//     apart. That the server ends it so is unverified against Telegram.
func editedLiveLocation(m *mtproto.Message, in *mtproto.TLInputMediaGeoLive, now int32) *mtproto.MessageMedia {
	old := m.GetMedia()
	media := &mtproto.MessageMedia{
		Geo:                         old.GetGeo(),
		Heading:                     in.GetHeading(),
		Period:                      old.GetPeriod(),
		ProximityNotificationRadius: old.GetProximityNotificationRadius(),
	}
	if p := in.GetGeoPoint(); p.GetPredicateName() == mtproto.Predicate_inputGeoPoint {
		media.Geo = mtproto.MakeGeoPointByInput(p)
	}
	if r := in.GetProximityNotificationRadius(); r != nil {
		media.ProximityNotificationRadius = r
	}
	if p := in.GetPeriod(); p != nil && p.GetValue() > 0 {
		media.Period = p.GetValue()
	}
	if in.GetStopped() {
		media.Heading = nil
		if elapsed := int64(now) - int64(m.GetDate()); elapsed < int64(media.Period) {
			media.Period = int32(max(elapsed, 0))
		}
	}
	return mtproto.MakeTLMessageMediaGeoLive(media).To_MessageMedia()
}
