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

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/tgexport"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// importIds issues the ids of the polls of imported messages (idgen).
type importIds interface {
	NextId(ctx context.Context) (int64, error)
}

// importBuilder makes the records of an export into what
// channel.importMessages writes (ADR 0051, p. 4): the message as a client
// of its author would have sent it, with the date it had.
type importBuilder struct {
	mapping  *tgexport.Mapping
	resolver *tgexport.Resolver
	files    *importFiles
	ids      importIds
}

// skipped is the record of an item that is not imported.
func skipped(it *tgexport.Item) *channel.ImportedMessage {
	return channel.MakeTLImportedMessage(&channel.ImportedMessage{
		Seq: it.Rec.Seq, SourceId: it.Rec.Id, SkipReason: it.Skip,
		Message: mtproto.MakeTLMessageEmpty(nil).To_Message(),
	}).To_ImportedMessage()
}

// build makes the record of it, uploading its file.
func (b *importBuilder) build(ctx context.Context, it *tgexport.Item) (*channel.ImportedMessage, error) {
	if it.Skip != "" {
		return skipped(it), nil
	}
	rec := it.Rec
	im := &channel.ImportedMessage{Seq: rec.Seq, SourceId: rec.Id, SenderId: it.Sender, ReplyToSourceId: it.ReplyTo}
	date := int32(rec.Date)
	if rec.Kind == tgexport.KindService {
		action, err := b.action(ctx, it)
		if err != nil {
			return nil, err
		}
		im.Message = mtproto.MakeTLMessageService(&mtproto.Message{Date: date, Action: action}).To_Message()
		return channel.MakeTLImportedMessage(im).To_ImportedMessage(), nil
	}

	text, entities, mentioned := tgexport.Entities(rec.Text, b.resolver.User)
	m := &mtproto.Message{Date: date, Message: text, Entities: entities}
	if rec.Edited > 0 {
		m.EditDate = wrapperspb.Int32(int32(rec.Edited))
	}
	im.Mentioned = mentioned
	switch {
	case it.ForwardUser != 0:
		m.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromId: mtproto.MakePeerUser(it.ForwardUser), Date: date}).To_MessageFwdHeader()
	case it.ForwardName != "":
		m.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{FromName: wrapperspb.String(it.ForwardName), Date: date}).To_MessageFwdHeader()
	}
	if media := rec.Media; media != nil {
		var err error
		switch {
		case media.Type.HasFile():
			if it.File != nil {
				m.Media, err = b.files.media(ctx, it.File)
			}
		case media.Type == tgexport.MediaContact:
			m.Media = mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{
				PhoneNumber: digits(media.Contact.Phone), FirstName: media.Contact.FirstName, LastName: media.Contact.LastName,
			}).To_MessageMedia()
		case media.Type == tgexport.MediaGeo:
			m.Media = mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{Geo: geoPoint(media.Geo)}).To_MessageMedia()
		case media.Type == tgexport.MediaVenue:
			m.Media = mtproto.MakeTLMessageMediaVenue(&mtproto.MessageMedia{
				Geo: geoPoint(&media.Venue.Geo), Title: media.Venue.Title, Address: media.Venue.Address,
			}).To_MessageMedia()
		case media.Type == tgexport.MediaPoll:
			err = b.poll(ctx, im, m, media.Poll)
		case media.Type == tgexport.MediaTodo:
			m.Media, err = todoMedia(media.Todo)
		}
		if err != nil {
			return nil, err
		}
	}
	for _, r := range it.Reactions {
		im.Reactions = append(im.Reactions, channel.MakeTLImportedReaction(&channel.ImportedReaction{
			UserId: r.User, Reaction: "e" + r.Emoji, Date: r.Date,
		}).To_ImportedReaction())
	}
	im.Message = mtproto.MakeTLMessage(m).To_Message()
	return channel.MakeTLImportedMessage(im).To_ImportedMessage(), nil
}

// digits are the digits of a phone number the export wrote for people.
func digits(phone string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
}

func geoPoint(g *tgexport.Geo) *mtproto.GeoPoint {
	if g == nil || g.Lat == 0 && g.Long == 0 {
		return mtproto.MakeTLGeoPointEmpty(nil).To_GeoPoint()
	}
	return mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: g.Lat, Long: g.Long}).To_GeoPoint()
}

func plainText(parts []tgexport.TextPart) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: tgexport.Plain(parts), Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

// poll makes the poll of a message as messages.sendMedia makes a new one
// (polls.Check, polls.NewMedia), which the channel service stores with the
// message. The export tells neither its kind nor who voted, except the
// answers its maker chose: those are the votes of the mapping's exporter.
func (b *importBuilder) poll(ctx context.Context, im *channel.ImportedMessage, m *mtproto.Message, p *tgexport.Poll) error {
	answers := make([]*mtproto.PollAnswer, 0, len(p.Answers))
	for _, a := range p.Answers {
		answers = append(answers, mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: plainText(a.Text)}).To_PollAnswer())
	}
	checked, err := polls.Check(mtproto.MakeTLInputMediaPoll(&mtproto.InputMedia{
		Poll: mtproto.MakeTLPoll(&mtproto.Poll{Closed: p.Closed, Question_TEXTWITHENTITIES: plainText(p.Question), Answers: answers}).To_Poll(),
	}).To_InputMedia(), int64(m.Date))
	if err != nil {
		return fmt.Errorf("record %d: poll: %w", im.Seq, err)
	}
	m.Media = polls.NewMedia(checked, nil, nil)
	if im.PollId, err = b.ids.NextId(ctx); err != nil {
		return err
	}
	if b.mapping.Exporter > 0 {
		for i, a := range p.Answers {
			if a.Chosen {
				im.PollVoterId = b.mapping.Exporter
				im.PollChosen = append(im.PollChosen, int32(i))
			}
		}
	}
	return nil
}

func todoMedia(t *tgexport.Todo) (*mtproto.MessageMedia, error) {
	list := &mtproto.TodoList{OthersCanAppend: t.OthersCanAppend, OthersCanComplete: t.OthersCanComplete, Title: plainText(t.Title)}
	for _, i := range t.Items {
		list.List = append(list.List, mtproto.MakeTLTodoItem(&mtproto.TodoItem{Id: i.Id, Title: plainText(i.Text)}).To_TodoItem())
	}
	made := mtproto.MakeTLTodoList(list).To_TodoList()
	if err := polls.CheckTodo(made); err != nil {
		return nil, err
	}
	return polls.TodoMedia(made, nil), nil
}

// action is the action of a service message, by the Pluma accounts the
// resolver found for the people it names.
func (b *importBuilder) action(ctx context.Context, it *tgexport.Item) (*mtproto.MessageAction, error) {
	a := it.Rec.Action
	switch a.Name {
	case "create_group", "create_channel":
		return mtproto.MakeTLMessageActionChannelCreate(&mtproto.MessageAction{Title_STRING: a.Title}).To_MessageAction(), nil
	case "edit_group_title":
		return mtproto.MakeTLMessageActionChatEditTitle(&mtproto.MessageAction{Title_STRING: a.Title}).To_MessageAction(), nil
	case "edit_group_photo":
		if it.File == nil {
			return nil, errors.New("a group photo without its file")
		}
		media, err := b.files.media(ctx, it.File)
		if err != nil {
			return nil, err
		}
		return mtproto.MakeTLMessageActionChatEditPhoto(&mtproto.MessageAction{Photo: media.GetPhoto_FLAGPHOTO()}).To_MessageAction(), nil
	case "delete_group_photo":
		return mtproto.MakeTLMessageActionChatDeletePhoto(nil).To_MessageAction(), nil
	case "invite_members":
		return mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: it.ActionUsers}).To_MessageAction(), nil
	case "remove_members":
		return mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: it.ActionUsers[0]}).To_MessageAction(), nil
	case "join_group_by_link":
		return mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{InviterId: it.ActionUsers[0]}).To_MessageAction(), nil
	case "join_group_by_request":
		return mtproto.MakeTLMessageActionChatJoinedByRequest(nil).To_MessageAction(), nil
	case "pin_message":
		return mtproto.MakeTLMessageActionPinMessage(nil).To_MessageAction(), nil
	}
	return nil, fmt.Errorf("record %d: action %s is not imported", it.Rec.Seq, a.Name)
}

// creationMessage is the first message of an imported group when the
// export does not begin with one: the creator made the group, at the time
// the export begins.
func creationMessage(creator int64, title string, date int64) *channel.ImportedMessage {
	return channel.MakeTLImportedMessage(&channel.ImportedMessage{
		SenderId: creator,
		Message: mtproto.MakeTLMessageService(&mtproto.Message{
			Date:   int32(date),
			Action: mtproto.MakeTLMessageActionChannelCreate(&mtproto.MessageAction{Title_STRING: title}).To_MessageAction(),
		}).To_Message(),
	}).To_ImportedMessage()
}
