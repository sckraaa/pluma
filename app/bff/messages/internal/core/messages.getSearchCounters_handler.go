// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
)

// MessagesGetSearchCounters
// messages.getSearchCounters#1bbcf300 flags:# peer:InputPeer saved_peer_id:flags.2?InputPeer top_msg_id:flags.0?int filters:Vector<MessagesFilter> = Vector<messages.SearchCounter>;
func (c *MessagesCore) MessagesGetSearchCounters(in *mtproto.TLMessagesGetSearchCounters) (*mtproto.Vector_Messages_SearchCounter, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	counters := &mtproto.Vector_Messages_SearchCounter{
		Datas: make([]*mtproto.Messages_SearchCounter, 0, len(in.GetFilters())),
	}
	for _, filter := range in.GetFilters() {
		var n int32
		if kind, ok := msgsearch.KindOf(filter); ok {
			var err error
			if n, err = c.searchCount(in, peer, kind); err != nil {
				c.Logger.Errorf("messages.getSearchCounters - error: %v", err)
				return nil, err
			}
		}
		counters.Datas = append(counters.Datas, mtproto.MakeTLMessagesSearchCounter(&mtproto.Messages_SearchCounter{
			Filter: filter,
			Count:  n,
		}).To_Messages_SearchCounter())
	}
	return counters, nil
}

func (c *MessagesCore) searchCount(in *mtproto.TLMessagesGetSearchCounters, peer *mtproto.PeerUtil, kind int32) (int32, error) {
	if peer.IsChannel() {
		// Saved dialogs are of the account's own Saved Messages only.
		if in.SavedPeerId != nil {
			return 0, nil
		}
		n, err := c.svcCtx.Dao.Channel.ChannelSearchCount(c.ctx, &channelpb.TLChannelSearchCount{
			SelfId: c.MD.UserId, ChannelId: peer.PeerId, TopMsgId: in.GetTopMsgId().GetValue(), MediaType: kind,
		})
		if err != nil {
			return 0, err
		}
		return n.GetV(), nil
	}
	if in.GetTopMsgId() != nil {
		// A box has no threads.
		return 0, nil
	}
	q := msgsearch.BoxQuery{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, Kind: kind}
	c.applySaved(&q, in.SavedPeerId)
	return msgsearch.Count(c.ctx, c.svcCtx.Search, q.Source())
}

func (c *MessagesCore) applySaved(q *msgsearch.BoxQuery, saved *mtproto.InputPeer) {
	if saved == nil {
		return
	}
	p := mtproto.FromInputPeer2(c.MD.UserId, saved)
	if p.PeerType == mtproto.PEER_SELF {
		p = mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId)
	}
	q.HasSaved, q.SavedPeerType, q.SavedPeerId = true, p.PeerType, p.PeerId
}
