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
