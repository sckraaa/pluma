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

package dao

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// viewSource is the source of userId's view of the post id, -1 when the
// view was not counted.
func (f *fixture) viewSource(channelId int64, id int32, userId int64) int {
	f.t.Helper()
	var source []int
	if err := f.d.Mysql.QueryRowsPartial(context.Background(), &source,
		"SELECT source FROM channel_message_views WHERE channel_id = ? AND message_id = ? AND user_id = ?", channelId, id, userId); err != nil {
		f.t.Fatal(err)
	}
	if len(source) == 0 {
		return -1
	}
	return source[0]
}

// TestViewSources: a view keeps where it came from (task 103): a member in
// the channel is a subscriber, a reader of a public channel who is no
// member is another; a copy elsewhere names its kind of chat. The first
// view keeps its source, as the view is counted once.
func TestViewSources(t *testing.T) {
	ctx := context.Background()
	f := viewsFixture(t)
	author, member, reader, other := itId(), itId(), itId(), itId()
	id := f.create(author, false)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
		fmt.Sprintf("views%d", id), mtproto.PEER_CHANNEL, id)
	f.join(id, member)
	f.post(id, author, 1)
	f.post(id, author, 2)

	for _, v := range []struct {
		user   int64
		post   int32
		source channelstats.ViewSource
	}{
		{member, 1, channelstats.ViewSubscriber},
		{reader, 1, channelstats.ViewSubscriber},
		{other, 1, channelstats.ViewGroup},
		{member, 2, channelstats.ViewChannel},
		{reader, 2, channelstats.ViewPrivate},
	} {
		if err := f.d.IncrementViews(ctx, v.user, id, []int32{v.post}, v.source); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []struct {
		user   int64
		post   int32
		source channelstats.ViewSource
	}{
		{member, 1, channelstats.ViewSubscriber},
		{reader, 1, channelstats.ViewOther},
		{other, 1, channelstats.ViewGroup},
		{member, 2, channelstats.ViewChannel},
		{reader, 2, channelstats.ViewPrivate},
	} {
		if got := f.viewSource(id, want.post, want.user); got != int(want.source) {
			t.Errorf("post %d, user %d: source %d, want %d", want.post, want.user, got, want.source)
		}
	}

	// A view again, from elsewhere: counted once, with its first source.
	if err := f.d.IncrementViews(ctx, other, id, []int32{1}, channelstats.ViewPrivate); err != nil {
		t.Fatal(err)
	}
	if got := f.viewSource(id, 1, other); got != int(channelstats.ViewGroup) {
		t.Errorf("a repeated view: source %d", got)
	}
	if v, _ := f.counters(id, 1); v != 4 {
		t.Errorf("post 1: views %d, want the author's and three", v)
	}

	if err := f.d.IncrementViews(ctx, member, id, []int32{1}, channelstats.ViewPrivate+1); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Errorf("an unknown source: %v", err)
	}
}
