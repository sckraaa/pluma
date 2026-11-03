//go:build integration

package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// A new account carries the rules of every privacy key from the start, the
// defaults (task 034): a key without rules hides what it guards, so a user
// who had just signed up could not be found by number (AddedByPhone) until
// the cached data expired.
func TestSignUpPrivacyRules(t *testing.T) {
	c := userCore(t)
	id := signUp(t, nextPhone(), "Nia")
	viewer, _ := newUser(t, "Ola")
	r, err := c.UserGetMutableUsersV2(&user.TLUserGetMutableUsersV2{Id: []int64{id, viewer}, Privacy: true, HasTo: true, To: []int64{viewer}})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := r.GetImmutableUser(id)
	if !ok {
		t.Fatal("no new user")
	}
	if n := len(u.GetKeysPrivacyRules()); n != 14 {
		t.Errorf("%d privacy keys, want 14", n)
	}
	for key, want := range map[int]bool{
		mtproto.ADDED_BY_PHONE: true,
		mtproto.ABOUT:          true,
		mtproto.PROFILE_PHOTO:  true,
		mtproto.PHONE_NUMBER:   false, // nobody by default
		mtproto.BIRTHDAY:       false, // contacts by default
	} {
		if got := u.CheckPrivacy(key, viewer); got != want {
			t.Errorf("key %d for a stranger: %v, want %v", key, got, want)
		}
	}
}
