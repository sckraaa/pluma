//go:build integration

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
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/polls"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const importSource = "telegram"

// creation is the first message of an import, made for the creator.
func creation(creator int64, date int32) *channel.ImportedMessage {
	return &channel.ImportedMessage{SenderId: creator, Message: mtproto.MakeTLMessageService(&mtproto.Message{
		Date: date, Action: mtproto.MakeTLMessageActionChannelCreate(&mtproto.MessageAction{Title_STRING: "Friends"}).To_MessageAction(),
	}).To_Message()}
}

func (f *diffFixture) importCreate(creator, chat int64, mapping string) (*channel.ChannelImport, error) {
	return f.c.ChannelImportCreate(&channel.TLChannelImportCreate{
		CreatorId: creator, SourceType: importSource, SourceChatId: chat, Title: "Friends", Mapping: mapping, First: creation(creator, 1000),
	})
}

func (f *diffFixture) importBatch(importId, first int64, batch ...*channel.ImportedMessage) (*channel.ChannelImport, error) {
	return f.c.ChannelImportMessages(&channel.TLChannelImportMessages{ImportId: importId, FirstSeq: first, Messages: batch})
}

// imported is a text record of sender.
func imported(seq, source, sender int64, date int32) *channel.ImportedMessage {
	return &channel.ImportedMessage{Seq: seq, SourceId: source, SenderId: sender,
		Message: mtproto.MakeTLMessage(&mtproto.Message{Date: date, Message: "text"}).To_Message()}
}

func (f *diffFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// importedId is the channel message the import made of the record source.
func (f *diffFixture) importedId(importId, source int64) int32 {
	f.t.Helper()
	var id int32
	if err := f.box.QueryRow("SELECT message_id FROM channel_import_messages WHERE import_id = ? AND source_id = ?", importId, source).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

// TestImportHistory: an import writes a history with the dates and authors
// it had into a channel nobody is in, batch by batch, each batch whole or
// not at all and never twice; when it ends everything is read by everyone,
// the voice messages played, for those who join later too.
func TestImportHistory(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave")
	chat := itId()
	st, err := f.importCreate(creator, chat, `{"user1":`+itoa(creator)+`}`)
	if err != nil {
		t.Fatal(err)
	}
	id := st.ChannelId
	if st.State != dao.ImportRunning || st.NextSeq != 1 || st.TopMessage != 1 || st.Messages != 1 || st.CreatorId != creator || id == 0 {
		t.Fatalf("the new import: %v", st)
	}
	// The channel begins on the date its history does, with nobody in it.
	first := f.stored(id, 1)
	if first.GetDate() != 1000 || first.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChannelCreate || first.GetFromId().GetUserId() != creator {
		t.Errorf("message 1: %v", first)
	}
	if n := f.count("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ?", id); n != 0 {
		t.Errorf("%d participants of a channel being imported", n)
	}
	if n := f.count("SELECT COUNT(*) FROM dialogs WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, id); n != 0 {
		t.Errorf("%d dialogs with a channel being imported", n)
	}
	if n := f.count("SELECT megagroup AND NOT broadcast AND `date` = 1000 FROM channels WHERE id = ?", id); n != 1 {
		t.Error("the channel is no supergroup dated as its history")
	}

	voice := imported(2, 102, carol, 3000)
	voice.Message = mtproto.MakeTLMessage(&mtproto.Message{Date: 3000, MediaUnread: true, Out: true,
		Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: mtproto.MakeTLDocument(&mtproto.Document{
			Id: 1, MimeType: "audio/ogg",
			Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration: 3}).To_DocumentAttribute()},
		}).To_Document()}).To_MessageMedia()}).To_Message()
	voice.ReplyToSourceId, voice.Mentioned = 101, []int64{creator, carol}
	if st, err = f.importBatch(st.Id, 1,
		imported(1, 101, bob, 2000),
		voice,
		&channel.ImportedMessage{Seq: 3, SourceId: 103, SkipReason: "unsupported", Message: mtproto.MakeTLMessageEmpty(nil).To_Message()},
	); err != nil {
		t.Fatal(err)
	}
	if st.NextSeq != 4 || st.LastSourceId != 103 || st.TopMessage != 3 || st.Messages != 3 || st.Skipped != 1 {
		t.Fatalf("after the first batch: %v", st)
	}

	// The second batch: a reply into the thread, a pin, a poll with a
	// vote, reactions, a reply to a record that was not imported.
	threaded := imported(4, 104, creator, 4000)
	threaded.ReplyToSourceId = 102
	threaded.Message.EditDate = wrapperspb.Int32(4500)
	pin := &channel.ImportedMessage{Seq: 5, SourceId: 105, SenderId: bob, ReplyToSourceId: 101,
		Message: mtproto.MakeTLMessageService(&mtproto.Message{Date: 5000, Action: mtproto.MakeTLMessageActionPinMessage(nil).To_MessageAction()}).To_Message()}
	text := func(s string) *mtproto.TextWithEntities {
		return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: s, Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
	}
	checked, err := polls.Check(mtproto.MakeTLInputMediaPoll(&mtproto.InputMedia{Poll: mtproto.MakeTLPoll(&mtproto.Poll{
		Question_TEXTWITHENTITIES: text("Where?"),
		Answers: []*mtproto.PollAnswer{
			mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: text("Here")}).To_PollAnswer(),
			mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: text("There")}).To_PollAnswer(),
		},
	}).To_Poll()}).To_InputMedia(), 6000)
	if err != nil {
		t.Fatal(err)
	}
	pollId := itId()
	poll := imported(6, 106, carol, 6000)
	poll.Message.Message, poll.Message.Media = "", polls.NewMedia(checked, nil, nil)
	poll.PollId, poll.PollVoterId, poll.PollChosen = pollId, creator, []int32{1}
	reacted := imported(7, 107, bob, 7000)
	reacted.Reactions = []*channel.ImportedReaction{
		{UserId: carol, Reaction: "e\U0001F44D", Date: 7100}, {UserId: creator, Reaction: "e❤", Date: 7200}, {UserId: carol, Reaction: "e❤", Date: 7300},
	}
	orphan := imported(8, 108, carol, 8000)
	orphan.ReplyToSourceId = 103
	second := []*channel.ImportedMessage{threaded, pin, poll, reacted, orphan}
	if st, err = f.importBatch(st.Id, 4, second...); err != nil {
		t.Fatal(err)
	}
	if st.NextSeq != 9 || st.TopMessage != 8 || st.Messages != 8 || st.Skipped != 1 {
		t.Fatalf("after the second batch: %v", st)
	}

	// One pts and one journal row a message, without a gap.
	rep, err := channelbox.CheckJournal(context.Background(), f.box, id, channelbox.BasePts)
	if err != nil || !rep.OK() || rep.Rows != 8 || rep.State.Pts != channelbox.BasePts+8 || rep.State.LastMessageId != 8 {
		t.Errorf("the channel's journal: %+v, %v", rep, err)
	}
	// The messages as SendChannel writes them, with their own dates.
	bobs := f.stored(id, 2)
	if bobs.GetDate() != 2000 || bobs.GetFromId().GetUserId() != bob || bobs.GetPeerId().GetChannelId() != id || bobs.GetOut() || bobs.GetReplyTo() != nil {
		t.Errorf("bob's message: %v", bobs)
	}
	v := f.stored(id, 3)
	if v.GetReplyTo().GetReplyToMsgId() != 2 || v.GetReplyTo().GetReplyToTopId() != nil || v.GetMediaUnread() || v.GetOut() || v.GetFromId().GetUserId() != carol {
		t.Errorf("the voice reply: %v", v)
	}
	th := f.stored(id, 4)
	if th.GetReplyTo().GetReplyToMsgId() != 3 || th.GetReplyTo().GetReplyToTopId().GetValue() != 2 || th.GetEditDate().GetValue() != 4500 {
		t.Errorf("a reply in the thread: %v", th)
	}
	if p := f.stored(id, 5); p.GetPredicateName() != mtproto.Predicate_messageService || p.GetReplyTo().GetReplyToMsgId() != 2 || p.GetFromId().GetUserId() != bob {
		t.Errorf("the pin: %v", p)
	}
	if o := f.stored(id, 8); o.GetReplyTo() != nil {
		t.Errorf("a reply to a record that was not imported: %v", o.GetReplyTo())
	}
	if f.count("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND date2 <> JSON_EXTRACT(message_data, '$.date')", id) != 0 {
		t.Error("date2 of an imported message is not its date")
	}
	if f.count("SELECT reply_top_id FROM channel_messages WHERE channel_id = ? AND id = 3", id) != 2 ||
		f.count("SELECT reply_top_id FROM channel_messages WHERE channel_id = ? AND id = 4", id) != 2 ||
		f.count("SELECT reply_top_id FROM channel_messages WHERE channel_id = ? AND id = 5", id) != 0 {
		t.Error("the threads of the replies")
	}
	// The mentions are read from the start: the user mentioned by name and
	// the author replied to, never the sender itself.
	if f.count("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ?", id) != 3 ||
		f.count("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND `read` = 0", id) != 0 ||
		f.count("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND message_id = 3 AND user_id IN (?, ?)", id, creator, bob) != 2 ||
		f.count("SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND message_id = 3", id, carol) != 0 {
		t.Error("the mentions of the imported messages")
	}
	if f.count("SELECT COUNT(*) FROM channel_message_reactions WHERE channel_id = ? AND message_id = 7 AND unread = 0", id) != 3 ||
		f.count("SELECT `date` FROM channel_message_reactions WHERE channel_id = ? AND message_id = 7 AND user_id = ? AND reaction = ?", id, creator, "e❤") != 7200 ||
		f.count("SELECT chosen_order FROM channel_message_reactions WHERE channel_id = ? AND message_id = 7 AND user_id = ? AND reaction = ?", id, carol, "e❤") != 1 {
		t.Error("the reactions of the imported message")
	}
	if f.count("SELECT COUNT(*) FROM polls WHERE id = ? AND peer_type = ? AND peer_id = ? AND message_id = 6 AND creator_user_id = ? AND `date` = 6000", pollId, mtproto.PEER_CHANNEL, id, carol) != 1 ||
		f.count("SELECT COUNT(*) FROM poll_votes WHERE poll_id = ? AND user_id = ? AND `date` = 6000", pollId, creator) != 1 {
		t.Error("the poll of the imported message")
	}
	if stored := f.stored(id, 6).GetMedia().GetPoll(); stored.GetId() != pollId || stored.GetCreator() {
		t.Errorf("the stored poll: %v", stored)
	}
	if f.importedId(st.Id, 104) != 4 || f.importedId(st.Id, 103) != 0 || f.importedId(st.Id, 0) != 1 {
		t.Error("the import's journal")
	}
	if n := f.count("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id IN (?, ?, ?, ?)", id, creator, bob, carol); n != 0 {
		t.Errorf("%d events of an import under way", n)
	}

	// A batch again, a batch ahead, a batch with a fault in its middle:
	// nothing is written.
	before := f.count("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", id)
	if _, err = f.importBatch(st.Id, 4, second...); !errors.Is(err, dao.ErrImportOutOfOrder) {
		t.Errorf("the same batch again: %v", err)
	}
	if _, err = f.importBatch(st.Id, 10, imported(10, 110, bob, 9000)); !errors.Is(err, dao.ErrImportOutOfOrder) {
		t.Errorf("a batch ahead: %v", err)
	}
	if _, err = f.importBatch(st.Id, 9, imported(9, 109, bob, 9000), imported(11, 111, bob, 9000)); !errors.Is(err, dao.ErrImportOutOfOrder) {
		t.Errorf("records not in a row: %v", err)
	}
	if _, err = f.importBatch(st.Id, 9, imported(9, 109, bob, 9000), imported(10, 110, 0, 9000)); !errors.Is(err, dao.ErrImportInvalid) {
		t.Errorf("a record of nobody: %v", err)
	}
	lost := imported(9, 109, bob, 9000)
	lost.ReplyToSourceId = 999
	if _, err = f.importBatch(st.Id, 9, lost); !errors.Is(err, dao.ErrImportInvalid) {
		t.Errorf("a reply to a record the journal does not have: %v", err)
	}
	if _, err = f.importBatch(st.Id, 9, imported(9, 107, bob, 9000)); err == nil {
		t.Error("a source id was journaled twice")
	}
	if after := f.count("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", id); after != before {
		t.Errorf("refused batches wrote %d messages", after-before)
	}
	if rep, err = channelbox.CheckJournal(context.Background(), f.box, id, channelbox.BasePts); err != nil || !rep.OK() || rep.State.LastMessageId != 8 {
		t.Errorf("the journal after refused batches: %+v, %v", rep, err)
	}

	// The end: the creator is in, everything read.
	if st, err = f.c.ChannelImportFinish(&channel.TLChannelImportFinish{ImportId: st.Id}); err != nil || st.State != dao.ImportFinished {
		t.Fatalf("finish: %v, %v", st, err)
	}
	if _, err = f.c.ChannelImportFinish(&channel.TLChannelImportFinish{ImportId: st.Id}); err != nil {
		t.Errorf("finish again: %v", err)
	}
	if _, err = f.importBatch(st.Id, 9, imported(9, 109, bob, 9000)); !errors.Is(err, dao.ErrImportFinished) {
		t.Errorf("a batch after the end: %v", err)
	}
	if f.count("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND user_id = ? AND role = ? AND state = 0 AND admin_rights <> 0", id, creator, channel.RoleCreator) != 1 ||
		f.count("SELECT participants_count = 1 AND admins_count = 1 AND imported_max_id = 8 FROM channels WHERE id = ?", id) != 1 ||
		f.count("SELECT COUNT(*) FROM chat_invites WHERE chat_id = ? AND permanent = 1", id) != 1 {
		t.Error("the channel after the import")
	}
	d := f.dialog(creator, id)
	if d.GetTopMessage() != 8 || d.GetReadInboxMaxId() != 8 || d.GetUnreadCount() != 0 || d.GetUnreadMentionsCount() != 0 || d.GetUnreadReactionsCount() != 0 {
		t.Errorf("the creator's dialog: %v", d)
	}
	if len(f.seqUpdates(creator, mtproto.Predicate_updateChannel)) != 1 {
		t.Error("the creator's devices were not told of the channel")
	}

	// The members come the ordinary way: every imported message is dated
	// before they were added, which is what reads their dialogs up to the
	// end (msg, InvitedToChannel).
	if _, err = f.invite(creator, id, bob, carol); err != nil {
		t.Fatal(err)
	}
	top := f.top(id)
	for _, u := range []int64{bob, carol} {
		if f.invitedEvents(u) != 1 {
			t.Errorf("user %d was not told it was added", u)
		}
		read := f.count("SELECT COALESCE(MAX(m.id), 0) FROM channel_messages m JOIN channel_participants p ON p.channel_id = m.channel_id AND p.user_id = ?"+
			" WHERE m.channel_id = ? AND m.date2 <= p.`date`", u, id)
		if int32(read) != top || top != 9 {
			t.Errorf("user %d is added with the history read up to %d of %d", u, read, top)
		}
		f.exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)",
			u, mtproto.PEER_CHANNEL, id, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, id), top, read, time.Now().Unix())
		if d = f.dialog(u, id); d.GetUnreadCount() != 0 || d.GetUnreadMentionsCount() != 0 || d.GetUnreadReactionsCount() != 0 {
			t.Errorf("the dialog of user %d: %v", u, d)
		}
	}
	// The authors see their old messages read by the others.
	if d = f.dialog(creator, id); d.GetReadOutboxMaxId() != top {
		t.Errorf("the creator's outbox is read up to %d of %d", d.GetReadOutboxMaxId(), top)
	}
	// The imported voice message is played for everyone, one who joins
	// later too; a voice message after the import is not.
	if _, err = f.invite(creator, id, dave); err != nil {
		t.Fatal(err)
	}
	later, _ := f.voicePost(id, creator)
	for _, u := range []int64{creator, bob, dave} {
		if h, g := f.unreadIn(u, id, 3); h || g {
			t.Errorf("the imported voice message is unread for %d: in the history %v, in getMessages %v", u, h, g)
		}
	}
	for _, u := range []int64{bob, dave} {
		if h, g := f.unreadIn(u, id, later); !h || !g {
			t.Errorf("a voice message after the import is read for %d", u)
		}
	}

	// The history by date goes by the dates the messages had.
	h, err := f.c.ChannelGetHistory(&channel.TLChannelGetHistory{SelfId: bob, ChannelId: id, OffsetDate: 2500, Limit: 10})
	if err != nil || len(h.GetMessages()) == 0 || h.GetMessages()[0].GetId() != 2 {
		t.Errorf("the history before date 2500: %v, %v", h.GetMessages(), err)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestImportAgain: a chat has one import under way, which a second create
// returns, adding to its mapping; an author already decided cannot change,
// nor the creator.
func TestImportAgain(t *testing.T) {
	f := newDiffFixture(t)
	creator, other := f.user("Creator"), f.user("Other")
	chat := itId()
	st, err := f.importCreate(creator, chat, `{"user1":5,"user2":0}`)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.importCreate(creator, chat, `{"user3":7}`)
	if err != nil || again.Id != st.Id || again.ChannelId != st.ChannelId || again.Mapping != `{"user1":5,"user2":0,"user3":7}` {
		t.Fatalf("the second create: %v, %v", again, err)
	}
	if f.count("SELECT COUNT(*) FROM channel_imports WHERE source_chat_id = ?", chat) != 1 || f.count("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", st.ChannelId) != 1 {
		t.Error("the second create wrote")
	}
	if _, err = f.importCreate(creator, chat, `{"user1":6}`); !errors.Is(err, dao.ErrImportMappingChanged) {
		t.Errorf("another account for an author: %v", err)
	}
	if _, err = f.importCreate(creator, chat, `{"user2":9}`); !errors.Is(err, dao.ErrImportMappingChanged) {
		t.Errorf("an account for a skipped author: %v", err)
	}
	if _, err = f.importCreate(other, chat, ""); !errors.Is(err, dao.ErrImportMappingChanged) {
		t.Errorf("another creator: %v", err)
	}
	got, err := f.c.ChannelImportGetState(&channel.TLChannelImportGetState{SourceType: importSource, SourceChatId: chat})
	if err != nil || got.Id != st.Id {
		t.Errorf("the import of the chat: %v, %v", got, err)
	}
	if _, err = f.c.ChannelImportGetState(&channel.TLChannelImportGetState{SourceType: importSource, SourceChatId: itId()}); !errors.Is(err, dao.ErrImportNotFound) {
		t.Errorf("the import of a chat never imported: %v", err)
	}
	// A first message that is no creation of the group is refused.
	bad := &channel.TLChannelImportCreate{CreatorId: creator, SourceType: importSource, SourceChatId: itId(), Title: "t", First: imported(1, 1, creator, 10)}
	if _, err = f.c.ChannelImportCreate(bad); !errors.Is(err, dao.ErrImportInvalid) {
		t.Errorf("a text as the first message: %v", err)
	}
}

// TestImportAbort: an import that is not finished is dropped with its
// channel, and the chat can be imported anew; a finished one stays.
func TestImportAbort(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	chat := itId()
	st, err := f.importCreate(creator, chat, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.importBatch(st.Id, 1, imported(1, 101, creator, 2000)); err != nil {
		t.Fatal(err)
	}
	aborted, err := f.c.ChannelImportAbort(&channel.TLChannelImportAbort{ImportId: st.Id})
	if err != nil || aborted.State != dao.ImportAborted {
		t.Fatalf("abort: %v, %v", aborted, err)
	}
	if f.count("SELECT deleted FROM channels WHERE id = ?", st.ChannelId) != 1 {
		t.Error("the channel of an aborted import is there")
	}
	if _, err = f.c.ChannelImportAbort(&channel.TLChannelImportAbort{ImportId: st.Id}); err != nil {
		t.Errorf("abort again: %v", err)
	}
	if _, err = f.importBatch(st.Id, 2, imported(2, 102, creator, 3000)); !errors.Is(err, dao.ErrImportFinished) {
		t.Errorf("a batch of an aborted import: %v", err)
	}
	if _, err = f.c.ChannelImportFinish(&channel.TLChannelImportFinish{ImportId: st.Id}); !errors.Is(err, dao.ErrImportFinished) {
		t.Errorf("the end of an aborted import: %v", err)
	}
	if _, err = f.c.ChannelImportGetState(&channel.TLChannelImportGetState{SourceType: importSource, SourceChatId: chat}); !errors.Is(err, dao.ErrImportNotFound) {
		t.Errorf("the chat's import after the abort: %v", err)
	}
	anew, err := f.importCreate(creator, chat, "")
	if err != nil || anew.Id == st.Id || anew.ChannelId == st.ChannelId || anew.State != dao.ImportRunning {
		t.Fatalf("the chat imported anew: %v, %v", anew, err)
	}
	if _, err = f.c.ChannelImportFinish(&channel.TLChannelImportFinish{ImportId: anew.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.c.ChannelImportAbort(&channel.TLChannelImportAbort{ImportId: anew.Id}); !errors.Is(err, dao.ErrImportFinished) {
		t.Errorf("the abort of a finished import: %v", err)
	}
	if _, err = f.c.ChannelImportAbort(&channel.TLChannelImportAbort{ImportId: itId()}); !errors.Is(err, dao.ErrImportNotFound) {
		t.Errorf("the abort of no import: %v", err)
	}
}
