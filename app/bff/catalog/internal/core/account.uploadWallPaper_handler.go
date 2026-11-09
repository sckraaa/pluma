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
	"crypto/rand"
	"encoding/binary"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appearance"
)

// AccountUploadWallPaper
// account.uploadWallPaper#e39a8f03 flags:# for_chat:flags.0?true file:InputFile mime_type:string settings:WallPaperSettings = WallPaper;
//
// A wallpaper from an image the client uploaded. Only a JPEG or a PNG:
// clients send "image/jpeg" (tdesktop background_preview_box.cpp:632, Android
// MessagesController uploadWallPaper). A WebP would come back to tdesktop as a
// sticker instead of a wallpaper (checkWallPaperProperties needs FileDocument).
// tweb sends the type of the chosen file, so a PNG is a picture here too; a
// pattern wallpaper needs a TGV (a gzipped SVG) or a PNG meant as one, which no
// client uploads and dfs cannot tell apart (core.telegram.org/api/wallpapers).
//
// A wallpaper not for one chat joins the uploader's list: tdesktop never
// saves one after the upload (window_theme.cpp:660) and expects it back.
func (c *CatalogCore) AccountUploadWallPaper(in *mtproto.TLAccountUploadWallPaper) (*mtproto.WallPaper, error) {
	mime := in.GetMimeType()
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, mtproto.ErrWallpaperMimeInvalid
	}
	document, err := c.uploadWallpaperFile(in.GetFile(), mime)
	if err != nil {
		c.Logger.Errorf("account.uploadWallPaper - error: %v", err)
		return nil, err
	}
	id, err := appearance.NewID()
	if err != nil {
		return nil, err
	}
	var hash [8]byte
	if _, err = rand.Read(hash[:]); err != nil {
		return nil, err
	}
	slug, err := appearance.NewSlug()
	if err != nil {
		return nil, err
	}
	w := mtproto.MakeTLWallPaper(&mtproto.WallPaper{
		Id:         id,
		AccessHash: int64(binary.BigEndian.Uint64(hash[:])),
		Slug:       slug,
		Document:   document,
		Settings:   in.GetSettings(),
	}).To_WallPaper()
	if err = c.svcCtx.Appearance.AddWallpaper(c.ctx, c.MD.UserId, in.GetForChat(), w); err != nil {
		c.Logger.Errorf("account.uploadWallPaper - error: %v", err)
		return nil, err
	}
	// The list is the uploader's alone, so its entry says who made it.
	w.Creator = true
	if !in.GetForChat() {
		if err = c.svcCtx.Appearance.SaveWallpaper(c.ctx, c.MD.UserId, w); err != nil {
			c.Logger.Errorf("account.uploadWallPaper - error: %v", err)
			return nil, err
		}
	}
	return w, nil
}
