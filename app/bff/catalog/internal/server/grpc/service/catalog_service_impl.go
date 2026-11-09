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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/core"
)

// mtproto.RPCReactions

// MessagesSendReaction
// messages.sendReaction#d30d78d4 flags:# big:flags.1?true add_to_recent:flags.2?true peer:InputPeer msg_id:int reaction:flags.0?Vector<Reaction> = Updates;
func (s *Service) MessagesSendReaction(ctx context.Context, request *mtproto.TLMessagesSendReaction) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendReaction - metadata: {%s}", c.MD)
	return c.MessagesSendReaction(request)
}

// MessagesGetMessagesReactions
// messages.getMessagesReactions#8bba90e6 peer:InputPeer id:Vector<int> = Updates;
func (s *Service) MessagesGetMessagesReactions(ctx context.Context, request *mtproto.TLMessagesGetMessagesReactions) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getMessagesReactions - metadata: {%s}", c.MD)
	return c.MessagesGetMessagesReactions(request)
}

// MessagesGetMessageReactionsList
// messages.getMessageReactionsList#461b3f48 flags:# peer:InputPeer id:int reaction:flags.0?Reaction offset:flags.1?string limit:int = messages.MessageReactionsList;
func (s *Service) MessagesGetMessageReactionsList(ctx context.Context, request *mtproto.TLMessagesGetMessageReactionsList) (*mtproto.Messages_MessageReactionsList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getMessageReactionsList - metadata: {%s}", c.MD)
	return c.MessagesGetMessageReactionsList(request)
}

// MessagesSetChatAvailableReactions
// messages.setChatAvailableReactions#864b2581 flags:# peer:InputPeer available_reactions:ChatReactions reactions_limit:flags.0?int paid_enabled:flags.1?Bool = Updates;
func (s *Service) MessagesSetChatAvailableReactions(ctx context.Context, request *mtproto.TLMessagesSetChatAvailableReactions) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setChatAvailableReactions - metadata: {%s}", c.MD)
	return c.MessagesSetChatAvailableReactions(request)
}

// MessagesGetAvailableReactions
// messages.getAvailableReactions#18dea0ac hash:int = messages.AvailableReactions;
func (s *Service) MessagesGetAvailableReactions(ctx context.Context, request *mtproto.TLMessagesGetAvailableReactions) (*mtproto.Messages_AvailableReactions, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getAvailableReactions - metadata: {%s}", c.MD)
	return c.MessagesGetAvailableReactions(request)
}

// MessagesSetDefaultReaction
// messages.setDefaultReaction#4f47a016 reaction:Reaction = Bool;
func (s *Service) MessagesSetDefaultReaction(ctx context.Context, request *mtproto.TLMessagesSetDefaultReaction) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setDefaultReaction - metadata: {%s}", c.MD)
	return c.MessagesSetDefaultReaction(request)
}

// MessagesGetUnreadReactions
// messages.getUnreadReactions#bd7f90ac flags:# peer:InputPeer top_msg_id:flags.0?int saved_peer_id:flags.1?InputPeer offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (s *Service) MessagesGetUnreadReactions(ctx context.Context, request *mtproto.TLMessagesGetUnreadReactions) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getUnreadReactions - metadata: {%s}", c.MD)
	return c.MessagesGetUnreadReactions(request)
}

// MessagesReadReactions
// messages.readReactions#9ec44f93 flags:# peer:InputPeer top_msg_id:flags.0?int saved_peer_id:flags.1?InputPeer = messages.AffectedHistory;
func (s *Service) MessagesReadReactions(ctx context.Context, request *mtproto.TLMessagesReadReactions) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.readReactions - metadata: {%s}", c.MD)
	return c.MessagesReadReactions(request)
}

// MessagesReportReaction is not served: messages.reportReaction#3f64c076 peer:InputPeer id:int reaction_peer:InputPeer = Bool;
func (s *Service) MessagesReportReaction(ctx context.Context, request *mtproto.TLMessagesReportReaction) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetTopReactions
// messages.getTopReactions#bb8125ba limit:int hash:long = messages.Reactions;
func (s *Service) MessagesGetTopReactions(ctx context.Context, request *mtproto.TLMessagesGetTopReactions) (*mtproto.Messages_Reactions, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getTopReactions - metadata: {%s}", c.MD)
	return c.MessagesGetTopReactions(request)
}

// MessagesGetRecentReactions
// messages.getRecentReactions#39461db2 limit:int hash:long = messages.Reactions;
func (s *Service) MessagesGetRecentReactions(ctx context.Context, request *mtproto.TLMessagesGetRecentReactions) (*mtproto.Messages_Reactions, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getRecentReactions - metadata: {%s}", c.MD)
	return c.MessagesGetRecentReactions(request)
}

// MessagesClearRecentReactions
// messages.clearRecentReactions#9dfeefb4 = Bool;
func (s *Service) MessagesClearRecentReactions(ctx context.Context, request *mtproto.TLMessagesClearRecentReactions) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.clearRecentReactions - metadata: {%s}", c.MD)
	return c.MessagesClearRecentReactions(request)
}

// MessagesSendPaidReaction is not served: messages.sendPaidReaction#58bbcb50 flags:# peer:InputPeer msg_id:int count:int random_id:long private:flags.0?PaidReactionPrivacy = Updates;
func (s *Service) MessagesSendPaidReaction(ctx context.Context, request *mtproto.TLMessagesSendPaidReaction) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesTogglePaidReactionPrivacy is not served: messages.togglePaidReactionPrivacy#435885b5 peer:InputPeer msg_id:int private:PaidReactionPrivacy = Bool;
func (s *Service) MessagesTogglePaidReactionPrivacy(ctx context.Context, request *mtproto.TLMessagesTogglePaidReactionPrivacy) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetPaidReactionPrivacy is not served: messages.getPaidReactionPrivacy#472455aa = Updates;
func (s *Service) MessagesGetPaidReactionPrivacy(ctx context.Context, request *mtproto.TLMessagesGetPaidReactionPrivacy) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesDeleteParticipantReactions is not served: messages.deleteParticipantReactions#a0b80cf8 peer:InputPeer participant:InputPeer = Bool;
func (s *Service) MessagesDeleteParticipantReactions(ctx context.Context, request *mtproto.TLMessagesDeleteParticipantReactions) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesDeleteParticipantReaction is not served: messages.deleteParticipantReaction#e3b7f82c peer:InputPeer msg_id:int participant:InputPeer = Updates;
func (s *Service) MessagesDeleteParticipantReaction(ctx context.Context, request *mtproto.TLMessagesDeleteParticipantReaction) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCStickers

// MessagesGetStickers
// messages.getStickers#d5a5d3a1 emoticon:string hash:long = messages.Stickers;
func (s *Service) MessagesGetStickers(ctx context.Context, request *mtproto.TLMessagesGetStickers) (*mtproto.Messages_Stickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getStickers - metadata: {%s}", c.MD)
	return c.MessagesGetStickers(request)
}

// MessagesGetAllStickers
// messages.getAllStickers#b8a0a1a8 hash:long = messages.AllStickers;
func (s *Service) MessagesGetAllStickers(ctx context.Context, request *mtproto.TLMessagesGetAllStickers) (*mtproto.Messages_AllStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getAllStickers - metadata: {%s}", c.MD)
	return c.MessagesGetAllStickers(request)
}

// MessagesGetStickerSet
// messages.getStickerSet#c8a0ec74 stickerset:InputStickerSet hash:int = messages.StickerSet;
func (s *Service) MessagesGetStickerSet(ctx context.Context, request *mtproto.TLMessagesGetStickerSet) (*mtproto.Messages_StickerSet, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getStickerSet - metadata: {%s}", c.MD)
	return c.MessagesGetStickerSet(request)
}

// MessagesInstallStickerSet
// messages.installStickerSet#c78fe460 stickerset:InputStickerSet archived:Bool = messages.StickerSetInstallResult;
func (s *Service) MessagesInstallStickerSet(ctx context.Context, request *mtproto.TLMessagesInstallStickerSet) (*mtproto.Messages_StickerSetInstallResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.installStickerSet - metadata: {%s}", c.MD)
	return c.MessagesInstallStickerSet(request)
}

// MessagesUninstallStickerSet
// messages.uninstallStickerSet#f96e55de stickerset:InputStickerSet = Bool;
func (s *Service) MessagesUninstallStickerSet(ctx context.Context, request *mtproto.TLMessagesUninstallStickerSet) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.uninstallStickerSet - metadata: {%s}", c.MD)
	return c.MessagesUninstallStickerSet(request)
}

// MessagesReorderStickerSets
// messages.reorderStickerSets#78337739 flags:# masks:flags.0?true emojis:flags.1?true order:Vector<long> = Bool;
func (s *Service) MessagesReorderStickerSets(ctx context.Context, request *mtproto.TLMessagesReorderStickerSets) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.reorderStickerSets - metadata: {%s}", c.MD)
	return c.MessagesReorderStickerSets(request)
}

// MessagesGetFeaturedStickers
// messages.getFeaturedStickers#64780b14 hash:long = messages.FeaturedStickers;
func (s *Service) MessagesGetFeaturedStickers(ctx context.Context, request *mtproto.TLMessagesGetFeaturedStickers) (*mtproto.Messages_FeaturedStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getFeaturedStickers - metadata: {%s}", c.MD)
	return c.MessagesGetFeaturedStickers(request)
}

// MessagesReadFeaturedStickers is not served: messages.readFeaturedStickers#5b118126 id:Vector<long> = Bool;
func (s *Service) MessagesReadFeaturedStickers(ctx context.Context, request *mtproto.TLMessagesReadFeaturedStickers) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetRecentStickers
// messages.getRecentStickers#9da9403b flags:# attached:flags.0?true hash:long = messages.RecentStickers;
func (s *Service) MessagesGetRecentStickers(ctx context.Context, request *mtproto.TLMessagesGetRecentStickers) (*mtproto.Messages_RecentStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getRecentStickers - metadata: {%s}", c.MD)
	return c.MessagesGetRecentStickers(request)
}

// MessagesSaveRecentSticker is not served: messages.saveRecentSticker#392718f8 flags:# attached:flags.0?true id:InputDocument unsave:Bool = Bool;
func (s *Service) MessagesSaveRecentSticker(ctx context.Context, request *mtproto.TLMessagesSaveRecentSticker) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesClearRecentStickers is not served: messages.clearRecentStickers#8999602d flags:# attached:flags.0?true = Bool;
func (s *Service) MessagesClearRecentStickers(ctx context.Context, request *mtproto.TLMessagesClearRecentStickers) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetArchivedStickers
// messages.getArchivedStickers#57f17692 flags:# masks:flags.0?true emojis:flags.1?true offset_id:long limit:int = messages.ArchivedStickers;
func (s *Service) MessagesGetArchivedStickers(ctx context.Context, request *mtproto.TLMessagesGetArchivedStickers) (*mtproto.Messages_ArchivedStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getArchivedStickers - metadata: {%s}", c.MD)
	return c.MessagesGetArchivedStickers(request)
}

// MessagesGetMaskStickers
// messages.getMaskStickers#640f82b8 hash:long = messages.AllStickers;
func (s *Service) MessagesGetMaskStickers(ctx context.Context, request *mtproto.TLMessagesGetMaskStickers) (*mtproto.Messages_AllStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getMaskStickers - metadata: {%s}", c.MD)
	return c.MessagesGetMaskStickers(request)
}

// MessagesGetAttachedStickers is not served: messages.getAttachedStickers#cc5b67cc media:InputStickeredMedia = Vector<StickerSetCovered>;
func (s *Service) MessagesGetAttachedStickers(ctx context.Context, request *mtproto.TLMessagesGetAttachedStickers) (*mtproto.Vector_StickerSetCovered, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetFavedStickers
// messages.getFavedStickers#4f1aaa9 hash:long = messages.FavedStickers;
func (s *Service) MessagesGetFavedStickers(ctx context.Context, request *mtproto.TLMessagesGetFavedStickers) (*mtproto.Messages_FavedStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getFavedStickers - metadata: {%s}", c.MD)
	return c.MessagesGetFavedStickers(request)
}

// MessagesFaveSticker is not served: messages.faveSticker#b9ffc55b id:InputDocument unfave:Bool = Bool;
func (s *Service) MessagesFaveSticker(ctx context.Context, request *mtproto.TLMessagesFaveSticker) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSearchStickerSets is not served: messages.searchStickerSets#35705b8a flags:# exclude_featured:flags.0?true q:string hash:long = messages.FoundStickerSets;
func (s *Service) MessagesSearchStickerSets(ctx context.Context, request *mtproto.TLMessagesSearchStickerSets) (*mtproto.Messages_FoundStickerSets, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesToggleStickerSets
// messages.toggleStickerSets#b5052fea flags:# uninstall:flags.0?true archive:flags.1?true unarchive:flags.2?true stickersets:Vector<InputStickerSet> = Bool;
func (s *Service) MessagesToggleStickerSets(ctx context.Context, request *mtproto.TLMessagesToggleStickerSets) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.toggleStickerSets - metadata: {%s}", c.MD)
	return c.MessagesToggleStickerSets(request)
}

// MessagesGetOldFeaturedStickers
// messages.getOldFeaturedStickers#7ed094a1 offset:int limit:int hash:long = messages.FeaturedStickers;
func (s *Service) MessagesGetOldFeaturedStickers(ctx context.Context, request *mtproto.TLMessagesGetOldFeaturedStickers) (*mtproto.Messages_FeaturedStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getOldFeaturedStickers - metadata: {%s}", c.MD)
	return c.MessagesGetOldFeaturedStickers(request)
}

// MessagesSearchEmojiStickerSets is not served: messages.searchEmojiStickerSets#92b4494c flags:# exclude_featured:flags.0?true q:string hash:long = messages.FoundStickerSets;
func (s *Service) MessagesSearchEmojiStickerSets(ctx context.Context, request *mtproto.TLMessagesSearchEmojiStickerSets) (*mtproto.Messages_FoundStickerSets, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetMyStickers is not served: messages.getMyStickers#d0b5e1fc offset_id:long limit:int = messages.MyStickers;
func (s *Service) MessagesGetMyStickers(ctx context.Context, request *mtproto.TLMessagesGetMyStickers) (*mtproto.Messages_MyStickers, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSearchStickers is not served: messages.searchStickers#29b1c66a flags:# emojis:flags.0?true q:string emoticon:string lang_code:Vector<string> offset:int limit:int hash:long = messages.FoundStickers;
func (s *Service) MessagesSearchStickers(ctx context.Context, request *mtproto.TLMessagesSearchStickers) (*mtproto.Messages_FoundStickers, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersCreateStickerSet is not served: stickers.createStickerSet#9021ab67 flags:# masks:flags.0?true emojis:flags.5?true text_color:flags.6?true user_id:InputUser title:string short_name:string thumb:flags.2?InputDocument stickers:Vector<InputStickerSetItem> software:flags.3?string = messages.StickerSet;
func (s *Service) StickersCreateStickerSet(ctx context.Context, request *mtproto.TLStickersCreateStickerSet) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersRemoveStickerFromSet is not served: stickers.removeStickerFromSet#f7760f51 sticker:InputDocument = messages.StickerSet;
func (s *Service) StickersRemoveStickerFromSet(ctx context.Context, request *mtproto.TLStickersRemoveStickerFromSet) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersChangeStickerPosition is not served: stickers.changeStickerPosition#ffb6d4ca sticker:InputDocument position:int = messages.StickerSet;
func (s *Service) StickersChangeStickerPosition(ctx context.Context, request *mtproto.TLStickersChangeStickerPosition) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersAddStickerToSet is not served: stickers.addStickerToSet#8653febe stickerset:InputStickerSet sticker:InputStickerSetItem = messages.StickerSet;
func (s *Service) StickersAddStickerToSet(ctx context.Context, request *mtproto.TLStickersAddStickerToSet) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersSetStickerSetThumb is not served: stickers.setStickerSetThumb#a76a5392 flags:# stickerset:InputStickerSet thumb:flags.0?InputDocument thumb_document_id:flags.1?long = messages.StickerSet;
func (s *Service) StickersSetStickerSetThumb(ctx context.Context, request *mtproto.TLStickersSetStickerSetThumb) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersCheckShortName is not served: stickers.checkShortName#284b3639 short_name:string = Bool;
func (s *Service) StickersCheckShortName(ctx context.Context, request *mtproto.TLStickersCheckShortName) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersSuggestShortName is not served: stickers.suggestShortName#4dafc503 title:string = stickers.SuggestedShortName;
func (s *Service) StickersSuggestShortName(ctx context.Context, request *mtproto.TLStickersSuggestShortName) (*mtproto.Stickers_SuggestedShortName, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersChangeSticker is not served: stickers.changeSticker#f5537ebc flags:# sticker:InputDocument emoji:flags.0?string mask_coords:flags.1?MaskCoords keywords:flags.2?string = messages.StickerSet;
func (s *Service) StickersChangeSticker(ctx context.Context, request *mtproto.TLStickersChangeSticker) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersRenameStickerSet is not served: stickers.renameStickerSet#124b1c00 stickerset:InputStickerSet title:string = messages.StickerSet;
func (s *Service) StickersRenameStickerSet(ctx context.Context, request *mtproto.TLStickersRenameStickerSet) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersDeleteStickerSet is not served: stickers.deleteStickerSet#87704394 stickerset:InputStickerSet = Bool;
func (s *Service) StickersDeleteStickerSet(ctx context.Context, request *mtproto.TLStickersDeleteStickerSet) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// StickersReplaceSticker is not served: stickers.replaceSticker#4696459a sticker:InputDocument new_sticker:InputStickerSetItem = messages.StickerSet;
func (s *Service) StickersReplaceSticker(ctx context.Context, request *mtproto.TLStickersReplaceSticker) (*mtproto.Messages_StickerSet, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCCustomEmojis

// AccountGetDefaultProfilePhotoEmojis
// account.getDefaultProfilePhotoEmojis#e2750328 hash:long = EmojiList;
func (s *Service) AccountGetDefaultProfilePhotoEmojis(ctx context.Context, request *mtproto.TLAccountGetDefaultProfilePhotoEmojis) (*mtproto.EmojiList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getDefaultProfilePhotoEmojis - metadata: {%s}", c.MD)
	return c.AccountGetDefaultProfilePhotoEmojis(request)
}

// AccountGetDefaultGroupPhotoEmojis
// account.getDefaultGroupPhotoEmojis#915860ae hash:long = EmojiList;
func (s *Service) AccountGetDefaultGroupPhotoEmojis(ctx context.Context, request *mtproto.TLAccountGetDefaultGroupPhotoEmojis) (*mtproto.EmojiList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getDefaultGroupPhotoEmojis - metadata: {%s}", c.MD)
	return c.AccountGetDefaultGroupPhotoEmojis(request)
}

// MessagesGetCustomEmojiDocuments
// messages.getCustomEmojiDocuments#d9ab0f54 document_id:Vector<long> = Vector<Document>;
func (s *Service) MessagesGetCustomEmojiDocuments(ctx context.Context, request *mtproto.TLMessagesGetCustomEmojiDocuments) (*mtproto.Vector_Document, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getCustomEmojiDocuments - metadata: {%s}", c.MD)
	return c.MessagesGetCustomEmojiDocuments(request)
}

// MessagesGetEmojiStickers
// messages.getEmojiStickers#fbfca18f hash:long = messages.AllStickers;
func (s *Service) MessagesGetEmojiStickers(ctx context.Context, request *mtproto.TLMessagesGetEmojiStickers) (*mtproto.Messages_AllStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiStickers - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiStickers(request)
}

// MessagesGetFeaturedEmojiStickers
// messages.getFeaturedEmojiStickers#ecf6736 hash:long = messages.FeaturedStickers;
func (s *Service) MessagesGetFeaturedEmojiStickers(ctx context.Context, request *mtproto.TLMessagesGetFeaturedEmojiStickers) (*mtproto.Messages_FeaturedStickers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getFeaturedEmojiStickers - metadata: {%s}", c.MD)
	return c.MessagesGetFeaturedEmojiStickers(request)
}

// MessagesSearchCustomEmoji is not served: messages.searchCustomEmoji#2c11c0d7 emoticon:string hash:long = EmojiList;
func (s *Service) MessagesSearchCustomEmoji(ctx context.Context, request *mtproto.TLMessagesSearchCustomEmoji) (*mtproto.EmojiList, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCEmoji

// MessagesGetEmojiKeywords
// messages.getEmojiKeywords#35a0e062 lang_code:string = EmojiKeywordsDifference;
func (s *Service) MessagesGetEmojiKeywords(ctx context.Context, request *mtproto.TLMessagesGetEmojiKeywords) (*mtproto.EmojiKeywordsDifference, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiKeywords - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiKeywords(request)
}

// MessagesGetEmojiKeywordsDifference
// messages.getEmojiKeywordsDifference#1508b6af lang_code:string from_version:int = EmojiKeywordsDifference;
func (s *Service) MessagesGetEmojiKeywordsDifference(ctx context.Context, request *mtproto.TLMessagesGetEmojiKeywordsDifference) (*mtproto.EmojiKeywordsDifference, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiKeywordsDifference - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiKeywordsDifference(request)
}

// MessagesGetEmojiKeywordsLanguages
// messages.getEmojiKeywordsLanguages#4e9963b2 lang_codes:Vector<string> = Vector<EmojiLanguage>;
func (s *Service) MessagesGetEmojiKeywordsLanguages(ctx context.Context, request *mtproto.TLMessagesGetEmojiKeywordsLanguages) (*mtproto.Vector_EmojiLanguage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiKeywordsLanguages - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiKeywordsLanguages(request)
}

// MessagesGetEmojiURL is not served: messages.getEmojiURL#d5b10c26 lang_code:string = EmojiURL;
func (s *Service) MessagesGetEmojiURL(ctx context.Context, request *mtproto.TLMessagesGetEmojiURL) (*mtproto.EmojiURL, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCEmojiCategories

// MessagesGetEmojiGroups
// messages.getEmojiGroups#7488ce5b hash:int = messages.EmojiGroups;
func (s *Service) MessagesGetEmojiGroups(ctx context.Context, request *mtproto.TLMessagesGetEmojiGroups) (*mtproto.Messages_EmojiGroups, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiGroups - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiGroups(request)
}

// MessagesGetEmojiStatusGroups
// messages.getEmojiStatusGroups#2ecd56cd hash:int = messages.EmojiGroups;
func (s *Service) MessagesGetEmojiStatusGroups(ctx context.Context, request *mtproto.TLMessagesGetEmojiStatusGroups) (*mtproto.Messages_EmojiGroups, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiStatusGroups - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiStatusGroups(request)
}

// MessagesGetEmojiProfilePhotoGroups
// messages.getEmojiProfilePhotoGroups#21a548f3 hash:int = messages.EmojiGroups;
func (s *Service) MessagesGetEmojiProfilePhotoGroups(ctx context.Context, request *mtproto.TLMessagesGetEmojiProfilePhotoGroups) (*mtproto.Messages_EmojiGroups, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiProfilePhotoGroups - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiProfilePhotoGroups(request)
}

// MessagesGetEmojiStickerGroups
// messages.getEmojiStickerGroups#1dd840f5 hash:int = messages.EmojiGroups;
func (s *Service) MessagesGetEmojiStickerGroups(ctx context.Context, request *mtproto.TLMessagesGetEmojiStickerGroups) (*mtproto.Messages_EmojiGroups, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiStickerGroups - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiStickerGroups(request)
}

// mtproto.RPCEmojiStatus

// AccountUpdateEmojiStatus is not served: account.updateEmojiStatus#fbd3de6b emoji_status:EmojiStatus = Bool;
func (s *Service) AccountUpdateEmojiStatus(ctx context.Context, request *mtproto.TLAccountUpdateEmojiStatus) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetDefaultEmojiStatuses
// account.getDefaultEmojiStatuses#d6753386 hash:long = account.EmojiStatuses;
func (s *Service) AccountGetDefaultEmojiStatuses(ctx context.Context, request *mtproto.TLAccountGetDefaultEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getDefaultEmojiStatuses - metadata: {%s}", c.MD)
	return c.AccountGetDefaultEmojiStatuses(request)
}

// AccountGetRecentEmojiStatuses
// account.getRecentEmojiStatuses#f578105 hash:long = account.EmojiStatuses;
func (s *Service) AccountGetRecentEmojiStatuses(ctx context.Context, request *mtproto.TLAccountGetRecentEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getRecentEmojiStatuses - metadata: {%s}", c.MD)
	return c.AccountGetRecentEmojiStatuses(request)
}

// AccountClearRecentEmojiStatuses is not served: account.clearRecentEmojiStatuses#18201aae = Bool;
func (s *Service) AccountClearRecentEmojiStatuses(ctx context.Context, request *mtproto.TLAccountClearRecentEmojiStatuses) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetChannelDefaultEmojiStatuses
// account.getChannelDefaultEmojiStatuses#7727a7d5 hash:long = account.EmojiStatuses;
func (s *Service) AccountGetChannelDefaultEmojiStatuses(ctx context.Context, request *mtproto.TLAccountGetChannelDefaultEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getChannelDefaultEmojiStatuses - metadata: {%s}", c.MD)
	return c.AccountGetChannelDefaultEmojiStatuses(request)
}

// AccountGetChannelRestrictedStatusEmojis
// account.getChannelRestrictedStatusEmojis#35a9e0d5 hash:long = EmojiList;
func (s *Service) AccountGetChannelRestrictedStatusEmojis(ctx context.Context, request *mtproto.TLAccountGetChannelRestrictedStatusEmojis) (*mtproto.EmojiList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getChannelRestrictedStatusEmojis - metadata: {%s}", c.MD)
	return c.AccountGetChannelRestrictedStatusEmojis(request)
}

// AccountGetCollectibleEmojiStatuses is not served: account.getCollectibleEmojiStatuses#2e7b4543 hash:long = account.EmojiStatuses;
func (s *Service) AccountGetCollectibleEmojiStatuses(ctx context.Context, request *mtproto.TLAccountGetCollectibleEmojiStatuses) (*mtproto.Account_EmojiStatuses, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsUpdateEmojiStatus is not served: channels.updateEmojiStatus#f0d3e6a8 channel:InputChannel emoji_status:EmojiStatus = Updates;
func (s *Service) ChannelsUpdateEmojiStatus(ctx context.Context, request *mtproto.TLChannelsUpdateEmojiStatus) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsUpdateUserEmojiStatus is not served: bots.updateUserEmojiStatus#ed9f30c5 user_id:InputUser emoji_status:EmojiStatus = Bool;
func (s *Service) BotsUpdateUserEmojiStatus(ctx context.Context, request *mtproto.TLBotsUpdateUserEmojiStatus) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// BotsToggleUserEmojiStatusPermission is not served: bots.toggleUserEmojiStatusPermission#6de6392 bot:InputUser enabled:Bool = Bool;
func (s *Service) BotsToggleUserEmojiStatusPermission(ctx context.Context, request *mtproto.TLBotsToggleUserEmojiStatusPermission) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCAccentColors

// AccountUpdateColor is not served: account.updateColor#684d214e flags:# for_profile:flags.1?true color:flags.2?PeerColor = Bool;
func (s *Service) AccountUpdateColor(ctx context.Context, request *mtproto.TLAccountUpdateColor) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountGetDefaultBackgroundEmojis
// account.getDefaultBackgroundEmojis#a60ab9ce hash:long = EmojiList;
func (s *Service) AccountGetDefaultBackgroundEmojis(ctx context.Context, request *mtproto.TLAccountGetDefaultBackgroundEmojis) (*mtproto.EmojiList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getDefaultBackgroundEmojis - metadata: {%s}", c.MD)
	return c.AccountGetDefaultBackgroundEmojis(request)
}

// HelpGetPeerColors
// help.getPeerColors#da80f42f hash:int = help.PeerColors;
func (s *Service) HelpGetPeerColors(ctx context.Context, request *mtproto.TLHelpGetPeerColors) (*mtproto.Help_PeerColors, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.getPeerColors - metadata: {%s}", c.MD)
	return c.HelpGetPeerColors(request)
}

// HelpGetPeerProfileColors
// help.getPeerProfileColors#abcfa9fd hash:int = help.PeerColors;
func (s *Service) HelpGetPeerProfileColors(ctx context.Context, request *mtproto.TLHelpGetPeerProfileColors) (*mtproto.Help_PeerColors, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.getPeerProfileColors - metadata: {%s}", c.MD)
	return c.HelpGetPeerProfileColors(request)
}

// ChannelsUpdateColor is not served: channels.updateColor#d8aa3671 flags:# for_profile:flags.1?true channel:InputChannel color:flags.2?int background_emoji_id:flags.0?long = Updates;
func (s *Service) ChannelsUpdateColor(ctx context.Context, request *mtproto.TLChannelsUpdateColor) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// mtproto.RPCThemes

// AccountUploadTheme
// account.uploadTheme#1c3db333 flags:# file:InputFile thumb:flags.0?InputFile file_name:string mime_type:string = Document;
func (s *Service) AccountUploadTheme(ctx context.Context, request *mtproto.TLAccountUploadTheme) (*mtproto.Document, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.uploadTheme - metadata: {%s}", c.MD)
	return c.AccountUploadTheme(request)
}

// AccountCreateTheme
// account.createTheme#652e4400 flags:# slug:string title:string document:flags.2?InputDocument settings:flags.3?Vector<InputThemeSettings> = Theme;
func (s *Service) AccountCreateTheme(ctx context.Context, request *mtproto.TLAccountCreateTheme) (*mtproto.Theme, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.createTheme - metadata: {%s}", c.MD)
	return c.AccountCreateTheme(request)
}

// AccountUpdateTheme
// account.updateTheme#2bf40ccc flags:# format:string theme:InputTheme slug:flags.0?string title:flags.1?string document:flags.2?InputDocument settings:flags.3?Vector<InputThemeSettings> = Theme;
func (s *Service) AccountUpdateTheme(ctx context.Context, request *mtproto.TLAccountUpdateTheme) (*mtproto.Theme, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateTheme - metadata: {%s}", c.MD)
	return c.AccountUpdateTheme(request)
}

// AccountSaveTheme
// account.saveTheme#f257106c theme:InputTheme unsave:Bool = Bool;
func (s *Service) AccountSaveTheme(ctx context.Context, request *mtproto.TLAccountSaveTheme) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.saveTheme - metadata: {%s}", c.MD)
	return c.AccountSaveTheme(request)
}

// AccountInstallTheme
// account.installTheme#c727bb3b flags:# dark:flags.0?true theme:flags.1?InputTheme format:flags.2?string base_theme:flags.3?BaseTheme = Bool;
func (s *Service) AccountInstallTheme(ctx context.Context, request *mtproto.TLAccountInstallTheme) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.installTheme - metadata: {%s}", c.MD)
	return c.AccountInstallTheme(request)
}

// AccountGetTheme
// account.getTheme#3a5869ec format:string theme:InputTheme = Theme;
func (s *Service) AccountGetTheme(ctx context.Context, request *mtproto.TLAccountGetTheme) (*mtproto.Theme, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getTheme - metadata: {%s}", c.MD)
	return c.AccountGetTheme(request)
}

// AccountGetThemes
// account.getThemes#7206e458 format:string hash:long = account.Themes;
func (s *Service) AccountGetThemes(ctx context.Context, request *mtproto.TLAccountGetThemes) (*mtproto.Account_Themes, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getThemes - metadata: {%s}", c.MD)
	return c.AccountGetThemes(request)
}

// AccountGetChatThemes
// account.getChatThemes#d638de89 hash:long = account.Themes;
func (s *Service) AccountGetChatThemes(ctx context.Context, request *mtproto.TLAccountGetChatThemes) (*mtproto.Account_Themes, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getChatThemes - metadata: {%s}", c.MD)
	return c.AccountGetChatThemes(request)
}

// AccountGetUniqueGiftChatThemes is not served: account.getUniqueGiftChatThemes#e42ce9c9 offset:string limit:int hash:long = account.ChatThemes;
func (s *Service) AccountGetUniqueGiftChatThemes(ctx context.Context, request *mtproto.TLAccountGetUniqueGiftChatThemes) (*mtproto.Account_ChatThemes, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSetChatTheme
// messages.setChatTheme#81202c9 peer:InputPeer theme:InputChatTheme = Updates;
func (s *Service) MessagesSetChatTheme(ctx context.Context, request *mtproto.TLMessagesSetChatTheme) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setChatTheme - metadata: {%s}", c.MD)
	return c.MessagesSetChatTheme(request)
}

// mtproto.RPCWallpapers

// AccountGetWallPapers
// account.getWallPapers#7967d36 hash:long = account.WallPapers;
func (s *Service) AccountGetWallPapers(ctx context.Context, request *mtproto.TLAccountGetWallPapers) (*mtproto.Account_WallPapers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getWallPapers - metadata: {%s}", c.MD)
	return c.AccountGetWallPapers(request)
}

// AccountGetWallPaper
// account.getWallPaper#fc8ddbea wallpaper:InputWallPaper = WallPaper;
func (s *Service) AccountGetWallPaper(ctx context.Context, request *mtproto.TLAccountGetWallPaper) (*mtproto.WallPaper, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getWallPaper - metadata: {%s}", c.MD)
	return c.AccountGetWallPaper(request)
}

// AccountUploadWallPaper
// account.uploadWallPaper#e39a8f03 flags:# for_chat:flags.0?true file:InputFile mime_type:string settings:WallPaperSettings = WallPaper;
func (s *Service) AccountUploadWallPaper(ctx context.Context, request *mtproto.TLAccountUploadWallPaper) (*mtproto.WallPaper, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.uploadWallPaper - metadata: {%s}", c.MD)
	return c.AccountUploadWallPaper(request)
}

// AccountSaveWallPaper
// account.saveWallPaper#6c5a5b37 wallpaper:InputWallPaper unsave:Bool settings:WallPaperSettings = Bool;
func (s *Service) AccountSaveWallPaper(ctx context.Context, request *mtproto.TLAccountSaveWallPaper) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.saveWallPaper - metadata: {%s}", c.MD)
	return c.AccountSaveWallPaper(request)
}

// AccountInstallWallPaper
// account.installWallPaper#feed5769 wallpaper:InputWallPaper settings:WallPaperSettings = Bool;
func (s *Service) AccountInstallWallPaper(ctx context.Context, request *mtproto.TLAccountInstallWallPaper) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.installWallPaper - metadata: {%s}", c.MD)
	return c.AccountInstallWallPaper(request)
}

// AccountResetWallPapers
// account.resetWallPapers#bb3b9804 = Bool;
func (s *Service) AccountResetWallPapers(ctx context.Context, request *mtproto.TLAccountResetWallPapers) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.resetWallPapers - metadata: {%s}", c.MD)
	return c.AccountResetWallPapers(request)
}

// AccountGetMultiWallPapers
// account.getMultiWallPapers#65ad71dc wallpapers:Vector<InputWallPaper> = Vector<WallPaper>;
func (s *Service) AccountGetMultiWallPapers(ctx context.Context, request *mtproto.TLAccountGetMultiWallPapers) (*mtproto.Vector_WallPaper, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getMultiWallPapers - metadata: {%s}", c.MD)
	return c.AccountGetMultiWallPapers(request)
}

// MessagesSetChatWallPaper
// messages.setChatWallPaper#8ffacae1 flags:# for_both:flags.3?true revert:flags.4?true peer:InputPeer wallpaper:flags.0?InputWallPaper settings:flags.2?WallPaperSettings id:flags.1?int = Updates;
func (s *Service) MessagesSetChatWallPaper(ctx context.Context, request *mtproto.TLMessagesSetChatWallPaper) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setChatWallPaper - metadata: {%s}", c.MD)
	return c.MessagesSetChatWallPaper(request)
}

// mtproto.RPCTimezones

// HelpGetTimezonesList
// help.getTimezonesList#49b30240 hash:int = help.TimezonesList;
func (s *Service) HelpGetTimezonesList(ctx context.Context, request *mtproto.TLHelpGetTimezonesList) (*mtproto.Help_TimezonesList, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("help.getTimezonesList - metadata: {%s}", c.MD)
	return c.HelpGetTimezonesList(request)
}

// mtproto.RPCMessageEffects

// MessagesGetAvailableEffects
// messages.getAvailableEffects#dea20a39 hash:int = messages.AvailableEffects;
func (s *Service) MessagesGetAvailableEffects(ctx context.Context, request *mtproto.TLMessagesGetAvailableEffects) (*mtproto.Messages_AvailableEffects, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getAvailableEffects - metadata: {%s}", c.MD)
	return c.MessagesGetAvailableEffects(request)
}

// mtproto.RPCSavedMessageTags

// MessagesGetSavedReactionTags
// messages.getSavedReactionTags#3637e05b flags:# peer:flags.0?InputPeer hash:long = messages.SavedReactionTags;
func (s *Service) MessagesGetSavedReactionTags(ctx context.Context, request *mtproto.TLMessagesGetSavedReactionTags) (*mtproto.Messages_SavedReactionTags, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getSavedReactionTags - metadata: {%s}", c.MD)
	return c.MessagesGetSavedReactionTags(request)
}

// MessagesUpdateSavedReactionTag
// messages.updateSavedReactionTag#60297dec flags:# reaction:Reaction title:flags.0?string = Bool;
func (s *Service) MessagesUpdateSavedReactionTag(ctx context.Context, request *mtproto.TLMessagesUpdateSavedReactionTag) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.updateSavedReactionTag - metadata: {%s}", c.MD)
	return c.MessagesUpdateSavedReactionTag(request)
}

// MessagesGetDefaultTagReactions
// messages.getDefaultTagReactions#bdf93428 hash:long = messages.Reactions;
func (s *Service) MessagesGetDefaultTagReactions(ctx context.Context, request *mtproto.TLMessagesGetDefaultTagReactions) (*mtproto.Messages_Reactions, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getDefaultTagReactions - metadata: {%s}", c.MD)
	return c.MessagesGetDefaultTagReactions(request)
}
