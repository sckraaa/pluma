// pluma: tests of the photo each viewer is shown (task 051, ADR 0033)

package mtproto

import (
	"testing"
)

func testPhoto(id int64) *Photo {
	return MakeTLPhoto(&Photo{
		Id:    id,
		Sizes: []*PhotoSize{MakeTLPhotoSize(&PhotoSize{Type: "a", W: 160, H: 160}).To_PhotoSize()},
	}).To_Photo()
}

// The photo of a user U for a viewer V: V's personal photo of U first,
// whatever U's privacy says; then U's main photo if the privacy lets V see
// it; otherwise U's public photo, with no flag. The account itself and bots
// show the main photo. ToUnsafeUser, ToUser and ToUserFor agree, the row of
// the viewer loaded on either side.
func TestPhotoFor(t *testing.T) {
	const viewerId, ownerId = 2, 1
	owner := func(main, fallback *Photo, photoRule *PrivacyRule, bot bool) *ImmutableUser {
		data := MakeTLUserData(&UserData{Id: ownerId, ProfilePhoto: main, FallbackPhoto: fallback}).To_UserData()
		if bot {
			data.Bot = MakeTLBotData(&BotData{Id: ownerId}).To_BotData()
		}
		return MakeTLImmutableUser(&ImmutableUser{
			User: data,
			KeysPrivacyRules: []*PrivacyKeyRules{
				MakeTLPrivacyKeyRules(&PrivacyKeyRules{Key: PROFILE_PHOTO, Rules: []*PrivacyRule{photoRule}}).To_PrivacyKeyRules(),
			},
		}).To_ImmutableUser()
	}
	row := func(personal int64) *ContactData {
		return MakeTLContactData(&ContactData{
			UserId: viewerId, ContactUserId: ownerId, PersonalPhotoId: personal, PersonalPhotoHasVideo: personal == 31,
		}).To_ContactData()
	}

	type want struct {
		id       int64
		personal bool
		video    bool
	}
	for _, tc := range []struct {
		name  string
		owner *ImmutableUser
		row   *ContactData
		want  want
	}{
		{"main visible", owner(testPhoto(10), testPhoto(20), allowAll(), false), nil, want{id: 10}},
		{"main hidden, public photo", owner(testPhoto(10), testPhoto(20), disallowAll(), false), nil, want{id: 20}},
		{"main hidden, no public photo", owner(testPhoto(10), nil, disallowAll(), false), nil, want{}},
		{"no main photo, the public one is not shown", owner(nil, testPhoto(20), allowAll(), false), nil, want{}},
		{"personal over a visible main photo", owner(testPhoto(10), testPhoto(20), allowAll(), false), row(30), want{id: 30, personal: true}},
		{"personal over a hidden one", owner(testPhoto(10), testPhoto(20), disallowAll(), false), row(31), want{id: 31, personal: true, video: true}},
		{"contact without a personal photo", owner(testPhoto(10), testPhoto(20), disallowAll(), false), row(0), want{id: 20}},
		{"bot", owner(testPhoto(10), nil, disallowAll(), true), row(30), want{id: 10}},
	} {
		viewer := MakeTLImmutableUser(&ImmutableUser{User: MakeTLUserData(&UserData{Id: viewerId}).To_UserData()}).To_ImmutableUser()
		check := func(path string, p *UserProfilePhoto) {
			t.Helper()
			if p.GetPhotoId() != tc.want.id || p.GetPersonal() != tc.want.personal || p.GetHasVideo() != tc.want.video {
				t.Errorf("%s, %s: photo %d personal %v video %v, want %+v", tc.name, path, p.GetPhotoId(), p.GetPersonal(), p.GetHasVideo(), tc.want)
			}
		}
		check("PhotoFor", tc.owner.PhotoFor(viewer, tc.row))

		// The viewer's row in the viewer's Contacts.
		v := viewer
		if tc.row != nil {
			v = MakeTLImmutableUser(&ImmutableUser{User: viewer.User, Contacts: []*ContactData{tc.row}}).To_ImmutableUser()
		}
		check("ToUnsafeUser, the viewer's rows", tc.owner.ToUnsafeUser(v).GetPhoto())

		// The viewer's row in the owner's ReverseContacts.
		o := tc.owner
		if tc.row != nil {
			o = MakeTLImmutableUser(&ImmutableUser{User: tc.owner.User, KeysPrivacyRules: tc.owner.KeysPrivacyRules,
				ReverseContacts: []*ContactData{tc.row}}).To_ImmutableUser()
		}
		check("ToUnsafeUser, the owner's reverse rows", o.ToUnsafeUser(viewer).GetPhoto())
		check("ToUserFor", o.ToUserFor(viewer).GetPhoto())
		check("ToUser", o.ToUser(viewerId).GetPhoto())
	}

	// The account sees its main photo, never the public one in user.photo.
	self := owner(testPhoto(10), testPhoto(20), disallowAll(), false)
	if p := self.ToSelfUser().GetPhoto(); p.GetPhotoId() != 10 || p.GetPersonal() {
		t.Errorf("the account itself: %v", p)
	}
	if p := self.PhotoFor(self, row(30)); p.GetPhotoId() != 10 {
		t.Errorf("PhotoFor of the account itself: %v", p)
	}
}
