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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/usernames/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"

	"github.com/zeromicro/go-zero/core/logx"
)

// usernameTable is biz user over the username table: the peers by name and
// the users by id.
type usernameTable struct {
	user_client.UserClient
	peers map[string]*mtproto.Peer
	users map[int64]*mtproto.UserData
}

func (f *usernameTable) UserResolveUsername(_ context.Context, in *userpb.TLUserResolveUsername) (*mtproto.Peer, error) {
	if p, ok := f.peers[in.Username]; ok {
		return p, nil
	}
	return nil, mtproto.ErrUsernameNotOccupied
}

func (f *usernameTable) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		if u, ok := f.users[id]; ok {
			out.Datas = append(out.Datas, mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: u}).To_ImmutableUser())
		}
	}
	return out, nil
}

// publicChannels is biz channel with public channels by id.
type publicChannels struct {
	channel_client.ChannelClient
	titles map[int64]string
}

func (f *publicChannels) ChannelGetChannelListByIdList(_ context.Context, in *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	out := &channelpb.Vector_MutableChannel{}
	for _, id := range in.GetIdList() {
		if title, ok := f.titles[id]; ok {
			out.Datas = append(out.Datas, &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{
				Id: id, AccessHash: id * 3, Title: title, Broadcast: true, Username: "youtube",
			}})
		}
	}
	return out, nil
}

func userData(id int64, name, username string) *mtproto.UserData {
	return mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: id * 7, FirstName: name, Username: username}).To_UserData()
}

// A username names whoever has it in the username table: "music" a person
// and "youtube" a channel, not the bots of the upstream name list, and "gif"
// the built-in bot of the registry.
func TestResolveUsernameByTable(t *testing.T) {
	const self, musician, channel = int64(1), int64(136907800), int64(1500)
	users := &usernameTable{
		peers: map[string]*mtproto.Peer{
			"music":   mtproto.MakePeerUser(musician),
			"youtube": mtproto.MakePeerChannel(channel),
			"gif":     mtproto.MakePeerUser(inlinebots.Gif.Id),
			"places":  mtproto.MakePeerUser(inlinebots.Places.Id),
		},
		users: map[int64]*mtproto.UserData{
			self:                 userData(self, "Me", ""),
			musician:             userData(musician, "Musician", "music"),
			inlinebots.Gif.Id:    userData(inlinebots.Gif.Id, "GIF", "gif"),
			inlinebots.Places.Id: userData(inlinebots.Places.Id, "Places", "places"),
		},
	}
	ctx := context.Background()
	c := &UsernamesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{UserClient: users, Channel: &publicChannels{titles: map[int64]string{channel: "Videos"}}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: self},
	}
	resolve := func(username string) (*mtproto.Contacts_ResolvedPeer, error) {
		return c.ContactsResolveUsername(&mtproto.TLContactsResolveUsername{Username: username})
	}

	for username, want := range map[string]int64{"music": musician, "gif": inlinebots.Gif.Id, "places": inlinebots.Places.Id} {
		r, err := resolve(username)
		if err != nil {
			t.Fatalf("%s: %v", username, err)
		}
		if got := r.GetPeer().GetUserId(); got != want || r.GetPeer().GetPredicateName() != mtproto.Predicate_peerUser {
			t.Errorf("%s: peer %v, want user %d", username, r.GetPeer(), want)
		}
		if len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != want {
			t.Errorf("%s: users %v", username, r.GetUsers())
		}
	}

	r, err := resolve("youtube")
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPeer().GetChannelId() != channel || len(r.GetChats()) != 1 || r.GetChats()[0].GetId() != channel {
		t.Errorf("youtube: peer %v, chats %v", r.GetPeer(), r.GetChats())
	}

	// A name of the upstream list nobody has is free.
	if _, err := resolve("foursquare"); !errors.Is(err, mtproto.ErrUsernameNotOccupied) {
		t.Errorf("foursquare: %v, want USERNAME_NOT_OCCUPIED", err)
	}
}
