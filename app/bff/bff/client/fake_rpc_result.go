// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package bff_proxy_client

import (
	"context"
	"reflect"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
)

func (c *BFFProxyClient) TryReturnFakeRpcResult(ctx context.Context, object mtproto.TLObject) (mtproto.TLObject, error) {
	rt := reflect.TypeOf(object)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	switch rt.Name() {
	// langpack
	case "TLLangpackGetDifference":
		in := object.(*mtproto.TLLangpackGetDifference)
		return mtproto.MakeTLLangPackDifference(&mtproto.LangPackDifference{
			LangCode:    in.GetLangCode(),
			FromVersion: in.GetFromVersion(),
			Version:     in.GetFromVersion(),
			Strings:     []*mtproto.LangPackString{},
		}).To_LangPackDifference(), nil
	case "TLLangpackGetLangPack":
		in := object.(*mtproto.TLLangpackGetLangPack)
		return mtproto.MakeTLLangPackDifference(&mtproto.LangPackDifference{
			LangCode:    in.GetLangCode(),
			FromVersion: 0,
			Version:     0,
			Strings:     []*mtproto.LangPackString{},
		}).To_LangPackDifference(), nil
	case "TLLangpackGetLanguages":
		return &mtproto.Vector_LangPackLanguage{
			Datas: []*mtproto.LangPackLanguage{},
		}, nil
	case "TLLangpackGetStrings":
		return &mtproto.Vector_LangPackString{
			Datas: []*mtproto.LangPackString{},
		}, nil

	// wallpaper
	case "TLAccountGetWallPapers":
		return mtproto.MakeTLAccountWallPapers(&mtproto.Account_WallPapers{
			Hash:       0,
			Wallpapers: []*mtproto.WallPaper{},
		}).To_Account_WallPapers(), nil

	// tos
	case "TLHelpAcceptTermsOfService":
		return mtproto.BoolTrue, nil
	case "TLHelpGetTermsOfServiceUpdate":
		return mtproto.MakeTLHelpTermsOfServiceUpdateEmpty(&mtproto.Help_TermsOfServiceUpdate{
			Expires: int32(time.Now().Unix() + 3600),
		}).To_Help_TermsOfServiceUpdate(), nil

	// themes
	case "TLAccountGetThemes":
		return mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{
			Hash:   0,
			Themes: []*mtproto.Theme{},
		}).To_Account_Themes(), nil
	case "TLAccountGetChatThemes":
		return mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{
			Hash:   0,
			Themes: []*mtproto.Theme{},
		}).To_Account_Themes(), nil

	// stickers
	case "TLMessagesGetAllStickers":
		return mtproto.MakeTLMessagesAllStickers(&mtproto.Messages_AllStickers{
			Hash: 0,
			Sets: []*mtproto.StickerSet{},
		}).To_Messages_AllStickers(), nil
	case "TLMessagesGetArchivedStickers":
		return mtproto.MakeTLMessagesArchivedStickers(&mtproto.Messages_ArchivedStickers{
			Count: 0,
			Sets:  []*mtproto.StickerSetCovered{},
		}).To_Messages_ArchivedStickers(), nil
	case "TLMessagesGetFavedStickers":
		return mtproto.MakeTLMessagesFavedStickers(&mtproto.Messages_FavedStickers{
			Hash:     0,
			Packs:    []*mtproto.StickerPack{},
			Stickers: []*mtproto.Document{},
		}).To_Messages_FavedStickers(), nil
	case "TLMessagesGetMaskStickers":
		fallthrough
	case "TLMessagesGetEmojiStickers":
		return mtproto.MakeTLMessagesAllStickers(&mtproto.Messages_AllStickers{
			Hash: 0,
			Sets: []*mtproto.StickerSet{},
		}).To_Messages_AllStickers(), nil
	case "TLMessagesGetOldFeaturedStickers":
		return mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{
			Count:  0,
			Hash:   0,
			Sets:   []*mtproto.StickerSetCovered{},
			Unread: []int64{},
		}).To_Messages_FeaturedStickers(), nil
	case "TLMessagesGetRecentStickers":
		return mtproto.MakeTLMessagesRecentStickers(&mtproto.Messages_RecentStickers{
			Hash:     0,
			Packs:    []*mtproto.StickerPack{},
			Stickers: []*mtproto.Document{},
			Dates:    []int32{},
		}).To_Messages_RecentStickers(), nil
	case "TLMessagesGetStickers":
		return mtproto.MakeTLMessagesStickers(&mtproto.Messages_Stickers{
			Hash:     0,
			Stickers: []*mtproto.Document{},
		}).To_Messages_Stickers(), nil
	case "TLMessagesGetFeaturedStickers":
		fallthrough
	case "TLMessagesGetFeaturedEmojiStickers":
		return mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{
			Count:  0,
			Hash:   0,
			Sets:   []*mtproto.StickerSetCovered{},
			Unread: []int64{},
		}).To_Messages_FeaturedStickers(), nil
	//case "TLMessagesGetStickerSet":
	//	// logx.WithContext(ct)
	//	return nil, mtproto.ErrMethodNotImpl
	//	// return mtproto.MakeTLMessagesStickerSetNotModified(&mtproto.Messages_StickerSet{}).To_Messages_StickerSet(), nil

	// 	scheduledmessages
	case "TLMessagesGetScheduledMessages":
		return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
			Messages: []*mtproto.Message{},
			Chats:    []*mtproto.Chat{},
			Users:    []*mtproto.User{},
		}).To_Messages_Messages(), nil

	// reactions
	case "TLMessagesGetAvailableReactions":
		return mtproto.MakeTLMessagesAvailableReactions(&mtproto.Messages_AvailableReactions{
			Hash:      0,
			Reactions: []*mtproto.AvailableReaction{},
		}).To_Messages_AvailableReactions(), nil

	// emoji
	case "TLMessagesGetEmojiKeywords":
		in := object.(*mtproto.TLMessagesGetEmojiKeywords)
		return mtproto.MakeTLEmojiKeywordsDifference(&mtproto.EmojiKeywordsDifference{
			LangCode:    in.LangCode,
			FromVersion: 0,
			Version:     0,
			Keywords:    []*mtproto.EmojiKeyword{},
		}).To_EmojiKeywordsDifference(), nil
	case "TLMessagesGetEmojiKeywordsDifference":
		in := object.(*mtproto.TLMessagesGetEmojiKeywordsDifference)
		return mtproto.MakeTLEmojiKeywordsDifference(&mtproto.EmojiKeywordsDifference{
			LangCode:    in.LangCode,
			FromVersion: in.FromVersion,
			Version:     in.FromVersion,
			Keywords:    []*mtproto.EmojiKeyword{},
		}).To_EmojiKeywordsDifference(), nil
	case "TLMessagesGetEmojiKeywordsLanguages":
		return &mtproto.Vector_EmojiLanguage{
			Datas: []*mtproto.EmojiLanguage{},
		}, nil

	// reports
	case "TLAccountReportPeer":
		return mtproto.BoolTrue, nil
	case "TLAccountReportProfilePhoto":
		return mtproto.BoolTrue, nil
	case "TLChannelsReportSpam":
		return mtproto.BoolTrue, nil
	case "TLMessagesReportSpam":
		return mtproto.BoolTrue, nil

	// phone
	case "TLPhoneGetCallConfig":
		return mtproto.MakeTLDataJSON(&mtproto.DataJSON{
			Data: "{}",
		}).To_DataJSON(), nil

	case "TLAccountGetAuthorizations":
		return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{
			AuthorizationTtlDays: 0,
			Authorizations:       []*mtproto.Authorization{},
		}).To_Account_Authorizations(), nil

	case "TLAccountGetWebAuthorizations":
		return mtproto.MakeTLAccountWebAuthorizations(&mtproto.Account_WebAuthorizations{
			Authorizations: []*mtproto.WebAuthorization{},
			Users:          []*mtproto.User{},
		}).To_Account_WebAuthorizations(), nil
	}

	logx.WithContext(ctx).Errorf("%s blocked, License key from https://teamgram.net required to unlock enterprise features.", rt.Name())
	return nil, mtproto.ErrEnterpriseIsBlocked
}
