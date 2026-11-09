// Copyright (c) 2024-present,  Teamgram Studio (https://teamgram.io).
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package mtproto

import (
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (m *ImmutableUser) Id() int64 {
	return m.User.Id
}

func (m *ImmutableUser) AccessHash() int64 {
	return m.User.AccessHash
}

func (m *ImmutableUser) FirstName() string {
	return m.User.FirstName
}

func (m *ImmutableUser) LastName() string {
	return m.User.LastName
}

func (m *ImmutableUser) SetFirstName(v string) {
	m.User.FirstName = v
}

func (m *ImmutableUser) SetLastName(v string) {
	m.User.LastName = v
}

func (m *ImmutableUser) Username() string {
	return m.User.Username
}

func (m *ImmutableUser) SetUsername(v string) {
	m.User.Username = v
}

func (m *ImmutableUser) Phone() string {
	return m.User.Phone
}

func (m *ImmutableUser) Deleted() bool {
	return m.User.Deleted
}

func (m *ImmutableUser) Restricted() bool {
	return m.User.Restricted
}

func (m *ImmutableUser) ProfilePhoto() *UserProfilePhoto {
	return MakeUserProfilePhotoByPhoto(m.User.ProfilePhoto)
}

func (m *ImmutableUser) Photo() *Photo {
	return m.User.ProfilePhoto
}

// pluma: FallbackPhoto is the public photo of the user, the one shown to the
// users the privacy of the profile photo hides the main photo from
// (photos.uploadProfilePhoto with fallback, task 051).
func (m *ImmutableUser) FallbackPhoto() *Photo {
	return m.User.GetFallbackPhoto()
}

// pluma: PersonalProfilePhoto is the personal photo of the contact row c, the
// photo its owner set for the contact (photos.uploadContactProfilePhoto with
// save), as the contact's user.photo: with personal, which clients show
// instead of the contact's own (userProfilePhoto.personal). nil without one.
func PersonalProfilePhoto(c *ContactData) *UserProfilePhoto {
	if c.GetPersonalPhotoId() == 0 {
		return nil
	}
	return MakeTLUserProfilePhoto(&UserProfilePhoto{
		HasVideo: c.GetPersonalPhotoHasVideo(),
		Personal: true,
		PhotoId:  c.GetPersonalPhotoId(),
		DcId:     1,
	}).To_UserProfilePhoto()
}

// pluma: ContactRowOf is the contact row the viewer has of m, from whichever
// side the loader filled: the viewer's Contacts or m's ReverseContacts. nil
// when m is not the viewer's contact or the row is not loaded.
func (m *ImmutableUser) ContactRowOf(viewer *ImmutableUser) *ContactData {
	if c := viewer.GetContactData(m.Id()); c != nil {
		return c
	}
	return m.GetReverseContactData(viewer.Id())
}

// pluma: PhotoFor is user.photo of m as viewer sees it when the viewer's
// contact row of m is c, nil for none (task 051): the account and bots
// show their main photo; otherwise photoFor with the privacy of the profile
// photo for viewer; a deleted account none. It tells what a change of the
// row shows before the loaded users carry it.
func (m *ImmutableUser) PhotoFor(viewer *ImmutableUser, c *ContactData) *UserProfilePhoto {
	if m.Deleted() {
		return nil
	}
	if m.Id() == viewer.Id() || m.IsBot() {
		return m.ProfilePhoto()
	}
	return m.photoFor(c, m.CheckPrivacyFor(PROFILE_PHOTO, viewer))
}

// pluma: photoFor is user.photo of m for a viewer with the contact row c of
// m, when the privacy of the profile photo lets the viewer see it (allowed)
// or not (task 051): the viewer's personal photo of m first, whatever the
// privacy says; then the main photo when allowed; otherwise the public
// photo, which carries no flag (clients tell it by userFull.fallback_photo).
func (m *ImmutableUser) photoFor(c *ContactData, allowed bool) *UserProfilePhoto {
	if p := PersonalProfilePhoto(c); p != nil {
		return p
	}
	if allowed {
		return m.ProfilePhoto()
	}
	return MakeUserProfilePhotoByPhoto(m.FallbackPhoto())
}

func (m *ImmutableUser) About() string {
	return m.User.About.GetValue()
}

func (m *ImmutableUser) SetAbout(v string) {
	m.User.About = MakeFlagsString(v)
}

func (m *ImmutableUser) IsBot() bool {
	return m.User.Bot != nil
}

func (m *ImmutableUser) BotChatHistory() bool {
	return m.User.Bot.GetBotChatHistory()
}

func (m *ImmutableUser) BotNochats() bool {
	return m.User.Bot.GetBotNochats()
}

func (m *ImmutableUser) BotInlineGeo() bool {
	return m.User.Bot.GetBotInlineGeo()
}

func (m *ImmutableUser) RestrictionReason() []*RestrictionReason {
	return m.User.RestrictionReason
}

func (m *ImmutableUser) BotInlinePlaceholder() *wrapperspb.StringValue {
	return m.User.Bot.GetBotInlinePlaceholder()
}

func (m *ImmutableUser) Verified() bool {
	return m.User.Verified
}

func (m *ImmutableUser) SetVerified(v bool) {
	m.User.Verified = v
}

func (m *ImmutableUser) Support() bool {
	return m.User.Support
}

func (m *ImmutableUser) Scam() bool {
	return m.User.Scam
}

func (m *ImmutableUser) Fake() bool {
	return m.User.Fake
}

func (m *ImmutableUser) BotInfoVersion() int32 {
	return m.User.Bot.GetBotInfoVersion()
}

func (m *ImmutableUser) BotAttachMenu() bool {
	return m.User.Bot.GetBotAttachMenu()
}

// pluma: Premium is the one place that says who has Premium: every user but
// a bot (ADR 0041), whatever users.premium stores. Clients read the flag of
// their own user for their limits and features and the flag of another user
// for its animated avatar and the links of its bio (task 071).
func (m *ImmutableUser) Premium() bool {
	return !m.IsBot()
}

// ContactRequirePremium tells whether only contacts, chats there already and
// Premium users may write to the user: the user's setting, which works
// while the user has Premium (pluma: task 014).
func (m *ImmutableUser) ContactRequirePremium() bool {
	return m.User.GetNewNoncontactPeersRequirePremium() && m.Premium()
}

func (m *ImmutableUser) AttachMenuEnabled() bool {
	return m.User.Bot.GetBotAttachMenu()
}

func (m *ImmutableUser) BotCanEdit() bool {
	return m.User.Bot.GetBotCanEdit()
}

func (m *ImmutableUser) StoriesUnavailable() bool {
	return m.User.StoriesUnavailable
}

func (m *ImmutableUser) BotBusiness() bool {
	return m.User.Bot.GetBotBusiness()
}

func (m *ImmutableUser) BotHasMainApp() bool {
	return m.User.Bot.GetBotHasMainApp()
}

func (m *ImmutableUser) EmojiStatus() *EmojiStatus {
	return m.User.EmojiStatus
}

func (m *ImmutableUser) StoriesMaxId() *wrapperspb.Int32Value {
	return MakeFlagsInt32(m.User.StoriesMaxId)
}

func (m *ImmutableUser) StoriesMaxId_FLAGRECENTSTORY() *RecentStory {
	var (
		maxId *RecentStory
	)

	if m.User.StoriesMaxId > 0 {
		maxId = MakeTLRecentStory(&RecentStory{
			Live:  false,
			MaxId: MakeFlagsInt32(m.User.StoriesMaxId),
		}).To_RecentStory()
	}

	return maxId
}

func (m *ImmutableUser) Color() *PeerColor {
	return m.User.Color
}

func (m *ImmutableUser) ProfileColor() *PeerColor {
	return m.User.ProfileColor
}

func (m *ImmutableUser) Birthday() *Birthday {
	return FromBirthdayString(m.User.Birthday)
}

func (m *ImmutableUser) BotActiveUsers() *wrapperspb.Int32Value {
	return nil
}

func (m *ImmutableUser) Usernames() []*Username {
	return m.User.Usernames
}

func (m *ImmutableUser) CheckContact(cId int64) (bool, bool) {
	// TODO: sort.Search
	for _, c := range m.Contacts {
		if cId == c.ContactUserId {
			return true, c.MutualContact
		}
	}

	return false, false
}

func (m *ImmutableUser) GetContactData(cId int64) *ContactData {
	// TODO: sort.Search
	for _, c := range m.Contacts {
		if cId == c.ContactUserId {
			return c
		}
	}

	return nil
}

func (m *ImmutableUser) CheckReverseContact(cId int64) (bool, bool) {
	// TODO: sort.Search
	for _, c := range m.ReverseContacts {
		if cId == c.UserId {
			return true, c.MutualContact
		}
	}

	return false, false
}

func (m *ImmutableUser) GetReverseContactData(cId int64) *ContactData {
	// TODO: sort.Search
	for _, c := range m.ReverseContacts {
		if cId == c.UserId {
			return c
		}
	}

	return nil
}

// pluma: CheckPrivacy answers whether the user id may see what the rules of
// keyType guard (EvaluatePrivacy, task 034). The account always sees its
// own; without the rules of the key loaded nobody else does.
func (m *ImmutableUser) CheckPrivacy(keyType int, id int64) bool {
	if m.Id() == id {
		return true
	}
	isContact, _ := m.CheckContact(id)
	return m.checkPrivacy(keyType, PrivacyViewer{Id: id, Contact: isContact})
}

// pluma: CheckPrivacyFor is CheckPrivacy for a viewer whose Premium and bot
// flags count too (AllowPremium, Allow/DisallowBots).
func (m *ImmutableUser) CheckPrivacyFor(keyType int, viewer *ImmutableUser) bool {
	if m.Id() == viewer.Id() {
		return true
	}
	isContact, _ := m.CheckContact(viewer.Id())
	return m.checkPrivacy(keyType, PrivacyViewer{
		Id:      viewer.Id(),
		Contact: isContact,
		Premium: viewer.Premium(),
		Bot:     viewer.IsBot(),
	})
}

func (m *ImmutableUser) checkPrivacy(keyType int, v PrivacyViewer) bool {
	for _, k := range m.KeysPrivacyRules {
		if k.Key == int32(keyType) {
			return EvaluatePrivacy(k.Rules, v)
		}
	}
	return false
}

// pluma: StatusFor is the status of m as viewer sees it. The exact time
// needs both: m's rule lets viewer see it, and viewer's own rule lets m see
// viewer's, unless viewer has Premium. When only viewer's own rule stands
// in the way, the rounded status says so with by_me
// (core.telegram.org/constructor/privacyKeyStatusTimestamp).
func (m *ImmutableUser) StatusFor(viewer *ImmutableUser) *UserStatus {
	if m.Id() == viewer.Id() {
		return MakeUserStatus(m.LastSeenAt, true)
	}
	ownerAllows := m.CheckPrivacyFor(STATUS_TIMESTAMP, viewer)
	viewerAllows := viewer.Premium() || viewer.CheckPrivacy(STATUS_TIMESTAMP, m.Id())
	status := MakeUserStatus(m.LastSeenAt, ownerAllows && viewerAllows)
	if ownerAllows && !viewerAllows && status != nil {
		status.ByMe = true
	}
	return status
}

func (m *ImmutableUser) ToUnsafeUser(selfUser *ImmutableUser) *User {
	if m.Deleted() {
		return m.ToDeletedUser()
	}

	if m.Id() == selfUser.Id() {
		return m.ToSelfUser()
	}

	user := MakeTLUser(&User{
		Self:                         false,
		Contact:                      false,
		MutualContact:                false,
		Deleted:                      false,
		Bot:                          m.IsBot(),
		BotChatHistory:               m.BotChatHistory(),
		BotNochats:                   m.BotNochats(),
		Verified:                     m.Verified(),
		Restricted:                   m.Restricted(),
		Min:                          false,
		BotInlineGeo:                 m.BotInlineGeo(),
		Support:                      m.Support(),
		Scam:                         m.Scam(),
		ApplyMinPhoto:                false,
		Fake:                         m.Fake(),
		BotAttachMenu:                m.BotAttachMenu(),
		Premium:                      m.Premium(),
		AttachMenuEnabled:            m.AttachMenuEnabled(),
		BotCanEdit:                   m.BotCanEdit(),
		CloseFriend:                  false,
		StoriesHidden:                false,
		StoriesUnavailable:           m.StoriesUnavailable(),
		ContactRequirePremium:        m.ContactRequirePremium(), // pluma: new_noncontact_peers_require_premium
		BotBusiness:                  m.BotBusiness(),
		BotHasMainApp:                m.BotHasMainApp(),
		Id:                           m.Id(),
		AccessHash:                   MakeFlagsInt64(m.AccessHash()),
		FirstName:                    MakeFlagsString(m.FirstName()),
		LastName:                     MakeFlagsString(m.LastName()),
		Username:                     nil,
		Phone:                        nil,
		Photo:                        nil,
		Status:                       MakeUserStatus(m.LastSeenAt, true),
		BotInfoVersion:               MakeFlagsInt32(m.BotInfoVersion()),
		RestrictionReason:            m.RestrictionReason(),
		BotInlinePlaceholder:         m.BotInlinePlaceholder(),
		LangCode:                     nil,
		EmojiStatus:                  m.EmojiStatus(),
		Usernames:                    nil,
		StoriesMaxId_FLAGINT32:       nil,
		StoriesMaxId_FLAGRECENTSTORY: nil,
		Color_FLAGPEERCOLOR:          m.Color(),
		Color:                        m.Color().GetColor(),
		Color_FLAGINT32:              m.Color().GetColor(),
		BackgroundEmojiId:            m.Color().GetBackgroundEmojiId(),
		ProfileColor:                 m.ProfileColor(),
		BotActiveUsers:               m.BotActiveUsers(),
	}).To_User()

	if len(m.Usernames()) > 1 {
		user.Username = nil
		user.Usernames = m.Usernames()
	} else {
		user.Username = MakeFlagsString(m.Username())
		user.Usernames = nil
	}

	if m.IsBot() {
		user.Photo = m.ProfilePhoto()
		user.Status = nil
		user.StoriesUnavailable = true
		return user
	}

	// contact
	contact := selfUser.GetContactData(m.Id())
	row := contact
	if contact != nil {
		user.Contact = true
		user.MutualContact = contact.MutualContact
		user.FirstName = contact.FirstName
		user.LastName = contact.LastName
		// user.Phone = contact.Phone
		user.CloseFriend = contact.CloseFriend
		user.StoriesHidden = contact.StoriesHidden
		user.StoriesMaxId_FLAGINT32 = m.StoriesMaxId()
		user.StoriesMaxId_FLAGRECENTSTORY = m.StoriesMaxId_FLAGRECENTSTORY()
	} else {
		// reverse contact
		reverseContact := m.GetReverseContactData(selfUser.Id())
		row = reverseContact
		if reverseContact != nil {
			user.Contact = true
			user.MutualContact = reverseContact.MutualContact
			user.FirstName = reverseContact.FirstName
			user.LastName = reverseContact.LastName
			// user.Phone = reverseContact.Phone
			user.CloseFriend = reverseContact.CloseFriend
			user.StoriesHidden = reverseContact.StoriesHidden
		}
	}

	// phone
	if m.CheckPrivacyFor(PHONE_NUMBER, selfUser) {
		user.Phone = MakeFlagsString(m.Phone())
	}

	// photo: personal, main or public (pluma: task 051)
	user.Photo = m.photoFor(row, m.CheckPrivacyFor(PROFILE_PHOTO, selfUser))

	// status
	user.Status = m.StatusFor(selfUser)

	//// TODO: check stories_unavailable
	// user.StoriesUnavailable = false

	return user
}

func (m *ImmutableUser) ToSelfUser() *User {
	user := MakeTLUser(&User{
		Self: true,
		// pluma: Telegram does not mark the account a contact of itself
		// and sets apply_min_photo (conformance fixture saved_messages).
		Contact:                      false,
		MutualContact:                false,
		Deleted:                      false,
		Bot:                          m.IsBot(),
		BotChatHistory:               m.BotChatHistory(),
		BotNochats:                   m.BotNochats(),
		Verified:                     m.Verified(),
		Restricted:                   m.Restricted(),
		Min:                          false,
		BotInlineGeo:                 m.BotInlineGeo(),
		Support:                      m.Support(),
		Scam:                         m.Scam(),
		ApplyMinPhoto:                true, // pluma: see Contact
		Fake:                         m.Fake(),
		BotAttachMenu:                m.BotAttachMenu(),
		Premium:                      m.Premium(),
		AttachMenuEnabled:            m.AttachMenuEnabled(),
		BotCanEdit:                   m.BotCanEdit(),
		CloseFriend:                  false,
		StoriesHidden:                false,
		StoriesUnavailable:           m.StoriesUnavailable(),
		ContactRequirePremium:        false,
		BotBusiness:                  m.BotBusiness(),
		BotHasMainApp:                m.BotHasMainApp(),
		Id:                           m.Id(),
		AccessHash:                   MakeFlagsInt64(m.AccessHash()),
		FirstName:                    MakeFlagsString(m.FirstName()),
		LastName:                     MakeFlagsString(m.LastName()),
		Username:                     nil,
		Phone:                        MakeFlagsString(m.Phone()),
		Photo:                        m.ProfilePhoto(),
		Status:                       MakeUserStatus(m.LastSeenAt, true),
		BotInfoVersion:               MakeFlagsInt32(m.BotInfoVersion()),
		RestrictionReason:            m.RestrictionReason(),
		BotInlinePlaceholder:         m.BotInlinePlaceholder(),
		LangCode:                     nil,
		EmojiStatus:                  m.EmojiStatus(),
		Usernames:                    nil,
		StoriesMaxId_FLAGINT32:       m.StoriesMaxId(),
		StoriesMaxId_FLAGRECENTSTORY: m.StoriesMaxId_FLAGRECENTSTORY(),
		Color_FLAGPEERCOLOR:          m.Color(),
		Color:                        m.Color().GetColor(),
		Color_FLAGINT32:              m.Color().GetColor(),
		BackgroundEmojiId:            m.Color().GetBackgroundEmojiId(),
		ProfileColor:                 m.ProfileColor(),
		BotActiveUsers:               m.BotActiveUsers(),
	}).To_User()

	if m.IsBot() {
		user.Photo = m.ProfilePhoto()
		user.Status = nil
		user.StoriesUnavailable = true
	}

	if len(m.Usernames()) > 1 {
		user.Username = nil
		user.Usernames = m.Usernames()
	} else {
		user.Username = MakeFlagsString(m.Username())
		user.Usernames = nil
	}

	return user
}

func (m *ImmutableUser) ToDeletedUser() *User {
	return MakeTLUser(&User{
		Self:                         false,
		Contact:                      false,
		MutualContact:                false,
		Deleted:                      true,
		Bot:                          false,
		BotChatHistory:               false,
		BotNochats:                   false,
		Verified:                     false,
		Restricted:                   false,
		Min:                          false,
		BotInlineGeo:                 false,
		Support:                      false,
		Scam:                         false,
		ApplyMinPhoto:                false,
		Fake:                         false,
		BotAttachMenu:                false,
		Premium:                      false,
		AttachMenuEnabled:            false,
		BotCanEdit:                   false,
		CloseFriend:                  false,
		StoriesHidden:                false,
		StoriesUnavailable:           false,
		ContactRequirePremium:        false,
		BotBusiness:                  false,
		BotHasMainApp:                false,
		Id:                           m.Id(),
		AccessHash:                   MakeFlagsInt64(m.AccessHash()),
		FirstName:                    nil,
		LastName:                     nil,
		Username:                     nil,
		Phone:                        nil,
		Photo:                        nil,
		Status:                       nil,
		BotInfoVersion:               nil,
		RestrictionReason:            nil,
		BotInlinePlaceholder:         nil,
		LangCode:                     nil,
		EmojiStatus:                  nil,
		Usernames:                    nil,
		StoriesMaxId_FLAGINT32:       nil,
		StoriesMaxId_FLAGRECENTSTORY: nil,
		Color_FLAGPEERCOLOR:          nil,
		Color:                        nil,
		Color_FLAGINT32:              nil,
		BackgroundEmojiId:            nil,
		ProfileColor:                 nil,
		BotActiveUsers:               nil,
	}).To_User()
}

// ToUser is m as the user selfUserId sees it when nothing else is known of
// that user: its Premium and its own last seen rule do not count (ToUserFor).
func (m *ImmutableUser) ToUser(selfUserId int64) *User {
	return m.toUser(selfUserId, nil)
}

// pluma: ToUserFor is ToUser for a viewer whose Premium and own last seen
// rule count, as in ToUnsafeUser (task 034).
func (m *ImmutableUser) ToUserFor(viewer *ImmutableUser) *User {
	return m.toUser(viewer.Id(), viewer)
}

func (m *ImmutableUser) checkPrivacyOf(keyType int, selfUserId int64, viewer *ImmutableUser) bool {
	if viewer != nil {
		return m.CheckPrivacyFor(keyType, viewer)
	}
	return m.CheckPrivacy(keyType, selfUserId)
}

func (m *ImmutableUser) toUser(selfUserId int64, viewer *ImmutableUser) *User {
	if m.Deleted() {
		return m.ToDeletedUser()
	}

	if m.Id() == selfUserId {
		return m.ToSelfUser()
	}

	user := MakeTLUser(&User{
		Self:                         false,
		Contact:                      false,
		MutualContact:                false,
		Deleted:                      false,
		Bot:                          m.IsBot(),
		BotChatHistory:               m.BotChatHistory(),
		BotNochats:                   m.BotNochats(),
		Verified:                     m.Verified(),
		Restricted:                   m.Restricted(),
		Min:                          false,
		BotInlineGeo:                 m.BotInlineGeo(),
		Support:                      m.Support(),
		Scam:                         m.Scam(),
		ApplyMinPhoto:                false,
		Fake:                         m.Fake(),
		BotAttachMenu:                m.BotAttachMenu(),
		Premium:                      m.Premium(),
		AttachMenuEnabled:            m.AttachMenuEnabled(),
		BotCanEdit:                   m.BotCanEdit(),
		CloseFriend:                  false,
		StoriesHidden:                false,
		StoriesUnavailable:           m.StoriesUnavailable(),
		Id:                           m.Id(),
		ContactRequirePremium:        m.ContactRequirePremium(), // pluma: new_noncontact_peers_require_premium
		BotBusiness:                  m.BotBusiness(),
		BotHasMainApp:                m.BotHasMainApp(),
		AccessHash:                   MakeFlagsInt64(m.AccessHash()),
		FirstName:                    MakeFlagsString(m.FirstName()),
		LastName:                     MakeFlagsString(m.LastName()),
		Username:                     nil,
		Phone:                        nil,
		Photo:                        nil,
		Status:                       MakeUserStatus(m.LastSeenAt, false),
		BotInfoVersion:               MakeFlagsInt32(m.BotInfoVersion()),
		RestrictionReason:            m.RestrictionReason(),
		BotInlinePlaceholder:         m.BotInlinePlaceholder(),
		LangCode:                     nil,
		EmojiStatus:                  m.EmojiStatus(),
		Usernames:                    nil,
		StoriesMaxId_FLAGINT32:       nil,
		StoriesMaxId_FLAGRECENTSTORY: nil,
		Color_FLAGPEERCOLOR:          m.Color(),
		Color:                        m.Color().GetColor(),
		Color_FLAGINT32:              m.Color().GetColor(),
		BackgroundEmojiId:            m.Color().GetBackgroundEmojiId(),
		ProfileColor:                 m.ProfileColor(),
		BotActiveUsers:               m.BotActiveUsers(),
	}).To_User()

	if len(m.Usernames()) > 1 {
		user.Username = nil
		user.Usernames = m.Usernames()
	} else {
		user.Username = MakeFlagsString(m.Username())
		user.Usernames = nil
	}

	if m.IsBot() {
		user.Photo = m.ProfilePhoto()
		user.Status = nil
		user.StoriesUnavailable = true
		return user
	}

	reverseContact := m.GetReverseContactData(selfUserId)
	if reverseContact != nil {
		user.Contact = true
		user.MutualContact = reverseContact.MutualContact
		user.FirstName = reverseContact.FirstName
		user.LastName = reverseContact.LastName
		// user.Phone = reverseContact.Phone
		user.CloseFriend = reverseContact.CloseFriend
		user.StoriesHidden = reverseContact.StoriesHidden
		user.StoriesMaxId_FLAGINT32 = m.StoriesMaxId()
		user.StoriesMaxId_FLAGRECENTSTORY = m.StoriesMaxId_FLAGRECENTSTORY()
	}

	// phone
	if m.checkPrivacyOf(PHONE_NUMBER, selfUserId, viewer) {
		user.Phone = MakeFlagsString(m.Phone())
	}

	// photo: personal, main or public (pluma: task 051)
	user.Photo = m.photoFor(reverseContact, m.checkPrivacyOf(PROFILE_PHOTO, selfUserId, viewer))

	// status
	if viewer != nil {
		user.Status = m.StatusFor(viewer)
	} else {
		user.Status = MakeUserStatus(m.LastSeenAt, m.CheckPrivacy(STATUS_TIMESTAMP, selfUserId))
	}

	// TODO: check stories_unavailable
	// user.StoriesUnavailable = false

	return user
}
