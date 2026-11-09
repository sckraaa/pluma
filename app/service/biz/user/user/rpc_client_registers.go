/*
 * WARNING! All changes made in this file will be lost!
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2024-present,  Teamgram Authors.
 *  All rights reserved.
 *
 * Author: Benqi (wubenqi@gmail.com)
 */

package user

import (
	"reflect"

	"github.com/teamgram/proto/mtproto"
)

var _ *mtproto.Bool

type newRPCReplyFunc func() interface{}

type RPCContextTuple struct {
	Method       string
	NewReplyFunc newRPCReplyFunc
}

var rpcContextRegisters = map[string]RPCContextTuple{
	"TLUserGetLastSeens":                  {"/mtproto.RPCUser/user_getLastSeens", func() interface{} { return new(Vector_LastSeenData) }},
	"TLUserUpdateLastSeen":                {"/mtproto.RPCUser/user_updateLastSeen", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetLastSeen":                   {"/mtproto.RPCUser/user_getLastSeen", func() interface{} { return new(LastSeenData) }},
	"TLUserGetImmutableUser":              {"/mtproto.RPCUser/user_getImmutableUser", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserGetMutableUsers":               {"/mtproto.RPCUser/user_getMutableUsers", func() interface{} { return new(Vector_ImmutableUser) }},
	"TLUserGetImmutableUserByPhone":       {"/mtproto.RPCUser/user_getImmutableUserByPhone", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserGetImmutableUserByToken":       {"/mtproto.RPCUser/user_getImmutableUserByToken", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserSetAccountDaysTTL":             {"/mtproto.RPCUser/user_setAccountDaysTTL", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetAccountDaysTTL":             {"/mtproto.RPCUser/user_getAccountDaysTTL", func() interface{} { return new(mtproto.AccountDaysTTL) }},
	"TLUserGetNotifySettings":             {"/mtproto.RPCUser/user_getNotifySettings", func() interface{} { return new(mtproto.PeerNotifySettings) }},
	"TLUserGetNotifySettingsList":         {"/mtproto.RPCUser/user_getNotifySettingsList", func() interface{} { return new(Vector_PeerPeerNotifySettings) }},
	"TLUserSetNotifySettings":             {"/mtproto.RPCUser/user_setNotifySettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserResetNotifySettings":           {"/mtproto.RPCUser/user_resetNotifySettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetAllNotifySettings":          {"/mtproto.RPCUser/user_getAllNotifySettings", func() interface{} { return new(Vector_PeerPeerNotifySettings) }},
	"TLUserGetGlobalPrivacySettings":      {"/mtproto.RPCUser/user_getGlobalPrivacySettings", func() interface{} { return new(mtproto.GlobalPrivacySettings) }},
	"TLUserSetGlobalPrivacySettings":      {"/mtproto.RPCUser/user_setGlobalPrivacySettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetPrivacy":                    {"/mtproto.RPCUser/user_getPrivacy", func() interface{} { return new(Vector_PrivacyRule) }},
	"TLUserSetPrivacy":                    {"/mtproto.RPCUser/user_setPrivacy", func() interface{} { return new(mtproto.Bool) }},
	"TLUserCheckPrivacy":                  {"/mtproto.RPCUser/user_checkPrivacy", func() interface{} { return new(mtproto.Bool) }},
	"TLUserAddPeerSettings":               {"/mtproto.RPCUser/user_addPeerSettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetPeerSettings":               {"/mtproto.RPCUser/user_getPeerSettings", func() interface{} { return new(mtproto.PeerSettings) }},
	"TLUserDeletePeerSettings":            {"/mtproto.RPCUser/user_deletePeerSettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserChangePhone":                   {"/mtproto.RPCUser/user_changePhone", func() interface{} { return new(mtproto.Bool) }},
	"TLUserCreateNewUser":                 {"/mtproto.RPCUser/user_createNewUser", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserDeleteUser":                    {"/mtproto.RPCUser/user_deleteUser", func() interface{} { return new(mtproto.Bool) }},
	"TLUserBlockPeer":                     {"/mtproto.RPCUser/user_blockPeer", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUnBlockPeer":                   {"/mtproto.RPCUser/user_unBlockPeer", func() interface{} { return new(mtproto.Bool) }},
	"TLUserBlockedByUser":                 {"/mtproto.RPCUser/user_blockedByUser", func() interface{} { return new(mtproto.Bool) }},
	"TLUserIsBlockedByUser":               {"/mtproto.RPCUser/user_isBlockedByUser", func() interface{} { return new(mtproto.Bool) }},
	"TLUserCheckBlockUserList":            {"/mtproto.RPCUser/user_checkBlockUserList", func() interface{} { return new(Vector_Long) }},
	"TLUserGetBlockedList":                {"/mtproto.RPCUser/user_getBlockedList", func() interface{} { return new(Vector_PeerBlocked) }},
	"TLUserGetContactSignUpNotification":  {"/mtproto.RPCUser/user_getContactSignUpNotification", func() interface{} { return new(mtproto.Bool) }},
	"TLUserSetContactSignUpNotification":  {"/mtproto.RPCUser/user_setContactSignUpNotification", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetContentSettings":            {"/mtproto.RPCUser/user_getContentSettings", func() interface{} { return new(mtproto.Account_ContentSettings) }},
	"TLUserSetContentSettings":            {"/mtproto.RPCUser/user_setContentSettings", func() interface{} { return new(mtproto.Bool) }},
	"TLUserDeleteContact":                 {"/mtproto.RPCUser/user_deleteContact", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetContactList":                {"/mtproto.RPCUser/user_getContactList", func() interface{} { return new(Vector_ContactData) }},
	"TLUserGetContactIdList":              {"/mtproto.RPCUser/user_getContactIdList", func() interface{} { return new(Vector_Long) }},
	"TLUserGetContact":                    {"/mtproto.RPCUser/user_getContact", func() interface{} { return new(mtproto.ContactData) }},
	"TLUserAddContact":                    {"/mtproto.RPCUser/user_addContact", func() interface{} { return new(mtproto.Bool) }},
	"TLUserCheckContact":                  {"/mtproto.RPCUser/user_checkContact", func() interface{} { return new(mtproto.Bool) }},
	"TLUserImportContacts":                {"/mtproto.RPCUser/user_importContacts", func() interface{} { return new(UserImportedContacts) }},
	"TLUserGetCountryCode":                {"/mtproto.RPCUser/user_getCountryCode", func() interface{} { return new(mtproto.String) }},
	"TLUserUpdateAbout":                   {"/mtproto.RPCUser/user_updateAbout", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdateFirstAndLastName":        {"/mtproto.RPCUser/user_updateFirstAndLastName", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdateVerified":                {"/mtproto.RPCUser/user_updateVerified", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdateUsername":                {"/mtproto.RPCUser/user_updateUsername", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdateProfilePhoto":            {"/mtproto.RPCUser/user_updateProfilePhoto", func() interface{} { return new(mtproto.Int64) }},
	"TLUserDeleteProfilePhotos":           {"/mtproto.RPCUser/user_deleteProfilePhotos", func() interface{} { return new(mtproto.Int64) }},
	"TLUserGetProfilePhotos":              {"/mtproto.RPCUser/user_getProfilePhotos", func() interface{} { return new(Vector_Long) }},
	"TLUserSetBotCommands":                {"/mtproto.RPCUser/user_setBotCommands", func() interface{} { return new(mtproto.Bool) }},
	"TLUserIsBot":                         {"/mtproto.RPCUser/user_isBot", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetBotInfo":                    {"/mtproto.RPCUser/user_getBotInfo", func() interface{} { return new(mtproto.BotInfo) }},
	"TLUserCheckBots":                     {"/mtproto.RPCUser/user_checkBots", func() interface{} { return new(Vector_Long) }},
	"TLUserGetFullUser":                   {"/mtproto.RPCUser/user_getFullUser", func() interface{} { return new(mtproto.Users_UserFull) }},
	"TLUserUpdateEmojiStatus":             {"/mtproto.RPCUser/user_updateEmojiStatus", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetUserDataById":               {"/mtproto.RPCUser/user_getUserDataById", func() interface{} { return new(mtproto.UserData) }},
	"TLUserGetUserDataListByIdList":       {"/mtproto.RPCUser/user_getUserDataListByIdList", func() interface{} { return new(Vector_UserData) }},
	"TLUserGetUserDataByToken":            {"/mtproto.RPCUser/user_getUserDataByToken", func() interface{} { return new(mtproto.UserData) }},
	"TLUserSearch":                        {"/mtproto.RPCUser/user_search", func() interface{} { return new(UsersFound) }},
	"TLUserUpdateBotData":                 {"/mtproto.RPCUser/user_updateBotData", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetImmutableUserV2":            {"/mtproto.RPCUser/user_getImmutableUserV2", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserGetMutableUsersV2":             {"/mtproto.RPCUser/user_getMutableUsersV2", func() interface{} { return new(mtproto.MutableUsers) }},
	"TLUserCreateNewTestUser":             {"/mtproto.RPCUser/user_createNewTestUser", func() interface{} { return new(mtproto.ImmutableUser) }},
	"TLUserEditCloseFriends":              {"/mtproto.RPCUser/user_editCloseFriends", func() interface{} { return new(mtproto.Bool) }},
	"TLUserSetStoriesMaxId":               {"/mtproto.RPCUser/user_setStoriesMaxId", func() interface{} { return new(mtproto.Bool) }},
	"TLUserSetColor":                      {"/mtproto.RPCUser/user_setColor", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdateBirthday":                {"/mtproto.RPCUser/user_updateBirthday", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetBirthdays":                  {"/mtproto.RPCUser/user_getBirthdays", func() interface{} { return new(Vector_ContactBirthday) }},
	"TLUserSetStoriesHidden":              {"/mtproto.RPCUser/user_setStoriesHidden", func() interface{} { return new(mtproto.Bool) }},
	"TLUserUpdatePersonalChannel":         {"/mtproto.RPCUser/user_updatePersonalChannel", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetUserIdByPhone":              {"/mtproto.RPCUser/user_getUserIdByPhone", func() interface{} { return new(mtproto.Int64) }},
	"TLUserSetAuthorizationTTL":           {"/mtproto.RPCUser/user_setAuthorizationTTL", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetAuthorizationTTL":           {"/mtproto.RPCUser/user_getAuthorizationTTL", func() interface{} { return new(mtproto.AccountDaysTTL) }},
	"TLUserUpdatePremium":                 {"/mtproto.RPCUser/user_updatePremium", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetBotInfoV2":                  {"/mtproto.RPCUser/user_getBotInfoV2", func() interface{} { return new(BotInfoData) }},
	"TLUserSaveMusic":                     {"/mtproto.RPCUser/user_saveMusic", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetSavedMusicIdList":           {"/mtproto.RPCUser/user_getSavedMusicIdList", func() interface{} { return new(Vector_Long) }},
	"TLUserSetMainProfileTab":             {"/mtproto.RPCUser/user_setMainProfileTab", func() interface{} { return new(mtproto.Bool) }},
	"TLUserSetDefaultHistoryTTL":          {"/mtproto.RPCUser/user_setDefaultHistoryTTL", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetDefaultHistoryTTL":          {"/mtproto.RPCUser/user_getDefaultHistoryTTL", func() interface{} { return new(mtproto.DefaultHistoryTTL) }},
	"TLUserGetAccountUsername":            {"/mtproto.RPCUser/user_getAccountUsername", func() interface{} { return new(UsernameData) }},
	"TLUserCheckAccountUsername":          {"/mtproto.RPCUser/user_checkAccountUsername", func() interface{} { return new(UsernameExist) }},
	"TLUserGetChannelUsername":            {"/mtproto.RPCUser/user_getChannelUsername", func() interface{} { return new(UsernameData) }},
	"TLUserCheckChannelUsername":          {"/mtproto.RPCUser/user_checkChannelUsername", func() interface{} { return new(UsernameExist) }},
	"TLUserUpdateUsernameByPeer":          {"/mtproto.RPCUser/user_updateUsernameByPeer", func() interface{} { return new(mtproto.Bool) }},
	"TLUserCheckUsername":                 {"/mtproto.RPCUser/user_checkUsername", func() interface{} { return new(UsernameExist) }},
	"TLUserUpdateUsernameByUsername":      {"/mtproto.RPCUser/user_updateUsernameByUsername", func() interface{} { return new(mtproto.Bool) }},
	"TLUserDeleteUsername":                {"/mtproto.RPCUser/user_deleteUsername", func() interface{} { return new(mtproto.Bool) }},
	"TLUserResolveUsername":               {"/mtproto.RPCUser/user_resolveUsername", func() interface{} { return new(mtproto.Peer) }},
	"TLUserGetListByUsernameList":         {"/mtproto.RPCUser/user_getListByUsernameList", func() interface{} { return new(Vector_UsernameData) }},
	"TLUserDeleteUsernameByPeer":          {"/mtproto.RPCUser/user_deleteUsernameByPeer", func() interface{} { return new(mtproto.Bool) }},
	"TLUserSearchUsername":                {"/mtproto.RPCUser/user_searchUsername", func() interface{} { return new(Vector_UsernameData) }},
	"TLUserToggleUsername":                {"/mtproto.RPCUser/user_toggleUsername", func() interface{} { return new(mtproto.Bool) }},
	"TLUserReorderUsernames":              {"/mtproto.RPCUser/user_reorderUsernames", func() interface{} { return new(mtproto.Bool) }},
	"TLUserDeactivateAllChannelUsernames": {"/mtproto.RPCUser/user_deactivateAllChannelUsernames", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetSavedContacts":              {"/mtproto.RPCUser/user_getSavedContacts", func() interface{} { return new(Vector_SavedContact) }},
	"TLUserResetSavedContacts":            {"/mtproto.RPCUser/user_resetSavedContacts", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetTopPeers":                   {"/mtproto.RPCUser/user_getTopPeers", func() interface{} { return new(mtproto.Contacts_TopPeers) }},
	"TLUserToggleTopPeers":                {"/mtproto.RPCUser/user_toggleTopPeers", func() interface{} { return new(mtproto.Bool) }},
	"TLUserResetTopPeerRating":            {"/mtproto.RPCUser/user_resetTopPeerRating", func() interface{} { return new(mtproto.Bool) }},

	// pluma: task 019
	"TLUserNewAuthorization": {"/mtproto.RPCUser/user_newAuthorization", func() interface{} { return new(mtproto.Bool) }},

	// pluma: task 020
	"TLUserGetSavedGifIdList":          {"/mtproto.RPCUser/user_getSavedGifIdList", func() interface{} { return new(Vector_Long) }},
	"TLUserSaveGif":                    {"/mtproto.RPCUser/user_saveGif", func() interface{} { return new(mtproto.Bool) }},
	"TLUserGetReactionsNotifySettings": {"/mtproto.RPCUser/user_getReactionsNotifySettings", func() interface{} { return new(mtproto.ReactionsNotifySettings) }},
	"TLUserSetReactionsNotifySettings": {"/mtproto.RPCUser/user_setReactionsNotifySettings", func() interface{} { return new(mtproto.ReactionsNotifySettings) }},

	// pluma: task 032
	"TLUserGetStatusWatchers": {"/mtproto.RPCUser/user_getStatusWatchers", func() interface{} { return new(Vector_Long) }},

	// pluma: task 051
	"TLUserSetContactPersonalPhoto": {"/mtproto.RPCUser/user_setContactPersonalPhoto", func() interface{} { return new(mtproto.User) }},
	"TLUserAddSuggestedPhoto":       {"/mtproto.RPCUser/user_addSuggestedPhoto", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 067
	"TLUserSetTopicNotifySettings": {"/mtproto.RPCUser/user_setTopicNotifySettings", func() interface{} { return new(mtproto.Bool) }},
	// pluma: task 067
	"TLUserGetTopicNotifySettings": {"/mtproto.RPCUser/user_getTopicNotifySettings", func() interface{} { return new(mtproto.PeerNotifySettings) }},
	// pluma: task 067
	"TLUserGetTopicNotifyExceptions": {"/mtproto.RPCUser/user_getTopicNotifyExceptions", func() interface{} { return new(mtproto.Updates) }},
}

func FindRPCContextTuple(t interface{}) *RPCContextTuple {
	rt := reflect.TypeOf(t)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	m, ok := rpcContextRegisters[rt.Name()]
	if !ok {
		// log.Errorf("Can't find name: %s", rt.Name())
		return nil
	}
	return &m
}

func GetRPCContextRegisters() map[string]RPCContextTuple {
	return rpcContextRegisters
}
