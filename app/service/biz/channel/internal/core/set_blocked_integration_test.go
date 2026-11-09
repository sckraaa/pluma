//go:build integration

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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// TestSetBlocked: the owner of the server blocks a channel and unblocks it
// (the panel): it is restricted for its members while blocked, its readers
// are refused, a repeat changes nothing, and a deleted channel cannot be
// blocked.
func TestSetBlocked(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.create(creator)

	set := func(blocked bool) error {
		_, err := f.c.ChannelSetBlocked(&channel.TLChannelSetBlocked{ChannelId: id, Blocked: mtproto.ToBool(blocked)})
		return err
	}
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	mc, err := f.c.svcCtx.Dao.GetMutableChannel(f.c.ctx, id, []int64{creator})
	if err != nil {
		t.Fatal(err)
	}
	if !mc.GetChannel().GetBlocked() {
		t.Fatal("not blocked in the store")
	}
	if c := mc.ToChat(creator, 0); !c.GetRestricted() || len(c.GetRestrictionReason()) != 1 || c.GetRestrictionReason()[0].GetPlatform() != "all" {
		t.Errorf("view of a blocked channel: %+v", c)
	}
	version := func() int32 {
		mc, err := f.c.svcCtx.Dao.GetMutableChannel(f.c.ctx, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		return mc.GetChannel().GetVersion()
	}
	before := version()
	if err := set(true); err != nil {
		t.Fatal(err)
	}
	if version() != before {
		t.Error("a repeat changed the channel")
	}
	// A catch-up goes on, so that the clients do not take the channel for
	// lost (Dao.ViewerOf); reading the history is refused.
	if v, err := f.c.svcCtx.Dao.ViewerOf(f.c.ctx, creator, id); err != nil || !v.CanRead {
		t.Errorf("difference while blocked: %+v %v", v, err)
	}
	_, err = f.c.ChannelGetHistory(&channel.TLChannelGetHistory{SelfId: creator, ChannelId: id, Limit: 10})
	wantErr(t, "history while blocked", err, mtproto.ErrChannelPrivate)

	if err := set(false); err != nil {
		t.Fatal(err)
	}
	mc, err = f.c.svcCtx.Dao.GetMutableChannel(f.c.ctx, id, []int64{creator})
	if err != nil || mc.GetChannel().GetBlocked() || mc.ToChat(creator, 0).GetRestricted() {
		t.Errorf("after the unblock: %+v, %v", mc.GetChannel(), err)
	}

	if _, err := f.deleteChannel(creator, id); err != nil {
		t.Fatal(err)
	}
	if err := set(true); err == nil {
		t.Error("a deleted channel was blocked")
	}
}
