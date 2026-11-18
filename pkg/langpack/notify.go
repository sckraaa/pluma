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

package langpack

import (
	"context"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
)

// The clients learn of a new version of their language from
// updateLangPackTooLong at once, not only at their next help.getConfig
// (task 114). TooLong rather than updateLangPack with the difference: a
// difference can be the whole pack, and each client asks for it from its own
// version (langpack.getDifference):
//
//   - Android: MessagesController.java:19623-19625 ->
//     LocaleController.reloadCurrentRemoteLocale (LocaleController.java:841-848)
//     for its language or its base language;
//   - iOS: UpdateInterpreter+Settings.swift:71-72 -> StateReplay+Settings.swift:81-89
//     (pollLangPacks) -> StateReplay+Finalize.swift:404-406
//     (addSynchronizeLocalizationUpdatesOperation) ->
//     ManagedLocalizationUpdatesOperations.swift:158-170, the difference of
//     its primary and secondary languages;
//   - tdesktop: api_updates.cpp:2918-2924 ->
//     CloudManager::requestLangPackDifference (lang_cloud_manager.cpp:193-201),
//     its language or its base;
//   - Web K: appLangPackManager.ts:11-13 -> langPack.ts:940-971
//     (handleUpdateLangPackTooLong, checkLangPackForUpdates), its language.

// Reader is an authorization whose client holds a language of a pack.
type Reader struct {
	UserId        int64
	PermAuthKeyId int64
}

// Readers returns the authorizations of users whose client holds language
// lang of pack (as initConnection named them, auths) or a language with lang
// as its base.
func (s *Store) Readers(ctx context.Context, pack, lang string) ([]Reader, error) {
	langs := []any{lang}
	var children []struct {
		LangCode string `db:"lang_code"`
	}
	if err := s.db.QueryRowsPartial(ctx, &children,
		"SELECT lang_code FROM langpack_languages WHERE lang_pack = ? AND base_lang_code = ?", pack, lang); err != nil {
		return nil, err
	}
	for _, c := range children {
		langs = append(langs, c.LangCode)
	}
	packs := []any{}
	for _, name := range packNames(pack) {
		packs = append(packs, name)
	}
	var rows []struct {
		UserId    int64 `db:"user_id"`
		AuthKeyId int64 `db:"auth_key_id"`
	}
	// The codes are compared as stored (CanonicalPack, CanonicalLangCode):
	// clients send them in other cases and with '_'.
	q := "SELECT u.user_id, a.auth_key_id FROM auths a" +
		" JOIN auth_users u ON u.auth_key_id = a.auth_key_id AND u.deleted = 0 AND u.user_id > 0" +
		" WHERE a.deleted = 0 AND LOWER(TRIM(a.lang_pack)) IN (" + placeholders(len(packs)) + ")" +
		" AND REPLACE(LOWER(TRIM(a.lang_code)), '_', '-') IN (" + placeholders(len(langs)) + ")" +
		" ORDER BY u.user_id, a.auth_key_id"
	if err := s.db.QueryRowsPartial(ctx, &rows, q, append(packs, langs...)...); err != nil {
		return nil, err
	}
	out := make([]Reader, len(rows))
	for i, r := range rows {
		out[i] = Reader{UserId: r.UserId, PermAuthKeyId: r.AuthKeyId}
	}
	return out, nil
}

// packNames returns the names a client may give pack in initConnection,
// lower case (CanonicalPack).
func packNames(pack string) []string {
	if pack == "webk" {
		return []string{"webk", "web"}
	}
	return []string{pack}
}

// UpdatesMe pushes updates to the sessions of one authorization
// (sync.updatesMe).
type UpdatesMe interface {
	SyncUpdatesMe(ctx context.Context, in *sync.TLSyncUpdatesMe) (*mtproto.Void, error)
}

// ReaderStore is what NotifyChanged needs of Store.
type ReaderStore interface {
	Readers(ctx context.Context, pack, lang string) ([]Reader, error)
}

// NotifyChanged pushes updateLangPackTooLong{lang} to the online sessions of
// every authorization that holds lang of pack (Readers), after an import
// raised its version. sync drops the push for a session that is offline:
// such a client finds the version in its next help.getConfig. It returns
// how many authorizations were told; on an error, those told before it.
func NotifyChanged(ctx context.Context, store ReaderStore, push UpdatesMe, pack, lang string) (int, error) {
	readers, err := store.Readers(ctx, pack, lang)
	if err != nil {
		return 0, fmt.Errorf("langpack: readers of %s/%s: %w", pack, lang, err)
	}
	for i, r := range readers {
		_, err := push.SyncUpdatesMe(ctx, &sync.TLSyncUpdatesMe{
			UserId:        r.UserId,
			PermAuthKeyId: r.PermAuthKeyId,
			Updates: mtproto.MakeUpdatesByUpdates(
				mtproto.MakeTLUpdateLangPackTooLong(&mtproto.Update{LangCode: lang}).To_Update()),
		})
		if err != nil {
			return i, fmt.Errorf("langpack: tell the sessions of %s/%s: %w", pack, lang, err)
		}
	}
	return len(readers), nil
}

// Raised reports whether the import raised the version of a language the
// clients may hold: a new language has none yet.
func (r *ImportResult) Raised() bool {
	return r.OldVersion > 0 && r.Version != r.OldVersion
}
