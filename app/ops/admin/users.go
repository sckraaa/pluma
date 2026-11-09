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
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/teamgram/teamgram-server/app/bff/authorization/servicemsg"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// messageLengthMax is the limit of a message of the service account
// (message_length_max of help.getConfig, core.telegram.org/api/config).
const messageLengthMax = 4096

// protectedUser: the service account is not a person to restrict or delete.
func protectedUser(id int64) bool {
	return id == servicemsg.UserId
}

// requireTarget reads an action's target, 404 for one that does not exist.
func (s *server) requireTarget(w http.ResponseWriter, r *http.Request, id int64) (*userRow, bool) {
	if protectedUser(id) {
		httpError(w, http.StatusBadRequest, "the service account cannot be changed")
		return nil, false
	}
	u, err := s.store.user(r.Context(), id)
	if err != nil {
		s.fail(w, r, "user", err)
		return nil, false
	}
	if u == nil {
		httpError(w, http.StatusNotFound, "no such user")
		return nil, false
	}
	return u, true
}

func (s *server) handleUsersSearch(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.searchUsers(r.Context(), r.URL.Query().Get("q"), listPage)
	if err != nil {
		s.fail(w, r, "users", err)
		return
	}
	writeJSON(w, map[string]any{"users": rows, "protected_id": int64(servicemsg.UserId)})
}

// deviceRow is a session of a user, as account.getAuthorizations shows it.
type deviceRow struct {
	Hash        int64  `json:"hash"`
	DeviceModel string `json:"device_model"`
	Platform    string `json:"platform"`
	Version     string `json:"system_version"`
	AppName     string `json:"app_name"`
	AppVersion  string `json:"app_version"`
	Date        int32  `json:"date_active"`
	Created     int32  `json:"date_created"`
	Ip          string `json:"ip"`
	Country     string `json:"country"`
}

func (s *server) devices(ctx context.Context, id int64) ([]deviceRow, error) {
	auths, err := s.auth.AuthsessionGetAuthorizations(ctx, &authsession.TLAuthsessionGetAuthorizations{UserId: id})
	if err != nil {
		return nil, err
	}
	out := make([]deviceRow, 0, len(auths.GetAuthorizations()))
	for _, d := range auths.GetAuthorizations() {
		out = append(out, deviceRow{
			Hash: d.GetHash(), DeviceModel: d.GetDeviceModel(), Platform: d.GetPlatform(), Version: d.GetSystemVersion(),
			AppName: d.GetAppName(), AppVersion: d.GetAppVersion(), Date: d.GetDateActive(), Created: d.GetDateCreated(),
			Ip: d.GetIp(), Country: d.GetCountry(),
		})
	}
	return out, nil
}

// handleUserCard shows a user: profile, devices, restriction, reports. It
// is personal data: the journal gets the view first. A device list that
// cannot be read does not hide the rest of the card.
func (s *server) handleUserCard(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	u, err := s.store.user(r.Context(), id)
	if err != nil {
		s.fail(w, r, "user", err)
		return
	}
	if u == nil {
		httpError(w, http.StatusNotFound, "no such user")
		return
	}
	if !s.journal(w, r, a, "view_user", fmt.Sprintf("user:%d", id), "") {
		return
	}
	restriction, err := s.dao.Restriction(r.Context(), id)
	if err != nil {
		s.fail(w, r, "restriction", err)
		return
	}
	if !restriction.Active(time.Now().Unix()) {
		restriction = nil
	}
	reports, err := s.dao.ReportsOfTarget(r.Context(), id, 30)
	if err != nil {
		s.fail(w, r, "reports", err)
		return
	}
	out := map[string]any{"user": u, "restriction": restriction, "reports": reports, "protected": protectedUser(id)}
	if devices, err := s.devices(r.Context(), id); err != nil {
		s.logWarn(r, "devices", err)
		out["devices_error"] = true
	} else {
		out["devices"] = devices
	}
	writeJSON(w, out)
}

func (s *server) handleUserTerminate(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if _, ok := s.requireTarget(w, r, id); !ok {
		return
	}
	if !s.journal(w, r, a, "terminate_sessions", fmt.Sprintf("user:%d", id), body.Reason) {
		return
	}
	if err := s.runner.Revoke(r.Context(), id); err != nil {
		s.logWarn(r, "terminate sessions", err)
		httpError(w, http.StatusBadGateway, "the sessions could not be ended")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleUserRestrict limits or bans a user outside a report: the panel's
// own way to the steps of pluma-admin moderation.
func (s *server) handleUserRestrict(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Until  int32  `json:"until"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Action != moderation.ActionRestrict && body.Action != moderation.ActionBan {
		httpError(w, http.StatusBadRequest, "bad action")
		return
	}
	if body.Until != 0 && int64(body.Until) <= time.Now().Unix() {
		httpError(w, http.StatusBadRequest, "the date has passed")
		return
	}
	if _, ok := s.requireTarget(w, r, id); !ok {
		return
	}
	// ApplyDirect journals the restriction with the decision.
	if err := s.runner.ApplyDirect(r.Context(), id, body.Action == moderation.ActionBan, body.Until, body.Reason, a.login); err != nil {
		s.logWarn(r, "restrict", err)
		writeJSON(w, map[string]any{"ok": true, "effects_error": "steps are being retried"})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleUserCode shows one login code to the owner, on request: the
// deliberate exception of the brief (task 072). The code goes in the
// response only, never the journal or the log.
func (s *server) handleUserCode(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		RequestId string `json:"request_id"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	u, ok := s.requireTarget(w, r, id)
	if !ok {
		return
	}
	s.issueCode(w, r, a, u.Phone, fmt.Sprintf("user:%d", id), body.RequestId)
}

// issueCode gives a fresh login code for a pending request of phone. When
// more than one is pending, it answers 409 with the requests to choose from:
// a person asking for someone else's code must be told apart from the owner
// of the phone (pluma-admin login-code).
func (s *server) issueCode(w http.ResponseWriter, r *http.Request, a *actor, phone, target, requestId string) {
	if s.codes == nil {
		httpError(w, http.StatusServiceUnavailable, "login codes are not configured")
		return
	}
	if !s.journal(w, r, a, "login_code", target, requestId) {
		return
	}
	reissued, err := s.codes.Reissue(r.Context(), phone, requestId)
	switch {
	case errors.Is(err, logincode.ErrAmbiguousRequest):
		pending, listErr := s.codes.Pending(r.Context(), phone)
		if listErr != nil {
			s.fail(w, r, "pending codes", listErr)
			return
		}
		out := make([]map[string]any, 0, len(pending))
		for _, p := range pending {
			out = append(out, pendingJSON(&p))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"pending": out})
		return
	case errors.Is(err, logincode.ErrNoPendingRequest):
		httpError(w, http.StatusConflict, "no pending request")
		return
	case errors.Is(err, logincode.ErrNotAllowed):
		httpError(w, http.StatusForbidden, "the number is not allowed to log in")
		return
	case err != nil:
		s.fail(w, r, "login code", err)
		return
	}
	writeJSON(w, map[string]any{"code": reissued.Code, "expires_at": reissued.ExpiresAt.Unix(), "request": pendingJSON(&reissued.PendingRequest)})
}

func pendingJSON(p *logincode.PendingRequest) map[string]any {
	return map[string]any{"id": p.Id, "requested_at": p.RequestedAt.Unix(), "ends_at": p.EndsAt.Unix(), "client_ip": p.ClientIp, "attempts": p.Attempts, "has_code": p.HasCode}
}

func (s *server) handleUserMessage(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	n := utf8.RuneCountInString(body.Text)
	if n == 0 || n > messageLengthMax {
		httpError(w, http.StatusBadRequest, "bad text")
		return
	}
	u, ok := s.requireTarget(w, r, id)
	if !ok {
		return
	}
	if u.Deleted {
		httpError(w, http.StatusConflict, "the account is deleted")
		return
	}
	// The journal has its length, not its words (task 072).
	if !s.journal(w, r, a, "message_user", fmt.Sprintf("user:%d", id), fmt.Sprintf("chars=%d", n)) {
		return
	}
	if err := servicemsg.Send(r.Context(), s.msg, id, time.Now(), body.Text, nil); err != nil {
		s.logWarn(r, "message", err)
		httpError(w, http.StatusBadGateway, "the message could not be sent")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// deleteBody is the separate confirmation of a deletion: besides the
// passkey, the target's id typed again.
type deleteBody struct {
	Confirm string `json:"confirm"`
}

// handleUserDelete deletes an account for good, the same steps as
// account.deleteAccount (bff account). Only an account blocked first, with a
// separate confirmation; a step that fails leaves the deletion to be tried
// again, each step is safe to repeat.
func (s *server) handleUserDelete(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	var body deleteBody
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	u, ok := s.requireTarget(w, r, id)
	if !ok {
		return
	}
	if body.Confirm != strconv.FormatInt(id, 10) {
		httpError(w, http.StatusBadRequest, "the confirmation does not match")
		return
	}
	restriction, err := s.dao.Restriction(r.Context(), id)
	if err != nil {
		s.fail(w, r, "restriction", err)
		return
	}
	if restriction == nil || restriction.Kind != moderation.RestrictBanned || !restriction.Active(time.Now().Unix()) {
		httpError(w, http.StatusConflict, "block the account first")
		return
	}
	if !s.journal(w, r, a, "delete_user", fmt.Sprintf("user:%d", id), "") {
		return
	}
	if u.Username != "" {
		if _, err = s.user.UserDeleteUsername(r.Context(), &user.TLUserDeleteUsername{Username: u.Username}); err != nil {
			s.logWarn(r, "delete username", err)
			httpError(w, http.StatusBadGateway, "the account could not be deleted")
			return
		}
	}
	if _, err = s.user.UserDeleteUser(r.Context(), &user.TLUserDeleteUser{UserId: id, Reason: "panel", Phone: u.Phone}); err != nil {
		s.logWarn(r, "delete user", err)
		httpError(w, http.StatusBadGateway, "the account could not be deleted")
		return
	}
	if err = s.twofa.Forget(r.Context(), id); err != nil {
		s.logWarn(r, "delete 2fa", err)
	}
	if err = s.runner.Revoke(r.Context(), id); err != nil {
		s.logWarn(r, "delete sessions", err)
	}
	writeJSON(w, map[string]any{"ok": true})
}
