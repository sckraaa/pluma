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

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func photosPhoto(t *testing.T, o mtproto.TLObject, err error, what string) *mtproto.Photos_Photo {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	r, ok := o.(interface{ To_Photos_Photo() *mtproto.Photos_Photo })
	if !ok {
		t.Fatalf("%s returned %T", what, o)
	}
	return r.To_Photos_Photo()
}

// uploadProfilePhoto uploads a new photo of u, the public one with fallback.
func uploadProfilePhoto(ctx context.Context, t *testing.T, u *user, fallback bool) *mtproto.Photo {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLPhotosUploadProfilePhoto{
		Constructor: u.c.Constructor(mtproto.Predicate_photos_uploadProfilePhoto),
		Fallback:    fallback,
		File:        uploadPhoto(ctx, t, u).GetFile(),
	})
	return photosPhoto(t, o, err, "photos.uploadProfilePhoto").GetPhoto()
}

// photosAs is what viewer sees of owner: user.photo, and the three photos
// of the full profile, by id (0 for none).
type photosAs struct {
	photo             int64
	personal          bool
	profile, fallback int64
	personalFull      int64
}

func photosOf(ctx context.Context, t *testing.T, viewer, owner *user) photosAs {
	t.Helper()
	input := inputSelf(viewer)
	if viewer != owner {
		input = inputUserOf(ctx, t, viewer, owner)
	}
	full, u := fullUser(ctx, t, viewer, input)
	return photosAs{
		photo:        u.GetPhoto().GetPhotoId(),
		personal:     u.GetPhoto().GetPersonal(),
		profile:      full.GetProfilePhoto().GetId(),
		fallback:     full.GetFallbackPhoto().GetId(),
		personalFull: full.GetPersonalPhoto().GetId(),
	}
}

// The public photo (task 051): Bob shows his photo to his contacts only and
// sets a public one in its place. Alice, no contact of his, sees the public
// photo in user.photo and userFull.fallback_photo; Carol, his contact, sees
// the main photo and no public one. A personal photo Alice sets for Bob is
// seen by Alice alone, on both her devices; a photo she suggests reaches
// Bob as a service message, and Bob makes it his main photo.
func TestPublicAndPersonalPhoto(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	aliceLaptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	main := uploadProfilePhoto(ctx, t, bob, false)
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, phones[2], "Carol", ""))
	mustSetPrivacy(ctx, t, bob, mtproto.Predicate_inputPrivacyKeyProfilePhoto,
		ruleOf(bob, mtproto.Predicate_inputPrivacyValueAllowContacts))
	public := uploadProfilePhoto(ctx, t, bob, true)
	if public.GetId() == main.GetId() {
		t.Fatal("the public photo is the main one")
	}

	if got, want := photosOf(ctx, t, alice, bob), (photosAs{photo: public.GetId(), fallback: public.GetId()}); got != want {
		t.Errorf("Alice, from whom the main photo is hidden: %+v, want %+v", got, want)
	}
	if got, want := photosOf(ctx, t, carol, bob), (photosAs{photo: main.GetId(), profile: main.GetId()}); got != want {
		t.Errorf("Carol, Bob's contact: %+v, want %+v", got, want)
	}
	if got, want := photosOf(ctx, t, bob, bob), (photosAs{photo: main.GetId(), profile: main.GetId(), fallback: public.GetId()}); got != want {
		t.Errorf("Bob himself: %+v, want %+v", got, want)
	}
	// tdesktop draws the public photo in its privacy box from the inline
	// thumbnail until a size is loaded, and stops without one
	// (prepare_short_info_box.cpp:179-183 at a1e89e1f).
	if full, _ := fullUser(ctx, t, bob, inputSelf(bob)); !hasStrippedSize(full.GetFallbackPhoto()) {
		t.Errorf("Bob's public photo has no photoStrippedSize: %v", full.GetFallbackPhoto().GetSizes())
	}
	if !hasStrippedSize(main) {
		t.Errorf("Bob's main photo has no photoStrippedSize: %v", main.GetSizes())
	}
	// The public photo is not one of Bob's photos: the clients add it to
	// the gallery themselves.
	o, err := bob.c.Invoke(ctx, &mtproto.TLPhotosGetUserPhotos{
		Constructor: bob.c.Constructor(mtproto.Predicate_photos_getUserPhotos),
		UserId:      inputSelf(bob),
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("photos.getUserPhotos: %v", err)
	}
	if photos := o.(interface{ To_Photos_Photos() *mtproto.Photos_Photos }).To_Photos_Photos().GetPhotos(); len(photos) != 1 || photos[0].GetId() != main.GetId() {
		t.Errorf("Bob's photos: %v, want the main photo alone", photos)
	}

	// A personal photo needs a contact.
	bobForAlice := inputUserOf(ctx, t, alice, bob)
	contactPhoto := func(u *user, to *mtproto.InputUser, save, suggest, file bool) (*mtproto.Photos_Photo, error) {
		in := &mtproto.TLPhotosUploadContactProfilePhoto{
			Constructor: u.c.Constructor(mtproto.Predicate_photos_uploadContactProfilePhoto),
			Save:        save,
			Suggest:     suggest,
			UserId:      to,
		}
		if file {
			in.File = uploadPhoto(ctx, t, u).GetFile()
		}
		o, err := u.c.Invoke(ctx, in)
		if err != nil {
			return nil, err
		}
		return photosPhoto(t, o, nil, "photos.uploadContactProfilePhoto"), nil
	}
	if _, err := contactPhoto(alice, bobForAlice, true, false, true); !mtclient.IsRPCError(err, "CONTACT_MISSING") {
		t.Fatalf("a personal photo of a non-contact: %v, want CONTACT_MISSING", err)
	}
	if _, err := contactPhoto(alice, bobForAlice, false, false, true); !mtclient.IsRPCError(err, "NEED_ACTION_MISSING") {
		t.Fatalf("neither save nor suggest: %v, want NEED_ACTION_MISSING", err)
	}
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, phones[1], "Bob", ""))

	getState(ctx, t, aliceLaptop)
	r, err := contactPhoto(alice, bobForAlice, true, false, true)
	if err != nil {
		t.Fatalf("personal photo: %v", err)
	}
	personal := r.GetPhoto()
	if len(r.GetUsers()) != 1 || r.GetUsers()[0].GetPhoto().GetPhotoId() != personal.GetId() || !r.GetUsers()[0].GetPhoto().GetPersonal() {
		t.Errorf("the answer's users: %v, want Bob with the personal photo", r.GetUsers())
	}
	waitUpdate(ctx, t, aliceLaptop, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == bob.self.GetId() })

	want := photosAs{photo: personal.GetId(), personal: true, fallback: public.GetId(), personalFull: personal.GetId()}
	if got := photosOf(ctx, t, alice, bob); got != want {
		t.Errorf("Alice with her personal photo of Bob: %+v, want %+v", got, want)
	}
	if got := photosOf(ctx, t, carol, bob); got.photo != main.GetId() || got.personal {
		t.Errorf("Carol: %+v, want Bob's main photo", got)
	}
	if got := photosOf(ctx, t, bob, bob); got.photo != main.GetId() || got.personal {
		t.Errorf("Bob himself: %+v, want his main photo", got)
	}

	// save without a file removes the personal photo.
	if r, err = contactPhoto(alice, bobForAlice, true, false, false); err != nil {
		t.Fatalf("removal of the personal photo: %v", err)
	}
	if r.GetPhoto().GetPredicateName() != mtproto.Predicate_photoEmpty || len(r.GetUsers()) != 1 ||
		r.GetUsers()[0].GetPhoto().GetPhotoId() != public.GetId() || r.GetUsers()[0].GetPhoto().GetPersonal() {
		t.Errorf("the answer to the removal: %v, want photoEmpty and Bob with his public photo", r)
	}
	if got, want := photosOf(ctx, t, alice, bob), (photosAs{photo: public.GetId(), fallback: public.GetId()}); got != want {
		t.Errorf("Alice after the removal: %+v, want %+v", got, want)
	}

	// A suggestion reaches Bob and Alice's own device as a service message.
	getState(ctx, t, bob)
	getState(ctx, t, alice)
	if r, err = contactPhoto(alice, bobForAlice, false, true, true); err != nil {
		t.Fatalf("suggestion: %v", err)
	}
	suggested := r.GetPhoto()
	isSuggestion := func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionSuggestProfilePhoto && a.GetPhoto().GetId() == suggested.GetId()
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, isSuggestion)
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, isSuggestion)
	if got := photosOf(ctx, t, alice, bob); got.personal {
		t.Errorf("a suggestion alone set a personal photo: %+v", got)
	}

	// Carol may not take the photo; Bob accepts it.
	update := func(u *user, p *mtproto.Photo, fallback bool) (*mtproto.Photos_Photo, error) {
		id := mtproto.MakeTLInputPhotoEmpty(nil).To_InputPhoto()
		if p != nil {
			id = mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{
				Id: p.GetId(), AccessHash: p.GetAccessHash(), FileReference: p.GetFileReference(),
			}).To_InputPhoto()
		}
		o, err := u.c.Invoke(ctx, &mtproto.TLPhotosUpdateProfilePhoto{
			Constructor: u.c.Constructor(mtproto.Predicate_photos_updateProfilePhoto),
			Fallback:    fallback,
			Id:          id,
		})
		if err != nil {
			return nil, err
		}
		return photosPhoto(t, o, nil, "photos.updateProfilePhoto"), nil
	}
	if _, err = update(carol, suggested, false); !mtclient.IsRPCError(err, "PHOTO_ID_INVALID") {
		t.Errorf("Carol takes the photo suggested to Bob: %v, want PHOTO_ID_INVALID", err)
	}
	if r, err = update(bob, suggested, false); err != nil || r.GetPhoto().GetId() != suggested.GetId() {
		t.Fatalf("Bob accepts the suggested photo: %v, %v", r, err)
	}
	if got := photosOf(ctx, t, carol, bob); got.photo != suggested.GetId() {
		t.Errorf("Carol after Bob accepted: %+v, want photo %d", got, suggested.GetId())
	}

	// Bob removes the public photo: Alice sees no photo; the main stays.
	if r, err = update(bob, nil, true); err != nil || r.GetPhoto().GetPredicateName() != mtproto.Predicate_photoEmpty {
		t.Fatalf("removal of the public photo: %v, %v", r, err)
	}
	if got := photosOf(ctx, t, alice, bob); got != (photosAs{}) {
		t.Errorf("Alice after the removal of the public photo: %+v, want nothing", got)
	}
	if got := photosOf(ctx, t, bob, bob); got.photo != suggested.GetId() || got.fallback != 0 {
		t.Errorf("Bob after the removal of the public photo: %+v", got)
	}
}

// hasStrippedSize tells whether the photo carries a photoStrippedSize "i"
// whose bytes have the 0x01 prefix the clients inflate.
func hasStrippedSize(p *mtproto.Photo) bool {
	for _, sz := range p.GetSizes() {
		if sz.GetPredicateName() == mtproto.Predicate_photoStrippedSize && sz.GetType() == mtproto.PhotoSZStrippedType &&
			len(sz.GetBytes()) > 3 && sz.GetBytes()[0] == 0x01 {
			return true
		}
	}
	return false
}
