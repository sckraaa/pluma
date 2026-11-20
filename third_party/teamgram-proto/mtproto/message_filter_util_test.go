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

package mtproto

import "testing"

// pluma: TestGetMediaTypeGeo: a live location is MEDIA_GEO_LIVE, the type
// messages.getRecentLocations selects by; a plain location and a venue stay
// MEDIA_EMPTY, as inputMessagesFilterGeo reads the media itself.
func TestGetMediaTypeGeo(t *testing.T) {
	geo := MakeTLGeoPoint(&GeoPoint{Lat: 55.75, Long: 37.62}).To_GeoPoint()
	for _, c := range []struct {
		media *MessageMedia
		want  int32
	}{
		{MakeTLMessageMediaGeoLive(&MessageMedia{Geo: geo, Period: 900}).To_MessageMedia(), MEDIA_GEO_LIVE},
		{MakeTLMessageMediaGeoLive(&MessageMedia{Geo: geo, Period: 0x7fffffff}).To_MessageMedia(), MEDIA_GEO_LIVE},
		{MakeTLMessageMediaGeo(&MessageMedia{Geo: geo}).To_MessageMedia(), MEDIA_EMPTY},
		{MakeTLMessageMediaVenue(&MessageMedia{Geo: geo, Title: "t"}).To_MessageMedia(), MEDIA_EMPTY},
	} {
		m := MakeTLMessage(&Message{Id: 1, Media: c.media}).To_Message()
		if got := GetMediaType(m); got != c.want {
			t.Errorf("%s: media type %d, want %d", c.media.GetPredicateName(), got, c.want)
		}
	}
}

// pluma: TestGetMediaTypeCalls: an invitation to a conference is a call as a
// one-to-one call is: inputMessagesFilterPhoneCalls finds both, as the tab
// of calls of Android lists them (CallLogActivity.java:1207-1210, :291-300).
func TestGetMediaTypeCalls(t *testing.T) {
	for _, a := range []*MessageAction{
		MakeTLMessageActionPhoneCall(&MessageAction{CallId: 1}).To_MessageAction(),
		MakeTLMessageActionConferenceCall(&MessageAction{CallId: 1, Missed: true}).To_MessageAction(),
	} {
		m := MakeTLMessageService(&Message{Id: 1, Action: a}).To_Message()
		if got := GetMediaType(m); got != MEDIA_PHONE_CALL {
			t.Errorf("%s: media type %d, want %d", a.GetPredicateName(), got, MEDIA_PHONE_CALL)
		}
	}
}
