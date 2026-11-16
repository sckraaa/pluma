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

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func geoLiveEdit(stopped bool, point *mtproto.InputGeoPoint, period, heading *wrapperspb.Int32Value) *mtproto.TLInputMediaGeoLive {
	return mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{
		Stopped: stopped, GeoPoint: point, Period: period, Heading: heading,
	})
}

func inputPoint(lat, long float64) *mtproto.InputGeoPoint {
	return mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint()
}

// The edits the clients send for a live location: Android moves it without
// the period and stops it with no point, iOS sends the period every time
// and the last point on the stop; extending sends the whole period.
func TestEditedLiveLocation(t *testing.T) {
	const date = locNow - 600
	stored := func() *mtproto.Message {
		m := liveLocationMessage(5, date, 900)
		m.Media.ProximityNotificationRadius = &wrapperspb.Int32Value{Value: 100}
		return m
	}

	// Android: a new point and heading, no period.
	got := editedLiveLocation(stored(), geoLiveEdit(false, inputPoint(1.5, 2.5), nil, &wrapperspb.Int32Value{Value: 90}), locNow)
	if got.GetPredicateName() != mtproto.Predicate_messageMediaGeoLive || got.GetPeriod() != 900 ||
		got.GetGeo().GetLat() != 1.5 || got.GetGeo().GetLong() != 2.5 || got.GetHeading().GetValue() != 90 ||
		got.GetProximityNotificationRadius().GetValue() != 100 {
		t.Errorf("a move without the period: %v", got)
	}
	if !liveLocationNow(&mtproto.Message{PredicateName: mtproto.Predicate_message, Date: date, Media: got}, locNow) {
		t.Error("a moved location is not live")
	}

	// Extending: the whole period from the date; the indefinite one too.
	if got = editedLiveLocation(stored(), geoLiveEdit(false, inputPoint(1, 2), wrapperspb.Int32(900+3600), nil), locNow); got.GetPeriod() != 4500 {
		t.Errorf("extended period %d, want 4500", got.GetPeriod())
	}
	if got = editedLiveLocation(stored(), geoLiveEdit(false, inputPoint(1, 2), wrapperspb.Int32(0x7fffffff), nil), locNow); got.GetPeriod() != 0x7fffffff {
		t.Errorf("indefinite period %d", got.GetPeriod())
	}

	// Android's stop: no point, the last one stays; it ends now.
	empty := mtproto.MakeTLInputGeoPointEmpty(nil).To_InputGeoPoint()
	got = editedLiveLocation(stored(), geoLiveEdit(true, empty, nil, nil), locNow)
	if got.GetGeo().GetPredicateName() != mtproto.Predicate_geoPoint || got.GetGeo().GetLat() != 55.75 || got.GetPeriod() != 600 || got.GetHeading() != nil {
		t.Errorf("a stop with no point: %v", got)
	}
	if liveLocationNow(&mtproto.Message{PredicateName: mtproto.Predicate_message, Date: date, Media: got}, locNow) {
		t.Error("a stopped location is live")
	}
	// iOS's stop: the last point.
	if got = editedLiveLocation(stored(), geoLiveEdit(true, inputPoint(3, 4), nil, nil), locNow); got.GetGeo().GetLat() != 3 || got.GetPeriod() != 600 {
		t.Errorf("a stop with the point: %v", got)
	}
	// A stop of an indefinite location, and of one already ended, which
	// keeps its period.
	m := stored()
	m.Media.Period = 0x7fffffff
	if got = editedLiveLocation(m, geoLiveEdit(true, empty, nil, nil), locNow); got.GetPeriod() != 600 {
		t.Errorf("a stopped indefinite location: period %d, want 600", got.GetPeriod())
	}
	m = stored()
	m.Media.Period = 60
	if got = editedLiveLocation(m, geoLiveEdit(true, empty, nil, nil), locNow); got.GetPeriod() != 60 {
		t.Errorf("a stop of an ended location: period %d, want 60", got.GetPeriod())
	}
}
