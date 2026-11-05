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

package data

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
)

type fakeServices struct {
	peers    map[string]*mtproto.Peer
	users    map[int64]*mtproto.ImmutableUser
	channels map[int64]*channelpb.MutableChannel
	photos   map[string][]byte // "<size>/<photo id>"
}

func (f *fakeServices) UserResolveUsername(_ context.Context, in *userpb.TLUserResolveUsername) (*mtproto.Peer, error) {
	if p, ok := f.peers[in.Username]; ok {
		return p, nil
	}
	return nil, mtproto.ErrUsernameNotOccupied
}

func (f *fakeServices) UserGetMutableUsersV2(_ context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	out := &mtproto.MutableUsers{}
	for _, id := range in.Id {
		if u, ok := f.users[id]; ok {
			out.Users = append(out.Users, u)
		}
	}
	return out, nil
}

func (f *fakeServices) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if c, ok := f.channels[in.ChannelId]; ok {
		return c, nil
	}
	return nil, mtproto.ErrChannelInvalid
}

func (f *fakeServices) DfsDownloadFile(_ context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	b := f.photos[in.Location.GetThumbSize()+"/"+itoa64(in.Location.GetId())]
	if int(in.Offset) >= len(b) {
		b = nil
	} else {
		b = b[in.Offset:]
	}
	return &mtproto.Upload_File{Bytes: b}, nil
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	for n > 0 {
		s = append([]byte{byte('0' + n%10)}, s...)
		n /= 10
	}
	return string(s)
}

func photo(id int64) *mtproto.Photo {
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: id}).To_Photo()
}

func everybody(key int) *mtproto.PrivacyKeyRules {
	return &mtproto.PrivacyKeyRules{Key: int32(key), Rules: []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}}
}

func contactsOnly(key int) *mtproto.PrivacyKeyRules {
	return &mtproto.PrivacyKeyRules{Key: int32(key), Rules: []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}}
}

func newFake() *Source {
	f := &fakeServices{
		peers: map[string]*mtproto.Peer{
			"open_user":  mtproto.MakePeerUser(1),
			"closed_one": mtproto.MakePeerUser(2),
			"plumanews":  mtproto.MakePeerChannel(100),
			"chatgroup":  mtproto.MakePeerChannel(101),
			"gone_user":  mtproto.MakePeerUser(3),
		},
		users: map[int64]*mtproto.ImmutableUser{
			1: {User: &mtproto.UserData{Id: 1, FirstName: "Open", LastName: "User", Username: "open_user", About: wrapperspb.String("hello"), ProfilePhoto: photo(11)},
				KeysPrivacyRules: []*mtproto.PrivacyKeyRules{everybody(mtproto.ABOUT), everybody(mtproto.PROFILE_PHOTO)}},
			2: {User: &mtproto.UserData{Id: 2, FirstName: "Closed", Username: "closed_one", About: wrapperspb.String("secret"), ProfilePhoto: photo(21), FallbackPhoto: photo(22)},
				KeysPrivacyRules: []*mtproto.PrivacyKeyRules{contactsOnly(mtproto.ABOUT), contactsOnly(mtproto.PROFILE_PHOTO)}},
			3: {User: &mtproto.UserData{Id: 3, FirstName: "Gone", Username: "gone_user", Deleted: true}},
		},
		channels: map[int64]*channelpb.MutableChannel{
			100: {Channel: &channelpb.ImmutableChannel{Id: 100, Title: "Pluma News", Username: "plumanews", About: "news", ParticipantsCount: 5, Broadcast: true}},
			101: {Channel: &channelpb.ImmutableChannel{Id: 101, Title: "A group", Username: "chatgroup", ParticipantsCount: 3, Megagroup: true}},
		},
		photos: map[string][]byte{"c/11": []byte("main"), "a/22": []byte("public")},
	}
	return &Source{User: f, Channel: f, Dfs: f}
}

// TestPeerPrivacy: a page shows of a user what its privacy lets anybody
// see: the about text and the main photo for everybody, else neither (its
// public photo instead); a deleted user is nobody.
func TestPeerPrivacy(t *testing.T) {
	s := newFake()
	ctx := context.Background()
	p, err := s.Peer(ctx, "open_user")
	if err != nil || p == nil || p.Name != "Open User" || p.About != "hello" || !p.HasPhoto || p.Kind != links.PeerUser {
		t.Fatalf("open user: %+v, %v", p, err)
	}
	if b, _ := s.Avatar(ctx, "open_user"); string(b) != "main" {
		t.Errorf("open user's photo: %q", b)
	}
	p, err = s.Peer(ctx, "closed_one")
	if err != nil || p == nil || p.About != "" || !p.HasPhoto {
		t.Fatalf("closed user: %+v, %v", p, err)
	}
	if b, _ := s.Avatar(ctx, "closed_one"); string(b) != "public" {
		t.Errorf("closed user's photo: %q, want the public one", b)
	}
	for _, name := range []string{"gone_user", "nobody_here"} {
		if p, err := s.Peer(ctx, name); err != nil || p != nil {
			t.Errorf("%s: %+v, %v", name, p, err)
		}
	}
	if p, _ := s.Peer(ctx, "plumanews"); p == nil || p.Kind != links.PeerChannel || !p.Public || p.Members != 5 {
		t.Errorf("channel: %+v", p)
	}
	if p, _ := s.Peer(ctx, "chatgroup"); p == nil || p.Kind != links.PeerGroup || p.Public {
		t.Errorf("group: %+v", p)
	}
	pv := &Preview{Source: s}
	if p, _, _ := pv.Channel(ctx, "chatgroup"); p != nil {
		t.Error("a group has a preview")
	}
}

// TestSpoilerPhoto: a photo under a spoiler is not shown in the preview;
// the post says it has a photo.
func TestSpoilerPhoto(t *testing.T) {
	m := func(spoiler bool) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{Id: 5, Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: photo(9), Spoiler: spoiler}).To_MessageMedia()}).To_Message()
	}
	if p := postOf(m(false)); !p.HasPhoto || p.Media != "photo" {
		t.Errorf("a photo: %+v", p)
	}
	if p := postOf(m(true)); p.HasPhoto || p.Media != "photo" {
		t.Errorf("a photo under a spoiler: %+v", p)
	}
}
