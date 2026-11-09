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
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// AccountUploadTheme
// account.uploadTheme#1c3db333 flags:# file:InputFile thumb:flags.0?InputFile file_name:string mime_type:string = Document;
//
// Stores a theme's file for account.createTheme or updateTheme; the MIME type
// names the format (application/x-tgtheme-android: Android
// MessagesController.java:6247, tdesktop window_theme_editor_box.cpp:587).
func (c *CatalogCore) AccountUploadTheme(in *mtproto.TLAccountUploadTheme) (*mtproto.Document, error) {
	if err := checkThemeMime(in.GetMimeType()); err != nil {
		return nil, err
	}
	if in.GetFile() == nil {
		return nil, mtproto.ErrThemeFileInvalid
	}
	d, err := c.svcCtx.Media.MediaUploadThemeFile(c.ctx, &media.TLMediaUploadThemeFile{
		OwnerId:  c.MD.UserId,
		File:     in.GetFile(),
		Thumb:    in.GetThumb(),
		MimeType: in.GetMimeType(),
		FileName: in.GetFileName(),
	})
	if err != nil {
		c.Logger.Errorf("account.uploadTheme - error: %v", err)
		return nil, err
	}
	return d, nil
}
