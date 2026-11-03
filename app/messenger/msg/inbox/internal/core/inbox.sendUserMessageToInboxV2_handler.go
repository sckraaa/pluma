// Copyright 2024 Teamgram Authors
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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// InboxSendUserMessageToInboxV2
// inbox.sendUserMessageToInboxV2 flags:# user_id:long from_id:long peer_type:int peer_id:long box_list:Vector<MessageBox> users:flags.1?Vector<User> chats:flags.2?Vector<Chat> = Void;
//
// All the messages of the event land in the recipient's box in one
// transaction. A private chat also follows the recipient's archive settings
// (task 050): a new chat from a user who is no contact goes to the archive,
// muted and marked autoarchived when the recipient asked for it; an
// archived chat that is not kept there comes back to the main list, with
// updateFolderPeers next to the new message.
func (c *InboxCore) InboxSendUserMessageToInboxV2(in *inbox.TLInboxSendUserMessageToInboxV2) error {
	bot := isBot(in.UserId, in.Users)
	var archive dao.IncomingArchive
	if in.PeerType == mtproto.PEER_USER && !bot {
		var err error
		if archive, err = c.svcCtx.Dao.IncomingArchiveOf(c.ctx, in.UserId, in.FromId); err != nil {
			return err
		}
		// The mute and the bar of an archived chat first: both are kept
		// by user, are the same when the event is applied again, and must
		// not be missing once the chat is in the archive.
		if archive.New {
			if err = c.markAutoarchived(in); err != nil {
				return err
			}
		}
	}
	return c.apply(in.UserId, recipientPeer(in.FromId, in.PeerType, in.PeerId), func(b *userbox.Box) (bool, error) {
		received, err := c.svcCtx.Dao.ReceiveIncoming(b, in.FromId, in.PeerType, in.PeerId, in.BoxList)
		if err != nil || len(received.Updates) == 0 {
			return true, err
		}
		updates := received.Updates
		switch {
		case archive.New:
			if err = c.svcCtx.Dao.ArchiveNewDialog(b, mtproto.PEER_USER, in.FromId); err != nil {
				return true, err
			}
		case archive.Unarchive:
			moved, err := c.svcCtx.Dao.SetPeerFolders(b, []dao.FolderPeer{{PeerType: mtproto.PEER_USER, PeerId: in.FromId}}, dao.FolderMain)
			if err != nil {
				return true, err
			}
			if moved != nil {
				updates = append(updates, moved)
			}
		}
		return true, c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, bot,
			mtproto.MakeUpdatesByUpdatesUsersChats(in.Users, in.Chats, updates...))
	})
}

// muteForever is the mute_until of a chat muted until it is unmuted
// (core.telegram.org/constructor/peerNotifySettings: a date; the clients
// read anything past 2038 as "forever").
const muteForever = 2147483647

// markAutoarchived mutes the recipient's new chat with the sender and sets
// the bar of an automatically archived chat (peerSettings.autoarchived),
// which the clients show with Unarchive, Block and Report
// (034-research/archive.md, section 2).
func (c *InboxCore) markAutoarchived(in *inbox.TLInboxSendUserMessageToInboxV2) error {
	if _, err := c.svcCtx.Dao.UserClient.UserSetNotifySettings(c.ctx, &userpb.TLUserSetNotifySettings{
		UserId:   in.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   in.FromId,
		Settings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
			MuteUntil: &wrapperspb.Int32Value{Value: muteForever},
		}).To_PeerNotifySettings(),
		Users: in.Users,
	}); err != nil {
		return err
	}
	_, err := c.svcCtx.Dao.UserClient.UserAddPeerSettings(c.ctx, &userpb.TLUserAddPeerSettings{
		UserId:   in.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   in.FromId,
		Settings: mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{
			ReportSpam:   true,
			BlockContact: true,
			Autoarchived: true,
		}).To_PeerSettings(),
		Users: in.Users,
	})
	return err
}
