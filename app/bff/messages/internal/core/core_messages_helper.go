// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
//  All rights reserved.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"context"
	"math/rand"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userclient "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/mediaerr"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// doClearDraft clears the draft a sent message came from, of the chat or of
// its topic, and tells the user's other devices (clear_draft of
// messages.sendMessage).
func (c *MessagesCore) doClearDraft(ctx context.Context, userId int64, authKeyId int64, peer *mtproto.PeerUtil, topMsgId int32) {
	var (
		rV  *mtproto.Bool
		err error
	)
	if topMsgId != 0 {
		rV, err = c.svcCtx.Dao.DialogClient.DialogSaveThreadDraft(ctx, &dialog.TLDialogSaveThreadDraft{
			UserId:   userId,
			PeerType: peer.PeerType,
			PeerId:   peer.PeerId,
			TopMsgId: topMsgId,
			Message:  mtproto.MakeTLDraftMessageEmpty(nil).To_DraftMessage(),
		})
	} else {
		rV, err = c.svcCtx.Dao.DialogClient.DialogClearDraftMessage(ctx, &dialog.TLDialogClearDraftMessage{
			UserId:   userId,
			PeerType: peer.PeerType,
			PeerId:   peer.PeerId,
		})
	}
	if err != nil {
		c.Logger.Errorf("clear draft - error: %v", err)
		return
	}
	if !mtproto.FromBool(rV) {
		return
	}
	updateDraftMessage := mtproto.MakeTLUpdateDraftMessage(&mtproto.Update{
		Peer_PEER:          peer.ToPeer(),
		TopMsgId_FLAGINT32: mtproto.MakeFlagsInt32(topMsgId),
		Draft: mtproto.MakeTLDraftMessageEmpty(&mtproto.DraftMessage{
			Date_FLAGINT32: mtproto.MakeFlagsInt32(int32(time.Now().Unix())),
		}).To_DraftMessage(),
	}).To_Update()
	if _, err = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        userId,
		PermAuthKeyId: authKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(updateDraftMessage),
	}); err != nil {
		c.Logger.Errorf("clear draft - error: %v", err)
	}
}

// webPageWait is how long a message with a chosen card waits for it: the
// client asked for it while the user typed, so it is usually stored.
const webPageWait = 8 * time.Second

// errWebpageNotFound is the answer to a chosen card that cannot be made.
var errWebpageNotFound = status.Error(mtproto.ErrBadRequest, "WEBPAGE_NOT_FOUND")

func (c *MessagesCore) makeMediaByInputMedia(media *mtproto.InputMedia) (messageMedia *mtproto.MessageMedia, err error) {
	var (
		now = int32(time.Now().Unix())
	)

	switch media.PredicateName {
	case mtproto.Predicate_inputMediaEmpty:
		// inputMediaEmpty#9664f57f = InputMedia;

		messageMedia = mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaUploadedPhoto:
		// inputMediaUploadedPhoto#1e287d04 flags:#
		//	file:InputFile
		//	stickers:flags.0?Vector<InputDocument>
		//	ttl_seconds:flags.1?int = InputMedia;

		var (
			photo *mtproto.Photo
		)
		photo, err = c.svcCtx.Dao.MediaClient.MediaUploadPhotoFile(c.ctx, &mediapb.TLMediaUploadPhotoFile{
			OwnerId:    c.MD.PermAuthKeyId,
			File:       media.File,
			Stickers:   nil,
			TtlSeconds: nil,
		})
		if err != nil {
			c.Logger.Errorf("UploadPhoto error: %v, by %s", err, media.GetPredicateName())
			return
		}

		messageMedia = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
			Spoiler:         media.Spoiler,
			Photo_FLAGPHOTO: photo,
			TtlSeconds:      media.TtlSeconds,
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaPhoto:
		// inputMediaPhoto#b3ba0635 flags:#
		//	id:InputPhoto
		//	ttl_seconds:flags.0?int = InputMedia;

		mediaPhoto := media.To_InputMediaPhoto()
		sizeList, _ := c.svcCtx.Dao.MediaClient.MediaGetPhotoSizeList(c.ctx, &mediapb.TLMediaGetPhotoSizeList{
			SizeId: mediaPhoto.GetId_INPUTPHOTO().GetId(),
		})

		photo := mtproto.MakeTLPhoto(&mtproto.Photo{
			Id:          mediaPhoto.GetId_INPUTPHOTO().GetId(),
			HasStickers: false,
			AccessHash:  mediaPhoto.GetId_INPUTPHOTO().GetAccessHash(),
			Date:        now,
			Sizes:       sizeList.Sizes,
			DcId:        sizeList.DcId,
		})

		messageMedia = mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
			Spoiler:         mediaPhoto.GetSpoiler(),
			Photo_FLAGPHOTO: photo.To_Photo(),
			TtlSeconds:      mediaPhoto.GetTtlSeconds(),
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaGeoPoint:
		// inputMediaGeoPoint#f9c44144 geo_point:InputGeoPoint = InputMedia;

		messageMedia = mtproto.MakeTLMessageMediaGeo(&mtproto.MessageMedia{
			Geo: mtproto.MakeGeoPointByInput(media.To_InputMediaGeoPoint().GetGeoPoint()),
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaContact:
		// inputMediaContact#f8ab7dfb
		//	phone_number:string
		//	first_name:string
		//	last_name:string
		//	vcard:string = InputMedia;

		contact := media.To_InputMediaContact()

		messageMedia = mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{
			PhoneNumber: contact.GetPhoneNumber(),
			FirstName:   contact.GetFirstName(),
			LastName:    contact.GetLastName(),
			Vcard:       contact.GetVcard(),
			UserId:      0,
		}).To_MessageMedia()

		if phoneNumber, err2 := phonenumber.CheckAndGetPhoneNumber(contact.GetPhoneNumber()); err2 == nil {
			if messageMedia.UserId, err = userclient.ContactCardUserId(c.ctx, c.svcCtx.Dao.UserClient, c.MD.UserId, phoneNumber); err != nil {
				return
			}
		}
	case mtproto.Predicate_inputMediaUploadedDocument:
		// inputMediaUploadedDocument#5b38c6c1 flags:#
		//	nosound_video:flags.3?true
		//	force_file:flags.4?true
		//	file:InputFile
		//	thumb:flags.2?InputFile
		//	mime_type:string
		//	attributes:Vector<DocumentAttribute>
		//	stickers:flags.0?Vector<InputDocument>
		//	ttl_seconds:flags.1?int = InputMedia;
		documentMedia, err2 := c.svcCtx.Dao.MediaClient.MediaUploadedDocumentMedia(c.ctx, &mediapb.TLMediaUploadedDocumentMedia{
			OwnerId: c.MD.PermAuthKeyId,
			Media:   media,
		})
		if err2 != nil {
			err = mediaerr.ForClient(err2)
			return
		}
		messageMedia = documentMedia
	case mtproto.Predicate_inputMediaDocument:
		// inputMediaDocument#33473058 flags:#
		//	id:InputDocument
		//	ttl_seconds:flags.0?int
		//	query:flags.1?string = InputMedia;

		id := media.To_InputMediaDocument().GetId_INPUTDOCUMENT()
		document3, _ := c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{
			Id: id.GetId(),
		})

		// messageMediaDocument#7c4414d3 flags:# document:flags.0?Document caption:flags.1?string ttl_seconds:flags.2?int = MessageMedia;
		messageMedia = mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
			Spoiler:  media.To_InputMediaDocument().GetSpoiler(),
			Document: document3,
			// Caption:    media.To_InputMediaDocument().GetCaption(),
			TtlSeconds: media.To_InputMediaDocument().GetTtlSeconds(),
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaVenue:
		// inputMediaVenue#c13d1c11
		//	geo_point:InputGeoPoint
		//	title:string
		//	address:string
		//	provider:string
		//	venue_id:string
		//	venue_type:string = InputMedia;
		venue := media.To_InputMediaVenue()

		// messageMediaVenue#2ec0533f geo:GeoPoint title:string address:string provider:string venue_id:string venue_type:string = MessageMedia;
		messageMedia = mtproto.MakeTLMessageMediaVenue(&mtproto.MessageMedia{
			Geo:       mtproto.MakeGeoPointByInput(venue.GetGeoPoint()),
			Title:     venue.GetTitle(),
			Address:   venue.GetAddress(),
			Provider:  venue.GetProvider_STRING(),
			VenueId:   venue.GetVenueId(),
			VenueType: venue.GetVenueType(),
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaPhotoExternal:
		// inputMediaPhotoExternal#e5bbfe1a flags:# url:string ttl_seconds:flags.0?int = InputMedia;

		messageMedia = mtproto.MakeTLMessageMediaUnsupported(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaDocumentExternal:
		// TODO(@benqi): MessageMedia???
		// inputMediaDocumentExternal#fb52dc99 flags:# url:string ttl_seconds:flags.0?int = InputMedia;
		messageMedia = mtproto.MakeTLMessageMediaUnsupported(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaGame:
		// inputMediaGame#d33f43f3 id:InputGame = InputMedia;

		// TODO(@benqi): Not impl inputMediaGame
		messageMedia = mtproto.MakeTLMessageMediaUnsupported(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaInvoice:
		// inputMediaInvoice#d9799874 flags:# title:string description:string photo:flags.0?InputWebDocument invoice:Invoice payload:bytes provider:string provider_data:DataJSON start_param:flags.1?string = InputMedia;

		// TODO(@benqi): Not impl inputMediaGame
		messageMedia = mtproto.MakeTLMessageMediaUnsupported(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaGeoLive:
		// inputMediaGeoLive#971fa843 flags:# stopped:flags.0?true geo_point:InputGeoPoint heading:flags.2?int period:flags.1?int proximity_notification_radius:flags.3?int = InputMedia;

		messageMedia = mtproto.MakeTLMessageMediaGeoLive(&mtproto.MessageMedia{
			Geo:    mtproto.MakeGeoPointByInput(media.To_InputMediaGeoLive().GetGeoPoint()),
			Period: media.To_InputMediaGeoLive().GetPeriod().GetValue(),
		}).To_MessageMedia()
	case mtproto.Predicate_inputMediaPoll:
		// inputMediaPoll#f94e5f1 flags:# poll:Poll correct_answers:flags.0?Vector<bytes> solution:flags.1?string solution_entities:flags.1?Vector<MessageEntity> = InputMedia;

		// pluma: checked here; msg stores the poll in the send transaction
		// (task 062, pkg/polls).
		messageMedia, err = c.makePollMedia(media)
	case mtproto.Predicate_inputMediaDice:
		// inputMediaDice#e66fbf7b emoticon:string = InputMedia;

		if media.Emoticon == "🎲" {
			messageMedia = mtproto.MakeTLMessageMediaDice(&mtproto.MessageMedia{
				Value:    rand.Int31()%6 + 1,
				Emoticon: media.Emoticon,
			}).To_MessageMedia()
		} else if media.Emoticon == "🎯" {
			messageMedia = mtproto.MakeTLMessageMediaDice(&mtproto.MessageMedia{
				Value:    rand.Int31()%6 + 1,
				Emoticon: media.Emoticon,
			}).To_MessageMedia()
		} else if media.Emoticon == "🏀" {
			messageMedia = mtproto.MakeTLMessageMediaDice(&mtproto.MessageMedia{
				Value:    rand.Int31()%5 + 1,
				Emoticon: media.Emoticon,
			}).To_MessageMedia()
		} else {
			messageMedia = mtproto.MakeTLMessageMediaDice(&mtproto.MessageMedia{
				Value:    rand.Int31()%6 + 1,
				Emoticon: media.Emoticon,
			}).To_MessageMedia()
		}
	case mtproto.Predicate_inputMediaStory:
		// inputMediaStory#89fdd778 peer:InputPeer id:int = InputMedia;
		//
		// pluma: a story shared to a chat (task 084). The message names
		// the story and carries no copy of it: each reader is shown what
		// the story lets it see, which the clients ask by its id (Android
		// StoriesStorage.java:559-641, fillMessagesWithStories).
		if messageMedia, err = c.storyMedia(media); err != nil {
			return
		}
	case mtproto.Predicate_inputMediaWebPage:
		// inputMediaWebPage#c21b8849 flags:# force_large_media:flags.0?true force_small_media:flags.1?true optional:flags.2?true url:string = InputMedia;
		//
		// pluma: the card the user chose while typing, with the size of its
		// picture (task 023): manual, so the message keeps it. A link
		// without a card is WEBPAGE_NOT_FOUND, or plain text if optional
		// (core.telegram.org/constructor/inputMediaWebPage).
		ctx, cancel := context.WithTimeout(c.ctx, webPageWait)
		page, perr := c.svcCtx.WebPages.Preview(ctx, media.GetUrl())
		cancel()
		switch {
		case perr == nil && page.GetPredicateName() == mtproto.Predicate_webPage:
			messageMedia = mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
				ForceLargeMedia: media.GetForceLargeMedia(),
				ForceSmallMedia: media.GetForceSmallMedia(),
				Manual:          true,
				Webpage:         page,
			}).To_MessageMedia()
		case media.GetOptional():
			messageMedia = mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia()
		default:
			err = errWebpageNotFound
		}
	case mtproto.Predicate_inputMediaPaidMedia:
		// inputMediaPaidMedia#c4103386 flags:# stars_amount:long extended_media:Vector<InputMedia> payload:flags.0?string = InputMedia;

		messageMedia = mtproto.MakeTLMessageMediaUnsupported(nil).To_MessageMedia()
	case mtproto.Predicate_inputMediaTodo:
		// inputMediaTodo#9fc55fde todo:TodoList = InputMedia;

		if err = polls.CheckTodo(media.GetTodo()); err == nil {
			messageMedia = polls.TodoMedia(media.GetTodo(), nil)
		}
	default:
		err = mtproto.ErrMediaInvalid
	}

	return
}

//// TODO(@benqi): mention...
//func (c *MessagesCore) fixMessageEntities(fromId int64, peer *mtproto.PeerUtil, noWebpage bool, message *mtproto.Message, hasBot func() bool) (*mtproto.Message, error) {
//	var (
//		entities mtproto.MessageEntitySlice
//		idxList  []int
//	)
//
//	/*
//		fmt.Println(*update.ChannelPost.Entities)
//		// For each entity
//		for _, e := range *update.ChannelPost.Entities {
//			// Get the whole update Text
//			str := update.ChannelPost.Text
//			// Encode it into utf16
//			utfEncodedString := utf16.Encode([]rune(str))
//			// Decode just the piece of string I need
//			runeString := utf16.Decode(utfEncodedString[e.Offset : e.Offset+e.Length])
//			// Transform []rune into string
//			str = string(runeString)
//			fmt.Println(str)
//		}
//	*/
//	getIdxList := func() []int {
//		if len(idxList) == 0 {
//			idxList = mention.EncodeStringToUTF16Index(message.Message)
//		}
//		return idxList
//	}
//
//	var firstUrl string
//	rIndexes := xurls.Relaxed().FindAllStringIndex(message.Message, -1)
//	if len(rIndexes) > 0 {
//		if len(idxList) == 0 {
//			getIdxList()
//		}
//		for idx, v := range rIndexes {
//			if idx == 0 {
//				firstUrl = message.Message[v[0]:v[1]]
//			}
//			entityUrl := mtproto.MakeTLMessageEntityUrl(&mtproto.MessageEntity{
//				Offset: int32(idxList[v[0]]),
//				Length: int32(idxList[v[1]] - idxList[v[0]]),
//			})
//			entities = append(entities, entityUrl.To_MessageEntity())
//		}
//	}
//
//	if !noWebpage && firstUrl != "" {
//		// TODO(@benqi): disable
//		if c.svcCtx.Plugin != nil {
//			ctx, _ := metadata.RpcMetadataToOutgoing(c.ctx, c.MD)
//			webpage, _ := c.svcCtx.Plugin.GetWebpagePreview(ctx, firstUrl)
//			if webpage != nil {
//				message.Media = mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{
//					Webpage: webpage,
//				}).To_MessageMedia()
//			}
//		}
//	}
//
//	for _, entity := range message.Entities {
//		switch entity.PredicateName {
//		case mtproto.Predicate_inputMessageEntityMentionName:
//			if entity.GetUserId_INPUTUSER().GetPredicateName() == mtproto.Predicate_inputUserSelf {
//				entity.UserId_INPUTUSER.UserId = fromId
//			}
//
//			if entity.UserId_INPUTUSER.UserId != 0 {
//				// TODO(@benqi): check user_id
//				entityMentionName := mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{
//					Offset:       entity.Offset,
//					Length:       entity.Length,
//					UserId_INT64: entity.UserId_INPUTUSER.UserId,
//				})
//
//				entities = append(entities, entityMentionName.To_MessageEntity())
//			}
//			// }
//		default:
//			entities = append(entities, entity)
//		}
//	}
//
//	tags := mention.GetTags('@', message.Message, '(', ')')
//	if len(tags) > 0 {
//		var nameList = make([]string, 0, len(tags))
//		for _, tag := range tags {
//			nameList = append(nameList, tag.Tag)
//		}
//		//names, _ := c.svcCtx.Dao.UsernameClient.UsernameGetListByUsernameList(c.ctx, &username.TLUsernameGetListByUsernameList{
//		//	Names:                nameList,
//		//})
//		// c.Logger.Debugf("nameList: %v", names)
//
//		for _, tag := range tags {
//			if len(idxList) == 0 {
//				getIdxList()
//			}
//			mention2 := mtproto.MakeTLMessageEntityMention(&mtproto.MessageEntity{
//				Offset: int32(idxList[tag.Index]),
//				Length: int32(idxList[tag.Index+len(tag.Tag)+1] - idxList[tag.Index]),
//			}).To_MessageEntity()
//
//			// stole field UserId_5
//			if v, _ := c.svcCtx.Dao.UsernameClient.UsernameResolveUsername(c.ctx, &username.TLUsernameResolveUsername{
//				Username: tag.Tag,
//			}); v != nil {
//				if v.GetPredicateName() == mtproto.Predicate_peerUser {
//					mention2.UserId_INT64 = v.UserId
//				}
//			}
//			//for _, v := range names.Datas {
//			//	//
//			//	if uname, ok := names[tag.Tag]; ok {
//			//		if uname.PeerType == model.PEER_USER {
//			//			mention2.UserId_INT32 = uname.PeerId
//			//		}
//			//	}
//			//}
//			entities = append(entities, mention2)
//			c.Logger.Infof("mention2: %v", mention2)
//		}
//	}
//
//	tags = mention.GetTags('#', message.Message)
//	for _, tag := range tags {
//		if len(idxList) == 0 {
//			getIdxList()
//		}
//		hashtag := mtproto.MakeTLMessageEntityHashtag(&mtproto.MessageEntity{
//			Offset: int32(idxList[tag.Index]),
//			Length: int32(idxList[tag.Index+len(tag.Tag)+1] - idxList[tag.Index]),
//			Url:    "#" + tag.Tag, // NOTE: hack, steal url field
//		}).To_MessageEntity()
//		entities = append(entities, hashtag)
//	}
//
//	if hasBot != nil && hasBot() {
//		tags = mention.GetTags('/', message.Message)
//		for _, tag := range tags {
//			if len(idxList) == 0 {
//				getIdxList()
//			}
//			hashtag := mtproto.MakeTLMessageEntityBotCommand(&mtproto.MessageEntity{
//				Offset: int32(idxList[tag.Index]),
//				Length: int32(idxList[tag.Index+len(tag.Tag)+1] - idxList[tag.Index]),
//			}).To_MessageEntity()
//			entities = append(entities, hashtag)
//		}
//	}
//
//	sort.Sort(entities)
//	message.Entities = entities
//	return message, nil
//}

// withRepeatPeriod gives m the schedule_repeat_period of a request that
// schedules it (task 067); a send without a date has no use for it.
func withRepeatPeriod(m *mtproto.Message, date, period *wrapperspb.Int32Value) *mtproto.Message {
	if date != nil {
		m.ScheduleRepeatPeriod = period
	}
	return m
}
