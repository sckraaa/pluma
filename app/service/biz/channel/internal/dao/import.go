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

package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The import of a group from an export (task 083, ADR 0051). The history is
// written into a new supergroup that has no members until the import ends,
// so nobody is told of a message of it: no push, no dialog, no
// updateChannelTooLong. The ordinary send (msg, SendChannel) is not used:
// it dates a message now, applies the slow mode and the rights of the
// sender, writes mentions unread and pushes. Each imported message is
// written as SendChannel writes one (the channel's next id, one pts and one
// row of its journal, by pkg/channelbox) with the date it had, already read.
//
// The journal of an import (channel_import_messages) is written in the
// transaction of the messages: a batch is there whole with its journal, or
// not at all, and the next batch must start where the journal ends.

// States of an import (channel_imports.state).
const (
	ImportRunning  = 0
	ImportFinished = 1
	ImportAborted  = 2
)

// ImportBatchMax is the most records of one channel.importMessages: one
// transaction holds the creator's box and the channel's row for them.
const ImportBatchMax = 200

// Errors of the import, for the tool that runs it.
var (
	// ErrImportOutOfOrder: the batch does not start at the import's next
	// record, or its records are not consecutive.
	ErrImportOutOfOrder = status.Error(mtproto.ErrBadRequest, "IMPORT_OUT_OF_ORDER")
	// ErrImportFinished: the import is finished or aborted.
	ErrImportFinished = status.Error(mtproto.ErrBadRequest, "IMPORT_FINISHED")
	// ErrImportNotFound: there is no such import.
	ErrImportNotFound = status.Error(mtproto.ErrBadRequest, "IMPORT_NOT_FOUND")
	// ErrImportMappingChanged: the mapping of a resumed import names
	// another account for an author, or another creator.
	ErrImportMappingChanged = status.Error(mtproto.ErrBadRequest, "IMPORT_MAPPING_CHANGED")
	// ErrImportInvalid: a record that cannot be written as given.
	ErrImportInvalid = status.Error(mtproto.ErrBadRequest, "IMPORT_INVALID")
)

type importRow struct {
	Id            int64  `db:"id"`
	SourceType    string `db:"source_type"`
	SourceChatId  int64  `db:"source_chat_id"`
	ChannelId     int64  `db:"channel_id"`
	CreatorUserId int64  `db:"creator_user_id"`
	State         int32  `db:"state"`
	Messages      int64  `db:"messages"`
	Skipped       int64  `db:"skipped"`
	Mapping       string `db:"mapping"`
	Date2         int64  `db:"date2"`
}

const importColumns = "id, source_type, source_chat_id, channel_id, creator_user_id, state, messages, skipped, mapping, date2"

type importSeqRow struct {
	Seq      int64 `db:"seq"`
	SourceId int64 `db:"source_id"`
}

// ImportState reads the import importId, or with 0 the import of the chat
// that is not aborted. ErrImportNotFound for none.
func (d *Dao) ImportState(ctx context.Context, importId int64, sourceType string, sourceChatId int64) (*channel.ChannelImport, error) {
	var rows []importRow
	var err error
	if importId != 0 {
		err = d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+importColumns+" FROM channel_imports WHERE id = ?", importId)
	} else {
		err = d.Mysql.QueryRowsPartial(ctx, &rows, "SELECT "+importColumns+" FROM channel_imports WHERE source_type = ? AND active_source_chat_id = ?", sourceType, sourceChatId)
	}
	if err != nil {
		return nil, fmt.Errorf("import %d of chat %d: %w", importId, sourceChatId, err)
	}
	if len(rows) == 0 {
		return nil, ErrImportNotFound
	}
	r := rows[0]
	out := &channel.ChannelImport{
		Id: r.Id, SourceType: r.SourceType, SourceChatId: r.SourceChatId, ChannelId: r.ChannelId, CreatorId: r.CreatorUserId,
		State: r.State, Messages: r.Messages, Skipped: r.Skipped, Mapping: r.Mapping, Date: r.Date2,
	}
	var last []importSeqRow
	if err = d.Mysql.QueryRowsPartial(ctx, &last, "SELECT seq, source_id FROM channel_import_messages WHERE import_id = ? ORDER BY seq DESC LIMIT 1", r.Id); err != nil {
		return nil, fmt.Errorf("import %d: journal: %w", r.Id, err)
	}
	if len(last) > 0 {
		out.NextSeq, out.LastSourceId = last[0].Seq+1, last[0].SourceId
	}
	var top []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &top, "SELECT top_message FROM channels WHERE id = ?", r.ChannelId); err != nil {
		return nil, fmt.Errorf("import %d: channel %d: %w", r.Id, r.ChannelId, err)
	}
	if len(top) > 0 {
		out.TopMessage = top[0]
	}
	return channel.MakeTLChannelImport(out).To_ChannelImport(), nil
}

// lockImport locks the row of the import in b's transaction, which must be
// of its creator.
func lockImport(b *userbox.Box, importId int64) (*importRow, error) {
	var rows []importRow
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT "+importColumns+" FROM channel_imports WHERE id = ? FOR UPDATE", importId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].CreatorUserId != b.UserId() {
		return nil, ErrImportNotFound
	}
	return &rows[0], nil
}

// MergeImportMapping adds the authors of next to those of old, both the
// JSON of export key to Pluma user id (0: skipped). An author old has
// decided must be the same in next when next names it: its messages are
// written already.
func MergeImportMapping(old, next string) (string, error) {
	merged := map[string]int64{}
	if old != "" {
		if err := json.Unmarshal([]byte(old), &merged); err != nil {
			return "", fmt.Errorf("stored mapping: %w", err)
		}
	}
	added := map[string]int64{}
	if next != "" {
		if err := json.Unmarshal([]byte(next), &added); err != nil {
			return "", ErrImportInvalid
		}
	}
	for key, id := range added {
		if id < 0 {
			return "", ErrImportInvalid
		}
		if was, ok := merged[key]; ok && was != id {
			return "", ErrImportMappingChanged
		}
		merged[key] = id
	}
	out, err := json.Marshal(merged)
	return string(out), err
}

// ImportCreate starts the import of in's chat in b's transaction, of the
// creator: a supergroup without members and without dialogs, its message 1
// (in.First, messageActionChannelCreate, dated as the export begins) and the
// import's row. For a chat with an import that is not aborted it changes
// nothing but the mapping, which it adds to, and returns that import.
func (d *Dao) ImportCreate(b *userbox.Box, in *channel.TLChannelImportCreate) (int64, error) {
	if in.SourceType == "" || in.SourceChatId == 0 {
		return 0, ErrImportInvalid
	}
	var rows []importRow
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT "+importColumns+" FROM channel_imports WHERE source_type = ? AND active_source_chat_id = ? FOR UPDATE",
		in.SourceType, in.SourceChatId); err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		r := rows[0]
		if r.CreatorUserId != b.UserId() {
			return 0, ErrImportMappingChanged
		}
		mapping, err := MergeImportMapping(r.Mapping, in.Mapping)
		if err != nil {
			return 0, err
		}
		if r.State == ImportRunning && mapping != r.Mapping {
			if _, err = b.Tx().Exec("UPDATE channel_imports SET mapping = ? WHERE id = ?", mapping, r.Id); err != nil {
				return 0, err
			}
		}
		return r.Id, nil
	}

	first := in.GetFirst()
	m := first.GetMessage()
	if first.GetSkipReason() != "" || first.GetSeq() < 0 || first.GetSeq() > 1 ||
		m.GetPredicateName() != mtproto.Predicate_messageService || m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChannelCreate {
		return 0, ErrImportInvalid
	}
	mapping, err := MergeImportMapping("", in.Mapping)
	if err != nil {
		return 0, err
	}
	if err = checkChannelsLimit(b, b.UserId()); err != nil {
		return 0, err
	}
	ch, err := channelbox.Create(b, channelbox.NewChannel{
		AccessHash:    rand.Int64(), //nolint:gosec // an access hash checks a client's reference, it is no secret of crypto
		CreatorUserId: b.UserId(),
		Title:         in.Title,
		About:         in.About,
		Megagroup:     true,
		Date:          int64(m.GetDate()),
	})
	if err != nil {
		return 0, err
	}
	res, err := b.Tx().Exec("INSERT INTO channel_imports (source_type, source_chat_id, channel_id, creator_user_id, state, mapping, date2) VALUES (?, ?, ?, ?, ?, ?, ?)",
		in.SourceType, in.SourceChatId, ch.Id(), b.UserId(), ImportRunning, mapping, b.Date())
	if err != nil {
		return 0, err
	}
	importId, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	id, err := d.writeImported(b, ch, importId, first)
	if err != nil {
		return 0, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", id, ch.Id()); err != nil {
		return 0, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_imports SET messages = 1 WHERE id = ?", importId); err != nil {
		return 0, err
	}
	return importId, nil
}

// ImportMessages writes the records batch of the import, the first of them
// firstSeq, in b's transaction, of the import's creator, with the channel
// locked. firstSeq must be the record after the last one journaled, and the
// records consecutive: ErrImportOutOfOrder otherwise, so that neither a
// repeated batch nor a second run of the tool writes anything twice.
func (d *Dao) ImportMessages(b *userbox.Box, importId, firstSeq int64, batch []*channel.ImportedMessage) error {
	if len(batch) == 0 || len(batch) > ImportBatchMax {
		return ErrImportInvalid
	}
	imp, err := lockImport(b, importId)
	if err != nil {
		return err
	}
	if imp.State != ImportRunning {
		return ErrImportFinished
	}
	ch, err := openChannel(b, imp.ChannelId)
	if err != nil {
		return err
	}
	var last []importSeqRow
	if err = b.Tx().QueryRowsPartial(&last, "SELECT seq, source_id FROM channel_import_messages WHERE import_id = ? ORDER BY seq DESC LIMIT 1", importId); err != nil {
		return err
	}
	if len(last) == 0 || firstSeq != last[0].Seq+1 {
		return ErrImportOutOfOrder
	}
	var written, skipped int64
	var top int32
	for i, im := range batch {
		if im.GetSeq() != firstSeq+int64(i) {
			return ErrImportOutOfOrder
		}
		if im.GetSkipReason() != "" {
			if _, err = b.Tx().Exec("INSERT INTO channel_import_messages (import_id, seq, source_id, message_id, skip_reason) VALUES (?, ?, ?, 0, ?)",
				importId, im.GetSeq(), im.GetSourceId(), im.GetSkipReason()); err != nil {
				return err
			}
			skipped++
			continue
		}
		if top, err = d.writeImported(b, ch, importId, im); err != nil {
			return err
		}
		written++
	}
	if written > 0 {
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", top, imp.ChannelId); err != nil {
			return err
		}
	}
	_, err = b.Tx().Exec("UPDATE channel_imports SET messages = messages + ?, skipped = skipped + ? WHERE id = ?", written, skipped, importId)
	return err
}

// writeImported writes the record im of the import as the next message of
// the locked channel and journals it; it returns the message's id. The
// message is im's with its id, peer and author set here, as SendChannel
// writes one of a supergroup, except that it keeps its own date (also
// date2, which the readers take for the message's date) and is read by
// everyone from the start: its mentions are written read, its reactions
// seen.
func (d *Dao) writeImported(b *userbox.Box, ch *channelbox.Channel, importId int64, im *channel.ImportedMessage) (int32, error) {
	channelId, sender := ch.Id(), im.GetSenderId()
	service := im.GetMessage().GetPredicateName() == mtproto.Predicate_messageService
	if sender <= 0 || im.GetMessage().GetDate() <= 0 || im.GetSourceId() == 0 && im.GetSeq() != 0 ||
		!service && im.GetMessage().GetPredicateName() != mtproto.Predicate_message ||
		service && (len(im.GetReactions()) > 0 || im.GetMessage().GetAction() == nil) {
		return 0, ErrImportInvalid
	}
	m := proto.Clone(im.GetMessage()).(*mtproto.Message)
	m.Id = ch.NextMessageId()
	m.PeerId = mtproto.MakePeerChannel(channelId)
	m.FromId = mtproto.MakePeerUser(sender)
	// What is each viewer's, or the channel's to decide, never comes with
	// the record.
	m.Out, m.Mentioned, m.MediaUnread, m.Post, m.Pinned, m.FromScheduled = false, false, false, false, false, false
	m.ReplyTo, m.Reactions, m.Replies, m.TtlPeriod, m.GroupedId, m.ReplyMarkup = nil, nil, nil, nil, nil, nil
	m.Views, m.Forwards, m.PostAuthor, m.FromRank = nil, nil, nil, nil

	who := append([]int64(nil), im.GetMentioned()...)
	var top int32
	if source := im.GetReplyToSourceId(); source != 0 {
		// The message replied to by its id in the export: the journal
		// knows what it became. One that was not imported leaves no reply.
		var target []int32
		if err := b.Tx().QueryRowsPartial(&target, "SELECT message_id FROM channel_import_messages WHERE import_id = ? AND source_id = ?", importId, source); err != nil {
			return 0, err
		}
		if len(target) == 0 {
			return 0, ErrImportInvalid
		}
		if r := target[0]; r != 0 {
			var replied []struct {
				SenderUserId int64 `db:"sender_user_id"`
				ReplyTopId   int32 `db:"reply_top_id"`
			}
			if err := b.Tx().QueryRowsPartial(&replied, "SELECT sender_user_id, reply_top_id FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, r); err != nil {
				return 0, err
			}
			if len(replied) == 0 {
				return 0, fmt.Errorf("import %d: message %d of channel %d is journaled and not there", importId, r, channelId)
			}
			h := &mtproto.MessageReplyHeader{ReplyToMsgId: r, ReplyToMsgId_INT32: r, ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(r)}
			if !service {
				// A reply is in the thread of the message replied to: the
				// thread that message is in, or the one it starts (task
				// 048, as SendChannel).
				who = append(who, replied[0].SenderUserId)
				top = r
				if replied[0].ReplyTopId != 0 {
					top = replied[0].ReplyTopId
					h.ReplyToTopId = wrapperspb.Int32(top)
				}
			}
			m.ReplyTo = mtproto.MakeTLMessageReplyHeader(h).To_MessageReplyHeader()
		}
	}

	var record *polls.Record
	if polls.IsNew(m.Media) {
		if service || im.GetPollId() == 0 {
			return 0, ErrImportInvalid
		}
		record = polls.Consume(m.Media, im.GetPollId(), sender, int64(m.Date))
		record.PeerType, record.PeerId, record.MessageId = mtproto.PEER_CHANNEL, channelId, m.Id
	}

	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return 0, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_filter_type, message_data, message, reply_top_id, topic_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		channelId, m.Id, sender, ch.Pts(), mtproto.GetMediaType(m), data, m.GetMessage(), top, channelbox.TopicColumn(0), int64(m.Date)); err != nil {
		return 0, err
	}
	if record != nil {
		args, err := record.Args()
		if err != nil {
			return 0, err
		}
		if _, err = b.Tx().Exec("INSERT INTO polls ("+polls.Columns+") VALUES ("+polls.InList(len(args))+")", args...); err != nil {
			return 0, err
		}
		answers := record.Poll.GetAnswers()
		for _, i := range im.GetPollChosen() {
			if im.GetPollVoterId() <= 0 || i < 0 || int(i) >= len(answers) {
				return 0, ErrImportInvalid
			}
			if _, err = b.Tx().Exec("INSERT INTO poll_votes (poll_id, user_id, opt, `date`) VALUES (?, ?, ?, ?)",
				record.Id, im.GetPollVoterId(), answers[i].GetOption(), int64(m.Date)); err != nil {
				return 0, err
			}
		}
	}
	sort.Slice(who, func(i, j int) bool { return who[i] < who[j] })
	for i, id := range who {
		if id <= 0 || id == sender || i > 0 && who[i-1] == id {
			continue
		}
		if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_message_mentions (channel_id, user_id, message_id, `read`) VALUES (?, ?, ?, 1)", channelId, id, m.Id); err != nil {
			return 0, err
		}
	}
	order := map[int64]int{}
	for _, r := range im.GetReactions() {
		if r.GetUserId() <= 0 || reactions.FromKey(r.GetReaction()) == nil || order[r.GetUserId()] >= reactions.UserMax {
			return 0, ErrImportInvalid
		}
		// A repeated reaction of a user fails on the table's key.
		if _, err = b.Tx().Exec("INSERT INTO channel_message_reactions (channel_id, message_id, user_id, reaction, chosen_order, big, unread, `date`) VALUES (?, ?, ?, ?, ?, 0, 0, ?)",
			channelId, m.Id, r.GetUserId(), r.GetReaction(), order[r.GetUserId()], max(r.GetDate(), int64(m.Date))); err != nil {
			return 0, err
		}
		order[r.GetUserId()]++
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_import_messages (import_id, seq, source_id, message_id) VALUES (?, ?, ?, ?)",
		importId, im.GetSeq(), im.GetSourceId(), m.Id); err != nil {
		return 0, err
	}
	return m.Id, nil
}

// ImportFinish ends the import in b's transaction, of its creator: the
// creator becomes the channel's member and owner with rights, its dialog is
// written read up to the last message, the channel gets its permanent
// invite link, and everything written so far is marked as the imported
// history (channels.imported_max_id), whose voice and round video messages
// nobody finds unplayed. It tells whether this call finished it; a finished
// import is left as it is.
func (d *Dao) ImportFinish(b *userbox.Box, importId int64, rights int32) (bool, error) {
	imp, err := lockImport(b, importId)
	if err != nil {
		return false, err
	}
	switch imp.State {
	case ImportFinished:
		return false, nil
	case ImportAborted:
		return false, ErrImportFinished
	}
	ch, err := openChannel(b, imp.ChannelId)
	if err != nil {
		return false, err
	}
	var top []int32
	if err = b.Tx().QueryRowsPartial(&top, "SELECT top_message FROM channels WHERE id = ?", imp.ChannelId); err != nil {
		return false, err
	}
	if len(top) == 0 || top[0] == 0 {
		return false, fmt.Errorf("import %d: channel %d has no message", importId, imp.ChannelId)
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, ?)",
		imp.ChannelId, b.UserId(), channel.RoleCreator, rights, b.Date()); err != nil {
		return false, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET participants_count = 1, admins_count = 1, imported_max_id = ? WHERE id = ?",
		ch.State().LastMessageId, imp.ChannelId); err != nil {
		return false, err
	}
	if err = upsertDialog(b, imp.ChannelId, top[0]); err != nil {
		return false, err
	}
	if _, err = b.Tx().Exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, date2) VALUES (?, ?, ?, 1, ?)",
		imp.ChannelId, b.UserId(), chatpb.GenChannelInviteHash(), b.Date()); err != nil {
		return false, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_imports SET state = ?, finished_at = ? WHERE id = ?", ImportFinished, b.Date(), importId); err != nil {
		return false, err
	}
	return true, nil
}

// ImportAbort drops an import that is not finished, in b's transaction, of
// its creator: the channel, which nobody is in, is marked deleted, as a
// deleted channel is (DeleteChannel), and the chat may be imported again.
// The files already uploaded stay known (channel_import_media), so the next
// import of the chat does not upload them again.
func (d *Dao) ImportAbort(b *userbox.Box, importId int64) error {
	imp, err := lockImport(b, importId)
	if err != nil {
		return err
	}
	switch imp.State {
	case ImportFinished:
		return ErrImportFinished
	case ImportAborted:
		return nil
	}
	if _, err = openChannel(b, imp.ChannelId); err != nil {
		return err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET deleted = 1, version = version + 1 WHERE id = ?", imp.ChannelId); err != nil {
		return err
	}
	_, err = b.Tx().Exec("UPDATE channel_imports SET state = ?, finished_at = ? WHERE id = ?", ImportAborted, b.Date(), importId)
	return err
}
