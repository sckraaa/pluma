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
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesGetChatsToSend
// stories.getChatsToSend#a56a8b60 = messages.Chats;
//
// The channels and supergroups the caller may post stories to: the ones it
// created or is an admin of with post_stories (Android offers them as the
// author of a new story, StoriesController.java:4725-4745).
func (c *StoriesCore) StoriesGetChatsToSend(in *mtproto.TLStoriesGetChatsToSend) (*mtproto.Messages_Chats, error) {
	chats := []*mtproto.Chat{}
	if !c.MD.IsBot {
		ids, err := stories.PostingChannels(c.ctx, c.svcCtx.Dao.DB, c.MD.UserId)
		if err != nil {
			c.Logger.Errorf("stories.getChatsToSend - error: %v", err)
			return nil, err
		}
		r := &refs{}
		r.channel(ids...)
		if _, chats, err = c.resolve(r); err != nil {
			c.Logger.Errorf("stories.getChatsToSend - error: %v", err)
			return nil, err
		}
		// A channel the owner of the server blocked comes as restricted
		// or forbidden: nothing is posted there.
		kept := chats[:0]
		for _, chat := range chats {
			if chat.GetPredicateName() == mtproto.Predicate_channel && !chat.GetRestricted() {
				kept = append(kept, chat)
			}
		}
		chats = kept
	}
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{
		Chats: chats,
	}).To_Messages_Chats(), nil
}
