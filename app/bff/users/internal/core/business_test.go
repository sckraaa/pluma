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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// The Business part of userFull (task 105): open_now is the server's at
// the moment of the answer, a sticker whose document is gone leaves the
// intro without one, and hours whose time zone cannot be read are left
// out while the rest is answered.
func TestSetBusinessProfile(t *testing.T) {
	monday0930Berlin := time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC)
	p := business.Profile{
		Hours:    &business.Hours{TimezoneID: "Europe/Berlin", Intervals: []business.Interval{{Start: 540, End: 1080}}},
		Location: &business.Location{Address: "Main St 1", HasGeo: true, Lat: 52.5, Long: 13.4},
		Intro:    &business.Intro{Title: "Hi", Description: "Ask", StickerID: 5},
	}
	noFail := func(err error) { t.Errorf("failed: %v", err) }

	full := &mtproto.UserFull{}
	setBusinessProfile(full, p, mtproto.MakeTLDocument(&mtproto.Document{Id: 5}).To_Document(), monday0930Berlin, noFail)
	if h := full.GetBusinessWorkHours(); !h.GetOpenNow() || h.GetTimezoneId() != "Europe/Berlin" || len(h.GetWeeklyOpen()) != 1 {
		t.Errorf("the hours at 09:30: %v", h)
	}
	if l := full.GetBusinessLocation(); l.GetAddress() != "Main St 1" || l.GetGeoPoint().GetLat() != 52.5 {
		t.Errorf("the location: %v", l)
	}
	if i := full.GetBusinessIntro(); i.GetTitle() != "Hi" || i.GetSticker().GetId() != 5 {
		t.Errorf("the intro: %v", i)
	}

	full = &mtproto.UserFull{}
	setBusinessProfile(full, p, mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 5}).To_Document(), monday0930Berlin.Add(9*time.Hour), noFail)
	if h := full.GetBusinessWorkHours(); h == nil || h.GetOpenNow() {
		t.Errorf("the hours at 18:30: %v", h)
	}
	if i := full.GetBusinessIntro(); i == nil || i.GetSticker() != nil {
		t.Errorf("the intro with its sticker gone: %v", i)
	}

	full = &mtproto.UserFull{}
	var failed error
	setBusinessProfile(full, business.Profile{
		Hours:    &business.Hours{TimezoneID: "Mars/Olympus", Intervals: []business.Interval{{Start: 0, End: 60}}},
		Location: &business.Location{Address: "Main St 1"},
	}, nil, monday0930Berlin, func(err error) { failed = err })
	if failed == nil || full.GetBusinessWorkHours() != nil || full.GetBusinessLocation().GetAddress() != "Main St 1" || full.GetBusinessIntro() != nil {
		t.Errorf("hours of an unknown time zone: %v, %v", failed, full)
	}

	full = &mtproto.UserFull{}
	setBusinessProfile(full, business.Profile{}, nil, monday0930Berlin, noFail)
	if full.GetBusinessWorkHours() != nil || full.GetBusinessLocation() != nil || full.GetBusinessIntro() != nil {
		t.Errorf("an empty profile: %v", full)
	}
}
