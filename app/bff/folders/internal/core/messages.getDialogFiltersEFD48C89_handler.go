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

// MessagesGetDialogFiltersEFD48C89
// messages.getDialogFilters#efd48c89 = messages.DialogFilters;
//
// The folders with "All chats" in its place; folder tags are on only for a
// user with Premium who turned them on.
func (c *FoldersCore) MessagesGetDialogFiltersEFD48C89(in *mtproto.TLMessagesGetDialogFiltersEFD48C89) (*mtproto.Messages_DialogFilters, error) {
	filters, tags, err := c.dialogFilters()
	if err != nil {
		c.Logger.Errorf("messages.getDialogFilters - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesDialogFilters(&mtproto.Messages_DialogFilters{
		TagsEnabled: tags,
		Filters:     filters,
	}).To_Messages_DialogFilters(), nil
}
