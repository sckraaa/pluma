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

import (
	"testing"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// pluma: new_noncontact_peers_require_premium of userData survives the TL
// codec, and the users others see carry contact_require_premium while the
// owner of the setting has Premium.
func TestUserDataRequirePremium(t *testing.T) {
	for _, on := range []bool{false, true} {
		in := MakeTLUserData(&UserData{Id: 7, FirstName: "Ann", NewNoncontactPeersRequirePremium: on}).To_UserData()
		x := NewEncodeBuf(256)
		if err := in.Encode(x, 0); err != nil {
			t.Fatal(err)
		}
		out := &UserData{}
		if err := out.Decode(NewDecodeBuf(x.GetBuf())); err != nil {
			t.Fatal(err)
		}
		if out.GetNewNoncontactPeersRequirePremium() != on || out.GetFirstName() != "Ann" {
			t.Fatalf("setting %v: decoded %+v", on, out)
		}
	}

	// Whatever users.premium stores, a person has Premium (ADR 0041) and
	// others see the setting of a person; a bot has no Premium.
	user := func(premium bool, bot bool) *ImmutableUser {
		data := &UserData{
			Id:                               7,
			Premium:                          premium,
			PremiumExpireDate:                wrapperspb.Int64(1),
			NewNoncontactPeersRequirePremium: true,
		}
		if bot {
			data.Bot = MakeTLBotData(&BotData{}).To_BotData()
		}
		return MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
	}
	viewer := MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{Id: 8}).To_UserData()}).To_ImmutableUser()
	for _, tc := range []struct {
		name string
		user *ImmutableUser
		want bool
	}{
		{"stored Premium", user(true, false), true},
		{"stored without Premium", user(false, false), true},
		{"a bot", user(true, true), false},
	} {
		if tc.user.Premium() == tc.user.IsBot() {
			t.Errorf("%s: Premium() %v", tc.name, tc.user.Premium())
		}
		if got := tc.user.ToUnsafeUser(viewer); got.GetContactRequirePremium() != tc.want || got.GetPremium() == tc.user.IsBot() {
			t.Errorf("%s: ToUnsafeUser contact_require_premium %v, premium %v", tc.name, got.GetContactRequirePremium(), got.GetPremium())
		}
		if got := tc.user.ToUser(viewer.Id()); got.GetContactRequirePremium() != tc.want || got.GetPremium() == tc.user.IsBot() {
			t.Errorf("%s: ToUser contact_require_premium %v, premium %v", tc.name, got.GetContactRequirePremium(), got.GetPremium())
		}
		if self := tc.user.ToSelfUser(); self.GetContactRequirePremium() || self.GetPremium() == tc.user.IsBot() {
			t.Errorf("%s: own view contact_require_premium %v, premium %v", tc.name, self.GetContactRequirePremium(), self.GetPremium())
		}
	}
}
