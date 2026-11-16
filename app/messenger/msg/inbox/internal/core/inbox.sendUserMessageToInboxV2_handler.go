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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/moderation"
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
//
// A private message from a person may trigger the automatic replies of
// Telegram Business of the recipient (task 105): the transaction that
// takes it queues them (autoReplies), and the worker of msg sends them.
func (c *InboxCore) InboxSendUserMessageToInboxV2(in *inbox.TLInboxSendUserMessageToInboxV2) error {
	bot := isBot(in.UserId, in.Users)
	var (
		archive dao.IncomingArchive
		auto    *autoReplyFacts
	)
	if in.PeerType == mtproto.PEER_USER && !bot {
		var err error
		if archive, err = c.svcCtx.Dao.IncomingArchiveOf(c.ctx, in.UserId, in.FromId); err != nil {
			return err
		}
		if auto, err = c.autoReplyFactsOf(in); err != nil {
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
		// The chat as it was before the message: the automatic replies
		// ask whether it is new and how long it was silent.
		var history business.ChatHistory
		if auto != nil {
			var err error
			if history, err = business.ChatHistoryTx(b.Tx(), in.UserId, in.FromId); err != nil {
				return true, err
			}
		}
		received, err := c.svcCtx.Dao.ReceiveIncoming(b, in.FromId, in.PeerType, in.PeerId, in.BoxList)
		if err != nil || len(received.Updates) == 0 {
			return true, err
		}
		if auto != nil && len(received.Boxes) > 0 {
			if err = queueAutoReplies(b, in.FromId, received.Boxes[0].MessageId, history, auto); err != nil {
				return true, err
			}
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
		if err = c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, bot,
			mtproto.MakeUpdatesByUpdatesUsersChats(in.Users, in.Chats, updates...)); err != nil || received.TTL == nil || bot {
			return true, err
		}
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, received.TTL, in.Users, in.Chats)
	})
}

// autoReplyFacts is what the automatic replies of the recipient of a
// private message need to know from other services, asked before its box
// transaction: the settings read then, whether the sender is a contact,
// the work hours and the status of the recipient.
type autoReplyFacts struct {
	settings  []*business.AutoReply
	contact   bool
	hours     *business.Hours
	seenUntil int64
}

// autoReplyFactsOf returns the facts for the automatic replies of the
// recipient of in, nil when none can go: the recipient set none, or the
// message is not one they answer. They answer a person writing to a
// private chat: not the account itself, a bot, the service notifications
// or a support account (decisions to confirm of task 105, 4; the chats
// business.ServedUser names), and not a service message. Nor do they
// answer a message that is itself an automatic reply (message.offline:
// "sent because of a scheduled action by the sender, e.g. as away, or a
// greeting service message", core.telegram.org/constructor/message): two
// accounts with away messages would answer each other.
func (c *InboxCore) autoReplyFactsOf(in *inbox.TLInboxSendUserMessageToInboxV2) (*autoReplyFacts, error) {
	if in.FromId == in.UserId || in.FromId == moderation.ServiceUserId || in.PeerId != in.UserId {
		return nil, nil
	}
	if i := slices.IndexFunc(in.Users, func(u *mtproto.User) bool { return u.GetId() == in.FromId }); i < 0 ||
		in.Users[i].GetBot() || in.Users[i].GetSupport() || in.Users[i].GetDeleted() {
		return nil, nil
	}
	if !slices.ContainsFunc(in.BoxList, func(box *mtproto.MessageBox) bool {
		m := box.GetMessage()
		return m.GetPredicateName() == mtproto.Predicate_message && !m.GetOffline()
	}) {
		return nil, nil
	}
	store := c.svcCtx.Dao.Business()
	settings, err := store.AutoReplies(c.ctx, in.UserId)
	if err != nil || len(settings) == 0 {
		return nil, err
	}
	f := &autoReplyFacts{settings: settings}
	contact, err := c.svcCtx.Dao.UserClient.UserCheckContact(c.ctx, &userpb.TLUserCheckContact{UserId: in.UserId, Id: in.FromId})
	if err != nil {
		return nil, err
	}
	f.contact = mtproto.FromBool(contact)
	if away := business.AutoReplyOf(settings, business.KindAway); away != nil {
		if away.Schedule == business.AwayOutsideWorkHours {
			p, err := store.Profile(c.ctx, in.UserId)
			if err != nil {
				return nil, err
			}
			f.hours = p.Hours
		}
		if away.OfflineOnly {
			// The status everyone sees (account.updateStatus, ADR 0030):
			// online until last_seen_at + expires.
			seen, err := c.svcCtx.Dao.UserClient.UserGetLastSeens(c.ctx, &userpb.TLUserGetLastSeens{Id: []int64{in.UserId}})
			if err != nil {
				return nil, err
			}
			for _, d := range seen.GetDatas() {
				if d.GetUserId() == in.UserId {
					f.seenUntil = d.GetLastSeenAt() + int64(d.GetExpires())
				}
			}
		}
	}
	return f, nil
}

// queueAutoReplies queues, in b, the box transaction of the recipient that
// takes the message triggerBoxId from fromId, the automatic replies it
// triggers. The settings are read again under the lock of the box, the one
// their changes take (account.updateBusinessGreetingMessage and
// account.updateBusinessAwayMessage), and only those read before as well
// count, as the facts were asked for them; a reply queued for a setting
// turned off meanwhile is dropped by the worker. history is the chat before
// the message.
func queueAutoReplies(b *userbox.Box, fromId int64, triggerBoxId int32, history business.ChatHistory, f *autoReplyFacts) error {
	userId := b.UserId()
	settings, err := business.AutoRepliesTx(b.Tx(), userId)
	if err != nil {
		return err
	}
	settings = slices.DeleteFunc(settings, func(a *business.AutoReply) bool {
		return business.AutoReplyOf(f.settings, a.Kind) == nil
	})
	if len(settings) == 0 {
		return nil
	}
	due, err := business.DecideAutoReplies(settings, business.Incoming{
		Peer:      business.Peer{UserID: fromId, Contact: f.contact},
		History:   history,
		Hours:     f.hours,
		SeenUntil: f.seenUntil,
		Now:       time.Unix(b.Date(), 0),
	}, business.TxAutoReplyState{Tx: b.Tx(), UserID: userId, PeerID: fromId})
	if err != nil {
		return err
	}
	for _, a := range due {
		if err = business.QueueAutoReplyTx(b.Tx(), business.QueuedAutoReply{
			UserID:       userId,
			PeerID:       fromId,
			Kind:         a.Kind,
			TriggerBoxID: triggerBoxId,
			ShortcutID:   a.ShortcutID,
		}, b.Date()*1000); err != nil {
			return err
		}
	}
	return nil
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
