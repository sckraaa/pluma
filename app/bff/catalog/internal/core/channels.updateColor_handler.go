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
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"google.golang.org/grpc/status"
)

// How a channel looks (task 078): the color of its name and of its profile
// and its emoji status, from the colors and the custom emoji of the
// catalog. Boosts do not limit them (ADR 0041): BOOSTS_REQUIRED is never
// answered.

// errColorInvalid is Telegram's answer to a palette it has not
// (core.telegram.org/method/account.updateColor).
var errColorInvalid = status.Error(mtproto.ErrBadRequest, "COLOR_INVALID")

// simpleColors is how many colors of names every client has built in,
// listed by help.getPeerColors or not (tdesktop kSimpleColorIndexCount,
// ui/chat/chat_style.h:54).
const simpleColors = 7

// ChannelsUpdateColor
// channels.updateColor#d8aa3671 flags:# for_profile:flags.1?true channel:InputChannel color:flags.2?int background_emoji_id:flags.0?long = Updates;
// channels.updateColor#621a201f flags:# channel:InputChannel color:int background_emoji_id:flags.0?long = Updates;
//
// The color of a channel's name or, with for_profile, of its profile, with
// a pattern of custom emoji; without color, none, and the clients take the
// color from the id. The color is one of help.getPeerColors (or of
// help.getPeerProfileColors for the profile), COLOR_INVALID otherwise; the
// emoji, 0 for none, one of the catalog, DOCUMENT_INVALID otherwise
// (core.telegram.org/method/account.updateColor answers so). biz channel
// checks the rights and keeps the change. The answer holds updateChannel
// and the channel, which Android and iOS apply (ChannelColorActivity.java:
// 678-682, UpdatePeerInfo.swift:143-158).
func (c *CatalogCore) ChannelsUpdateColor(in *mtproto.TLChannelsUpdateColor) (*mtproto.Updates, error) {
	channelId, err := c.channelOf(in.GetChannel())
	if err != nil {
		return nil, err
	}
	color := channelpb.NoColor
	switch {
	case in.GetColor_FLAGINT32() != nil:
		color = in.GetColor_FLAGINT32().GetValue()
	case in.GetConstructor() == mtproto.TLConstructor_CRC32_channels_updateColor_621a201f:
		color = in.GetColor_INT32()
	}
	if color != channelpb.NoColor {
		ok, err := c.colorListed(in.GetForProfile(), color)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errColorInvalid
		}
	}
	emojiId := in.GetBackgroundEmojiId().GetValue()
	if err = c.checkCustomEmoji(emojiId); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelUpdateColor(c.ctx, &channelpb.TLChannelUpdateColor{
		SelfId:            c.MD.UserId,
		PermAuthKeyId:     c.MD.PermAuthKeyId,
		ChannelId:         channelId,
		ForProfile:        mtproto.ToBool(in.GetForProfile()),
		Color:             color,
		BackgroundEmojiId: emojiId,
	})
	if err != nil {
		c.Logger.Errorf("channels.updateColor - error: %v", err)
		return nil, err
	}
	return r, nil
}

// channelOf is the id of the channel in, CHANNEL_INVALID for another input
// or a wrong access hash.
func (c *CatalogCore) channelOf(in *mtproto.InputChannel) (int64, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputChannel || in.GetChannelId() == 0 {
		return 0, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.GetChannelId(), Id: []int64{c.MD.UserId}})
	if err != nil {
		return 0, err
	}
	if mc.GetChannel().GetAccessHash() != in.GetAccessHash() {
		return 0, mtproto.ErrChannelInvalid
	}
	return in.GetChannelId(), nil
}

// colorListed tells whether color is a color of names (or with forProfile
// of profiles) the clients know: one built in, for names, or one of the
// catalog's list.
func (c *CatalogCore) colorListed(forProfile bool, color int32) (bool, error) {
	if color < 0 {
		return false, nil
	}
	method := "help.getPeerColors"
	if forProfile {
		method = "help.getPeerProfileColors"
	} else if color < simpleColors {
		return true, nil
	}
	list := &mtproto.Help_PeerColors{}
	_, found, err := c.stored(method, "", list)
	if err != nil || !found {
		return false, err
	}
	for _, o := range list.GetColors() {
		if o.GetColorId() == color {
			return true, nil
		}
	}
	return false, nil
}

// checkCustomEmoji checks that id, unless 0, is a custom emoji of the
// catalog, whose documents alone the clients can load
// (messages.getCustomEmojiDocuments): DOCUMENT_INVALID otherwise.
func (c *CatalogCore) checkCustomEmoji(id int64) error {
	if id == 0 {
		return nil
	}
	known, err := c.svcCtx.Catalog.CatalogDocuments(c.ctx, []int64{id})
	if err != nil {
		c.Logger.Errorf("custom emoji %d - the catalog: %v", id, err)
		return err
	}
	if len(known) == 0 {
		return mtproto.ErrDocumentInvalid
	}
	docs, err := c.svcCtx.Media.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: known})
	if err != nil {
		c.Logger.Errorf("custom emoji %d - media: %v", id, err)
		return err
	}
	for _, d := range docs.GetDatas() {
		if d.GetId() != id || d.GetPredicateName() != mtproto.Predicate_document {
			continue
		}
		for _, a := range d.GetAttributes() {
			if a.GetPredicateName() == mtproto.Predicate_documentAttributeCustomEmoji {
				return nil
			}
		}
	}
	return mtproto.ErrDocumentInvalid
}
