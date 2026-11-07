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
	"slices"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesSendReaction
// messages.sendReaction#d30d78d4 flags:# big:flags.1?true add_to_recent:flags.2?true peer:InputPeer msg_id:int reaction:flags.0?Vector<Reaction> = Updates;
//
// The account's reactions to the message become reaction, the whole list in
// its order (none takes them back); msg checks the limits and the group's
// reactions and tells the other members (task 053). An emoji must be one of
// the catalog's available reactions, as Telegram's REACTION_INVALID says.
// A channel's reactions are one list for every member (task 044, msg
// writes them with the channel locked).
func (c *CatalogCore) MessagesSendReaction(in *mtproto.TLMessagesSendReaction) (*mtproto.Updates, error) {
	list := in.GetReaction_FLAGVECTORREACTION()
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if peer.PeerType != mtproto.PEER_CHANNEL {
		var err error
		if peer, err = c.reactionPeer(in.Peer); err != nil {
			return nil, err
		}
	}
	if err := c.checkAvailable(list); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Msg.MsgSendReaction(c.ctx, &msgpb.TLMsgSendReaction{
		UserId:      c.MD.UserId,
		AuthKeyId:   c.MD.PermAuthKeyId,
		PeerType:    peer.PeerType,
		PeerId:      peer.PeerId,
		MsgId:       in.MsgId,
		Big:         mtproto.ToBool(in.Big),
		AddToRecent: mtproto.ToBool(in.AddToRecent),
		Reaction:    list,
	})
	if err != nil {
		c.Logger.Errorf("messages.sendReaction - error: %v", err)
		return nil, err
	}
	return r, nil
}

// checkAvailable checks that each emoji of list is an active reaction of the
// catalog. Without an imported catalog nothing is known to compare with, and
// the clients offer only what getAvailableReactions gave them: none.
func (c *CatalogCore) checkAvailable(list []*mtproto.Reaction) error {
	if !slices.ContainsFunc(list, func(r *mtproto.Reaction) bool { return r.GetPredicateName() == mtproto.Predicate_reactionEmoji }) {
		return nil
	}
	available := &mtproto.Messages_AvailableReactions{}
	_, found, err := c.stored("messages.getAvailableReactions", "", available)
	if err != nil || !found {
		return err
	}
	for _, r := range list {
		if r.GetPredicateName() != mtproto.Predicate_reactionEmoji {
			continue
		}
		if !slices.ContainsFunc(available.GetReactions(), func(a *mtproto.AvailableReaction) bool {
			return a.GetReaction() == r.GetEmoticon() && !a.GetInactive()
		}) {
			return mtproto.ErrReactionInvalid
		}
	}
	return nil
}
