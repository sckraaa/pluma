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

	user := func(premium bool, until int64) *ImmutableUser {
		return MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{
			Id:                               7,
			Premium:                          premium,
			PremiumExpireDate:                wrapperspb.Int64(until),
			NewNoncontactPeersRequirePremium: true,
		}).To_UserData()}).To_ImmutableUser()
	}
	viewer := MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{Id: 8}).To_UserData()}).To_ImmutableUser()
	for _, tc := range []struct {
		name string
		user *ImmutableUser
		want bool
	}{
		{"with Premium", user(true, 0), true},
		{"Premium ended", user(true, 1), false},
		{"without Premium", user(false, 0), false},
	} {
		if got := tc.user.ToUnsafeUser(viewer).GetContactRequirePremium(); got != tc.want {
			t.Errorf("%s: ToUnsafeUser %v, want %v", tc.name, got, tc.want)
		}
		if got := tc.user.ToUser(viewer.Id()).GetContactRequirePremium(); got != tc.want {
			t.Errorf("%s: ToUser %v, want %v", tc.name, got, tc.want)
		}
		if tc.user.ToSelfUser().GetContactRequirePremium() {
			t.Errorf("%s: the user's own view has contact_require_premium", tc.name)
		}
	}
}
