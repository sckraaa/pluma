package dao

import (
	"github.com/teamgram/marmota/pkg/container2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Outgoing is a message its sender writes to its own box.
type Outgoing struct {
	RandomId int64
	// DialogMessageId is the id of the message in every box it lands in
	// (snowflake, issued before the transaction).
	DialogMessageId int64
	// Message as remade for sending; its id and out flag are set here.
	Message *mtproto.Message
}

// SendOutgoing writes messages b's owner sends to peer (the user, or the
// chat) into its box: for each, in order, a box id, the messages row, its
// hashtags and the journal row updateNewMessage; then the dialog, with its
// unread mark cleared (resetUnreadMark), and the saved dialog when the
// owner sends to itself. It returns the boxes, with their pts.
//
// A message to the owner itself (Saved Messages) is neither outgoing nor
// from anyone, as Telegram has it: the chat is the sender (conformance
// fixture saved_messages).
func (d *Dao) SendOutgoing(b *userbox.Box, peerType int32, peerId int64, msgs []Outgoing) ([]*mtproto.MessageBox, error) {
	var (
		userId = b.UserId()
		peer   = boxPeer{PeerType: peerType, PeerId: peerId}
		did    = peer.dialogId(userId)
		boxes  = make([]*mtproto.MessageBox, 0, len(msgs))
		self   = peerType == mtproto.PEER_USER && peerId == userId
	)
	for _, m := range msgs {
		id := b.NextMessageId()
		// A poll or a checklist is written once, here, under the dialog
		// message id all its copies share.
		message, err := d.storeNewMedia(b, m.Message, peerType, peerId, id, m.DialogMessageId)
		if err != nil {
			return nil, err
		}
		message.Out = !self
		if self {
			message.FromId = nil
		}
		message.Id = id
		message.MediaUnread = mtproto.CheckHasMediaUnread(message)
		data, err := jsonx.Marshal(message)
		if err != nil {
			return nil, err
		}

		saved := mtproto.PeerUtil{PeerType: mtproto.PEER_EMPTY}
		if message.GetSavedPeerId() != nil {
			saved = *mtproto.FromPeer(message.GetSavedPeerId())
		}
		box := &mtproto.MessageBox{
			UserId:            userId,
			MessageId:         id,
			SenderUserId:      userId,
			PeerType:          peerType,
			PeerId:            peerId,
			RandomId:          m.RandomId,
			DialogId1:         did.A,
			DialogId2:         did.B,
			DialogMessageId:   m.DialogMessageId,
			MessageFilterType: mtproto.GetMediaType(message),
			Message:           message,
		}
		update := mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: message}).To_Update()
		b.AddPtsUpdate(update, 1)
		box.Pts, box.PtsCount = update.Pts_INT32, update.PtsCount
		if err = insertMessage(b, &dataobject.MessagesDO{
			UserId:            userId,
			UserMessageBoxId:  id,
			DialogId1:         did.A,
			DialogId2:         did.B,
			DialogMessageId:   m.DialogMessageId,
			SenderUserId:      userId,
			PeerType:          peerType,
			PeerId:            peerId,
			RandomId:          m.RandomId,
			MessageFilterType: box.MessageFilterType,
			MessageData:       string(data),
			Message:           message.GetMessage(),
			MediaUnread:       message.GetMediaUnread(),
			SavedPeerType:     saved.PeerType,
			SavedPeerId:       saved.PeerId,
			Date2:             int64(message.GetDate()),
			TtlPeriod:         message.GetTtlPeriod().GetValue(),
		}, box.Pts); err != nil {
			return nil, err
		}
		if err = scheduleExpiry(b, message); err != nil {
			return nil, err
		}
		if err = d.replaceHashTags(b, peer, id, message, false); err != nil {
			return nil, err
		}
		boxes = append(boxes, box)

		if peerType == mtproto.PEER_USER && peerId == userId && saved.PeerType != mtproto.PEER_EMPTY {
			if _, _, err = d.SavedDialogsDAO.InsertOrUpdateTx(b.Tx(), &dataobject.SavedDialogsDO{
				UserId:     userId,
				PeerType:   saved.PeerType,
				PeerId:     saved.PeerId,
				TopMessage: id,
			}); err != nil {
				return nil, err
			}
		}
	}
	if len(boxes) == 0 {
		return nil, nil
	}
	last := boxes[len(boxes)-1]
	if err := upsertOutgoingDialog(b, peer, last.MessageId, int64(last.Message.GetDate())); err != nil {
		return nil, err
	}
	if err := d.resetUnreadMark(b, peer); err != nil {
		return nil, err
	}
	return boxes, nil
}

// CountSavedUnread counts n messages delivered to Saved Messages as unread:
// the reminders of a schedule queue (task 061), which the clients keep
// unread and show a notification for.
func (d *Dao) CountSavedUnread(b *userbox.Box, n int32) error {
	return updateDialog(b, boxPeer{PeerType: mtproto.PEER_USER, PeerId: b.UserId()}, "unread_count = unread_count + ?", n)
}

// ReadSaved marks Saved Messages of b's owner read up to maxId, as Telegram
// does as soon as a message is sent there: updateReadHistoryInbox with its
// pts, after the updateNewMessage. It returns nil when the chat is read that
// far already.
func (d *Dao) ReadSaved(b *userbox.Box, maxId int32) (*mtproto.Update, error) {
	return d.readInbox(b, boxPeer{PeerType: mtproto.PEER_USER, PeerId: b.UserId()}, maxId)
}

// Incoming is the result of receiving messages into a box.
type Incoming struct {
	// Boxes are the recipient's copies, with their pts.
	Boxes []*mtproto.MessageBox
	// Updates are the journaled updates to push, in pts order.
	Updates []*mtproto.Update
	// TTL is updatePeerHistoryTTL when the messages change the period of
	// the dialog, nil otherwise: the caller journals it with seq.
	TTL *mtproto.Update
}

// ReceiveIncoming writes the messages fromId sent (boxes from the sender's
// box) into the box of b's owner: for each, in order, a box id, the
// messages row with the reply and mention resolved for the recipient, its
// hashtags and the journal row updateNewMessage; then the dialog with the
// unread counters. peerType and peerId are the dialog as the sender
// addressed it (the recipient, or the chat).
func (d *Dao) ReceiveIncoming(b *userbox.Box, fromId int64, peerType int32, peerId int64, boxes []*mtproto.MessageBox) (*Incoming, error) {
	var (
		userId   = b.UserId()
		rowPeer  = boxPeer{PeerType: peerType, PeerId: peerId}
		dlgPeer  = recipientPeer(fromId, peerType, peerId)
		did      = mtproto.MakeDialogId(fromId, peerType, peerId)
		out      = &Incoming{}
		mentions int32
		migrated int32
		period   int32
		changed  bool
	)
	// A basic group's message the box has already: a ghost's history copy
	// (CopyChatHistory) took it from another member's box before this
	// delivery came, which a send that read the chat after the ghost joined
	// makes. The copy stands.
	have, err := chatCopies(b, peerType, boxes)
	if err != nil {
		return nil, err
	}
	for _, src := range boxes {
		if have[src.GetDialogMessageId()] {
			continue
		}
		id := b.NextMessageId()
		message := proto.Clone(src.GetMessage()).(*mtproto.Message)
		message.Out = false
		message.Id = id
		// from_scheduled tells the sender its message went out (task 061).
		message.FromScheduled = false

		switch message.GetReplyTo().GetPredicateName() {
		case mtproto.Predicate_messageReplyHeader:
			// A reply to a message of another chat names no message: it
			// shows the origin of the message (reply_from), the same for
			// every reader, and mentions no one.
			if message.GetReplyTo().GetReplyFrom() != nil {
				break
			}
			replied, err := replyCopy(b, fromId, did, message.GetReplyTo().GetFixedReplyToMsgId())
			if err != nil {
				return nil, err
			}
			if replied == nil {
				message.ReplyTo = nil
				break
			}
			message.ReplyTo.ReplyToMsgId = replied.UserMessageBoxId
			message.ReplyTo.ReplyToMsgId_INT32 = replied.UserMessageBoxId
			message.ReplyTo.ReplyToMsgId_FLAGINT32 = mtproto.MakeFlagsInt32(replied.UserMessageBoxId)
			if peerType == mtproto.PEER_CHAT && replied.SenderUserId == userId {
				message.Mentioned = true
				if message.GetAction().GetPredicateName() != mtproto.Predicate_messageActionPinMessage {
					message.MediaUnread = true
				}
			}
		}

		switch peerType {
		case mtproto.PEER_CHAT:
			if !message.Mentioned && mtproto.CheckHasMention(message.Entities, userId) {
				message.Mentioned = true
				message.MediaUnread = true
			}
			if message.GetAction().GetPredicateName() == mtproto.Predicate_messageActionGroupCall &&
				container2.ContainsInt64(message.GetAction().GetUsers(), userId) {
				message.Mentioned = true
				message.MediaUnread = true
			}
		case mtproto.PEER_USER:
			message.FromId = nil
			message.PeerId = mtproto.MakePeerUser(fromId)
		}
		if !message.MediaUnread {
			message.MediaUnread = mtproto.CheckHasMediaUnread(message)
		}
		// The pin of the recipient's message mentions them without unread
		// media; clients do not count it as an unread mention.
		if message.Mentioned && message.MediaUnread {
			mentions++
		}
		data, err := jsonx.Marshal(message)
		if err != nil {
			return nil, err
		}

		box := &mtproto.MessageBox{
			UserId:            userId,
			MessageId:         id,
			SenderUserId:      fromId,
			PeerType:          peerType,
			PeerId:            peerId,
			RandomId:          src.GetRandomId(),
			DialogId1:         did.A,
			DialogId2:         did.B,
			DialogMessageId:   src.GetDialogMessageId(),
			MessageFilterType: mtproto.GetMediaType(message),
			Message:           message,
			Mentioned:         message.Mentioned,
			MediaUnread:       message.MediaUnread,
		}
		update := mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: message}).To_Update()
		b.AddPtsUpdate(update, 1)
		box.Pts, box.PtsCount = update.Pts_INT32, update.PtsCount
		if err = insertMessage(b, &dataobject.MessagesDO{
			UserId:            userId,
			UserMessageBoxId:  id,
			DialogId1:         did.A,
			DialogId2:         did.B,
			DialogMessageId:   box.DialogMessageId,
			SenderUserId:      fromId,
			PeerType:          peerType,
			PeerId:            peerId,
			RandomId:          box.RandomId,
			MessageFilterType: box.MessageFilterType,
			MessageData:       string(data),
			Message:           message.GetMessage(),
			Mentioned:         message.Mentioned,
			MediaUnread:       message.MediaUnread,
			Date2:             b.Date(),
			TtlPeriod:         message.GetTtlPeriod().GetValue(),
		}, box.Pts); err != nil {
			return nil, err
		}
		if err = scheduleExpiry(b, message); err != nil {
			return nil, err
		}
		if err = d.replaceHashTags(b, rowPeer, id, message, false); err != nil {
			return nil, err
		}
		out.Boxes = append(out.Boxes, box)
		out.Updates = append(out.Updates, update)

		if historyttl.IsChange(message) {
			period, changed = message.GetAction().GetPeriod(), true
		}
		if peerType == mtproto.PEER_CHAT && message.GetAction().GetPredicateName() == mtproto.Predicate_messageActionChatMigrateTo {
			migrated = id
		}
	}
	if len(out.Boxes) == 0 {
		return out, nil
	}

	last := out.Boxes[len(out.Boxes)-1]
	if err := upsertIncomingDialog(b, dlgPeer, last.MessageId, int32(len(out.Boxes)), mentions, b.Date()); err != nil {
		return nil, err
	}
	if changed {
		if err := updateDialog(b, dlgPeer, "ttl_period = ?", period); err != nil {
			return nil, err
		}
		out.TTL = historyttl.Update(dlgPeer.toPeer(), period)
	}

	// A chat migrated to a supergroup: its history is read up to the
	// migration message.
	if migrated != 0 {
		update, err := d.readInbox(b, dlgPeer, migrated)
		if err != nil {
			return nil, err
		}
		if update != nil {
			out.Updates = append(out.Updates, update)
		}
	}
	return out, nil
}

// chatCopies returns which messages of boxes, a basic group's, b's box has
// a copy of, deleted or not.
func chatCopies(b *userbox.Box, peerType int32, boxes []*mtproto.MessageBox) (map[int64]bool, error) {
	if peerType != mtproto.PEER_CHAT || len(boxes) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(boxes)+1)
	args = append(args, b.UserId())
	for _, box := range boxes {
		args = append(args, box.GetDialogMessageId())
	}
	var ids []int64
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT dialog_message_id FROM messages WHERE user_id = ? AND dialog_message_id IN ("+inList(len(boxes))+")", args...); err != nil {
		return nil, err
	}
	have := make(map[int64]bool, len(ids))
	for _, id := range ids {
		have[id] = true
	}
	return have, nil
}

// recipientPeer is the dialog of an event's recipient with its initiator
// fromId, from the dialog as the initiator addressed it (peerType, peerId):
// the initiator for a private chat, the chat itself for a basic group.
func recipientPeer(fromId int64, peerType int32, peerId int64) boxPeer {
	if peerType == mtproto.PEER_USER {
		return boxPeer{PeerType: mtproto.PEER_USER, PeerId: fromId}
	}
	return boxPeer{PeerType: peerType, PeerId: peerId}
}

// RecipientPeer is recipientPeer as a peer.
func RecipientPeer(fromId int64, peerType int32, peerId int64) *mtproto.PeerUtil {
	p := recipientPeer(fromId, peerType, peerId)
	return mtproto.MakePeerUtil(p.PeerType, p.PeerId)
}
