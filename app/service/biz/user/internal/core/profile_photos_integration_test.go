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

//go:build integration

package core

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The public photo and the personal photo of a contact (task 051, ADR 0033).

// photoRule sets the profile photo rule of u to rule.
func photoRule(t *testing.T, c *UserCore, u int64, rule *mtproto.PrivacyRule) {
	t.Helper()
	if _, err := c.UserSetPrivacy(&user.TLUserSetPrivacy{UserId: u, KeyType: mtproto.PROFILE_PHOTO, Rules: []*mtproto.PrivacyRule{rule}}); err != nil {
		t.Fatal(err)
	}
}

// photoSeen is user.photo of owner as viewer sees it, loaded as the
// services load it.
func photoSeen(t *testing.T, c *UserCore, owner, viewer int64) *mtproto.UserProfilePhoto {
	t.Helper()
	r, err := c.UserGetMutableUsersV2(&user.TLUserGetMutableUsersV2{Id: []int64{owner, viewer}, Privacy: true, HasTo: true, To: []int64{viewer}})
	if err != nil {
		t.Fatal(err)
	}
	o, ok := r.GetImmutableUser(owner)
	v, ok2 := r.GetImmutableUser(viewer)
	if !ok || !ok2 {
		t.Fatalf("users %d, %d not loaded", owner, viewer)
	}
	return o.ToUnsafeUser(v).GetPhoto()
}

func storedInt(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := itRaw.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// gatedMedia holds the first request for a photo once armed, as a slow
// media server would: the load of the user it belongs to stays in progress.
type gatedMedia struct {
	*fakeMedia
	armed   atomic.Bool
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *gatedMedia) MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if g.armed.Load() {
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	}
	return g.fakeMedia.MediaGetPhoto(ctx, in)
}

// A load of the user that began before a change and ends after it: a read
// made after the commit does not get what the load read, and the load does
// not put it into the cache.
func TestLoadOverlappingAChangeDoesNotFillTheCache(t *testing.T) {
	const public = 7301
	g := &gatedMedia{fakeMedia: newFakeMedia(public), entered: make(chan struct{}), release: make(chan struct{})}
	c := mediaCore(t, g.fakeMedia)
	u, _ := newUser(t, "Lea")
	if _, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: u, Id: public, Uploaded: true, Fallback: true}); err != nil {
		t.Fatal(err)
	}

	d := *c.svcCtx.Dao
	d.MediaClient = g
	ctx := context.Background()
	key := dao.GenCacheUserDataCacheKey(u)
	if err := d.CachedConn.DelCache(ctx, key); err != nil {
		t.Fatal(err)
	}
	fallbackOf := func() int64 { return d.GetCacheUserData(ctx, u).GetUserData().GetFallbackPhoto().GetId() }

	g.armed.Store(true)
	var unblock sync.Once
	defer unblock.Do(func() { close(g.release) })
	stale := make(chan int64, 1)
	go func() { stale <- fallbackOf() }()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the load did not reach the media server")
	}

	if _, err := d.RunBox(ctx, u, []string{key}, func(b *userbox.Box) error {
		_, err := b.Tx().Exec("UPDATE users SET fallback_photo_id = 0 WHERE id = ?", u)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	fresh := make(chan int64, 1)
	go func() { fresh <- fallbackOf() }()
	select {
	case got := <-fresh:
		if got != 0 {
			t.Errorf("a read after the commit: public photo %d, want none", got)
		}
	case <-time.After(10 * time.Second):
		t.Error("a read after the commit waits for a load begun before it")
	}

	unblock.Do(func() { close(g.release) })
	if got := <-stale; got != public {
		t.Errorf("the load overlapping the change read %d, want %d", got, public)
	}
	if got := fallbackOf(); got != 0 {
		t.Errorf("after the overlapping load: public photo %d, want none", got)
	}
}

// The public photo is kept apart from the main photo and the album: users
// the rule hides the main photo from see it, the others the main photo.
// It is replaced, removed with photos.updateProfilePhoto and with
// photos.deletePhotos, and each change goes to the account with seq.
func TestFallbackPhoto(t *testing.T) {
	const main, public, public2, other = 7201, 7202, 7203, 7204
	c := mediaCore(t, newFakeMedia(main, public, public2, other))
	u, _ := newUser(t, "Fay")
	v, _ := newUser(t, "Val")
	update := func(userId, id int64, uploaded, fallback bool) (int64, error) {
		r, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: userId, Id: id, Uploaded: uploaded, Fallback: fallback})
		return r.GetV(), err
	}
	must := func(what string, got int64, err error, want int64) {
		t.Helper()
		if err != nil || got != want {
			t.Fatalf("%s: %d, %v; want %d", what, got, err, want)
		}
	}
	state := func() (int64, int64) {
		t.Helper()
		return storedInt(t, "SELECT photo_id FROM users WHERE id = ?", u), storedInt(t, "SELECT fallback_photo_id FROM users WHERE id = ?", u)
	}

	got, err := update(u, main, true, false)
	must("main photo", got, err, main)
	got, err = update(u, public, true, true)
	must("public photo", got, err, public)
	if m, f := state(); m != main || f != public {
		t.Fatalf("stored main %d, public %d", m, f)
	}
	if n := count(t, "SELECT COUNT(*) FROM user_profile_photos WHERE user_id = ? AND photo_id = ?", u, public); n != 0 {
		t.Errorf("the public photo is in the album %d times", n)
	}

	photoRule(t, c, u, mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule())
	if p := photoSeen(t, c, u, v); p.GetPhotoId() != public || p.GetPersonal() {
		t.Errorf("hidden main photo: %d seen, want the public %d", p.GetPhotoId(), public)
	}
	if p := photoSeen(t, c, u, u); p.GetPhotoId() != main {
		t.Errorf("the account sees %d, want its main %d", p.GetPhotoId(), main)
	}
	photoRule(t, c, u, mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule())
	if p := photoSeen(t, c, u, v); p.GetPhotoId() != main {
		t.Errorf("visible main photo: %d seen, want %d", p.GetPhotoId(), main)
	}
	photoRule(t, c, u, mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule())

	// Another user's photo is not the account's to make public.
	if _, err = update(v, other, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err = update(u, other, false, true); !errors.Is(err, mtproto.ErrPhotoIdInvalid) {
		t.Errorf("another user's photo as the public photo: %v, want PHOTO_ID_INVALID", err)
	}
	// One of its own may be.
	got, err = update(u, main, false, true)
	must("the main photo as the public one", got, err, main)
	got, err = update(u, public2, true, true)
	must("a new public photo", got, err, public2)

	pushesBefore := len(seqPushes(t, u))
	got, err = update(u, public2, true, true) // changes nothing
	must("the same public photo", got, err, public2)
	if n := len(seqPushes(t, u)); n != pushesBefore {
		t.Errorf("the same public photo pushed %d updates", n-pushesBefore)
	}
	// Removed with inputPhotoEmpty; the main photo stays.
	got, err = update(u, 0, false, true)
	must("removal", got, err, 0)
	if m, f := state(); m != main || f != 0 {
		t.Fatalf("after the removal: main %d, public %d", m, f)
	}
	if p := photoSeen(t, c, u, v); p.GetPhotoId() != 0 {
		t.Errorf("no public photo: %d seen", p.GetPhotoId())
	}
	// Removed with photos.deletePhotos (Android); the main photo stays.
	got, err = update(u, public, true, true)
	must("public photo again", got, err, public)
	if _, err = c.UserDeleteProfilePhotos(&user.TLUserDeleteProfilePhotos{UserId: u, Id: []int64{public}}); err != nil {
		t.Fatal(err)
	}
	if m, f := state(); m != main || f != 0 {
		t.Fatalf("after deletePhotos: main %d, public %d", m, f)
	}
	// Removing the main photo leaves the public photo.
	got, err = update(u, public2, true, true)
	must("public photo for the removal of the main", got, err, public2)
	got, err = update(u, 0, false, false)
	must("removal of the main photo", got, err, 0)
	if m, f := state(); m != 0 || f != public2 {
		t.Fatalf("after the removal of the main photo: main %d, public %d", m, f)
	}

	// Every change of a photo was an updateUser of the account with seq;
	// the rule changes above pushed updatePrivacy.
	for _, p := range seqPushes(t, u) {
		if p.update.GetPredicateName() == mtproto.Predicate_updatePrivacy {
			continue
		}
		if p.update.GetPredicateName() != mtproto.Predicate_updateUser || p.update.GetUserId() != u {
			t.Errorf("push %d: %s of %d", p.seq, p.update.GetPredicateName(), p.update.GetUserId())
		}
	}
}

// A personal photo is set on the contact row and seen by its owner alone,
// whatever the contact's privacy; only for a contact; removed with the
// contact. The owner's devices get updateUser with seq carrying it.
func TestContactPersonalPhoto(t *testing.T) {
	const main, personal = 7301, 7302
	c := mediaCore(t, newFakeMedia(main, personal))
	u, _ := newUser(t, "Una")
	v, _ := newUser(t, "Vin")
	w, _ := newUser(t, "Wes")
	if _, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: v, Id: main, Uploaded: true}); err != nil {
		t.Fatal(err)
	}
	photoRule(t, c, v, mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule())
	set := func(owner, contact, photo int64, video bool) (*mtproto.User, error) {
		return c.UserSetContactPersonalPhoto(&user.TLUserSetContactPersonalPhoto{
			UserId: owner, ContactId: contact, PhotoId: photo, HasVideo: mtproto.ToBool(video),
		})
	}

	if _, err := set(u, v, personal, false); !errors.Is(err, dao.ErrContactMissing) {
		t.Fatalf("not a contact: %v, want CONTACT_MISSING", err)
	}
	if _, err := c.UserAddContact(&user.TLUserAddContact{UserId: u, Id: v, FirstName: "V", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	pushesBefore := len(seqPushes(t, u))
	contactEvents := len(eventsOf(t, v))
	seen, err := set(u, v, personal, true)
	if err != nil {
		t.Fatal(err)
	}
	if p := seen.GetPhoto(); p.GetPhotoId() != personal || !p.GetPersonal() || !p.GetHasVideo() || !seen.GetContact() {
		t.Errorf("answered %v", seen)
	}
	if n := storedInt(t, "SELECT personal_photo_id FROM user_contacts WHERE owner_user_id = ? AND contact_user_id = ?", u, v); n != personal {
		t.Errorf("stored %d", n)
	}
	if p := photoSeen(t, c, v, u); p.GetPhotoId() != personal || !p.GetPersonal() {
		t.Errorf("the owner sees %v, want the personal photo", p)
	}
	if p := photoSeen(t, c, v, w); p.GetPhotoId() != 0 {
		t.Errorf("another user sees %v, want none (the rule hides the main photo)", p)
	}
	if p := photoSeen(t, c, v, v); p.GetPhotoId() != main || p.GetPersonal() {
		t.Errorf("the contact sees %v, want its own main photo", p)
	}
	pushes := seqPushes(t, u)[pushesBefore:]
	if len(pushes) != 1 || pushes[0].update.GetPredicateName() != mtproto.Predicate_updateUser || pushes[0].update.GetUserId() != v {
		t.Fatalf("pushes %+v, want one updateUser of the contact", pushes)
	}
	if _, err = set(u, v, personal, true); err != nil { // changes nothing
		t.Fatal(err)
	}
	if n := len(seqPushes(t, u)) - pushesBefore; n != 1 {
		t.Errorf("%d pushes after the same photo again", n)
	}
	if n := len(eventsOf(t, v)) - contactEvents; n != 0 {
		t.Errorf("the contact got %d events: it must not be told", n)
	}

	// Removed: the main photo is hidden, so none.
	if seen, err = set(u, v, 0, true); err != nil || seen.GetPhoto().GetPhotoId() != 0 {
		t.Fatalf("removal: %v, %v", seen, err)
	}
	if n := storedInt(t, "SELECT personal_photo_has_video FROM user_contacts WHERE owner_user_id = ? AND contact_user_id = ?", u, v); n != 0 {
		t.Errorf("has_video %d after the removal", n)
	}

	// Gone with the contact: adding it again shows no personal photo.
	if _, err = set(u, v, personal, false); err != nil {
		t.Fatal(err)
	}
	if _, err = c.UserDeleteContact(&user.TLUserDeleteContact{UserId: u, Id: v}); err != nil {
		t.Fatal(err)
	}
	if n := storedInt(t, "SELECT personal_photo_id FROM user_contacts WHERE owner_user_id = ? AND contact_user_id = ?", u, v); n != 0 {
		t.Errorf("personal photo %d kept on the deleted row", n)
	}
	if _, err = c.UserAddContact(&user.TLUserAddContact{UserId: u, Id: v, FirstName: "V", AddPhonePrivacyException: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if p := photoSeen(t, c, v, u); p.GetPersonal() {
		t.Errorf("the contact added again shows %v", p)
	}
}

// A photo suggested to a user may become its main photo, although another
// user uploaded it; nobody else may make it theirs.
func TestAcceptSuggestedPhoto(t *testing.T) {
	const suggested = 7401
	c := mediaCore(t, newFakeMedia(suggested))
	u, _ := newUser(t, "Sue")
	v, _ := newUser(t, "Sam")
	w, _ := newUser(t, "Sid")
	update := func(userId int64) (int64, error) {
		r, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: userId, Id: suggested})
		return r.GetV(), err
	}
	if _, err := update(v); !errors.Is(err, mtproto.ErrPhotoIdInvalid) {
		t.Fatalf("before the suggestion: %v, want PHOTO_ID_INVALID", err)
	}
	for range 2 { // a repeat keeps one record
		if _, err := c.UserAddSuggestedPhoto(&user.TLUserAddSuggestedPhoto{UserId: v, FromUserId: u, PhotoId: suggested}); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM user_suggested_photos WHERE user_id = ?", v); n != 1 {
		t.Errorf("%d records", n)
	}
	if _, err := c.UserAddSuggestedPhoto(&user.TLUserAddSuggestedPhoto{UserId: v, FromUserId: v, PhotoId: suggested}); err == nil {
		t.Error("a suggestion to oneself accepted")
	}
	if got, err := update(v); err != nil || got != suggested {
		t.Fatalf("accepted: %d, %v", got, err)
	}
	if n := count(t, "SELECT COUNT(*) FROM user_profile_photos WHERE user_id = ? AND photo_id = ? AND deleted = 0", v, suggested); n != 1 {
		t.Errorf("the accepted photo is in the album %d times", n)
	}
	if _, err := update(w); !errors.Is(err, mtproto.ErrPhotoIdInvalid) {
		t.Errorf("a user it was not suggested to: %v, want PHOTO_ID_INVALID", err)
	}
}

// The migration runs again on a database that has it.
func TestProfilePhotosMigrationAgain(t *testing.T) {
	sqlText, err := os.ReadFile("../../../../../../teamgramd/deploy/sql/migrate-20261103-profile-photos.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := itRaw.Conn(ctx) // the session variable @ddl lives on one connection
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var lines []string
	for _, l := range strings.Split(string(sqlText), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "--") {
			lines = append(lines, l)
		}
	}
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";\n") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			if _, err = conn.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("%s: %v", stmt, err)
			}
		}
	}
	if n := count(t, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND COLUMN_NAME IN ('fallback_photo_id', 'personal_photo_id', 'personal_photo_has_video')"); n != 3 {
		t.Errorf("%d columns", n)
	}
}
