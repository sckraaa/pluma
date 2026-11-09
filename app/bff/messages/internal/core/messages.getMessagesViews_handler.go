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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

/*
## android client
- void setMessageObjectInternal(MessageObject messageObject)
```
    private void setMessageObjectInternal(MessageObject messageObject) {
        if (((messageObject.messageOwner.flags & TLRPC.MESSAGE_FLAG_HAS_VIEWS) != 0 || messageObject.messageOwner.replies != null) && !currentMessageObject.scheduled && !currentMessageObject.isSponsored()) {
            if (!currentMessageObject.viewsReloaded) {
                MessagesController.getInstance(currentAccount).addToViewsQueue(currentMessageObject);
                currentMessageObject.viewsReloaded = true;
            }
        }
```

- didReceivedNotification
```
        } else if (id == NotificationCenter.removeAllMessagesFromDialog) {
            long did = (Long) args[0];
            if (dialog_id == did) {
                if (threadMessageId != 0) {
                    if (forwardEndReached[0]) {
                        forwardEndReached[0] = false;
                        chatAdapter.notifyItemInserted(0);
                    }
                    getMessagesController().addToViewsQueue(threadMessageObject);
```
*/

// MessagesGetMessagesViews
// messages.getMessagesViews#5784d3e1 peer:InputPeer id:Vector<int> increment:Bool = messages.MessageViews;
func (c *MessagesCore) MessagesGetMessagesViews(in *mtproto.TLMessagesGetMessagesViews) (*mtproto.Messages_MessageViews, error) {
	type msgIdPeerIdPair struct {
		msgId  int32
		peerId int64
	}
	var (
		boxMsgList    *message.Vector_MessageBox
		channelIds    = make(map[int64][]int32, 0)
		reqIndexViews = make(map[int32]msgIdPeerIdPair, 0)
		peer          = mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	)

	if len(in.Id) == 0 {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("messages.getMessagesViews#5784d3e - error: %v, invalid request, len(id) == 0", err)
		return nil, err
	}

	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
		switch peer.PeerType {
		case mtproto.PEER_USER:
			mutableUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(
				c.ctx,
				&userpb.TLUserGetMutableUsers{
					Id: []int64{c.MD.UserId, peer.PeerId},
				})
			if err != nil {
				c.Logger.Errorf("messages.getMessagesViews#5784d3e - error: %v, not found peer(%v) == 0", err, peer)
				return nil, err
			}
			if mutableUsers.Length() != 2 {
				err = mtproto.ErrPeerIdInvalid
				c.Logger.Errorf("messages.getMessagesViews#5784d3e - error: %v, not found peer(%v) == 0", err, peer)
				return nil, err
			}
		case mtproto.PEER_CHAT:
			mutableChat, err := c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(
				c.ctx,
				&chatpb.TLChatGetMutableChat{
					ChatId: peer.PeerId,
				})
			if err != nil {
				c.Logger.Errorf("messages.getMessagesViews#5784d3e - error: not found chat by id(%v) type", peer)
				return nil, mtproto.ErrChatIdInvalid
			}
			if _, ok := mutableChat.GetImmutableChatParticipant(c.MD.UserId); !ok {
				c.Logger.Errorf("messages.getMessagesViews#5784d3e - error: not in chat(%v) type", in.Id)
				return nil, mtproto.ErrChatIdInvalid
			}
		}

		var (
			err error
		)
		boxMsgList, err = c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(
			c.ctx,
			&message.TLMessageGetUserMessageList{
				UserId: c.MD.UserId,
				IdList: in.Id,
			})
		if err != nil {
			c.Logger.Errorf("messages.getMessagesViews - error: %v", err)
			return nil, err
		}
		//// md.UserId, request.Id)
		//if len(request.Id) != len(boxMsgList) {
		//	log.Errorf("messages.getMessagesViews#5784d3e - error: boxMsgList empty by id(%v) type", request.Id)
		//	return nil, mtproto.ErrMsgIdInvalid
		//}
		// log.Debugf("user boxMsgList: %s", boxMsgList)

		// A post forwarded here counts as a view of the original
		// (core.telegram.org/method/messages.getMessagesViews: "sent or
		// forwarded from a channel"; the clients keep the views of a
		// forward, Android SendMessagesHelper.java:2428-2431).
		boxMsgList.Walk(func(idx int, v *mtproto.MessageBox) {
			fwdFrom := v.Message.GetFwdFrom()
			channelId, post := fwdFrom.GetFromId().GetChannelId(), fwdFrom.GetChannelPost().GetValue()
			if channelId == 0 || post == 0 {
				return
			}
			channelIds[channelId] = append(channelIds[channelId], post)
			reqIndexViews[v.MessageId] = msgIdPeerIdPair{msgId: post, peerId: channelId}
		})
	case mtproto.PEER_CHANNEL:
		return c.channelMessagesViews(peer.PeerId, in.Id, mtproto.FromBool(in.GetIncrement()))
	default:
		c.Logger.Errorf("messages.getMessagesViews#5784d3e -  error: invalid peer(%v) type", peer)
		return nil, mtproto.ErrInputRequestInvalid
	}

	// A channel the account may not read shows nothing: an empty one, as
	// channelMessagesViews gives for an id it may not see.
	posts := make(map[int64]map[int32]*mtproto.Message, len(channelIds))
	for channelId, ids := range channelIds {
		if mtproto.FromBool(in.GetIncrement()) {
			if _, err := c.svcCtx.Dao.Channel.ChannelIncrementMessageViews(c.ctx, &channelpb.TLChannelIncrementMessageViews{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids}); err != nil {
				c.Logger.Infof("messages.getMessagesViews - channel %d: %v", channelId, err)
			}
		}
		messages, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids})
		if err != nil {
			c.Logger.Infof("messages.getMessagesViews - channel %d: %v", channelId, err)
			continue
		}
		byId := make(map[int32]*mtproto.Message, len(messages.GetDatas()))
		for _, m := range messages.GetDatas() {
			byId[m.GetId()] = m
		}
		posts[channelId] = byId
	}

	rViews := mtproto.MakeTLMessagesMessageViews(&mtproto.Messages_MessageViews{
		Views: make([]*mtproto.MessageViews, 0, len(in.Id)),
		Chats: []*mtproto.Chat{},
		Users: []*mtproto.User{},
	}).To_Messages_MessageViews()
	for _, id := range in.Id {
		v := &mtproto.MessageViews{}
		if p, ok := reqIndexViews[id]; ok {
			if m := posts[p.peerId][p.msgId]; m != nil {
				v.Views, v.Forwards = m.GetViews(), m.GetForwards()
			}
		}
		rViews.Views = append(rViews.Views, mtproto.MakeTLMessageViews(v).To_MessageViews())
	}

	return rViews, nil
}
