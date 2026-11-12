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
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// failingBlocks is a user service whose blocks and unblocks fail.
type failingBlocks struct {
	*fakeUsers
}

var errBlocks = errors.New("biz user is down")

func (failingBlocks) UserBlockPeer(context.Context, *userpb.TLUserBlockPeer) (*mtproto.Bool, error) {
	return nil, errBlocks
}

func (failingBlocks) UserUnBlockPeer(context.Context, *userpb.TLUserUnBlockPeer) (*mtproto.Bool, error) {
	return nil, errBlocks
}

// contacts.block and contacts.unblock pass the peer to biz, which pushes
// updatePeerBlocked with seq to every device of the account (ADR 0020):
// bff pushes nothing itself.
func TestBlockPassesThePeer(t *testing.T) {
	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{
		me: immutableUser(me, "Me", "", "", "", nil),
		3:  immutableUser(3, "C", "", "", "", nil),
	}}
	push := &fakeSync{}
	c := newCore(users, push, nil, nil)
	if r, err := c.ContactsBlock(&mtproto.TLContactsBlock{Id: inputPeerUser(3)}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("block: %v, %v", r, err)
	}
	if r, err := c.ContactsUnblock(&mtproto.TLContactsUnblock{Id: inputPeerUser(3)}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("unblock: %v, %v", r, err)
	}
	if want := []string{"block 3 u3", "unblock 3 u3"}; !reflect.DeepEqual(users.blockCalls, want) {
		t.Errorf("calls %v, want %v", users.blockCalls, want)
	}
	if len(push.pushed) != 0 {
		t.Errorf("pushes %v", push.pushed)
	}

	// Oneself and unknown users are not blocked.
	users.blockCalls = nil
	if _, err := c.ContactsBlock(&mtproto.TLContactsBlock{Id: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()}); err == nil {
		t.Errorf("blocked oneself")
	}
	if _, err := c.ContactsUnblock(&mtproto.TLContactsUnblock{Id: inputPeerUser(9)}); err == nil {
		t.Errorf("unblocked an unknown user")
	}
	if len(users.blockCalls) != 0 {
		t.Errorf("calls %v", users.blockCalls)
	}

	// A failure of biz is the answer: nothing was blocked or pushed.
	c.svcCtx.Dao.UserClient = failingBlocks{users}
	if _, err := c.ContactsBlock(&mtproto.TLContactsBlock{Id: inputPeerUser(3)}); !errors.Is(err, errBlocks) {
		t.Errorf("block with biz down: %v", err)
	}
	if _, err := c.ContactsUnblock(&mtproto.TLContactsUnblock{Id: inputPeerUser(3)}); !errors.Is(err, errBlocks) {
		t.Errorf("unblock with biz down: %v", err)
	}
}

// fakeStoriesBlocklist records what the methods ask of the blocklist of
// stories.
type fakeStoriesBlocklist struct {
	calls []string
}

func (f *fakeStoriesBlocklist) SetStoriesBlocked(_ context.Context, userId int64, peer *mtproto.InputPeer, blocked bool) error {
	verb := "unhide"
	if blocked {
		verb = "hide"
	}
	f.calls = append(f.calls, verb+" "+itoa(userId)+" "+itoa(peer.GetUserId()))
	return nil
}

func (f *fakeStoriesBlocklist) GetStoriesBlocked(_ context.Context, userId int64, offset, limit int32) (*mtproto.Contacts_Blocked, error) {
	f.calls = append(f.calls, "list "+itoa(userId))
	return mtproto.MakeTLContactsBlocked(&mtproto.Contacts_Blocked{
		Blocked: []*mtproto.PeerBlocked{mtproto.MakeTLPeerBlocked(&mtproto.PeerBlocked{PeerId: mtproto.MakePeerUser(4)}).To_PeerBlocked()},
		Chats:   []*mtproto.Chat{},
		Users:   []*mtproto.User{},
	}).To_Contacts_Blocked(), nil
}

func (f *fakeStoriesBlocklist) ReplaceStoriesBlocked(_ context.Context, userId int64, peers []*mtproto.InputPeer, limit int32) error {
	f.calls = append(f.calls, "replace "+itoa(userId)+" "+itoa(int64(len(peers)))+" "+itoa(int64(limit)))
	return nil
}

// With my_stories_from the methods of contacts read and change the
// blocklist of stories, never the main one: "hide my stories from" in the
// list of viewers of a story must not block the user (Android
// StoriesController.java:4483-4513).
func TestMyStoriesFromIsNotTheMainBlocklist(t *testing.T) {
	users := &fakeUsers{
		users: map[int64]*mtproto.ImmutableUser{
			me: immutableUser(me, "Me", "", "", "", nil),
			3:  immutableUser(3, "C", "", "", "", nil),
		},
		blocked: []int64{3},
	}
	push := &fakeSync{}
	c := newCore(users, push, nil, nil)
	hidden := &fakeStoriesBlocklist{}
	c.svcCtx.Stories = hidden

	if r, err := c.ContactsBlock(&mtproto.TLContactsBlock{MyStoriesFrom: true, Id: inputPeerUser(3)}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("block: %v, %v", r, err)
	}
	if r, err := c.ContactsUnblock(&mtproto.TLContactsUnblock{MyStoriesFrom: true, Id: inputPeerUser(3)}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("unblock: %v, %v", r, err)
	}
	list, err := c.ContactsGetBlocked(&mtproto.TLContactsGetBlocked{MyStoriesFrom: true, Limit: 100})
	if err != nil || len(list.GetBlocked()) != 1 || list.GetBlocked()[0].GetPeerId().GetUserId() != 4 {
		t.Fatalf("getBlocked: %v, %v", list, err)
	}
	if r, err := c.ContactsSetBlocked(&mtproto.TLContactsSetBlocked{MyStoriesFrom: true, Id: []*mtproto.InputPeer{inputPeerUser(3)}, Limit: 1}); err != nil || !mtproto.FromBool(r) {
		t.Fatalf("setBlocked: %v, %v", r, err)
	}
	want := []string{"hide " + itoa(me) + " 3", "unhide " + itoa(me) + " 3", "list " + itoa(me), "replace " + itoa(me) + " 1 1"}
	if !reflect.DeepEqual(hidden.calls, want) {
		t.Errorf("blocklist of stories: %v, want %v", hidden.calls, want)
	}
	if len(users.blockCalls) != 0 || len(push.pushed) != 0 {
		t.Errorf("the main blocklist was touched: %v, %v", users.blockCalls, push.pushed)
	}

	// Where the stories are not served the list does not exist, and the
	// main one is still not touched.
	c.svcCtx.Stories = nil
	if _, err := c.ContactsBlock(&mtproto.TLContactsBlock{MyStoriesFrom: true, Id: inputPeerUser(3)}); err == nil {
		t.Errorf("block without the stories")
	}
	if _, err := c.ContactsGetBlocked(&mtproto.TLContactsGetBlocked{MyStoriesFrom: true, Limit: 100}); err == nil {
		t.Errorf("getBlocked without the stories")
	}
	if len(users.blockCalls) != 0 {
		t.Errorf("the main blocklist was touched: %v", users.blockCalls)
	}
}
