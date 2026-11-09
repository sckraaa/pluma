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
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/userchannelprofiles/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/userchannelprofiles/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"

	"github.com/zeromicro/go-zero/core/logx"
)

// fakeMedia has photos whose access_hash is ten times their id; another id
// is photoEmpty, as media answers.
type fakeMedia struct {
	media_client.MediaClient
	photos   []int64
	uploads  int
	uploaded *mediapb.TLMediaUploadProfilePhotoFile
}

func (f *fakeMedia) MediaGetPhoto(_ context.Context, in *mediapb.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if !slices.Contains(f.photos, in.PhotoId) {
		return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: in.PhotoId}).To_Photo(), nil
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: in.PhotoId, AccessHash: in.PhotoId * 10}).To_Photo(), nil
}

// MediaUploadProfilePhotoFile uploads photo 8.
func (f *fakeMedia) MediaUploadProfilePhotoFile(_ context.Context, in *mediapb.TLMediaUploadProfilePhotoFile) (*mtproto.Photo, error) {
	f.uploads++
	f.uploaded = in
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: 8, AccessHash: 80}).To_Photo(), nil
}

// fakeUsers records the photos biz is asked for, whether each was just
// uploaded and whether it is the public one; the removal of the main photo
// makes next main. Users 1 and 2 exist; 2 is a contact of 1 when contact.
type fakeUsers struct {
	user_client.UserClient
	owners    []int64
	asked     []int64
	uploaded  []bool
	fallback  []bool
	next      int64
	contact   bool
	personal  []*userpb.TLUserSetContactPersonalPhoto
	suggested []*userpb.TLUserAddSuggestedPhoto
}

func (f *fakeUsers) UserUpdateProfilePhoto(_ context.Context, in *userpb.TLUserUpdateProfilePhoto) (*mtproto.Int64, error) {
	f.owners = append(f.owners, in.UserId)
	f.asked = append(f.asked, in.Id)
	f.uploaded = append(f.uploaded, in.Uploaded)
	f.fallback = append(f.fallback, in.Fallback)
	if in.Id == 0 && !in.Fallback {
		return &mtproto.Int64{V: f.next}, nil
	}
	return &mtproto.Int64{V: in.Id}, nil
}

// UserGetImmutableUser: 3 is a bot of user 1, 9 a bot of user 5, 1 and 2
// are not bots.
func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	data := &mtproto.UserData{Id: in.Id, FirstName: "U"}
	switch in.Id {
	case 3:
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{Creator: 1}).To_BotData()
	case 9:
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{Creator: 5}).To_BotData()
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser(), nil
}

func (f *fakeUsers) UserGetMutableUsersV2(_ context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	users := &mtproto.MutableUsers{}
	for _, id := range in.Id {
		if id != 1 && id != 2 && id != 3 {
			continue
		}
		u := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
			User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id * 100, FirstName: "U"}).To_UserData(),
		}).To_ImmutableUser()
		if id == 1 && f.contact {
			u.Contacts = []*mtproto.ContactData{
				mtproto.MakeTLContactData(&mtproto.ContactData{UserId: 1, ContactUserId: 2}).To_ContactData(),
			}
		}
		users.Users = append(users.Users, u)
	}
	return users, nil
}

func (f *fakeUsers) UserSetContactPersonalPhoto(_ context.Context, in *userpb.TLUserSetContactPersonalPhoto) (*mtproto.User, error) {
	f.personal = append(f.personal, in)
	return mtproto.MakeTLUser(&mtproto.User{Id: in.ContactId, Photo: mtproto.PersonalProfilePhoto(&mtproto.ContactData{
		PersonalPhotoId: in.PhotoId,
	})}).To_User(), nil
}

func (f *fakeUsers) UserAddSuggestedPhoto(_ context.Context, in *userpb.TLUserAddSuggestedPhoto) (*mtproto.Bool, error) {
	f.suggested = append(f.suggested, in)
	return mtproto.BoolTrue, nil
}

// fakeMsg records the messages sent.
type fakeMsg struct {
	msg_client.MsgClient
	sent []*msgpb.TLMsgSendMessageV2
}

func (f *fakeMsg) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	f.sent = append(f.sent, in)
	return mtproto.MakeEmptyUpdates(), nil
}

func newTestCore(users *fakeUsers, media *fakeMedia, msg *fakeMsg) *UserChannelProfilesCore {
	ctx := context.Background()
	return &UserChannelProfilesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, MediaClient: media, MsgClient: msg}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1, PermAuthKeyId: 77},
	}
}

// photos.updateProfilePhoto makes a photo main only with its access_hash,
// as in Telegram; inputPhotoEmpty removes the main photo.
func TestUpdateProfilePhotoAccessHash(t *testing.T) {
	users := &fakeUsers{next: 6}
	c := newTestCore(users, &fakeMedia{photos: []int64{5, 6}}, nil)
	update := func(id *mtproto.InputPhoto) (int64, error) {
		r, err := c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{Id: id})
		return r.GetPhoto().GetId(), err
	}
	photo := func(id, accessHash int64) *mtproto.InputPhoto {
		return mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: id, AccessHash: accessHash}).To_InputPhoto()
	}

	if id, err := update(photo(5, 50)); err != nil || id != 5 {
		t.Errorf("photo 5: %d, %v", id, err)
	}
	for _, p := range []*mtproto.InputPhoto{photo(5, 51), photo(7, 70)} {
		if _, err := update(p); !errors.Is(err, mtproto.ErrPhotoIdInvalid) {
			t.Errorf("photo %d with access_hash %d: %v, want PHOTO_ID_INVALID", p.GetId(), p.GetAccessHash(), err)
		}
	}
	// The removal answers the photo that became main, or photoEmpty.
	if id, err := update(mtproto.MakeTLInputPhotoEmpty(nil).To_InputPhoto()); err != nil || id != 6 {
		t.Errorf("removal: %d, %v, want photo 6", id, err)
	}
	users.next = 0
	if r, err := c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{Id: mtproto.MakeTLInputPhotoEmpty(nil).To_InputPhoto()}); err != nil ||
		r.GetPhoto().GetPredicateName() != mtproto.Predicate_photoEmpty {
		t.Errorf("removal of the last photo: %v, %v", r, err)
	}
	if want := []int64{5, 0, 0}; !slices.Equal(users.asked, want) {
		t.Errorf("biz asked for %v, want %v", users.asked, want)
	}
	// biz checks that each is one of the account's photos.
	if want := []bool{false, false, false}; !slices.Equal(users.uploaded, want) {
		t.Errorf("uploaded %v, want %v", users.uploaded, want)
	}
}

// photos.uploadProfilePhoto makes the photo just uploaded main: one of the
// account's photos from now on. The answer carries the account itself.
func TestUploadProfilePhotoIsTheAccounts(t *testing.T) {
	users := &fakeUsers{}
	c := newTestCore(users, &fakeMedia{}, nil)
	r, err := c.PhotosUploadProfilePhoto(&mtproto.TLPhotosUploadProfilePhoto{
		File: mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: "a.jpg"}).To_InputFile(),
	})
	if err != nil || r.GetPhoto().GetId() != 8 {
		t.Fatalf("upload: %v, %v", r, err)
	}
	if !slices.Equal(users.asked, []int64{8}) || !slices.Equal(users.uploaded, []bool{true}) || !slices.Equal(users.fallback, []bool{false}) {
		t.Errorf("biz asked for %v, uploaded %v, fallback %v; want [8] [true] [false]", users.asked, users.uploaded, users.fallback)
	}
	if len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != 1 || !r.GetUsers()[0].GetSelf() {
		t.Errorf("users %v, want the account itself", r.GetUsers())
	}
}

// The public photo (fallback) is passed to biz as such, uploaded or chosen,
// and removed with inputPhotoEmpty, which answers photoEmpty (task 051).
func TestFallbackPhoto(t *testing.T) {
	users := &fakeUsers{next: 6}
	c := newTestCore(users, &fakeMedia{photos: []int64{5}}, nil)
	if _, err := c.PhotosUploadProfilePhoto(&mtproto.TLPhotosUploadProfilePhoto{
		Fallback: true,
		File:     mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: "a.jpg"}).To_InputFile(),
	}); err != nil {
		t.Fatal(err)
	}
	if r, err := c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{
		Fallback: true,
		Id:       mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: 50}).To_InputPhoto(),
	}); err != nil || r.GetPhoto().GetId() != 5 {
		t.Fatalf("public photo 5: %v, %v", r, err)
	}
	r, err := c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{
		Fallback: true,
		Id:       mtproto.MakeTLInputPhotoEmpty(nil).To_InputPhoto(),
	})
	if err != nil || r.GetPhoto().GetPredicateName() != mtproto.Predicate_photoEmpty {
		t.Fatalf("removal of the public photo: %v, %v", r, err)
	}
	if !slices.Equal(users.asked, []int64{8, 5, 0}) || !slices.Equal(users.fallback, []bool{true, true, true}) ||
		!slices.Equal(users.uploaded, []bool{true, false, false}) {
		t.Errorf("biz asked for %v, fallback %v, uploaded %v", users.asked, users.fallback, users.uploaded)
	}
}

// The bot flag: only of a bot the caller owns, and a bot has no public photo.
func TestProfilePhotoOfBot(t *testing.T) {
	users := &fakeUsers{}
	media := &fakeMedia{photos: []int64{5}}
	c := newTestCore(users, media, nil)
	bot := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 9, AccessHash: 1}).To_InputUser()
	file := mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: "a.jpg"}).To_InputFile()
	photo := mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: 50}).To_InputPhoto()
	for _, tc := range []struct {
		fallback bool
		want     error
	}{{false, mtproto.ErrBotInvalid}, {true, errBotFallbackUnsupported}} {
		if _, err := c.PhotosUploadProfilePhoto(&mtproto.TLPhotosUploadProfilePhoto{Bot: bot, Fallback: tc.fallback, File: file}); !errors.Is(err, tc.want) {
			t.Errorf("upload, fallback %v: %v, want %v", tc.fallback, err, tc.want)
		}
		if _, err := c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{Bot: bot, Fallback: tc.fallback, Id: photo}); !errors.Is(err, tc.want) {
			t.Errorf("update, fallback %v: %v, want %v", tc.fallback, err, tc.want)
		}
	}
	if len(users.asked) != 0 || media.uploads != 0 {
		t.Errorf("biz asked for %v, %d uploads: the caller's photo must not change", users.asked, media.uploads)
	}
}

// The owner of a bot changes the bot's photo, and nobody's else; the answer
// shows the bot.
func TestProfilePhotoOfOwnBot(t *testing.T) {
	users := &fakeUsers{}
	media := &fakeMedia{photos: []int64{5}}
	c := newTestCore(users, media, nil)
	bot := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 3, AccessHash: 1}).To_InputUser()
	file := mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: "a.jpg"}).To_InputFile()
	r, err := c.PhotosUploadProfilePhoto(&mtproto.TLPhotosUploadProfilePhoto{Bot: bot, File: file})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(users.owners, []int64{3}) || !slices.Equal(users.uploaded, []bool{true}) {
		t.Errorf("owners %v, uploaded %v", users.owners, users.uploaded)
	}
	if len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != 3 {
		t.Errorf("users %v", r.GetUsers())
	}
	photo := mtproto.MakeTLInputPhoto(&mtproto.InputPhoto{Id: 5, AccessHash: 50}).To_InputPhoto()
	if _, err = c.PhotosUpdateProfilePhoto(&mtproto.TLPhotosUpdateProfilePhoto{Bot: bot, Id: photo}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(users.owners, []int64{3, 3}) {
		t.Errorf("owners %v", users.owners)
	}
	// A user who is no bot is BOT_INVALID, however it is named.
	notBot := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 2, AccessHash: 1}).To_InputUser()
	if _, err = c.PhotosUploadProfilePhoto(&mtproto.TLPhotosUploadProfilePhoto{Bot: notBot, File: file}); !errors.Is(err, mtproto.ErrBotInvalid) {
		t.Errorf("not a bot: %v", err)
	}
}

// photos.uploadContactProfilePhoto: save sets the personal photo, save with
// no file removes it, suggest records the photo for the contact and sends
// it in a service message to every device of the caller too; neither flag,
// the caller itself and a non-contact are refused before any upload.
func TestUploadContactProfilePhoto(t *testing.T) {
	contactId := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 2, AccessHash: 200}).To_InputUser()
	file := mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: "a.jpg"}).To_InputFile()

	t.Run("refused", func(t *testing.T) {
		users := &fakeUsers{}
		media := &fakeMedia{}
		c := newTestCore(users, media, &fakeMsg{})
		for _, tc := range []struct {
			in   *mtproto.TLPhotosUploadContactProfilePhoto
			want error
		}{
			{&mtproto.TLPhotosUploadContactProfilePhoto{UserId: contactId, File: file}, errNeedActionMissing},
			{&mtproto.TLPhotosUploadContactProfilePhoto{Save: true, UserId: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), File: file}, mtproto.ErrUserIdInvalid},
			{&mtproto.TLPhotosUploadContactProfilePhoto{Save: true, UserId: contactId, File: file}, errContactMissing},
			{&mtproto.TLPhotosUploadContactProfilePhoto{Suggest: true, UserId: contactId, File: file}, errContactMissing},
		} {
			if _, err := c.PhotosUploadContactProfilePhoto(tc.in); !errors.Is(err, tc.want) {
				t.Errorf("%+v: %v, want %v", tc.in, err, tc.want)
			}
		}
		users.contact = true
		if _, err := c.PhotosUploadContactProfilePhoto(&mtproto.TLPhotosUploadContactProfilePhoto{Suggest: true, UserId: contactId}); !errors.Is(err, mtproto.ErrPhotoFileMissing) {
			t.Errorf("suggestion without a file: %v, want PHOTO_FILE_MISSING", err)
		}
		if media.uploads != 0 || len(users.personal) != 0 || len(users.suggested) != 0 {
			t.Errorf("%d uploads, %d personal, %d suggested after refusals", media.uploads, len(users.personal), len(users.suggested))
		}
	})

	t.Run("save and suggest", func(t *testing.T) {
		users := &fakeUsers{contact: true}
		media := &fakeMedia{}
		msg := &fakeMsg{}
		c := newTestCore(users, media, msg)
		r, err := c.PhotosUploadContactProfilePhoto(&mtproto.TLPhotosUploadContactProfilePhoto{
			Save: true, Suggest: true, UserId: contactId, File: file,
		})
		if err != nil || r.GetPhoto().GetId() != 8 {
			t.Fatalf("upload: %v, %v", r, err)
		}
		if media.uploaded.GetOwnerId() != 77 {
			t.Errorf("uploaded with owner %d, want the caller's key", media.uploaded.GetOwnerId())
		}
		if len(users.personal) != 1 || users.personal[0].GetContactId() != 2 || users.personal[0].GetPhotoId() != 8 {
			t.Errorf("personal %v", users.personal)
		}
		if len(r.GetUsers()) != 1 || !r.GetUsers()[0].GetPhoto().GetPersonal() {
			t.Errorf("users %v, want the contact with the personal photo", r.GetUsers())
		}
		if len(users.suggested) != 1 || users.suggested[0].GetUserId() != 2 || users.suggested[0].GetFromUserId() != 1 ||
			users.suggested[0].GetPhotoId() != 8 {
			t.Errorf("suggested %v", users.suggested)
		}
		if len(msg.sent) != 1 {
			t.Fatalf("%d messages sent, want 1", len(msg.sent))
		}
		sent := msg.sent[0]
		action := sent.GetMessage()[0].GetMessage().GetAction()
		if sent.GetAuthKeyId() != 0 || sent.GetPeerId() != 2 || sent.GetPeerType() != mtproto.PEER_USER ||
			action.GetPredicateName() != mtproto.Predicate_messageActionSuggestProfilePhoto || action.GetPhoto().GetId() != 8 {
			t.Errorf("sent %v", sent)
		}
	})

	t.Run("remove", func(t *testing.T) {
		users := &fakeUsers{contact: true}
		media := &fakeMedia{}
		msg := &fakeMsg{}
		c := newTestCore(users, media, msg)
		r, err := c.PhotosUploadContactProfilePhoto(&mtproto.TLPhotosUploadContactProfilePhoto{Save: true, UserId: contactId})
		if err != nil || r.GetPhoto().GetPredicateName() != mtproto.Predicate_photoEmpty {
			t.Fatalf("removal: %v, %v", r, err)
		}
		if media.uploads != 0 || len(msg.sent) != 0 || len(users.personal) != 1 || users.personal[0].GetPhotoId() != 0 {
			t.Errorf("%d uploads, %d sent, personal %v", media.uploads, len(msg.sent), users.personal)
		}
	})
}
