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

package business

import (
	"context"
	"fmt"
)

// userTables are the tables with rows of one account (user_id), in the
// order DeleteUser empties them: the auto replies waiting go first, so that
// none is sent from a half-deleted account, the paused chats before the
// connection they belong to, and the quick replies (written by msg, sent
// by the auto replies) before their shortcuts.
var userTables = []string{
	"business_auto_replies",
	"business_auto_reply_settings",
	"business_auto_reply_log",
	"business_bot_paused_peers",
	"business_bot_connections",
	"business_chat_links",
	"business_profiles",
	"quick_reply_messages",
	"quick_reply_shortcuts",
}

// DeleteUser removes the rows of userID, whose account is deleted: its
// business profile (with its address and location), its auto replies, its
// chat links (their slugs are free again), its connected bot, which no
// longer acts for it, and its quick replies. Each statement can run again,
// so a deletion that failed half way is repeated whole.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	for _, table := range userTables {
		if _, err := s.db.Exec(ctx, "DELETE FROM "+table+" WHERE user_id = ?", userID); err != nil {
			return fmt.Errorf("business: delete the rows of user %d from %s: %w", userID, table, err)
		}
	}
	return nil
}
