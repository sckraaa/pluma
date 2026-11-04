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
	"github.com/teamgram/teamgram-server/pkg/hashx"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetSavedReactionTags
// messages.getSavedReactionTags#3637e05b flags:# peer:flags.0?InputPeer hash:long = messages.SavedReactionTags;
//
// The tags of the account's Saved Messages with their titles and counts, of
// one saved dialog with peer (inputPeerEmpty for all, as Desktop sends,
// data_message_reactions.cpp:1072-1088), for everyone: Pluma has no
// Premium (task 053).
func (c *CatalogCore) MessagesGetSavedReactionTags(in *mtproto.TLMessagesGetSavedReactionTags) (*mtproto.Messages_SavedReactionTags, error) {
	var savedPeer *mtproto.PeerUtil
	if p := in.GetPeer(); p != nil && p.GetPredicateName() != mtproto.Predicate_inputPeerEmpty {
		savedPeer = mtproto.FromInputPeer2(c.MD.UserId, p)
		if savedPeer.PeerType == mtproto.PEER_SELF {
			savedPeer = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
		}
	}
	tags, err := reactions.Tags(c.ctx, c.svcCtx.Reactions, c.MD.UserId, savedPeer)
	if err != nil {
		c.Logger.Errorf("messages.getSavedReactionTags - error: %v", err)
		return nil, err
	}
	var hash int64
	out := make([]*mtproto.SavedReactionTag, 0, len(tags))
	for _, t := range tags {
		tag := &mtproto.SavedReactionTag{Reaction: reactions.FromKey(t.Reaction), Count: t.Count}
		if t.Title != "" {
			tag.Title = mtproto.MakeFlagsString(t.Title)
		}
		out = append(out, mtproto.MakeTLSavedReactionTag(tag).To_SavedReactionTag())
		for _, part := range []string{t.Reaction, t.Title} {
			for _, b := range []byte(part) {
				hash = hashx.CombineInt64Hash(hash, int64(b))
			}
		}
		hash = hashx.CombineInt64Hash(hash, int64(t.Count))
	}
	if notModified(in.Hash, hash) {
		return mtproto.MakeTLMessagesSavedReactionTagsNotModified(nil).To_Messages_SavedReactionTags(), nil
	}
	return mtproto.MakeTLMessagesSavedReactionTags(&mtproto.Messages_SavedReactionTags{Tags: out, Hash: hash}).To_Messages_SavedReactionTags(), nil
}
