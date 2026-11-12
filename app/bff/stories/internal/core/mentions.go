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
	"encoding/binary"
	"hash/fnv"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// mentionRandomId is the random_id of the message that tells user about
// the story: the same for every try, so a repeated send stores one
// message.
func mentionRandomId(st *stories.Story, user int64) int64 {
	h := fnv.New64a()
	var b [28]byte
	binary.BigEndian.PutUint32(b[0:], uint32(st.Peer.Type))
	binary.BigEndian.PutUint64(b[4:], uint64(st.Peer.Id))
	binary.BigEndian.PutUint32(b[12:], uint32(st.Id))
	binary.BigEndian.PutUint64(b[16:], uint64(user))
	binary.BigEndian.PutUint32(b[24:], 0x73746f72) // "stor"
	_, _ = h.Write(b[:])
	id := int64(h.Sum64() >> 1)
	if id == 0 {
		id = 1
	}
	return id
}

// tellMentions sends each user the story of a user mentions a message from
// its author with the story (messageMediaStory with via_mention), which
// the clients show as a mention in a story (Android MessageObject.java:6950,
// TYPE_STORY_MENTION). Only those who may open the
// story are told. When every message is sent the story is marked done;
// until then the worker tries again.
func (c *StoriesCore) tellMentions(st *stories.Story) {
	if len(st.Mentions) == 0 || !st.Peer.IsUser() {
		return
	}
	if err := c.sendMentions(st); err != nil {
		c.Logger.Errorf("stories: mentions of story %d of user %d: %v", st.Id, st.Peer.Id, err)
		return
	}
	if err := c.svcCtx.Dao.Store.SetMentionsPending(c.ctx, st.Peer, st.Id, false); err != nil {
		c.Logger.Errorf("stories: mentions of story %d of user %d: %v", st.Id, st.Peer.Id, err)
	}
}

func (c *StoriesCore) sendMentions(st *stories.Story) error {
	owner := st.Peer.Id
	all := append([]int64{owner}, st.Mentions...)
	users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: all, To: all})
	if err != nil {
		return err
	}
	ownerUser, ok := users.GetImmutableUser(owner)
	if !ok {
		return nil
	}
	rules, err := c.resolveChats(st.Privacy, st.Mentions)
	if err != nil {
		return err
	}
	blocked, err := c.svcCtx.Dao.UserCheckBlockUserList(c.ctx, &userpb.TLUserCheckBlockUserList{UserId: owner, Id: st.Mentions})
	if err != nil {
		return err
	}
	hiddenFrom, err := stories.BlockedOf(c.ctx, c.svcCtx.Dao.DB, owner, st.Mentions)
	if err != nil {
		return err
	}
	for _, id := range st.Mentions {
		u, ok := users.GetImmutableUser(id)
		if !ok || u.Deleted() || u.IsBot() || containsId(blocked.GetDatas(), id) || hiddenFrom[id] {
			continue
		}
		if !mtproto.EvaluateStoryPrivacy(rules, ownerUser.StoryViewerOf(id, false)) {
			continue
		}
		media := mtproto.MakeTLMessageMediaStory(&mtproto.MessageMedia{
			ViaMention: true,
			Peer:       st.Peer.ToPeer(),
			UserId:     owner,
			Id:         st.Id,
		}).To_MessageMedia()
		out := msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  mentionRandomId(st, id),
			Message: mtproto.MakeTLMessage(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(owner),
				PeerId: mtproto.MakePeerUser(id),
				Date:   now(),
				Media:  media,
			}).To_Message(),
		}).To_OutboxMessage()
		if _, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
			UserId:   owner,
			PeerType: mtproto.PEER_USER,
			PeerId:   id,
			Message:  []*msgpb.OutboxMessage{out},
		}); err != nil {
			if refused(err) {
				// The mentioned user takes no messages from the author
				// (its privacy, a restriction of the author): not told.
				continue
			}
			return err
		}
	}
	return nil
}
