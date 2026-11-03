// Copyright 2024 Teamgram Authors
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

/*************************************************************
	inputPrivacyValueAllowContacts#d09e07b = InputPrivacyRule;
	inputPrivacyValueAllowAll#184b35ce = InputPrivacyRule;
	inputPrivacyValueAllowUsers#131cc67f users:Vector<InputUser> = InputPrivacyRule;
	inputPrivacyValueDisallowContacts#ba52007 = InputPrivacyRule;
	inputPrivacyValueDisallowAll#d66b66c9 = InputPrivacyRule;
	inputPrivacyValueDisallowUsers#90110467 users:Vector<InputUser> = InputPrivacyRule;
	inputPrivacyValueAllowChatParticipants#840649cf chats:Vector<long> = InputPrivacyRule;
	inputPrivacyValueDisallowChatParticipants#e94f0f86 chats:Vector<long> = InputPrivacyRule;
	inputPrivacyValueAllowCloseFriends#2f453e49 = InputPrivacyRule;
	inputPrivacyValueAllowPremium#77cdc9f1 = InputPrivacyRule;
	inputPrivacyValueAllowBots#5a4fcce5 = InputPrivacyRule;
	inputPrivacyValueDisallowBots#c4e57915 = InputPrivacyRule;

	privacyValueAllowContacts#fffe1bac = PrivacyRule;
	privacyValueAllowAll#65427b82 = PrivacyRule;
	privacyValueAllowUsers#b8905fb2 users:Vector<long> = PrivacyRule;
	privacyValueDisallowContacts#f888fa1a = PrivacyRule;
	privacyValueDisallowAll#8b73e763 = PrivacyRule;
	privacyValueDisallowUsers#e4621141 users:Vector<long> = PrivacyRule;
	privacyValueAllowChatParticipants#6b134e8e chats:Vector<long> = PrivacyRule;
	privacyValueDisallowChatParticipants#41c87565 chats:Vector<long> = PrivacyRule;
	privacyValueAllowCloseFriends#f7e8d89b = PrivacyRule;
	privacyValueAllowPremium#ece9814b = PrivacyRule;
	privacyValueAllowBots#21461b5d = PrivacyRule;
	privacyValueDisallowBots#f6a5f82f = PrivacyRule;
**/

const (
	RULE_TYPE_INVALID          = 0
	ALLOW_CONTACTS             = 1
	ALLOW_ALL                  = 2
	ALLOW_USERS                = 3
	DISALLOW_CONTACTS          = 4
	DISALLOW_ALL               = 5
	DISALLOW_USERS             = 6
	ALLOW_CHAT_PARTICIPANTS    = 7
	DISALLOW_CHAT_PARTICIPANTS = 8
	ALLOW_CLOSE_FRIENDS        = 9
	ALLOW_PREMIUM              = 10
	ALOW_BOTS                  = 11
	DISALLOW_BOTS              = 12
)

/*************************************************************
	inputPrivacyKeyStatusTimestamp#4f96cb18 = InputPrivacyKey;
	inputPrivacyKeyChatInvite#bdfb0426 = InputPrivacyKey;
	inputPrivacyKeyPhoneCall#fabadc5f = InputPrivacyKey;
	inputPrivacyKeyPhoneP2P#db9e70d2 = InputPrivacyKey;
	inputPrivacyKeyForwards#a4dd4c08 = InputPrivacyKey;
	inputPrivacyKeyProfilePhoto#5719bacc = InputPrivacyKey;
	inputPrivacyKeyPhoneNumber#352dafa = InputPrivacyKey;
	inputPrivacyKeyAddedByPhone#d1219bdd = InputPrivacyKey;
	inputPrivacyKeyVoiceMessages#aee69d68 = InputPrivacyKey;
	inputPrivacyKeyAbout#3823cc40 = InputPrivacyKey;
	inputPrivacyKeyBirthday#d65a11cc = InputPrivacyKey;
	inputPrivacyKeyStarGiftsAutoSave#e1732341 = InputPrivacyKey;
	inputPrivacyKeyNoPaidMessages#bdc597b4 = InputPrivacyKey;
	inputPrivacyKeySavedMusic#4dbe9226 = InputPrivacyKey;

	privacyKeyStatusTimestamp#bc2eab30 = PrivacyKey;
	privacyKeyChatInvite#500e6dfa = PrivacyKey;
	privacyKeyPhoneCall#3d662b7b = PrivacyKey;
	privacyKeyPhoneP2P#39491cc8 = PrivacyKey;
	privacyKeyForwards#69ec56a3 = PrivacyKey;
	privacyKeyProfilePhoto#96151fed = PrivacyKey;
	privacyKeyPhoneNumber#d19ae46d = PrivacyKey;
	privacyKeyAddedByPhone#42ffd42b = PrivacyKey;
	privacyKeyVoiceMessages#697f414 = PrivacyKey;
	privacyKeyAbout#a486b761 = PrivacyKey;
	privacyKeyBirthday#2000a518 = PrivacyKey;
	privacyKeyStarGiftsAutoSave#2ca4fdf8 = PrivacyKey;
	privacyKeyNoPaidMessages#17d348d2 = PrivacyKey;
	privacyKeySavedMusic#ff7a571b = PrivacyKey;
**/

const (
	KEY_TYPE_INVALID     = 0
	STATUS_TIMESTAMP     = 1 //
	CHAT_INVITE          = 2
	PHONE_CALL           = 3
	PHONE_P2P            = 4
	FORWARDS             = 5
	PROFILE_PHOTO        = 6
	PHONE_NUMBER         = 7
	ADDED_BY_PHONE       = 8
	VOICE_MESSAGES       = 9
	ABOUT                = 10
	BIRTHDAY             = 11
	STAR_GIFTS_AUTO_SAVE = 12
	NO_PAID_MESSAGES     = 13
	SAVED_MUSIC          = 14
)

func FromInputPrivacyKeyType(k *InputPrivacyKey) int {
	switch k.PredicateName {
	case Predicate_inputPrivacyKeyStatusTimestamp:
		return STATUS_TIMESTAMP
	case Predicate_inputPrivacyKeyChatInvite:
		return CHAT_INVITE
	case Predicate_inputPrivacyKeyPhoneCall:
		return PHONE_CALL
	case Predicate_inputPrivacyKeyPhoneP2P:
		return PHONE_P2P
	case Predicate_inputPrivacyKeyForwards:
		return FORWARDS
	case Predicate_inputPrivacyKeyProfilePhoto:
		return PROFILE_PHOTO
	case Predicate_inputPrivacyKeyPhoneNumber:
		return PHONE_NUMBER
	case Predicate_inputPrivacyKeyAddedByPhone:
		return ADDED_BY_PHONE
	case Predicate_inputPrivacyKeyVoiceMessages:
		return VOICE_MESSAGES
	case Predicate_inputPrivacyKeyAbout:
		return ABOUT
	case Predicate_inputPrivacyKeyBirthday:
		return BIRTHDAY
	case Predicate_inputPrivacyKeyStarGiftsAutoSave:
		return STAR_GIFTS_AUTO_SAVE
	case Predicate_inputPrivacyKeyNoPaidMessages:
		return NO_PAID_MESSAGES
	case Predicate_inputPrivacyKeySavedMusic:
		return SAVED_MUSIC
	default:
		panic("type is invalid")
	}
	return KEY_TYPE_INVALID
}

func ToPrivacyKey(keyType int) (key *PrivacyKey) {
	switch keyType {
	case STATUS_TIMESTAMP:
		key = MakeTLPrivacyKeyStatusTimestamp(nil).To_PrivacyKey()
	case CHAT_INVITE:
		key = MakeTLPrivacyKeyChatInvite(nil).To_PrivacyKey()
	case PHONE_CALL:
		key = MakeTLPrivacyKeyPhoneCall(nil).To_PrivacyKey()
	case PHONE_P2P:
		key = MakeTLPrivacyKeyPhoneP2P(nil).To_PrivacyKey()
	case FORWARDS:
		key = MakeTLPrivacyKeyForwards(nil).To_PrivacyKey()
	case PROFILE_PHOTO:
		key = MakeTLPrivacyKeyProfilePhoto(nil).To_PrivacyKey()
	case PHONE_NUMBER:
		key = MakeTLPrivacyKeyPhoneNumber(nil).To_PrivacyKey()
	case ADDED_BY_PHONE:
		key = MakeTLPrivacyKeyAddedByPhone(nil).To_PrivacyKey()
	case VOICE_MESSAGES:
		key = MakeTLPrivacyKeyVoiceMessages(nil).To_PrivacyKey()
	case ABOUT:
		key = MakeTLPrivacyKeyAbout(nil).To_PrivacyKey()
	case BIRTHDAY:
		key = MakeTLPrivacyKeyBirthday(nil).To_PrivacyKey()
	case STAR_GIFTS_AUTO_SAVE:
		key = MakeTLPrivacyKeyStarGiftsAutoSave(nil).To_PrivacyKey()
	case NO_PAID_MESSAGES:
		key = MakeTLPrivacyKeyNoPaidMessages(nil).To_PrivacyKey()
	case SAVED_MUSIC:
		key = MakeTLPrivacyKeySavedMusic(nil).To_PrivacyKey()
	default:
		panic("type is invalid")
	}
	return
}

func ToPrivacyRuleByInput(userSelfId int64, inputRule *InputPrivacyRule) *PrivacyRule {
	switch inputRule.PredicateName {
	case Predicate_inputPrivacyValueAllowAll:
		return MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowContacts:
		return MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowUsers:
		return MakeTLPrivacyValueAllowUsers(&PrivacyRule{
			Users: ToUserIdListByInput(userSelfId, inputRule.GetUsers()),
		}).To_PrivacyRule()
	case Predicate_inputPrivacyValueDisallowAll:
		return MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueDisallowContacts:
		return MakeTLPrivacyValueDisallowContacts(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueDisallowUsers:
		return MakeTLPrivacyValueDisallowUsers(&PrivacyRule{
			Users: ToUserIdListByInput(userSelfId, inputRule.GetUsers()),
		}).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowChatParticipants:
		return MakeTLPrivacyValueAllowChatParticipants(&PrivacyRule{
			Chats: inputRule.GetChats(),
		}).To_PrivacyRule()
	case Predicate_inputPrivacyValueDisallowChatParticipants:
		return MakeTLPrivacyValueDisallowChatParticipants(&PrivacyRule{
			Chats: inputRule.GetChats(),
		}).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowCloseFriends:
		return MakeTLPrivacyValueAllowCloseFriends(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowPremium:
		return MakeTLPrivacyValueAllowPremium(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueAllowBots:
		return MakeTLPrivacyValueAllowBots(nil).To_PrivacyRule()
	case Predicate_inputPrivacyValueDisallowBots:
		return MakeTLPrivacyValueDisallowBots(nil).To_PrivacyRule()
	default:
		panic("type is invalid")
	}
	return nil
}

func ToPrivacyRuleListByInput(userSelfId int64, inputRules []*InputPrivacyRule) (rules []*PrivacyRule) {
	rules = make([]*PrivacyRule, 0, len(inputRules))
	for _, inputRule := range inputRules {
		rules = append(rules, ToPrivacyRuleByInput(userSelfId, inputRule))
	}
	return
}

// PickAllIdListByRules
// TODO(@benqi): pick chat and channel
func PickAllIdListByRules(rules []*PrivacyRule) (userIdList, chatIdList, channelIdList []int64) {
	userIdList = make([]int64, 0)
	chatIdList = make([]int64, 0)
	channelIdList = make([]int64, 0)
	for _, r := range rules {
		if len(r.Users) > 0 {
			userIdList = append(userIdList, r.Users...)
		}
		for _, id := range r.Chats {
			if id >= MinNebulaChatChannelID {
				channelIdList = append(channelIdList, id)
			} else {
				chatIdList = append(chatIdList, id)
			}
		}
	}
	return
}

// pluma: CheckPrivacyIsAllow answers whether the account selfId lets checkId
// see what rules guard; cbContact says whether selfId has checkId in its
// contacts and cbChat whether checkId is a member of any of the chats. It
// is EvaluatePrivacy for a viewer of whom nothing else is known (task 034).
func CheckPrivacyIsAllow(selfId int64,
	rules []*PrivacyRule,
	checkId int64,
	cbContact func(id, checkId int64) bool,
	cbChat func(checkId int64, idList []int64) bool) bool {
	if selfId == checkId {
		return true
	}
	v := PrivacyViewer{Id: checkId, Contact: cbContact(selfId, checkId)}
	if cbChat != nil {
		v.InChats = func(chats []int64) bool { return cbChat(checkId, chats) }
	}
	return EvaluatePrivacy(rules, v)
}

// pluma: PrivacyViewer is what the privacy rules of an account need to know
// of the user who looks at it (task 034, ADR 0031).
type PrivacyViewer struct {
	Id int64
	// Contact says the owner of the rules has the viewer in its contacts.
	Contact bool
	Premium bool
	Bot     bool
	// InChats answers whether the viewer is a member of any of the chats.
	// When it is nil and a chat rule is not resolved (ResolvePrivacyChats),
	// membership is unknown: an allowing chat rule does not match and a
	// disallowing one does.
	InChats func(chats []int64) bool
}

// pluma: the chat rules ResolvePrivacyChats has resolved for the viewers a
// request asked about: Users holds those of them who are members. They live
// only in the ImmutableUser views of the user service and never reach a
// client or the storage.
const (
	PredicatePrivacyAllowChatMembers    = "pluma_privacyValueAllowChatMembers"
	PredicatePrivacyDisallowChatMembers = "pluma_privacyValueDisallowChatMembers"
)

// pluma: ResolvePrivacyChats returns rules with every chat rule replaced by
// its resolved form: members(chats) gives the viewers asked about who are
// members of any of the chats. rules itself is not changed; without chat
// rules it is returned as is.
func ResolvePrivacyChats(rules []*PrivacyRule, members func(chats []int64) []int64) []*PrivacyRule {
	var out []*PrivacyRule
	for i, r := range rules {
		var predicate string
		switch r.GetPredicateName() {
		case Predicate_privacyValueAllowChatParticipants:
			predicate = PredicatePrivacyAllowChatMembers
		case Predicate_privacyValueDisallowChatParticipants:
			predicate = PredicatePrivacyDisallowChatMembers
		default:
			if out != nil {
				out = append(out, r)
			}
			continue
		}
		if out == nil {
			out = append(make([]*PrivacyRule, 0, len(rules)), rules[:i]...)
		}
		var users []int64
		if len(r.GetChats()) > 0 {
			users = members(r.GetChats())
		}
		out = append(out, &PrivacyRule{
			PredicateName: predicate,
			Constructor:   r.GetConstructor(),
			Users:         users,
			Chats:         r.GetChats(),
		})
	}
	if out == nil {
		return rules
	}
	return out
}

// pluma: EvaluatePrivacy answers whether rules let v see what they guard
// (task 034, ADR 0031). The result does not depend on the order of the
// rules, which the clients send differently (Android, iOS and tdesktop the
// exceptions first, Web K the base rule first). The more specific rule
// wins:
//  1. v is in a DisallowUsers rule: no; in an AllowUsers rule: yes;
//  2. v is a member of a chat of a DisallowChatParticipants rule: no; of an
//     AllowChatParticipants rule: yes (empty lists are ignored);
//  3. bots: DisallowBots no, AllowBots yes;
//  4. AllowPremium and v has Premium: yes;
//  5. DisallowContacts and v is a contact: no;
//  6. the base rule: AllowAll yes, AllowContacts yes for a contact, else no.
//
// With several base rules the most open one counts, as Android
// (PrivacyControlActivity) and iOS (PrivacySettings.swift) read them;
// account.setPrivacy stores one. No base rule means nobody. AllowCloseFriends
// only applies to stories and is ignored here.
func EvaluatePrivacy(rules []*PrivacyRule, v PrivacyViewer) bool {
	var (
		base                          = DISALLOW_ALL
		allowUser, disallowUser       bool
		allowChat, disallowChat       bool
		allowBots, disallowBots       bool
		allowPremium, disallowContact bool
	)
	member := func(r *PrivacyRule, resolved bool) bool {
		if len(r.GetChats()) == 0 {
			return false
		}
		if resolved {
			return containsId(r.GetUsers(), v.Id)
		}
		if v.InChats != nil {
			return v.InChats(r.GetChats())
		}
		return false
	}
	for _, r := range rules {
		switch r.GetPredicateName() {
		case Predicate_privacyValueAllowAll:
			base = ALLOW_ALL
		case Predicate_privacyValueAllowContacts:
			if base != ALLOW_ALL {
				base = ALLOW_CONTACTS
			}
		case Predicate_privacyValueDisallowAll:
		case Predicate_privacyValueAllowUsers:
			allowUser = allowUser || containsId(r.GetUsers(), v.Id)
		case Predicate_privacyValueDisallowUsers:
			disallowUser = disallowUser || containsId(r.GetUsers(), v.Id)
		case Predicate_privacyValueAllowChatParticipants:
			allowChat = allowChat || member(r, false)
		case PredicatePrivacyAllowChatMembers:
			allowChat = allowChat || member(r, true)
		case Predicate_privacyValueDisallowChatParticipants:
			// Unknown membership hides rather than shows.
			disallowChat = disallowChat || (len(r.GetChats()) > 0 && v.InChats == nil) || member(r, false)
		case PredicatePrivacyDisallowChatMembers:
			disallowChat = disallowChat || member(r, true)
		case Predicate_privacyValueAllowBots:
			allowBots = true
		case Predicate_privacyValueDisallowBots:
			disallowBots = true
		case Predicate_privacyValueAllowPremium:
			allowPremium = true
		case Predicate_privacyValueDisallowContacts:
			disallowContact = true
		}
	}
	switch {
	case disallowUser:
		return false
	case allowUser:
		return true
	case disallowChat:
		return false
	case allowChat:
		return true
	case v.Bot && disallowBots:
		return false
	case v.Bot && allowBots:
		return true
	case v.Premium && allowPremium:
		return true
	case v.Contact && disallowContact:
		return false
	}
	switch base {
	case ALLOW_ALL:
		return true
	case ALLOW_CONTACTS:
		return v.Contact
	}
	return false
}

func containsId(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

/*
privacyValueAllowContacts#fffe1bac = PrivacyRule;
privacyValueAllowAll#65427b82 = PrivacyRule;
privacyValueAllowUsers#4d5bbe0c users:Vector<int> = PrivacyRule;
privacyValueDisallowContacts#f888fa1a = PrivacyRule;
privacyValueDisallowAll#8b73e763 = PrivacyRule;
privacyValueDisallowUsers#c7f49b7 users:Vector<int> = PrivacyRule;
privacyValueAllowChatParticipants#18be796b chats:Vector<int> = PrivacyRule;
privacyValueDisallowChatParticipants#acae0690 chats:Vector<int> = PrivacyRule;
*/
// pluma: privacyIsAllow is EvaluatePrivacy for a viewer known by its id and
// whether the owner has it as a contact (task 034).
func privacyIsAllow(rules []*PrivacyRule, userId int64, isContact bool) bool {
	return EvaluatePrivacy(rules, PrivacyViewer{Id: userId, Contact: isContact})
}
