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

// MessagesGetSuggestedDialogFilters
// messages.getSuggestedDialogFilters#a29cd42c = Vector<DialogFilterSuggested>;
//
// No suggestions: Telegram's come with a title and a description in the
// server's language, which the clients show as they are, and Pluma does
// not localize them. The clients then offer no recommended folders and
// create folders as usual.
func (c *FoldersCore) MessagesGetSuggestedDialogFilters(in *mtproto.TLMessagesGetSuggestedDialogFilters) (*mtproto.Vector_DialogFilterSuggested, error) {
	return &mtproto.Vector_DialogFilterSuggested{
		Datas: []*mtproto.DialogFilterSuggested{},
	}, nil
}
