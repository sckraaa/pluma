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
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessagesGetSearchCounters
// messages.getSearchCounters#732eef00 peer:InputPeer filters:Vector<MessagesFilter> = Vector<messages.SearchCounter>;
func (c *MessagesCore) MessagesGetSearchCounters(in *mtproto.TLMessagesGetSearchCounters) (*mtproto.Vector_Messages_SearchCounter, error) {
	var (
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	)
	counters := &mtproto.Vector_Messages_SearchCounter{
		Datas: make([]*mtproto.Messages_SearchCounter, 0, len(in.GetFilters())),
	}

	if in.SavedPeerId != nil {
		// TODO: not impl
		return counters, nil
	}

	for _, filter := range in.GetFilters() {
		/*
			{
			    "constructor": "CRC32_messages_getSearchCounters",
			    "peer": {
			        "predicate_name": "inputPeerChat",
			        "constructor": "CRC32_inputPeerChat",
			        "chat_id": 10053
			    },
			    "filters": [
			        {
			            "predicate_name": "inputMessagesFilterPhotoVideo",
			            "constructor": "CRC32_inputMessagesFilterPhotoVideo"
			        },
			        {
			            "predicate_name": "inputMessagesFilterDocument",
			            "constructor": "CRC32_inputMessagesFilterDocument"
			        },
			        {
			            "predicate_name": "inputMessagesFilterRoundVoice",
			            "constructor": "CRC32_inputMessagesFilterRoundVoice"
			        },
			        {
			            "predicate_name": "inputMessagesFilterUrl",
			            "constructor": "CRC32_inputMessagesFilterUrl"
			        },
			        {
			            "predicate_name": "inputMessagesFilterMusic",
			            "constructor": "CRC32_inputMessagesFilterMusic"
			        },
			        {
			            "predicate_name": "inputMessagesFilterGif",
			            "constructor": "CRC32_inputMessagesFilterGif"
			        }
			    ]
			}
		*/

		/*
		 */
		var (
			fType = mtproto.FromMessagesFilter(filter)
			mType int32
		)

		switch fType {
		case mtproto.FilterPhotoVideo:
			mType = mtproto.MEDIA_PHOTOVIDEO
		case mtproto.FilterPhotos:
			mType = mtproto.MEDIA_PHOTOS_ONLY
		case mtproto.FilterVideo:
			mType = mtproto.MEDIA_VIDEOS_ONLY
		case mtproto.FilterDocument:
			mType = mtproto.MEDIA_FILE
		case mtproto.FilterUrl:
			mType = mtproto.MEDIA_URL
		case mtproto.FilterGif:
			mType = mtproto.MEDIA_GIF
		case mtproto.FilterMusic:
			mType = mtproto.MEDIA_MUSIC
		case mtproto.FilterRoundVoice:
			mType = mtproto.MEDIA_AUDIO
		case mtproto.FilterChatPhotos:
			mType = mtproto.MEDIA_CHAT_PHOTO
		default:
			counter := mtproto.MakeTLMessagesSearchCounter(&mtproto.Messages_SearchCounter{
				Inexact: false,
				Filter:  filter,
				Count:   0,
			}).To_Messages_SearchCounter()
			counters.Datas = append(counters.Datas, counter)
			continue
		}

		if peer.PeerType == mtproto.PEER_CHANNEL {
			n, err := c.channelSearchCount(peer.PeerId, in.GetTopMsgId().GetValue(), mType)
			if err != nil {
				c.Logger.Errorf("messages.getSearchCounters - error: %v", err)
				return nil, err
			}
			counters.Datas = append(counters.Datas, mtproto.MakeTLMessagesSearchCounter(&mtproto.Messages_SearchCounter{
				Inexact: false,
				Filter:  filter,
				Count:   n,
			}).To_Messages_SearchCounter())
			continue
		}

		sz, _ := c.svcCtx.Dao.MessageClient.MessageGetSearchCounter(
			c.ctx,
			&message.TLMessageGetSearchCounter{
				UserId:    c.MD.UserId,
				PeerType:  peer.PeerType,
				PeerId:    peer.PeerId,
				MediaType: mType,
			})

		counter := mtproto.MakeTLMessagesSearchCounter(&mtproto.Messages_SearchCounter{
			Inexact: false,
			Filter:  filter,
			Count:   sz.GetV(),
		}).To_Messages_SearchCounter()
		counters.Datas = append(counters.Datas, counter)
		c.Logger.Debugf("messages.getSearchCounters - result: {filter: %s, count: %d}", counter.GetFilter().GetPredicateName(), counter.GetCount())
	}

	return counters, nil
}

// channelSearchCount is how many messages of a channel, or of a topic or a
// thread with top_msg_id, have the media type: the count of a search page
// of one message (task 067; biz channel counts what it searches).
func (c *MessagesCore) channelSearchCount(channelId int64, top, mType int32) (int32, error) {
	var (
		r   *mtproto.Messages_Messages
		err error
	)
	if top != 0 {
		r, err = c.svcCtx.Dao.Channel.ChannelSearchTopic(c.ctx, &channelpb.TLChannelSearchTopic{
			SelfId: c.MD.UserId, ChannelId: channelId, TopMsgId: top, MediaType: mType, UnreadMentions: mtproto.BoolFalse, Limit: 1,
		})
	} else {
		r, err = c.svcCtx.Dao.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{SelfId: c.MD.UserId, ChannelId: channelId, MediaType: mType, Limit: 1})
	}
	if err != nil {
		return 0, err
	}
	return r.GetCount(), nil
}
