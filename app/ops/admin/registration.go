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
	"fmt"
	"net/http"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/phonenumber"
)

// maskPhone is a number for the journal: its last digits only. The number
// itself is shown only by the panel's pages.
func maskPhone(phone string) string {
	const shown = 4
	if len(phone) <= shown {
		return "+…"
	}
	return "+…" + phone[len(phone)-shown:]
}

func (s *server) handleRegistration(w http.ResponseWriter, r *http.Request, a *actor) {
	open, err := s.allowlist.RegistrationOpen(r.Context())
	if err != nil {
		s.fail(w, r, "registration mode", err)
		return
	}
	entries, err := s.allowlist.List(r.Context())
	if err != nil {
		s.fail(w, r, "allowlist", err)
		return
	}
	writeJSON(w, map[string]any{"open": open, "phones": entries})
}

// handleRegistrationMode switches registration between open and by list. It
// takes effect at the next auth.sendCode or auth.signUp, which read the
// setting from the database (pkg/phoneallowlist).
func (s *server) handleRegistrationMode(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Open bool `json:"open"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if !s.journal(w, r, a, "registration_mode", "registration", fmt.Sprintf("open=%v", body.Open)) {
		return
	}
	if err := s.allowlist.SetRegistrationOpen(r.Context(), body.Open, a.login); err != nil {
		s.fail(w, r, "registration mode", err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// normalizePhone reads a phone as typed in the panel into the stored form.
func normalizePhone(w http.ResponseWriter, phone string) (string, bool) {
	_, digits, err := phonenumber.CheckPhoneNumberInvalid(strings.TrimSpace(phone))
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad phone number")
		return "", false
	}
	return digits, true
}

func (s *server) handleRegistrationAdd(w http.ResponseWriter, r *http.Request, a *actor) {
	req, ok := s.readBody(w, r)
	if !ok {
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Note  string `json:"note"`
	}
	if err := parsePayload(req.Payload, &body); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	phone, ok := normalizePhone(w, body.Phone)
	if !ok {
		return
	}
	if !s.journal(w, r, a, "allowlist_add", "registration", maskPhone(phone)) {
		return
	}
	added, err := s.allowlist.Add(r.Context(), phone, body.Note)
	if err != nil {
		s.fail(w, r, "allowlist add", err)
		return
	}
	writeJSON(w, map[string]any{"added": added})
}

// handleRegistrationRemove takes a phone off the list: the sessions of the
// account stay (phoneallowlist.Remove), the account's next login is
// refused. A destructive action, with the passkey.
func (s *server) handleRegistrationRemove(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Phone string `json:"phone"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	phone, ok := normalizePhone(w, body.Phone)
	if !ok {
		return
	}
	if !s.journal(w, r, a, "allowlist_remove", "registration", maskPhone(phone)) {
		return
	}
	removed, err := s.allowlist.Remove(r.Context(), phone)
	if err != nil {
		s.fail(w, r, "allowlist remove", err)
		return
	}
	writeJSON(w, map[string]any{"removed": removed})
}

// handleRegistrationCode shows the code for a phone that has no account yet
// (a first login after being added to the list), the same way as on a user's
// card.
func (s *server) handleRegistrationCode(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Phone     string `json:"phone"`
		RequestId string `json:"request_id"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	phone, ok := normalizePhone(w, body.Phone)
	if !ok {
		return
	}
	s.issueCode(w, r, a, phone, "registration:"+maskPhone(phone), body.RequestId)
}
