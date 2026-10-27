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
	photos []int64
}

func (f *fakeMedia) MediaGetPhoto(_ context.Context, in *mediapb.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if !slices.Contains(f.photos, in.PhotoId) {
		return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: in.PhotoId}).To_Photo(), nil
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: in.PhotoId, AccessHash: in.PhotoId * 10}).To_Photo(), nil
}

// fakeUsers records the main photos biz is asked for; the removal of the
// main photo makes next main.
type fakeUsers struct {
	user_client.UserClient
	asked []int64
	next  int64
}

func (f *fakeUsers) UserUpdateProfilePhoto(_ context.Context, in *userpb.TLUserUpdateProfilePhoto) (*mtproto.Int64, error) {
	f.asked = append(f.asked, in.Id)
	if in.Id == 0 {
		return &mtproto.Int64{V: f.next}, nil
	}
	return &mtproto.Int64{V: in.Id}, nil
}

// photos.updateProfilePhoto makes a photo main only with its access_hash,
// as in Telegram; inputPhotoEmpty removes the main photo.
func TestUpdateProfilePhotoAccessHash(t *testing.T) {
	users := &fakeUsers{next: 6}
	ctx := context.Background()
	c := &UserChannelProfilesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, MediaClient: &fakeMedia{photos: []int64{5, 6}}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 1},
	}
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
}
