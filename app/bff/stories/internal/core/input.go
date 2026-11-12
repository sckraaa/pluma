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
	"slices"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/mediaerr"
	"github.com/teamgram/teamgram-server/pkg/mention"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

const (
	// maxPrivacyIds is the most users and chats the rules of a story name,
	// the bound of account.setPrivacy (ADR 0031).
	maxPrivacyIds = 1000
	// maxEntities bounds the entities of a caption.
	maxEntities = 100
	// maxMentions is the most users a story tells that it mentions them;
	// Telegram's own bound is not known.
	maxMentions = 10
)

// storyMedia makes the media of a story of what the client sends: a photo
// or a video it has just uploaded, or one the server already has. A story
// is a photo or a video and nothing else.
func (c *StoriesCore) storyMedia(in *mtproto.InputMedia) (*mtproto.MessageMedia, error) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputMediaUploadedPhoto:
		photo, err := c.svcCtx.Dao.MediaClient.MediaUploadPhotoFile(c.ctx, &mediapb.TLMediaUploadPhotoFile{
			OwnerId: c.MD.PermAuthKeyId,
			File:    in.GetFile(),
		})
		if err != nil {
			c.Logger.Errorf("stories: upload of a photo: %v", err)
			return nil, mediaerr.ForClient(err)
		}
		return mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: photo}).To_MessageMedia(), nil
	case mtproto.Predicate_inputMediaUploadedDocument:
		// The attributes of the video are the client's and are kept as
		// sent: documentAttributeVideo of a story carries nosound,
		// preload_prefix_size and video_start_ts.
		if !hasVideo(in.GetAttributes()) {
			return nil, mtproto.ErrMediaInvalid
		}
		media, err := c.svcCtx.Dao.MediaClient.MediaUploadedDocumentMedia(c.ctx, &mediapb.TLMediaUploadedDocumentMedia{
			OwnerId: c.MD.PermAuthKeyId,
			Media:   in,
		})
		if err != nil {
			c.Logger.Errorf("stories: upload of a video: %v", err)
			return nil, mediaerr.ForClient(err)
		}
		if media.GetDocument() == nil {
			return nil, mtproto.ErrMediaInvalid
		}
		// A story is never a round or a self-destructing video.
		return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
			Document:          media.GetDocument(),
			Video_FLAGBOOLEAN: true,
		}).To_MessageMedia(), nil
	case mtproto.Predicate_inputMediaPhoto:
		id := in.GetId_INPUTPHOTO()
		photo, err := c.svcCtx.Dao.MediaClient.MediaGetPhoto(c.ctx, &mediapb.TLMediaGetPhoto{PhotoId: id.GetId()})
		if err != nil {
			return nil, err
		}
		if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetAccessHash() != id.GetAccessHash() {
			return nil, mtproto.ErrMediaInvalid
		}
		return mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: photo}).To_MessageMedia(), nil
	case mtproto.Predicate_inputMediaDocument:
		id := in.GetId_INPUTDOCUMENT()
		doc, err := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: id.GetId()})
		if err != nil {
			return nil, err
		}
		if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetAccessHash() != id.GetAccessHash() || !hasVideo(doc.GetAttributes()) {
			return nil, mtproto.ErrMediaInvalid
		}
		return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc, Video_FLAGBOOLEAN: true}).To_MessageMedia(), nil
	case "", mtproto.Predicate_inputMediaEmpty:
		return nil, mtproto.ErrMediaEmpty
	}
	return nil, mtproto.ErrMediaInvalid
}

func hasVideo(attributes []*mtproto.DocumentAttribute) bool {
	for _, a := range attributes {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeVideo {
			return true
		}
	}
	return false
}

// storyMusic reads the track of a story: nil for none
// (inputDocumentEmpty, which the clients send to clear it).
func (c *StoriesCore) storyMusic(in *mtproto.InputDocument) (*mtproto.Document, error) {
	if in.GetPredicateName() != mtproto.Predicate_inputDocument {
		return nil, nil
	}
	doc, err := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: in.GetId()})
	if err != nil {
		return nil, err
	}
	if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetAccessHash() != in.GetAccessHash() {
		return nil, mtproto.ErrMediaInvalid
	}
	return doc, nil
}

// storyAreas checks the areas of a story and gives the input ones the form
// the story keeps: the post of a channel is named by the id of a channel
// the caller reads. A venue chosen from the results of an inline bot
// cannot be read back (the server keeps no such results for a story), so
// its query is refused.
func (c *StoriesCore) storyAreas(in []*mtproto.MediaArea) ([]*mtproto.MediaArea, error) {
	if len(in) > stories.MaxMediaAreas {
		return nil, mtproto.ErrMediaInvalid
	}
	var (
		out       = make([]*mtproto.MediaArea, 0, len(in))
		reactions int
	)
	for _, a := range in {
		if a.GetCoordinates() == nil {
			return nil, mtproto.ErrMediaInvalid
		}
		switch a.GetPredicateName() {
		case mtproto.Predicate_inputMediaAreaChannelPost:
			ch := a.GetChannel()
			if ch.GetPredicateName() != mtproto.Predicate_inputChannel {
				return nil, mtproto.ErrChannelInvalid
			}
			if _, err := c.viewOf(stories.ChannelPeer(ch.GetChannelId())); err != nil {
				return nil, err
			}
			messages, err := c.svcCtx.Dao.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{
				SelfId:    c.MD.UserId,
				ChannelId: ch.GetChannelId(),
				Id:        []int32{a.GetMsgId()},
			})
			if err != nil {
				return nil, err
			}
			if len(messages.GetDatas()) == 0 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
				return nil, mtproto.ErrMessageIdInvalid
			}
			out = append(out, mtproto.MakeTLMediaAreaChannelPost(&mtproto.MediaArea{
				Coordinates: a.GetCoordinates(),
				ChannelId:   ch.GetChannelId(),
				MsgId:       a.GetMsgId(),
			}).To_MediaArea())
		case mtproto.Predicate_inputMediaAreaVenue:
			return nil, mtproto.ErrQueryIdInvalid
		case mtproto.Predicate_mediaAreaSuggestedReaction:
			if reactions++; reactions > stories.MaxSuggestedReactions {
				return nil, mtproto.ErrMediaInvalid
			}
			out = append(out, a)
		case mtproto.Predicate_mediaAreaVenue, mtproto.Predicate_mediaAreaGeoPoint, mtproto.Predicate_mediaAreaUrl,
			mtproto.Predicate_mediaAreaWeather, mtproto.Predicate_mediaAreaStarGift, mtproto.Predicate_mediaAreaChannelPost:
			out = append(out, a)
		default:
			return nil, mtproto.ErrMediaInvalid
		}
	}
	return out, nil
}

// storyCaption checks the caption of a story and its entities, turns the
// mentions by input user into mentions by id, marks the @usernames of
// users as mentions and returns the users the caption mentions, the caller
// aside. The caption is not trimmed: the entities are counted against it
// as sent.
func (c *StoriesCore) storyCaption(caption string, entities []*mtproto.MessageEntity) ([]*mtproto.MessageEntity, []int64, error) {
	units := len(utf16.Encode([]rune(caption)))
	if units > stories.MaxCaption {
		return nil, nil, mtproto.ErrMediaCaptionTooLong
	}
	if len(entities) > maxEntities {
		return nil, nil, mtproto.ErrEntityBoundsInvalid
	}
	var (
		out       = make([]*mtproto.MessageEntity, 0, len(entities))
		mentioned []int64
		marked    = map[int32]bool{}
	)
	add := func(id int64) {
		if id != 0 && id != c.MD.UserId && !slices.Contains(mentioned, id) && len(mentioned) < maxMentions {
			mentioned = append(mentioned, id)
		}
	}
	for _, e := range entities {
		if e.GetOffset() < 0 || e.GetLength() <= 0 || int(e.GetOffset())+int(e.GetLength()) > units {
			return nil, nil, mtproto.ErrEntityBoundsInvalid
		}
		switch e.GetPredicateName() {
		case mtproto.Predicate_inputMessageEntityMentionName:
			id := e.GetUserId_INPUTUSER().GetUserId()
			if e.GetUserId_INPUTUSER().GetPredicateName() == mtproto.Predicate_inputUserSelf {
				id = c.MD.UserId
			}
			if id == 0 {
				continue
			}
			out = append(out, mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{
				Offset:       e.GetOffset(),
				Length:       e.GetLength(),
				UserId_INT64: id,
			}).To_MessageEntity())
			add(id)
		case mtproto.Predicate_messageEntityMentionName:
			out = append(out, e)
			add(e.GetUserId_INT64())
		case mtproto.Predicate_messageEntityMention:
			marked[e.GetOffset()] = true
			out = append(out, e)
		default:
			out = append(out, e)
		}
	}
	tags := mention.GetTags('@', caption, '(', ')')
	if len(tags) == 0 {
		return out, mentioned, nil
	}
	index := mention.EncodeStringToUTF16Index(caption)
	for _, tag := range tags {
		end := tag.Index + len(tag.Tag) + 1
		if tag.Index < 0 || end >= len(index) || end > len(caption) {
			continue
		}
		peer, err := c.svcCtx.Dao.UserResolveUsername(c.ctx, &userpb.TLUserResolveUsername{Username: tag.Tag})
		if err != nil || peer.GetPredicateName() != mtproto.Predicate_peerUser {
			// Not the name of a user: plain text.
			continue
		}
		add(peer.GetUserId())
		offset := int32(index[tag.Index])
		if !marked[offset] {
			marked[offset] = true
			out = append(out, mtproto.MakeTLMessageEntityMention(&mtproto.MessageEntity{
				Offset: offset,
				Length: int32(index[end] - index[tag.Index]),
			}).To_MessageEntity())
		}
	}
	return out, mentioned, nil
}

// storyRules reads who a user's story is for: everyone, the contacts, the
// close friends or the users and the members of the chats it names, each
// but those it keeps out. Rules that let nobody in, a rule stories do not
// have, and too many names are refused.
func storyRules(self int64, in []*mtproto.InputPrivacyRule) ([]*mtproto.PrivacyRule, error) {
	var (
		out    = make([]*mtproto.PrivacyRule, 0, len(in))
		allows bool
		names  int
	)
	for _, r := range in {
		switch r.GetPredicateName() {
		case mtproto.Predicate_inputPrivacyValueAllowAll:
			out = append(out, mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule())
			allows = true
		case mtproto.Predicate_inputPrivacyValueAllowContacts:
			out = append(out, mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule())
			allows = true
		case mtproto.Predicate_inputPrivacyValueAllowCloseFriends:
			out = append(out, mtproto.MakeTLPrivacyValueAllowCloseFriends(nil).To_PrivacyRule())
			allows = true
		case mtproto.Predicate_inputPrivacyValueAllowUsers, mtproto.Predicate_inputPrivacyValueDisallowUsers:
			users := mtproto.ToUserIdListByInput(self, r.GetUsers())
			if len(users) == 0 {
				continue
			}
			names += len(users)
			if r.GetPredicateName() == mtproto.Predicate_inputPrivacyValueAllowUsers {
				out = append(out, mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: users}).To_PrivacyRule())
				allows = true
			} else {
				out = append(out, mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: users}).To_PrivacyRule())
			}
		case mtproto.Predicate_inputPrivacyValueAllowChatParticipants, mtproto.Predicate_inputPrivacyValueDisallowChatParticipants:
			if len(r.GetChats()) == 0 {
				continue
			}
			names += len(r.GetChats())
			if r.GetPredicateName() == mtproto.Predicate_inputPrivacyValueAllowChatParticipants {
				out = append(out, mtproto.MakeTLPrivacyValueAllowChatParticipants(&mtproto.PrivacyRule{Chats: r.GetChats()}).To_PrivacyRule())
				allows = true
			} else {
				out = append(out, mtproto.MakeTLPrivacyValueDisallowChatParticipants(&mtproto.PrivacyRule{Chats: r.GetChats()}).To_PrivacyRule())
			}
		case mtproto.Predicate_inputPrivacyValueDisallowAll:
			// Nobody but those named: the absence of a base rule says it.
		default:
			return nil, mtproto.ErrPrivacyValueInvalid
		}
	}
	if !allows {
		return nil, mtproto.ErrPrivacyValueInvalid
	}
	if names > maxPrivacyIds {
		return nil, mtproto.ErrPrivacyTooLong
	}
	return out, nil
}
