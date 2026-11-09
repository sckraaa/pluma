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

package main

import (
	"context"
	"net/http"
	"time"
)

// passkeyRow is a passkey of the panel, without its key material.
type passkeyRow struct {
	Id        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
}

func (s *store) listPasskeys(ctx context.Context, login string) ([]passkeyRow, error) {
	var rows []struct {
		Id        []byte `db:"credential_id"`
		CreatedAt int64  `db:"created_at"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT credential_id, created_at FROM admin_passkeys WHERE login = ? ORDER BY created_at", login); err != nil {
		return nil, err
	}
	out := make([]passkeyRow, len(rows))
	for i, r := range rows {
		out[i] = passkeyRow{Id: b64(r.Id), CreatedAt: r.CreatedAt}
	}
	return out, nil
}

func (s *store) removePasskey(ctx context.Context, login string, id []byte) (bool, error) {
	r, err := s.db.Exec(ctx, "DELETE FROM admin_passkeys WHERE login = ? AND credential_id = ?", login, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

func (s *server) handlePasskeys(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.listPasskeys(r.Context(), a.login)
	if err != nil {
		s.fail(w, r, "passkeys", err)
		return
	}
	writeJSON(w, map[string]any{"passkeys": rows})
}

// handlePasskeyRemove takes one of the owner's passkeys away, never the last
// one: the panel would have no way in but pluma-admin panel reset-passkey.
func (s *server) handlePasskeyRemove(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Id string `json:"id"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	id, err := unb64(body.Id)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad id")
		return
	}
	rows, err := s.store.listPasskeys(r.Context(), a.login)
	if err != nil {
		s.fail(w, r, "passkeys", err)
		return
	}
	if len(rows) <= 1 {
		httpError(w, http.StatusConflict, "the last passkey cannot be removed")
		return
	}
	if !s.journal(w, r, a, "passkey_remove", "admin:"+a.login, "") {
		return
	}
	removed, err := s.store.removePasskey(r.Context(), a.login, id)
	if err != nil {
		s.fail(w, r, "remove passkey", err)
		return
	}
	if !removed {
		httpError(w, http.StatusNotFound, "no such passkey")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "at": time.Now().Unix()})
}
