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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
)

// PreviewChannelClient is what the channel preview reads of the channel
// service.
type PreviewChannelClient interface {
	ChannelClient
	ChannelGetHistory(ctx context.Context, in *channelpb.TLChannelGetHistory) (*mtproto.Messages_Messages, error)
	ChannelGetMessages(ctx context.Context, in *channelpb.TLChannelGetMessages) (*channelpb.Vector_Message, error)
}

// Preview is the links.PreviewSource of the services. The viewer is no
// account (self_id 0): the channel service lets it read a public channel
// only (dao.viewerOf).
type Preview struct {
	*Source
	Channels PreviewChannelClient
}

// Channel implements links.PreviewSource: the public broadcast channel of
// the username.
func (p *Preview) Channel(ctx context.Context, username string) (*links.Peer, int64, error) {
	o, err := p.resolve(ctx, username)
	if err != nil || o == nil || o.channel == nil {
		return nil, 0, err
	}
	ch := o.channel.GetChannel()
	if !ch.GetBroadcast() {
		return nil, 0, nil
	}
	peer, err := p.Peer(ctx, username)
	if err != nil || peer == nil {
		return nil, 0, err
	}
	return peer, ch.GetId(), nil
}

// Posts implements links.PreviewSource.
func (p *Preview) Posts(ctx context.Context, channelId int64, before, limit int) ([]links.Post, error) {
	h, err := p.Channels.ChannelGetHistory(ctx, &channelpb.TLChannelGetHistory{
		SelfId:    0,
		ChannelId: channelId,
		OffsetId:  int32(before),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]links.Post, 0, len(h.GetMessages()))
	for _, m := range h.GetMessages() {
		out = append(out, postOf(m))
	}
	return out, nil
}

func postOf(m *mtproto.Message) links.Post {
	post := links.Post{
		ID:   int(m.GetId()),
		Date: time.Unix(int64(m.GetDate()), 0),
		Text: m.GetMessage(),
	}
	if m.GetPredicateName() == mtproto.Predicate_messageService {
		post.Service = true
		return post
	}
	post.Views = int(m.GetViews().GetValue())
	for _, e := range m.GetEntities() {
		if kind, ok := entityKinds[e.GetPredicateName()]; ok {
			post.Entities = append(post.Entities, links.Entity{Kind: kind, Offset: int(e.GetOffset()), Length: int(e.GetLength()), URL: e.GetUrl()})
		}
	}
	switch media := m.GetMedia(); media.GetPredicateName() {
	case "", mtproto.Predicate_messageMediaEmpty, mtproto.Predicate_messageMediaWebPage:
	case mtproto.Predicate_messageMediaPhoto:
		post.Media, post.HasPhoto = "photo", photoId(media.GetPhoto_FLAGPHOTO()) != 0
	case mtproto.Predicate_messageMediaDocument:
		post.Media = "file"
		for _, a := range media.GetDocument().GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeVideo {
				post.Media = "video"
			}
		}
	default:
		post.Media = "media"
	}
	return post
}

var entityKinds = map[string]string{
	mtproto.Predicate_messageEntityBold:       "bold",
	mtproto.Predicate_messageEntityItalic:     "italic",
	mtproto.Predicate_messageEntityUnderline:  "underline",
	mtproto.Predicate_messageEntityStrike:     "strike",
	mtproto.Predicate_messageEntityCode:       "code",
	mtproto.Predicate_messageEntityPre:        "pre",
	mtproto.Predicate_messageEntitySpoiler:    "spoiler",
	mtproto.Predicate_messageEntityBlockquote: "blockquote",
	mtproto.Predicate_messageEntityUrl:        "url",
	mtproto.Predicate_messageEntityTextUrl:    "text_url",
	mtproto.Predicate_messageEntityMention:    "mention",
}

// postPhotoSizes are the sizes of a photo of a post, the one fitting the
// preview first (dfs: m 320, x 800, y 1280 px).
var postPhotoSizes = []string{"x", "y", "m"}

// PostPhoto implements links.PreviewSource.
func (p *Preview) PostPhoto(ctx context.Context, username string, id int) ([]byte, error) {
	_, channelId, err := p.Channel(ctx, username)
	if err != nil || channelId == 0 {
		return nil, err
	}
	msgs, err := p.Channels.ChannelGetMessages(ctx, &channelpb.TLChannelGetMessages{SelfId: 0, ChannelId: channelId, Id: []int32{int32(id)}})
	if err != nil {
		return nil, err
	}
	var photo int64
	for _, m := range msgs.GetDatas() {
		if m.GetId() == int32(id) && m.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaPhoto {
			photo = photoId(m.GetMedia().GetPhoto_FLAGPHOTO())
		}
	}
	if photo == 0 {
		return nil, nil
	}
	return p.photo(ctx, photo, postPhotoSizes)
}
