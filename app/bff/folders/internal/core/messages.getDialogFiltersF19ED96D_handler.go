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

// MessagesGetDialogFiltersF19ED96D
// messages.getDialogFilters#f19ed96d = Vector<DialogFilter>;
//
// The constructor of older clients: the same folders as a vector.
func (c *FoldersCore) MessagesGetDialogFiltersF19ED96D(in *mtproto.TLMessagesGetDialogFiltersF19ED96D) (*mtproto.Vector_DialogFilter, error) {
	filters, _, err := c.dialogFilters()
	if err != nil {
		c.Logger.Errorf("messages.getDialogFilters - error: %v", err)
		return nil, err
	}
	return &mtproto.Vector_DialogFilter{Datas: filters}, nil
}
