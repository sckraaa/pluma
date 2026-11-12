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

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// errStoryIdInvalid answers a story that cannot be put into a message
// where the stories are not served.
var errStoryIdInvalid = status.Error(mtproto.ErrBadRequest, "STORY_ID_INVALID")

// storyPeer reads the peer of a story a message refers to: a user or a
// channel.
func (c *MessagesCore) storyPeer(in *mtproto.InputPeer) (*mtproto.PeerUtil, error) {
	if in == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch p.PeerType {
	case mtproto.PEER_SELF:
		return mtproto.MakeUserPeerUtil(c.MD.UserId), nil
	case mtproto.PEER_USER, mtproto.PEER_CHANNEL:
		return p, nil
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// storyMedia makes messageMediaStory of inputMediaStory: the sender must be
// able to open the story, and the story must allow sharing.
func (c *MessagesCore) storyMedia(media *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	if c.svcCtx.Stories == nil {
		return nil, mtproto.ErrMediaInvalid
	}
	peer, err := c.storyPeer(media.GetPeer())
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Stories.CheckStory(c.ctx, c.MD.UserId, peer, media.GetId_INT32(), true); err != nil {
		return nil, err
	}
	out := &mtproto.MessageMedia{Peer: peer.ToPeer(), Id: media.GetId_INT32()}
	if peer.PeerType == mtproto.PEER_USER {
		out.UserId = peer.PeerId
	}
	return mtproto.MakeTLMessageMediaStory(out).To_MessageMedia(), nil
}

// storyShared counts the message with randomId just sent with media, when
// it shares a story: only a message that was sent counts, once. The
// message is sent either way, so a failure is logged.
func (c *MessagesCore) storyShared(media *mtproto.InputMedia, randomId int64) {
	if media.GetPredicateName() != mtproto.Predicate_inputMediaStory || c.svcCtx.Stories == nil {
		return
	}
	peer, err := c.storyPeer(media.GetPeer())
	if err == nil {
		err = c.svcCtx.Stories.StoryShared(c.ctx, c.MD.UserId, peer, media.GetId_INT32(), randomId)
	}
	if err != nil {
		c.Logger.Errorf("messages: share of story %d: %v", media.GetId_INT32(), err)
	}
}

// storyReplyHeader is the header of a message that replies to a story
// (inputReplyToStory), nil for any other message: the sender must be able
// to open the story. The clients name the story by its peer and id
// (Android SendMessagesHelper.java:168-173), and the header names it for
// both sides of the chat.
func (c *MessagesCore) storyReplyHeader(replyToMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) (*mtproto.MessageReplyHeader, error) {
	if replyToMsgId != nil || replyTo.GetPredicateName() != mtproto.Predicate_inputReplyToStory {
		return nil, nil
	}
	if c.svcCtx.Stories == nil {
		return nil, errStoryIdInvalid
	}
	var (
		peer *mtproto.PeerUtil
		err  error
	)
	if replyTo.GetPeer() != nil {
		peer, err = c.storyPeer(replyTo.GetPeer())
	} else if u := replyTo.GetUserId(); u != nil {
		// The form of the layers that had stories of users only.
		peer = mtproto.FromInputUser(c.MD.UserId, u)
		if peer.PeerType == mtproto.PEER_SELF {
			peer = mtproto.MakeUserPeerUtil(c.MD.UserId)
		}
		if peer.PeerType != mtproto.PEER_USER {
			err = mtproto.ErrPeerIdInvalid
		}
	} else {
		err = mtproto.ErrPeerIdInvalid
	}
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Stories.CheckStory(c.ctx, c.MD.UserId, peer, replyTo.GetStoryId(), false); err != nil {
		return nil, err
	}
	header := &mtproto.MessageReplyHeader{Peer: peer.ToPeer(), StoryId: replyTo.GetStoryId()}
	if peer.PeerType == mtproto.PEER_USER {
		header.UserId = peer.PeerId
	}
	return mtproto.MakeTLMessageReplyStoryHeader(header).To_MessageReplyHeader(), nil
}
