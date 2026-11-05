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

// Package data gives the pages of links what anybody may see of the
// objects they name, from the services of the server (task 026).
package data

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/catalog"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
)

// UserClient, ChannelClient, DfsClient and Sets are what the pages read of
// the services: the subsets of their clients.
type (
	UserClient interface {
		UserResolveUsername(ctx context.Context, in *userpb.TLUserResolveUsername) (*mtproto.Peer, error)
		UserGetMutableUsersV2(ctx context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error)
	}
	ChannelClient interface {
		ChannelGetMutableChannel(ctx context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error)
	}
	DfsClient interface {
		DfsDownloadFile(ctx context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error)
	}
	Sets interface {
		SetByShortName(ctx context.Context, name string) (*catalog.Set, error)
	}
)

// Source is the links.Resolver and links.Avatars of the services.
type Source struct {
	User    UserClient
	Channel ChannelClient
	Dfs     DfsClient
	Sets    Sets
}

// anybody is the visitor of a page: no account, no contact of anybody.
var anybody = mtproto.PrivacyViewer{}

// visible tells whether anybody may see the key of u (task 034, ADR 0031):
// its rules evaluated for no account, a missing rule hiding it, as
// ImmutableUser.CheckPrivacyFor does.
func visible(u *mtproto.ImmutableUser, key int) bool {
	for _, k := range u.KeysPrivacyRules {
		if k.Key == int32(key) {
			return mtproto.EvaluatePrivacy(k.Rules, anybody)
		}
	}
	return false
}

// owner is what the username names: a user and its view, a channel, or
// nothing.
type owner struct {
	user    *mtproto.ImmutableUser
	channel *channelpb.MutableChannel
}

func (s *Source) resolve(ctx context.Context, username string) (*owner, error) {
	peer, err := s.User.UserResolveUsername(ctx, &userpb.TLUserResolveUsername{Username: username})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == mtproto.ErrBadRequest {
			return nil, nil
		}
		return nil, err
	}
	p := mtproto.FromPeer(peer)
	switch p.PeerType {
	case mtproto.PEER_USER:
		users, err := s.User.UserGetMutableUsersV2(ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{p.PeerId}, Privacy: true, HasTo: true, To: []int64{p.PeerId}})
		if err != nil {
			return nil, err
		}
		u, ok := users.GetImmutableUser(p.PeerId)
		if !ok || u.Deleted() {
			return nil, nil
		}
		return &owner{user: u}, nil
	case mtproto.PEER_CHANNEL:
		mc, err := s.Channel.ChannelGetMutableChannel(ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: p.PeerId})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == mtproto.ErrBadRequest {
				return nil, nil
			}
			return nil, err
		}
		if ch := mc.GetChannel(); ch == nil || ch.GetDeleted() || ch.GetUsername() == "" {
			return nil, nil
		}
		return &owner{channel: mc}, nil
	}
	return nil, nil
}

// Peer implements links.Resolver.
func (s *Source) Peer(ctx context.Context, username string) (*links.Peer, error) {
	o, err := s.resolve(ctx, username)
	if err != nil || o == nil {
		return nil, err
	}
	if ch := o.channel.GetChannel(); ch != nil {
		p := &links.Peer{
			Kind:     links.PeerGroup,
			Username: ch.GetUsername(),
			Name:     ch.GetTitle(),
			About:    ch.GetAbout(),
			Members:  int(ch.GetParticipantsCount()),
			HasPhoto: photoId(ch.GetPhoto()) != 0,
		}
		if ch.GetBroadcast() {
			p.Kind, p.Public = links.PeerChannel, true
		}
		return p, nil
	}
	u := o.user
	p := &links.Peer{
		Kind:      links.PeerUser,
		Username:  u.Username(),
		Name:      fullName(u.FirstName(), u.LastName()),
		FirstName: u.FirstName(),
		HasPhoto:  userPhoto(u) != 0,
	}
	if u.IsBot() {
		p.Kind = links.PeerBot
	}
	if p.Username == "" {
		p.Username = username
	}
	if u.IsBot() || visible(u, mtproto.ABOUT) {
		p.About = u.User.GetAbout().GetValue()
	}
	return p, nil
}

// userPhoto is the photo of u anybody sees: the main one if its privacy
// lets everybody see it, else the public one (task 051), else none.
func userPhoto(u *mtproto.ImmutableUser) int64 {
	if id := photoId(u.User.GetProfilePhoto()); id != 0 && (u.IsBot() || visible(u, mtproto.PROFILE_PHOTO)) {
		return id
	}
	return photoId(u.FallbackPhoto())
}

func photoId(p *mtproto.Photo) int64 {
	if p.GetPredicateName() != mtproto.Predicate_photo {
		return 0
	}
	return p.GetId()
}

func fullName(first, last string) string {
	switch {
	case last == "":
		return first
	case first == "":
		return last
	}
	return first + " " + last
}

// avatarSizes are the sizes of a profile photo, the largest first (dfs:
// a 160, b 320, c 640 px).
var avatarSizes = []string{"c", "b", "a"}

// Avatar implements links.Avatars: the photo of the username's owner that
// anybody may see.
func (s *Source) Avatar(ctx context.Context, username string) ([]byte, error) {
	o, err := s.resolve(ctx, username)
	if err != nil || o == nil {
		return nil, err
	}
	var id int64
	if o.channel != nil {
		id = photoId(o.channel.GetChannel().GetPhoto())
	} else {
		id = userPhoto(o.user)
	}
	if id == 0 {
		return nil, nil
	}
	return s.photo(ctx, id, avatarSizes)
}

// maxPhoto bounds a photo read for a page.
const maxPhoto = 8 << 20

// photo reads the first of sizes the photo id has, in chunks of the 1 MB
// dfs answers at most.
func (s *Source) photo(ctx context.Context, id int64, sizes []string) ([]byte, error) {
	const chunk = 1 << 20
	for _, size := range sizes {
		var b []byte
		for {
			f, err := s.Dfs.DfsDownloadFile(ctx, &dfs.TLDfsDownloadFile{
				Location: mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: id, ThumbSize: size}).To_InputFileLocation(),
				Offset:   int64(len(b)),
				Limit:    chunk,
			})
			if err != nil {
				return nil, err
			}
			b = append(b, f.GetBytes()...)
			if len(f.GetBytes()) < chunk || len(b) >= maxPhoto {
				break
			}
		}
		if len(b) > 0 && len(b) < maxPhoto {
			return b, nil
		}
	}
	return nil, nil
}

// StickerSet implements links.Resolver: a set of the catalog by its short
// name.
func (s *Source) StickerSet(ctx context.Context, name string) (string, bool, bool, error) {
	set, err := s.Sets.SetByShortName(ctx, name)
	if err != nil || set == nil {
		return "", false, false, err
	}
	answer := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(set.Data, answer); err != nil {
		return "", false, false, err
	}
	return answer.GetSet().GetTitle(), answer.GetSet().GetEmojis(), true, nil
}
