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
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// The most UTF-16 code units of a caption: caption_length_limit_default and
// _premium of help.getAppConfig (core.telegram.org/api/config; clients
// count UTF-16 code units).
const (
	captionLimit        = 1024
	captionLimitPremium = 4096
)

// checkCaption answers MEDIA_CAPTION_TOO_LONG for a caption over the
// caller's limit; only a caption past the free limit costs a read of the
// user.
func (c *MessagesCore) checkCaption(caption string) error {
	n := len(utf16.Encode([]rune(caption)))
	if n <= captionLimit {
		return nil
	}
	if n > captionLimitPremium {
		return mtproto.ErrMediaCaptionTooLong
	}
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return err
	}
	if !me.Premium() {
		return mtproto.ErrMediaCaptionTooLong
	}
	return nil
}
