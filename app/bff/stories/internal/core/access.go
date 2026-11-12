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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// peerView is whose stories a request is about, as the caller sees that
// peer: its own account, another user (with what that user's stories need
// to know of the caller) or a channel the caller may read.
type peerView struct {
	peer stories.Peer
	// own: the caller's own account.
	own bool
	// owner is the other user, with its contact row of the caller.
	owner *mtproto.ImmutableUser
	// blocked: the owner blocked the caller, or hides its stories from it
	// (the blocklist of stories, contacts.block with my_stories_from).
	blocked bool
	// channel is the channel with the caller's row, me that row (nil for
	// one who never joined a public channel).
	channel *channelpb.MutableChannel
	me      *channelpb.ImmutableChannelParticipant
}

// view resolves the peer of a request. A user that does not exist is
// PEER_ID_INVALID, a channel the caller may not read CHANNEL_PRIVATE.
func (c *StoriesCore) view(in *mtproto.InputPeer) (*peerView, error) {
	if in == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch p.PeerType {
	case mtproto.PEER_SELF:
		return c.viewOf(stories.UserPeer(c.MD.UserId))
	case mtproto.PEER_USER:
		return c.viewOf(stories.UserPeer(p.PeerId))
	case mtproto.PEER_CHANNEL:
		return c.viewOf(stories.ChannelPeer(p.PeerId))
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// viewOf is view for a peer already read.
func (c *StoriesCore) viewOf(peer stories.Peer) (*peerView, error) {
	v := &peerView{peer: peer}
	if !peer.IsUser() {
		mc, err := c.svcCtx.Dao.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{
			ChannelId: peer.Id,
			Id:        []int64{c.MD.UserId},
		})
		if err != nil {
			return nil, err
		}
		if mc.ToChat(c.MD.UserId, now()).GetPredicateName() != mtproto.Predicate_channel {
			return nil, mtproto.ErrChannelPrivate
		}
		v.channel = mc
		v.me, _ = mc.GetParticipant(c.MD.UserId)
		return v, nil
	}
	if peer.Id == c.MD.UserId {
		v.own = true
		return v, nil
	}
	owner, err := c.svcCtx.Dao.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id:       peer.Id,
		Contacts: []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	if owner == nil || owner.Deleted() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	if v.blocked, err = c.shutOutBy(peer.Id); err != nil {
		return nil, err
	}
	v.owner = owner
	return v, nil
}

// shutOutBy tells whether the user ownerId shows the caller none of its
// stories: it blocked the caller, or hides its stories from it.
func (c *StoriesCore) shutOutBy(ownerId int64) (bool, error) {
	blocked, err := c.svcCtx.Dao.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{UserId: ownerId, PeerUserId: c.MD.UserId})
	if err != nil {
		return false, err
	}
	if mtproto.FromBool(blocked) {
		return true, nil
	}
	return c.HidesStoriesFrom(ownerId, c.MD.UserId)
}

// admin tells whether the caller holds the admin right of the channel: its
// creator holds every right.
func (v *peerView) admin(right int32) bool {
	if v.channel == nil || !v.me.Member() || v.channel.GetChannel().GetBlocked() {
		return false
	}
	switch v.me.GetRole() {
	case channelpb.RoleCreator:
		return true
	case channelpb.RoleAdmin:
		return v.me.GetAdminRights()&right != 0
	}
	return false
}

// canPost: the account posts its own stories, and a channel's are posted by
// its admins with post_stories (chatAdminRights of the schema). Bots post
// none.
func (v *peerView) canPost() bool {
	if v.peer.IsUser() {
		return v.own
	}
	return v.admin(mtproto.ADMIN_POST_STORIES)
}

// canEdit: one's own story with the right to post, another admin's with
// edit_stories.
func (v *peerView) canEdit(st *stories.Story, caller int64) bool {
	if v.peer.IsUser() {
		return v.own
	}
	if st.SenderId == caller && v.admin(mtproto.ADMIN_POST_STORIES) {
		return true
	}
	return v.admin(mtproto.ADMIN_EDIT_STORIES)
}

// canDelete: one's own story with the right to post, another admin's with
// delete_stories.
func (v *peerView) canDelete(st *stories.Story, caller int64) bool {
	if v.peer.IsUser() {
		return v.own
	}
	if st.SenderId == caller && v.admin(mtproto.ADMIN_POST_STORIES) {
		return true
	}
	return v.admin(mtproto.ADMIN_DELETE_STORIES)
}

// manages: the caller sees the archive of the peer and arranges its
// profile: the account itself, and for a channel any admin who may post,
// edit or delete stories.
func (v *peerView) manages() bool {
	if v.peer.IsUser() {
		return v.own
	}
	return v.admin(mtproto.ADMIN_POST_STORIES | mtproto.ADMIN_EDIT_STORIES | mtproto.ADMIN_DELETE_STORIES)
}

// visible keeps the stories of list the caller may open: all of its own
// and of a channel it reads, and of another user those whose rules let it
// in. A user who blocked the caller, or hides its stories from it, shows
// none.
func (c *StoriesCore) visible(v *peerView, list []*stories.Story) ([]*stories.Story, error) {
	if v.own || !v.peer.IsUser() {
		return list, nil
	}
	if v.blocked || c.MD.IsBot {
		return nil, nil
	}
	viewer := v.owner.StoryViewerOf(c.MD.UserId, false)
	out := make([]*stories.Story, 0, len(list))
	for _, st := range list {
		rules, err := c.resolveChats(st.Privacy, []int64{c.MD.UserId})
		if err != nil {
			return nil, err
		}
		if mtproto.EvaluateStoryPrivacy(rules, viewer) {
			out = append(out, st)
		}
	}
	return out, nil
}

// one returns the story id of the peer if the caller may open it, else
// STORY_ID_INVALID.
func (c *StoriesCore) one(v *peerView, id int32) (*stories.Story, error) {
	st, err := c.svcCtx.Dao.Store.One(c.ctx, v.peer, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errStoryIdInvalid
	}
	list, err := c.visible(v, []*stories.Story{st})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errStoryIdInvalid
	}
	return st, nil
}

// shown keeps of list what the caller is shown of the peer outside the
// feeds: its own account and the admins of a channel see everything, the
// others the stories in the feeds and those of the profile.
func (c *StoriesCore) shown(v *peerView, list []*stories.Story) ([]*stories.Story, error) {
	list, err := c.visible(v, list)
	if err != nil || v.manages() {
		return list, err
	}
	at := now()
	out := make([]*stories.Story, 0, len(list))
	for _, st := range list {
		if st.Active(at) || st.Pinned {
			out = append(out, st)
		}
	}
	return out, nil
}

// resolveChats resolves the chats the rules of a story name for the users
// viewers (stories.ResolveChats); rules that name none are read as they
// are, without a query.
func (c *StoriesCore) resolveChats(rules []*mtproto.PrivacyRule, viewers []int64) ([]*mtproto.PrivacyRule, error) {
	if len(stories.ChatsOf(rules)) == 0 {
		return rules, nil
	}
	return stories.ResolveChats(c.ctx, c.svcCtx.Dao.DB, rules, viewers)
}
