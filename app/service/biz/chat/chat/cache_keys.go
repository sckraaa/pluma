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

package chat

import "fmt"

// The cache keys of a basic group in biz chat's cache, for the services
// that change its rows in a transaction of their own (msg, the move to a
// supergroup, task 047) and drop them after the commit.

// CacheKey is the key of the chat chatId with its participant list.
func CacheKey(chatId int64) string {
	return fmt.Sprintf("chat#%d", chatId)
}

// ParticipantCacheKey is the key of a participant's row of chatId.
func ParticipantCacheKey(chatId, userId int64) string {
	return fmt.Sprintf("chat_participant.3#%d_%d", chatId, userId)
}
