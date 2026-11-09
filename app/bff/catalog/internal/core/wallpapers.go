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
	"errors"
	"hash/crc32"
	"math/rand"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// errWallpaperNotFound is for a revert or a message that names no wallpaper
// (core.telegram.org/method/messages.setChatWallPaper); iOS takes it as "the
// chat has none" (_internal_revertChatWallpaper).
var errWallpaperNotFound = status.Error(mtproto.ErrBadRequest, "WALLPAPER_NOT_FOUND")

// resolveUploaded finds a wallpaper uploaded to this server by slug or id
// (with its access hash), with the viewer's creator flag; nil if there is none.
func (c *CatalogCore) resolveUploaded(ask *mtproto.InputWallPaper) (*mtproto.WallPaper, error) {
	var (
		w       *mtproto.WallPaper
		creator int64
		err     error
	)
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputWallPaperSlug:
		w, creator, err = c.svcCtx.Appearance.WallpaperBySlug(c.ctx, ask.GetSlug())
	case mtproto.Predicate_inputWallPaper:
		w, creator, err = c.svcCtx.Appearance.WallpaperByID(c.ctx, ask.GetId())
		if err == nil && w != nil && w.GetAccessHash() != ask.GetAccessHash() {
			return nil, nil
		}
	default:
		return nil, nil
	}
	if err != nil || w == nil {
		return nil, err
	}
	shown := proto.Clone(w).(*mtproto.WallPaper)
	shown.Creator = creator == c.MD.UserId
	return shown, nil
}

// hasCatalogWallpaper reports whether the wallpaper is one of the catalog's;
// a preinstalled wallpaper removed from a list must stay removed.
func (c *CatalogCore) hasCatalogWallpaper(id int64) (bool, error) {
	list := &mtproto.Account_WallPapers{}
	_, found, err := c.stored("account.getWallPapers", "", list)
	if err != nil || !found {
		return false, err
	}
	for _, w := range list.GetWallpapers() {
		if w.GetId() == id {
			return true, nil
		}
	}
	return false, nil
}

func withSettings(w *mtproto.WallPaper, settings *mtproto.WallPaperSettings) *mtproto.WallPaper {
	shown := proto.Clone(w).(*mtproto.WallPaper)
	if settings != nil {
		shown.Settings = settings
	}
	return shown
}

// resolveFill is the wallpaper of an inputWallPaperNoFile: a fill the client
// made, with its id and the settings it sent (core.telegram.org/api/wallpapers;
// a channel's emoticon wallpaper has id 0, Android ChannelColorActivity), or
// one the catalog has under that id.
func (c *CatalogCore) resolveFill(ask *mtproto.InputWallPaper, settings *mtproto.WallPaperSettings) (*mtproto.WallPaper, error) {
	w, err := c.wallPaper(ask)
	switch {
	case err == nil:
		return withSettings(w, settings), nil
	case !errors.Is(err, mtproto.ErrWallpaperInvalid):
		return nil, err
	case settings == nil:
		return nil, mtproto.ErrWallpaperInvalid
	}
	return mtproto.MakeTLWallPaperNoFile(&mtproto.WallPaper{Id: ask.GetId(), Settings: settings}).To_WallPaper(), nil
}

// wallpaperForChat is what messages.setChatWallPaper puts in the chat: the
// wallpaper asked for with the settings given. creator is the uploader's
// alone and is not told to the others.
func (c *CatalogCore) wallpaperForChat(ask *mtproto.InputWallPaper, settings *mtproto.WallPaperSettings) (*mtproto.WallPaper, error) {
	var (
		w   *mtproto.WallPaper
		err error
	)
	if ask.GetPredicateName() == mtproto.Predicate_inputWallPaperNoFile {
		w, err = c.resolveFill(ask, settings)
	} else {
		w, err = c.wallPaper(ask)
		if err == nil {
			w = withSettings(w, settings)
		}
	}
	if err != nil {
		return nil, err
	}
	w.Creator = false
	return w, nil
}

// wallpaperFromMessage is the wallpaper of a messageActionSetChatWallPaper of
// the chat with the other user, applied with same (the other side's
// "apply" button: tdesktop setExistingForPeer, Android setWallpaperToPeer).
func (c *CatalogCore) wallpaperFromMessage(peerID int64, msgID int32, settings *mtproto.WallPaperSettings) (*mtproto.WallPaper, error) {
	box, err := c.svcCtx.Message.MessageGetUserMessage(c.ctx, &messagepb.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgID})
	if err != nil {
		return nil, errWallpaperNotFound
	}
	m := box.GetMessage()
	if box.GetPeerType() != mtproto.PEER_USER || box.GetPeerId() != peerID || m == nil ||
		m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionSetChatWallPaper || m.GetAction().GetWallpaper() == nil {
		return nil, errWallpaperNotFound
	}
	return withSettings(m.GetAction().GetWallpaper(), settings), nil
}

func wallpaperAction(w *mtproto.WallPaper, same, forBoth bool) *mtproto.MessageAction {
	return mtproto.MakeTLMessageActionSetChatWallPaper(&mtproto.MessageAction{
		Same:      same,
		ForBoth:   forBoth,
		Wallpaper: w,
	}).To_MessageAction()
}

// sendPrivateService writes the service message of a private chat, the way
// messages.setHistoryTTL sends its own (msg applies its usual checks: a
// blocked side gets the usual error).
func (c *CatalogCore) sendPrivateService(peer *mtproto.PeerUtil, action *mtproto.MessageAction) (*mtproto.Updates, error) {
	return c.svcCtx.Msg.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  rand.Int63(), //nolint:gosec // G404: a random_id is unique, not secret
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: peer.ToPeer(),
				Date:   int32(time.Now().Unix()),
				Action: action,
			}).To_Message(),
		}).To_OutboxMessage()},
	})
}

func updatesWith(u *mtproto.Update) *mtproto.Updates {
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{u},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates()
}

// setPrivateWallpaper serves messages.setChatWallPaper in a private chat.
//
// The service message goes first: it is the step that can refuse (blocked
// peers), and a blocked side must not get its wallpaper changed; the wallpaper
// of each side follows, one box each, idempotent on a retry. The sides are
// told by the updates the box of each emits, which both getDifference and a
// connected device receive.
func (c *CatalogCore) setPrivateWallpaper(peer *mtproto.PeerUtil, in *mtproto.TLMessagesSetChatWallPaper) (*mtproto.Updates, error) {
	if in.GetRevert() {
		return c.revertChatWallpaper(peer)
	}
	if in.GetWallpaper() == nil && in.GetId() == nil {
		// Resetting the chat's wallpaper is only the user's own side
		// (tdesktop BackgroundBox::resetForPeer, Android clearWallpaper).
		if err := c.svcCtx.Appearance.SetChatWallpaper(c.ctx, c.MD.UserId, peer.PeerId, nil); err != nil {
			c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
			return nil, err
		}
		return updatesWith(appearance.PeerWallpaperUpdate(mtproto.MakePeerUser(peer.PeerId), nil, false)), nil
	}

	var (
		w    *mtproto.WallPaper
		same bool
		err  error
	)
	if id := in.GetId(); id != nil {
		same = true
		w, err = c.wallpaperFromMessage(peer.PeerId, id.GetValue(), in.GetSettings())
	} else {
		w, err = c.wallpaperForChat(in.GetWallpaper(), in.GetSettings())
	}
	if err != nil {
		return nil, err
	}
	forBoth := in.GetForBoth() && !same
	if forBoth {
		if err = c.checkPremium(); err != nil {
			return nil, err
		}
	}

	r, err := c.sendPrivateService(peer, wallpaperAction(w, same, forBoth))
	if err != nil {
		c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Appearance.SetChatWallpaper(c.ctx, c.MD.UserId, peer.PeerId, w); err != nil {
		c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
		return nil, err
	}
	if forBoth {
		if err = c.svcCtx.Appearance.OverrideChatWallpaper(c.ctx, peer.PeerId, c.MD.UserId, w); err != nil {
			c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
			return nil, err
		}
	}
	r.Updates = append(r.Updates, appearance.PeerWallpaperUpdate(mtproto.MakePeerUser(peer.PeerId), w, false))
	return r, nil
}

func (c *CatalogCore) revertChatWallpaper(peer *mtproto.PeerUtil) (*mtproto.Updates, error) {
	w, err := c.svcCtx.Appearance.RevertChatWallpaper(c.ctx, c.MD.UserId, peer.PeerId)
	if errors.Is(err, appearance.ErrNotOverridden) {
		return nil, errWallpaperNotFound
	}
	if err != nil {
		c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
		return nil, err
	}
	return updatesWith(appearance.PeerWallpaperUpdate(mtproto.MakePeerUser(peer.PeerId), w, false)), nil
}

// checkPremium is the condition of for_both, which docs name "for Premium
// users" (core.telegram.org/method/messages.setChatWallPaper); Android's
// button is locked the same (ThemePreviewActivity.java:2459).
func (c *CatalogCore) checkPremium() error {
	me, err := c.svcCtx.User.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return err
	}
	if !me.Premium() {
		return mtproto.ErrPremiumAccountRequired
	}
	return nil
}

// setChannelWallpaper serves messages.setChatWallPaper in a channel or a
// supergroup: biz channel checks the right to change its info and records
// the wallpaper; for_both means nothing there (core.telegram.org/api/wallpapers).
func (c *CatalogCore) setChannelWallpaper(channelID int64, in *mtproto.TLMessagesSetChatWallPaper) (*mtproto.Updates, error) {
	var (
		w   *mtproto.WallPaper
		err error
	)
	if in.GetWallpaper() != nil {
		if w, err = c.wallpaperForChat(in.GetWallpaper(), in.GetSettings()); err != nil {
			return nil, err
		}
	}
	r, err := c.svcCtx.Channel.ChannelSetChatWallpaper(c.ctx, &channelpb.TLChannelSetChatWallpaper{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     channelID,
		Wallpaper:     w,
	})
	if err != nil {
		c.Logger.Errorf("messages.setChatWallPaper - error: %v", err)
		return nil, err
	}
	return r, nil
}

// wallpaperListHash is the hash of an account's own list on top of the
// catalog's: it must move when the list or the settings in it do.
func wallpaperListHash(catalogHash int64, saved []appearance.SavedWallpaper) int64 {
	h := catalogHash
	for _, e := range saved {
		h = appearance.CalcHash(h, e.WallpaperID)
		h = appearance.CalcHash(h, int64(crc32.ChecksumIEEE(e.Data)))
		if e.Removed {
			h = appearance.CalcHash(h, 1)
		}
	}
	return h
}

// uploadWallpaperFile stores the image of a wallpaper with its thumbnails.
func (c *CatalogCore) uploadWallpaperFile(file *mtproto.InputFile, mimeType string) (*mtproto.Document, error) {
	return c.svcCtx.Media.MediaUploadWallPaperFile(c.ctx, &media.TLMediaUploadWallPaperFile{
		OwnerId:  c.MD.UserId,
		File:     file,
		MimeType: mimeType,
		Admin:    mtproto.BoolFalse,
	})
}
