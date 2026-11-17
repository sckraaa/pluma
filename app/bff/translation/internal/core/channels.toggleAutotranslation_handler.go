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
)

// ChannelsToggleAutotranslation
// channels.toggleAutotranslation#167fc0a1 channel:InputChannel enabled:Bool = Updates;
//
// A setting of the channel (task 082, ADR 0050), served by the channels
// area with the other settings of channels. It is a method of the same gRPC
// service as the translation (RPCTranslation), which is registered once,
// here; the call goes on to the channels area with the caller's metadata.
func (c *TranslationCore) ChannelsToggleAutotranslation(in *mtproto.TLChannelsToggleAutotranslation) (*mtproto.Updates, error) {
	return c.svcCtx.Autotranslation.ChannelsToggleAutotranslation(c.ctx, in)
}
