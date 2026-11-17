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
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/messages/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelstats"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	viewsUser     = 7
	viewsOriginal = 10 // the broadcast channel of the post
	viewsPrivate  = 11 // a broadcast channel the account may not read
	viewsGroup    = 20
	viewsChannel  = 30
)

// viewsChannels is biz channel with the messages of a few channels; it
// keeps the views counted.
type viewsChannels struct {
	channel_client.ChannelClient
	messages map[int64][]*mtproto.Message
	counted  []string
}

func (f *viewsChannels) ChannelIncrementMessageViews(_ context.Context, in *channelpb.TLChannelIncrementMessageViews) (*mtproto.Bool, error) {
	if in.ChannelId == viewsPrivate {
		return nil, mtproto.ErrChannelPrivate
	}
	f.counted = append(f.counted, fmt.Sprintf("%d:%v:%d", in.ChannelId, in.Id, in.Source))
	return mtproto.BoolTrue, nil
}

func (f *viewsChannels) ChannelGetMessages(_ context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error) {
	if in.ChannelId == viewsPrivate {
		return nil, mtproto.ErrChannelPrivate
	}
	out := &channelpb.Vector_Message{}
	for _, m := range f.messages[in.ChannelId] {
		if slices.Contains(in.Id, m.Id) {
			out.Datas = append(out.Datas, m)
		}
	}
	return out, nil
}

func (f *viewsChannels) ChannelGetChannelListByIdList(context.Context, *channelpb.TLChannelGetChannelListByIdList) (*channelpb.Vector_MutableChannel, error) {
	return &channelpb.Vector_MutableChannel{}, nil
}

type viewsUsers struct{ user_client.UserClient }

func (viewsUsers) UserGetMutableUsersV2(context.Context, *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	return &mtproto.MutableUsers{}, nil
}

func viewsMessage(id int32, post bool, views int32, fwd *mtproto.MessageFwdHeader) *mtproto.Message {
	m := &mtproto.Message{Id: id, Post: post, Views: wrapperspb.Int32(views), Forwards: wrapperspb.Int32(0), FwdFrom: fwd}
	return mtproto.MakeTLMessage(m).To_Message()
}

func viewsFwd(from int64, post int32, discussion bool) *mtproto.MessageFwdHeader {
	h := &mtproto.MessageFwdHeader{FromId: mtproto.MakePeerChannel(from), ChannelPost: wrapperspb.Int32(post), Date: 1}
	if discussion {
		h.SavedFromPeer = mtproto.MakePeerChannel(from)
		h.SavedFromMsgId = wrapperspb.Int32(post)
	}
	return mtproto.MakeTLMessageFwdHeader(h).To_MessageFwdHeader()
}

// TestChannelViewsOfCopies: a forwarded copy of a post in a supergroup
// counts its view to the post from a group and shows the post's counters;
// a copy in a broadcast channel counts from a channel and keeps its own; the
// automatic copy in a discussion group, and a post the account may not
// read, count nothing elsewhere (task 103).
func TestChannelViewsOfCopies(t *testing.T) {
	original := viewsMessage(5, true, 42, nil)
	original.Forwards = wrapperspb.Int32(3)
	channels := &viewsChannels{messages: map[int64][]*mtproto.Message{
		viewsOriginal: {original},
		viewsGroup: {
			viewsMessage(1, false, 7, viewsFwd(viewsOriginal, 5, false)),
			viewsMessage(2, false, 0, nil),
			viewsMessage(3, false, 0, viewsFwd(viewsOriginal, 5, true)),
			viewsMessage(4, false, 9, viewsFwd(viewsPrivate, 8, false)),
		},
		viewsChannel: {viewsMessage(1, true, 1, viewsFwd(viewsOriginal, 5, false))},
	}}
	ctx := context.Background()
	c := &MessagesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{Channel: channels, UserClient: viewsUsers{}}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: viewsUser},
	}
	views := func(r *mtproto.Messages_MessageViews) string {
		var out []string
		for _, v := range r.GetViews() {
			out = append(out, fmt.Sprintf("%d/%d", v.GetViews().GetValue(), v.GetForwards().GetValue()))
		}
		return fmt.Sprint(out)
	}

	r, err := c.channelMessagesViews(viewsGroup, []int32{1, 2, 3, 4}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := views(r), "[42/3 0/0 0/0 9/0]"; got != want {
		t.Errorf("the supergroup: views %s, want %s", got, want)
	}
	want := fmt.Sprint([]string{
		fmt.Sprintf("%d:[1 2 3 4]:%d", viewsGroup, channelstats.ViewSubscriber),
		fmt.Sprintf("%d:[5]:%d", viewsOriginal, channelstats.ViewGroup),
	})
	if got := fmt.Sprint(channels.counted); got != want {
		t.Errorf("the supergroup: counted %s, want %s", got, want)
	}

	channels.counted = nil
	if r, err = c.channelMessagesViews(viewsChannel, []int32{1}, true); err != nil {
		t.Fatal(err)
	}
	if got, want := views(r), "[1/0]"; got != want {
		t.Errorf("the channel: views %s, want %s", got, want)
	}
	want = fmt.Sprint([]string{
		fmt.Sprintf("%d:[1]:%d", viewsChannel, channelstats.ViewSubscriber),
		fmt.Sprintf("%d:[5]:%d", viewsOriginal, channelstats.ViewChannel),
	})
	if got := fmt.Sprint(channels.counted); got != want {
		t.Errorf("the channel: counted %s, want %s", got, want)
	}

	// Without increment: the counters, and nothing counted.
	channels.counted = nil
	if r, err = c.channelMessagesViews(viewsGroup, []int32{1}, false); err != nil {
		t.Fatal(err)
	}
	if got := views(r); got != "[42/3]" || len(channels.counted) != 0 {
		t.Errorf("no increment: views %s, counted %v", got, channels.counted)
	}
}
