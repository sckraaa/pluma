/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright 2025 Teamgram Authors.
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/userchannelprofiles/internal/core"
)

// AccountUpdateProfile
// account.updateProfile#78515775 flags:# first_name:flags.0?string last_name:flags.1?string about:flags.2?string = User;
func (s *Service) AccountUpdateProfile(ctx context.Context, request *mtproto.TLAccountUpdateProfile) (*mtproto.User, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateProfile - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateProfile(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountUpdateStatus
// account.updateStatus#6628562c offline:Bool = Bool;
func (s *Service) AccountUpdateStatus(ctx context.Context, request *mtproto.TLAccountUpdateStatus) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateStatus - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateStatus(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountUpdateBirthday
// account.updateBirthday#cc6e0c11 flags:# birthday:flags.0?Birthday = Bool;
func (s *Service) AccountUpdateBirthday(ctx context.Context, request *mtproto.TLAccountUpdateBirthday) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateBirthday - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateBirthday(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountUpdatePersonalChannel
// account.updatePersonalChannel#d94305e0 channel:InputChannel = Bool;
func (s *Service) AccountUpdatePersonalChannel(ctx context.Context, request *mtproto.TLAccountUpdatePersonalChannel) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updatePersonalChannel - metadata: {%s}", c.MD)

	r, err := c.AccountUpdatePersonalChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountSetMainProfileTab
// account.setMainProfileTab#5dee78b0 tab:ProfileTab = Bool;
func (s *Service) AccountSetMainProfileTab(ctx context.Context, request *mtproto.TLAccountSetMainProfileTab) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.setMainProfileTab - metadata: {%s}", c.MD)

	r, err := c.AccountSetMainProfileTab(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountSaveMusic
// account.saveMusic#b26732a9 flags:# unsave:flags.0?true id:InputDocument after_id:flags.1?InputDocument = Bool;
func (s *Service) AccountSaveMusic(ctx context.Context, request *mtproto.TLAccountSaveMusic) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.saveMusic - metadata: {%s}", c.MD)

	r, err := c.AccountSaveMusic(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountGetSavedMusicIds
// account.getSavedMusicIds#e09d5faf hash:long = account.SavedMusicIds;
func (s *Service) AccountGetSavedMusicIds(ctx context.Context, request *mtproto.TLAccountGetSavedMusicIds) (*mtproto.Account_SavedMusicIds, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.getSavedMusicIds - metadata: {%s}", c.MD)

	r, err := c.AccountGetSavedMusicIds(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// UsersGetSavedMusic
// users.getSavedMusic#788d7fe3 id:InputUser offset:int limit:int hash:long = users.SavedMusic;
func (s *Service) UsersGetSavedMusic(ctx context.Context, request *mtproto.TLUsersGetSavedMusic) (*mtproto.Users_SavedMusic, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("users.getSavedMusic - metadata: {%s}", c.MD)

	r, err := c.UsersGetSavedMusic(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// UsersGetSavedMusicByID
// users.getSavedMusicByID#7573a4e9 id:InputUser documents:Vector<InputDocument> = users.SavedMusic;
func (s *Service) UsersGetSavedMusicByID(ctx context.Context, request *mtproto.TLUsersGetSavedMusicByID) (*mtproto.Users_SavedMusic, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("users.getSavedMusicByID - metadata: {%s}", c.MD)

	r, err := c.UsersGetSavedMusicByID(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// UsersSuggestBirthday
// users.suggestBirthday#fc533372 id:InputUser birthday:Birthday = Updates;
func (s *Service) UsersSuggestBirthday(ctx context.Context, request *mtproto.TLUsersSuggestBirthday) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("users.suggestBirthday - metadata: {%s}", c.MD)

	r, err := c.UsersSuggestBirthday(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ContactsGetBirthdays
// contacts.getBirthdays#daeda864 = contacts.ContactBirthdays;
func (s *Service) ContactsGetBirthdays(ctx context.Context, request *mtproto.TLContactsGetBirthdays) (*mtproto.Contacts_ContactBirthdays, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("contacts.getBirthdays - metadata: {%s}", c.MD)

	r, err := c.ContactsGetBirthdays(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PhotosUpdateProfilePhoto
// photos.updateProfilePhoto#9e82039 flags:# fallback:flags.0?true bot:flags.1?InputUser id:InputPhoto = photos.Photo;
func (s *Service) PhotosUpdateProfilePhoto(ctx context.Context, request *mtproto.TLPhotosUpdateProfilePhoto) (*mtproto.Photos_Photo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("photos.updateProfilePhoto - metadata: {%s}", c.MD)

	r, err := c.PhotosUpdateProfilePhoto(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PhotosUploadProfilePhoto
// photos.uploadProfilePhoto#388a3b5 flags:# fallback:flags.3?true bot:flags.5?InputUser file:flags.0?InputFile video:flags.1?InputFile video_start_ts:flags.2?double video_emoji_markup:flags.4?VideoSize = photos.Photo;
func (s *Service) PhotosUploadProfilePhoto(ctx context.Context, request *mtproto.TLPhotosUploadProfilePhoto) (*mtproto.Photos_Photo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("photos.uploadProfilePhoto - metadata: {%s}", c.MD)

	r, err := c.PhotosUploadProfilePhoto(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PhotosDeletePhotos
// photos.deletePhotos#87cf7f2f id:Vector<InputPhoto> = Vector<long>;
func (s *Service) PhotosDeletePhotos(ctx context.Context, request *mtproto.TLPhotosDeletePhotos) (*mtproto.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("photos.deletePhotos - metadata: {%s}", c.MD)

	r, err := c.PhotosDeletePhotos(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PhotosGetUserPhotos
// photos.getUserPhotos#91cd32a8 user_id:InputUser offset:int max_id:long limit:int = photos.Photos;
func (s *Service) PhotosGetUserPhotos(ctx context.Context, request *mtproto.TLPhotosGetUserPhotos) (*mtproto.Photos_Photos, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("photos.getUserPhotos - metadata: {%s}", c.MD)

	r, err := c.PhotosGetUserPhotos(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// PhotosUploadContactProfilePhoto
// photos.uploadContactProfilePhoto#e14c4a71 flags:# suggest:flags.3?true save:flags.4?true user_id:InputUser file:flags.0?InputFile video:flags.1?InputFile video_start_ts:flags.2?double video_emoji_markup:flags.5?VideoSize = photos.Photo;
func (s *Service) PhotosUploadContactProfilePhoto(ctx context.Context, request *mtproto.TLPhotosUploadContactProfilePhoto) (*mtproto.Photos_Photo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("photos.uploadContactProfilePhoto - metadata: {%s}", c.MD)

	r, err := c.PhotosUploadContactProfilePhoto(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelsSetMainProfileTab
// channels.setMainProfileTab#3583fcb1 channel:InputChannel tab:ProfileTab = Bool;
func (s *Service) ChannelsSetMainProfileTab(ctx context.Context, request *mtproto.TLChannelsSetMainProfileTab) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.setMainProfileTab - metadata: {%s}", c.MD)

	r, err := c.ChannelsSetMainProfileTab(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// AccountUpdateVerified
// account.updateVerified flags:# id:long verified:flags.0?true = User;
func (s *Service) AccountUpdateVerified(ctx context.Context, request *mtproto.TLAccountUpdateVerified) (*mtproto.User, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("account.updateVerified - metadata: {%s}", c.MD)

	r, err := c.AccountUpdateVerified(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
