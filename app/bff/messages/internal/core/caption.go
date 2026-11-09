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
)

// captionLimit is the most UTF-16 code units of a caption: the Premium limit
// (caption_length_limit_premium of help.getAppConfig, core.telegram.org/api/config;
// clients count UTF-16 code units), which everybody has (ADR 0041).
const captionLimit = 4096

// checkCaption answers MEDIA_CAPTION_TOO_LONG for a caption over the limit.
func (c *MessagesCore) checkCaption(caption string) error {
	if len(utf16.Encode([]rune(caption))) > captionLimit {
		return mtproto.ErrMediaCaptionTooLong
	}
	return nil
}
