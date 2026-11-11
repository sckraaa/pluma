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
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	channel_client "github.com/teamgram/teamgram-server/app/service/biz/channel/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	lookChannel int64 = 40
	lookHash    int64 = 41
	lookEmoji   int64 = 900 // a custom emoji of the catalog
	lookSticker int64 = 901 // a sticker of the catalog
)

// lookChannels is biz channel for one channel; it keeps what it was asked.
type lookChannels struct {
	channel_client.ChannelClient
	color  *channelpb.TLChannelUpdateColor
	status *channelpb.TLChannelUpdateEmojiStatus
}

func (f *lookChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if in.GetChannelId() != lookChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	return &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Id: lookChannel, AccessHash: lookHash}}, nil
}

func (f *lookChannels) ChannelUpdateColor(_ context.Context, in *channelpb.TLChannelUpdateColor) (*mtproto.Updates, error) {
	f.color = in
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

func (f *lookChannels) ChannelUpdateEmojiStatus(_ context.Context, in *channelpb.TLChannelUpdateEmojiStatus) (*mtproto.Updates, error) {
	f.status = in
	return mtproto.MakeTLUpdates(&mtproto.Updates{}).To_Updates(), nil
}

// emojiMedia gives the documents of the catalog: lookEmoji a custom emoji,
// the others stickers.
type emojiMedia struct{ media_client.MediaClient }

func (emojiMedia) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	out := &media.Vector_Document{}
	for _, id := range in.GetIdList() {
		attr := mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{}).To_DocumentAttribute()
		if id == lookEmoji {
			attr = mtproto.MakeTLDocumentAttributeCustomEmoji(&mtproto.DocumentAttribute{}).To_DocumentAttribute()
		}
		out.Datas = append(out.Datas, mtproto.MakeTLDocument(&mtproto.Document{Id: id, Attributes: []*mtproto.DocumentAttribute{attr}}).To_Document())
	}
	return out, nil
}

func newLookCore(t *testing.T) (*CatalogCore, *lookChannels, *memCatalog) {
	t.Helper()
	m := &memCatalog{docs: map[int64]bool{lookEmoji: true, lookSticker: true}}
	put(t, m, "help.getPeerColors", "", 1, mtproto.MakeTLHelpPeerColors(&mtproto.Help_PeerColors{Colors: []*mtproto.Help_PeerColorOption{
		mtproto.MakeTLHelpPeerColorOption(&mtproto.Help_PeerColorOption{ColorId: 7}).To_Help_PeerColorOption(),
	}}).To_Help_PeerColors())
	put(t, m, "help.getPeerProfileColors", "", 1, mtproto.MakeTLHelpPeerColors(&mtproto.Help_PeerColors{Colors: []*mtproto.Help_PeerColorOption{
		mtproto.MakeTLHelpPeerColorOption(&mtproto.Help_PeerColorOption{ColorId: 0}).To_Help_PeerColorOption(),
		mtproto.MakeTLHelpPeerColorOption(&mtproto.Help_PeerColorOption{ColorId: 15}).To_Help_PeerColorOption(),
	}}).To_Help_PeerColors())
	ch := &lookChannels{}
	c := New(context.Background(), &svc.ServiceContext{Catalog: m, Media: emojiMedia{}, Channel: ch})
	c.MD = &metadata.RpcMetadata{UserId: 1, PermAuthKeyId: 2}
	return c, ch, m
}

func lookInput(hash int64) *mtproto.InputChannel {
	return mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: lookChannel, AccessHash: hash}).To_InputChannel()
}

// A color is one built in or listed for its kind; without one it is none;
// the old constructor always has one; the pattern is a custom emoji of the
// catalog; a wrong access hash is CHANNEL_INVALID.
func TestChannelsUpdateColor(t *testing.T) {
	c, ch, _ := newLookCore(t)
	update := func(forProfile bool, color *int32, emoji int64) error {
		in := &mtproto.TLChannelsUpdateColor{
			Constructor: mtproto.TLConstructor_CRC32_channels_updateColor_d8aa3671,
			ForProfile:  forProfile, Channel: lookInput(lookHash),
		}
		if color != nil {
			in.Color_FLAGINT32 = wrapperspb.Int32(*color)
		}
		if emoji != 0 {
			in.BackgroundEmojiId = wrapperspb.Int64(emoji)
		}
		ch.color = nil
		_, err := c.ChannelsUpdateColor(in)
		return err
	}
	color := func(v int32) *int32 { return &v }
	for _, k := range []struct {
		name       string
		forProfile bool
		color      *int32
		emoji      int64
		want       error
		sent       int32
	}{
		{"a built-in name color", false, color(3), 0, nil, 3},
		{"a listed name color", false, color(7), lookEmoji, nil, 7},
		{"an unknown name color", false, color(8), 0, errColorInvalid, 0},
		{"a negative color", false, color(-2), 0, errColorInvalid, 0},
		{"no color", false, nil, 0, nil, channelpb.NoColor},
		{"a listed profile color", true, color(15), 0, nil, 15},
		{"a built-in name color for the profile", true, color(3), 0, errColorInvalid, 0},
		{"a pattern of a sticker", false, color(1), lookSticker, mtproto.ErrDocumentInvalid, 0},
		{"a pattern not in the catalog", true, nil, 12345, mtproto.ErrDocumentInvalid, 0},
	} {
		err := update(k.forProfile, k.color, k.emoji)
		if !errors.Is(err, k.want) {
			t.Errorf("%s: %v, want %v", k.name, err, k.want)
			continue
		}
		if k.want != nil {
			if ch.color != nil {
				t.Errorf("%s: sent to biz channel", k.name)
			}
			continue
		}
		if ch.color.GetColor() != k.sent || ch.color.GetBackgroundEmojiId() != k.emoji || mtproto.FromBool(ch.color.GetForProfile()) != k.forProfile ||
			ch.color.GetChannelId() != lookChannel || ch.color.GetSelfId() != 1 || ch.color.GetPermAuthKeyId() != 2 {
			t.Errorf("%s: sent %v", k.name, ch.color)
		}
	}

	if _, err := c.ChannelsUpdateColor(&mtproto.TLChannelsUpdateColor{
		Constructor: mtproto.TLConstructor_CRC32_channels_updateColor_621a201f, Channel: lookInput(lookHash), Color_INT32: 0,
	}); err != nil || ch.color.GetColor() != 0 {
		t.Errorf("the old constructor with color 0: %v, sent %v", err, ch.color)
	}
	if _, err := c.ChannelsUpdateColor(&mtproto.TLChannelsUpdateColor{Channel: lookInput(lookHash + 1)}); !errors.Is(err, mtproto.ErrChannelInvalid) {
		t.Errorf("a wrong access hash: %v", err)
	}
}

// A status is a custom emoji of the catalog not restricted for channels,
// or none; a collectible one is not.
func TestChannelsUpdateEmojiStatus(t *testing.T) {
	c, ch, m := newLookCore(t)
	update := func(s *mtproto.EmojiStatus) error {
		ch.status = nil
		_, err := c.ChannelsUpdateEmojiStatus(&mtproto.TLChannelsUpdateEmojiStatus{Channel: lookInput(lookHash), EmojiStatus: s})
		return err
	}
	if err := update(channelpb.MakeEmojiStatus(lookEmoji, 99)); err != nil || ch.status.GetDocumentId() != lookEmoji || ch.status.GetUntil() != 99 {
		t.Errorf("an emoji until 99: %v, sent %v", err, ch.status)
	}
	if err := update(channelpb.EmptyEmojiStatus()); err != nil || ch.status.GetDocumentId() != 0 || ch.status.GetUntil() != 0 {
		t.Errorf("none: %v, sent %v", err, ch.status)
	}
	for name, s := range map[string]*mtproto.EmojiStatus{
		"a sticker":      channelpb.MakeEmojiStatus(lookSticker, 0),
		"an unknown one": channelpb.MakeEmojiStatus(5, 0),
		"a collectible":  mtproto.MakeTLInputEmojiStatusCollectible(&mtproto.EmojiStatus{CollectibleId: 3}).To_EmojiStatus(),
	} {
		if err := update(s); !errors.Is(err, mtproto.ErrDocumentInvalid) || ch.status != nil {
			t.Errorf("%s: %v, sent %v", name, err, ch.status)
		}
	}
	put(t, m, "account.getChannelRestrictedStatusEmojis", "", 1, mtproto.MakeTLEmojiList(&mtproto.EmojiList{DocumentId: []int64{lookEmoji}}).To_EmojiList())
	if err := update(channelpb.MakeEmojiStatus(lookEmoji, 0)); !errors.Is(err, mtproto.ErrDocumentInvalid) {
		t.Errorf("a restricted emoji: %v", err)
	}
}
