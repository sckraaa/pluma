// Copyright 2022 Teamgram Authors
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

package dao

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/teamgram/marmota/pkg/hack"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/mr"
	"google.golang.org/protobuf/proto"
)

var (
	userPrivacyKeyStatusTimestampPrefix   = "user_privacy_status_timestamp"
	userPrivacyKeyChatInvitePrefix        = "user_privacy_chat_invite"
	userPrivacyKeyPhoneCallPrefix         = "user_privacy_phone_call"
	userPrivacyKeyPhoneP2PPrefix          = "user_privacy_phone_p2p"
	userPrivacyKeyForwardsPrefix          = "user_privacy_forwards"
	userPrivacyKeyProfilePhotoPrefix      = "user_privacy_profile_photo"
	userPrivacyKeyPhoneNumberPrefix       = "user_privacy_phone_number"
	userPrivacyKeyAddedByPhonePrefix      = "user_privacy_added_by_phone"
	userPrivacyKeyVoiceMessagesPrefix     = "user_privacy_voice_messages"
	userPrivacyKeyAboutPrefix             = "user_privacy_about"
	userPrivacyKeyBirthdayPrefix          = "user_privacy_birthday"
	userPrivacyKeyStarGiftsAutoSavePrefix = "user_privacy_star_gifts_auto_save"
	userPrivacyKeyNoPaidMessagesPrefix    = "user_privacy_no_paid_messages"
	userPrivacyKeySavedMusicPrefix        = "user_privacy_saved_music"
)

func genUserPrivacyKeyPrefix(id int64, keyType int32) string {
	switch keyType {
	case mtproto.STATUS_TIMESTAMP:
		return genUserPrivacyKeyStatusTimestampPrefix(id)
	case mtproto.CHAT_INVITE:
		return genUserPrivacyKeyChatInvitePrefix(id)
	case mtproto.PHONE_CALL:
		return genUserPrivacyKeyPhoneCallPrefix(id)
	case mtproto.PHONE_P2P:
		return genUserPrivacyKeyPhoneP2PPrefix(id)
	case mtproto.FORWARDS:
		return genUserPrivacyKeyForwardsPrefix(id)
	case mtproto.PROFILE_PHOTO:
		return genUserPrivacyKeyProfilePhotoPrefix(id)
	case mtproto.PHONE_NUMBER:
		return genUserPrivacyKeyPhoneNumberPrefix(id)
	case mtproto.ADDED_BY_PHONE:
		return genUserPrivacyKeyAddedByPhonePrefix(id)
	case mtproto.VOICE_MESSAGES:
		return genUserPrivacyKeyVoiceMessagesPrefix(id)
	case mtproto.ABOUT:
		return genUserPrivacyKeyAboutPrefix(id)
	case mtproto.BIRTHDAY:
		return genUserPrivacyKeyBirthdayPrefix(id)
	case mtproto.STAR_GIFTS_AUTO_SAVE:
		return genUserPrivacyKeyStarGiftsAutoSavePrefix(id)
	case mtproto.NO_PAID_MESSAGES:
		return genUserPrivacyKeyNoPaidMessagesPrefix(id)
	case mtproto.SAVED_MUSIC:
		return genUserPrivacyKeySavedMusicPrefix(id)
	default:
		return ""
	}
}

func genUserPrivacyKeyStatusTimestampPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyStatusTimestampPrefix, id)
}

func genUserPrivacyKeyChatInvitePrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyChatInvitePrefix, id)
}

func genUserPrivacyKeyPhoneCallPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyPhoneCallPrefix, id)
}

func genUserPrivacyKeyPhoneP2PPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyPhoneP2PPrefix, id)
}

func genUserPrivacyKeyForwardsPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyForwardsPrefix, id)
}

func genUserPrivacyKeyProfilePhotoPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyProfilePhotoPrefix, id)
}

func genUserPrivacyKeyPhoneNumberPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyPhoneNumberPrefix, id)
}

func genUserPrivacyKeyAddedByPhonePrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyAddedByPhonePrefix, id)
}

func genUserPrivacyKeyVoiceMessagesPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyVoiceMessagesPrefix, id)
}

func genUserPrivacyKeyAboutPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyAboutPrefix, id)
}

func genUserPrivacyKeyBirthdayPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyBirthdayPrefix, id)
}

// userPrivacyKeyStarGiftsAutoSavePrefix
func genUserPrivacyKeyStarGiftsAutoSavePrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyStarGiftsAutoSavePrefix, id)
}

// userPrivacyKeyNoPaidMessagesPrefix
func genUserPrivacyKeyNoPaidMessagesPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeyNoPaidMessagesPrefix, id)
}

// userPrivacyKeySavedMusicPrefix
func genUserPrivacyKeySavedMusicPrefix(id int64) string {
	return fmt.Sprintf("%s_%d", userPrivacyKeySavedMusicPrefix, id)
}

func (d *Dao) GetUserPrivacyRulesListByKeys(ctx context.Context, id int64, keys ...int32) []*mtproto.PrivacyKeyRules {
	var (
		cacheRules []*mtproto.PrivacyKeyRules
		// cacheKey   string
	)

	if len(keys) == 1 {
		rules, _ := d.GetUserPrivacyRules(ctx, id, keys[0])
		if rules != nil {
			cacheRules = append(cacheRules, rules)
		}
	} else if len(keys) > 1 {
		cacheRules2 := make([]*mtproto.PrivacyKeyRules, len(keys))
		mr.ForEach(
			func(source chan<- interface{}) {
				for i := 0; i < len(keys); i++ {
					source <- i
				}
			},
			func(item interface{}) {
				idx := item.(int)
				rules, _ := d.GetUserPrivacyRules(ctx, id, keys[idx])
				if rules != nil {
					cacheRules2[idx] = rules
				}
			})
		for _, v := range cacheRules2 {
			if v != nil {
				cacheRules = append(cacheRules, v)
			}
		}
	}

	return cacheRules
}

func (d *Dao) GetUserPrivacyRules(ctx context.Context, id int64, key int32) (*mtproto.PrivacyKeyRules, error) {
	cacheKey := genUserPrivacyKeyPrefix(id, key)
	if cacheKey == "" {
		return nil, mtproto.ErrPrivacyKeyInvalid
	}

	rules := mtproto.MakeTLPrivacyKeyRules(&mtproto.PrivacyKeyRules{
		Key:   key,
		Rules: nil,
	}).To_PrivacyKeyRules()
	err := d.CachedConn.QueryRow(
		ctx,
		rules,
		cacheKey,
		func(ctx context.Context, conn *sqlx.DB, v interface{}) error {
			do, err := d.UserPrivaciesDAO.SelectPrivacy(ctx, id, key)
			if err != nil {
				return err
			}
			rv := v.(*mtproto.PrivacyKeyRules)

			if do != nil {
				jsonx.UnmarshalFromString(do.Rules, &rv.Rules)
			}
			if rv.Rules == nil {
				rv.Rules = makeDefaultPrivacyRules(key)
			}

			return nil
		},
	)

	if err != nil {
		if errors.Is(err, sqlc.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return rules, nil
}

// AddPhonePrivacyException lets peerId see the phone number of id: the
// add_phone_privacy_exception of contacts.addContact, and
// contacts.acceptContact. Telegram adds the user to the exceptions of the
// phone number privacy ("Always share with"), where the user sees it.
//
// The rules are read and written under the lock of their row, which the
// first statement creates with the default rules or locks, as userbox
// locks user_state: rules account.setPrivacy committed meanwhile are not
// overwritten with older ones, and the box of id serializes the exceptions
// added at once.
func (d *Dao) AddPhonePrivacyException(ctx context.Context, id, peerId int64) error {
	keys := []string{genUserPrivacyKeyPrefix(id, mtproto.PHONE_NUMBER), genCacheUserDataCacheKey(id)}
	_, err := d.RunBox(ctx, id, keys, func(b *userbox.Box) error {
		if _, err := b.Tx().Exec("INSERT INTO user_privacies (user_id, key_type, rules) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE user_id = user_id",
			id, mtproto.PHONE_NUMBER, phoneNumberRulesData); err != nil {
			return err
		}
		var list []string
		if err := b.Tx().QueryRowsPartial(&list, "SELECT rules FROM user_privacies WHERE user_id = ? AND key_type = ?", id, mtproto.PHONE_NUMBER); err != nil {
			return err
		}
		var current []*mtproto.PrivacyRule
		if len(list) > 0 {
			if err := jsonx.UnmarshalFromString(list[0], &current); err != nil {
				return err
			}
		}
		if len(current) == 0 {
			current = makeDefaultPrivacyRules(mtproto.PHONE_NUMBER)
		}
		updated, changed := withAllowedUser(current, peerId)
		if !changed {
			return nil
		}
		data, err := jsonx.Marshal(updated)
		if err != nil {
			return err
		}
		_, err = b.Tx().Exec("UPDATE user_privacies SET rules = ? WHERE user_id = ? AND key_type = ?", hack.String(data), id, mtproto.PHONE_NUMBER)
		return err
	})
	return err
}

// withAllowedUser returns rules with peerId allowed explicitly: out of the
// disallowed users and among the allowed ones, unless everybody is allowed
// and peerId is not an exception. The rules are copied, not changed.
func withAllowedUser(rules []*mtproto.PrivacyRule, peerId int64) ([]*mtproto.PrivacyRule, bool) {
	var (
		out      = make([]*mtproto.PrivacyRule, 0, len(rules)+1)
		changed  bool
		allowAll bool
		allowed  bool
	)
	for _, r := range rules {
		switch r.GetPredicateName() {
		case mtproto.Predicate_privacyValueDisallowUsers:
			users := make([]int64, 0, len(r.GetUsers()))
			for _, u := range r.GetUsers() {
				if u == peerId {
					changed = true
					continue
				}
				users = append(users, u)
			}
			if len(users) > 0 {
				out = append(out, mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: users}).To_PrivacyRule())
			}
			continue
		case mtproto.Predicate_privacyValueAllowUsers:
			users := append([]int64(nil), r.GetUsers()...)
			if !slices.Contains(users, peerId) {
				users = append(users, peerId)
				changed = true
			}
			allowed = true
			out = append(out, mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: users}).To_PrivacyRule())
			continue
		case mtproto.Predicate_privacyValueAllowAll:
			allowAll = true
		}
		out = append(out, proto.Clone(r).(*mtproto.PrivacyRule))
	}
	if !allowed && !allowAll {
		out = append([]*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowUsers(&mtproto.PrivacyRule{Users: []int64{peerId}}).To_PrivacyRule()}, out...)
		changed = true
	}
	return out, changed
}

func (d *Dao) SetUserPrivacyRules(ctx context.Context, id int64, key int32, rules []*mtproto.PrivacyRule) bool {
	cacheKey := genUserPrivacyKeyPrefix(id, key)
	if cacheKey == "" {
		return false
	}

	cacheKeys := []string{cacheKey}
	switch key {
	case mtproto.STATUS_TIMESTAMP,
		mtproto.PROFILE_PHOTO,
		mtproto.PHONE_NUMBER:
		cacheKeys = append(cacheKeys, genCacheUserDataCacheKey(id))
	}

	d.CachedConn.Exec(
		ctx,
		func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
			rulesData, _ := jsonx.Marshal(rules)
			return d.UserPrivaciesDAO.InsertOrUpdate(ctx, &dataobject.UserPrivaciesDO{
				UserId:  id,
				KeyType: key,
				Rules:   hack.String(rulesData),
			})
		},
		cacheKeys...)

	return true
}
