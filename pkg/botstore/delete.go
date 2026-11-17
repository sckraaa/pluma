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

package botstore

import (
	"context"
	"fmt"
)

// userTables are the tables of the bot platform with rows of one user
// (user_id): the sites the user logged in to and the bots allowed to write
// to it (web_authorizations, bot_write_allowed), the mini apps it opened,
// the attachment menus it installed and the messages bots prepared for it.
// game_scores is not among them: a score is a line of the high score table
// of a chat, which the other players of the chat see, like the messages a
// deleted account sent there.
var userTables = []string{
	"web_authorizations",
	"bot_write_allowed",
	"bot_app_launches",
	"bot_attach_menu_installs",
	"bot_prepared",
}

// DeleteUser removes the rows of userID, whose account is deleted. Each
// statement can run again, so a deletion that failed half way is repeated
// whole.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	for _, table := range userTables {
		if _, err := s.db.Exec(ctx, "DELETE FROM "+table+" WHERE user_id = ?", userID); err != nil {
			return fmt.Errorf("botstore: delete the rows of user %d from %s: %w", userID, table, err)
		}
	}
	return nil
}
