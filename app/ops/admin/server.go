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
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/moderation"
	"github.com/teamgram/teamgram-server/pkg/moderation/effects"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// identityHeader carries the login of the person tailscale serve proxied
// (tailscale.com/kb/1312/serve: "Tailscale-User-Login", removed if a client
// sends it itself, absent for tagged devices and Funnel).
const identityHeader = "Tailscale-User-Login"

const (
	sessionCookie = "__Host-admin"
	maxBodyBytes  = 256 << 10
	mediaLimit    = 32 << 20 // bytes of a media file shown in the panel
	mediaChunk    = 1 << 20
	queuePage     = 50
	auditPage     = 100
)

// User is what the panel needs of the user service.
type userClient interface {
	UserUpdatePremium(ctx context.Context, in *userpb.TLUserUpdatePremium) (*mtproto.Bool, error)
}

// dfsClient reads the media of a reported message.
type dfsClient interface {
	DfsDownloadFile(ctx context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error)
}

// server is the HTTP API and pages of the panel.
type server struct {
	cfg    *config
	store  *store
	dao    *moderation.DAO
	runner *effects.Runner
	user   userClient
	dfs    dfsClient
}

func newServer(c *config, st *store, dao *moderation.DAO, runner *effects.Runner, user userClient, d dfsClient) *server {
	return &server{cfg: c, store: st, dao: dao, runner: runner, user: user, dfs: d}
}

// actor is the person behind a request.
type actor struct {
	login string
	role  int8
}

// peerIsLoopback tells whether the connection is from this machine, where
// only tailscale serve may be listening on the proxy side. A connection
// from anywhere else never has its identity header believed (ADR 0039).
func peerIsLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// identify returns the person of the request, nil if there is none or the
// request did not come through the local proxy.
func (s *server) identify(r *http.Request) (*admin, error) {
	if !peerIsLoopback(r.RemoteAddr) {
		return nil, nil
	}
	login := r.Header.Get(identityHeader)
	if login == "" {
		return nil, nil
	}
	a, err := s.store.admin(r.Context(), login)
	if err != nil || a == nil || a.Disabled {
		return nil, err
	}
	return a, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logx.Errorf("admin: encode response: %v", err)
	}
}

func httpError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; media-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
}

// wrap authenticates a request: tailnet identity, then the role, then a
// live session cookie of the same person. Only these checks apply to a
// passkey ceremony, which a person without a session does (needSession
// false).
func (s *server) wrap(minRole int8, needSession bool, fn func(w http.ResponseWriter, r *http.Request, a *actor)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		if r.Method != http.MethodGet && r.Header.Get("Origin") != s.cfg.Origin {
			httpError(w, http.StatusForbidden, "bad origin")
			return
		}
		a, err := s.identify(r)
		if err != nil {
			logx.WithContext(r.Context()).Errorf("admin: identify: %v", err)
			httpError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if a == nil {
			httpError(w, http.StatusForbidden, "not an administrator")
			return
		}
		if a.Role < minRole {
			httpError(w, http.StatusForbidden, "not allowed")
			return
		}
		if needSession {
			c, err := r.Cookie(sessionCookie)
			if err != nil {
				httpError(w, http.StatusUnauthorized, "log in with the passkey")
				return
			}
			login, err := s.store.sessionLogin(r.Context(), c.Value)
			if err != nil {
				logx.WithContext(r.Context()).Errorf("admin: session: %v", err)
				httpError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if login != a.Login {
				httpError(w, http.StatusUnauthorized, "log in with the passkey")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		fn(w, r, &actor{login: a.Login, role: a.Role})
	}
}

//go:embed ui
var uiFiles embed.FS

// routes builds the handler of the panel.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	ui, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(ui))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/me", s.wrap(roleObserver, false, s.handleMe))
	mux.HandleFunc("POST /api/passkey/register/begin", s.wrap(roleObserver, false, s.handleRegisterBegin))
	mux.HandleFunc("POST /api/passkey/register/finish", s.wrap(roleObserver, false, s.handleRegisterFinish))
	mux.HandleFunc("POST /api/login/begin", s.wrap(roleObserver, false, s.handleLoginBegin))
	mux.HandleFunc("POST /api/login/finish", s.wrap(roleObserver, false, s.handleLoginFinish))
	mux.HandleFunc("POST /api/logout", s.wrap(roleObserver, true, s.handleLogout))
	mux.HandleFunc("POST /api/stepup/begin", s.wrap(roleModerator, true, s.handleStepUpBegin))
	mux.HandleFunc("GET /api/reports", s.wrap(roleObserver, true, s.handleQueue))
	mux.HandleFunc("GET /api/reports/{id}", s.wrap(roleObserver, true, s.handleReport))
	mux.HandleFunc("GET /api/reports/{id}/media/{msg}", s.wrap(roleObserver, true, s.handleMedia))
	mux.HandleFunc("POST /api/reports/{id}/decide", s.wrap(roleModerator, true, s.handleDecide))
	mux.HandleFunc("POST /api/reports/{id}/retry", s.wrap(roleModerator, true, s.handleRetry))
	mux.HandleFunc("POST /api/users/{id}/lift", s.wrap(roleModerator, true, s.handleLift))
	mux.HandleFunc("GET /api/audit", s.wrap(roleObserver, true, s.handleAudit))
	mux.HandleFunc("GET /api/premium", s.wrap(roleOwner, true, s.handlePremiumList))
	mux.HandleFunc("POST /api/premium", s.wrap(roleOwner, true, s.handlePremiumSet))
	mux.HandleFunc("GET /api/admins", s.wrap(roleOwner, true, s.handleAdminsList))
	mux.HandleFunc("POST /api/admins", s.wrap(roleOwner, true, s.handleAdminSet))
	return mux
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func unb64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// assertionBody is a navigator.credentials.get response, base64url fields.
type assertionBody struct {
	CredentialId      string `json:"credentialId"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AuthenticatorData string `json:"authenticatorData"`
	Signature         string `json:"signature"`
	Challenge         string `json:"challenge"`
}

// credentialOptions are options common to get and create.
func (s *server) requestOptions(challenge string, credentials []*passkey) map[string]any {
	allow := make([]map[string]any, len(credentials))
	for i, p := range credentials {
		allow[i] = map[string]any{"type": "public-key", "id": b64(p.id)}
	}
	return map[string]any{
		"challenge":        challenge,
		"rpId":             s.cfg.rpID(),
		"userVerification": "required",
		"timeout":          60000,
		"allowCredentials": allow,
	}
}

// verifyAssertion checks an assertion against the person's passkeys and
// consumes its challenge, purpose and digest included, so that it cannot
// authorize anything else.
func (s *server) verifyPasskey(ctx context.Context, login, purpose, digest string, a *assertionBody) error {
	if err := s.store.takeChallenge(ctx, a.Challenge, login, purpose, digest); err != nil {
		return err
	}
	credentials, err := s.store.passkeysOf(ctx, login)
	if err != nil {
		return err
	}
	id, err := unb64(a.CredentialId)
	if err != nil {
		return errBadCeremony
	}
	var pk *passkey
	for _, p := range credentials {
		if string(p.id) == string(id) {
			pk = p
			break
		}
	}
	if pk == nil {
		return errBadCeremony
	}
	clientJSON, err1 := unb64(a.ClientDataJSON)
	authData, err2 := unb64(a.AuthenticatorData)
	signature, err3 := unb64(a.Signature)
	if err1 != nil || err2 != nil || err3 != nil {
		return errBadCeremony
	}
	count, err := verifyAssertion(pk, clientJSON, authData, signature, a.Challenge, s.cfg.Origin, s.cfg.rpID())
	if err != nil {
		return err
	}
	return s.store.updateSignCount(ctx, pk.id, count)
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request, a *actor) {
	credentials, err := s.store.passkeysOf(r.Context(), a.login)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: passkeys: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	loggedIn := false
	if c, err := r.Cookie(sessionCookie); err == nil {
		if login, err := s.store.sessionLogin(r.Context(), c.Value); err == nil {
			loggedIn = login == a.login
		}
	}
	writeJSON(w, map[string]any{"login": a.login, "role": a.role, "has_passkey": len(credentials) > 0, "logged_in": loggedIn})
}

func (s *server) handleRegisterBegin(w http.ResponseWriter, r *http.Request, a *actor) {
	credentials, err := s.store.passkeysOf(r.Context(), a.login)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: passkeys: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A person with a passkey adds another only from a live session: the
	// tailnet identity alone must not be enough to replace a passkey.
	if len(credentials) > 0 {
		c, err := r.Cookie(sessionCookie)
		var login string
		if err == nil {
			login, err = s.store.sessionLogin(r.Context(), c.Value)
		}
		if err != nil || login != a.login {
			httpError(w, http.StatusUnauthorized, "log in with the passkey")
			return
		}
	}
	challenge, err := newChallenge()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err = s.store.putChallenge(r.Context(), challenge, a.login, "register", ""); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: challenge: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	exclude := make([]map[string]any, len(credentials))
	for i, p := range credentials {
		exclude[i] = map[string]any{"type": "public-key", "id": b64(p.id)}
	}
	challengeBytes, err := hex.DecodeString(challenge)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{
		"challengeHex": challenge,
		"publicKey": map[string]any{
			"challenge":              b64(challengeBytes),
			"rp":                     map[string]any{"id": s.cfg.rpID(), "name": "Pluma admin"},
			"user":                   map[string]any{"id": b64([]byte(a.login)), "name": a.login, "displayName": a.login},
			"pubKeyCredParams":       []map[string]any{{"type": "public-key", "alg": -7}},
			"timeout":                60000,
			"attestation":            "none",
			"excludeCredentials":     exclude,
			"authenticatorSelection": map[string]any{"userVerification": "required", "residentKey": "preferred"},
		},
	})
}

func (s *server) handleRegisterFinish(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Challenge         string `json:"challenge"`
		ClientDataJSON    string `json:"clientDataJSON"`
		AttestationObject string `json:"attestationObject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad body")
		return
	}
	if err := s.store.takeChallenge(r.Context(), body.Challenge, a.login, "register", ""); err != nil {
		httpError(w, http.StatusForbidden, "ceremony expired")
		return
	}
	clientJSON, err1 := unb64(body.ClientDataJSON)
	attestation, err2 := unb64(body.AttestationObject)
	if err1 != nil || err2 != nil {
		httpError(w, http.StatusBadRequest, "bad body")
		return
	}
	p, err := verifyRegistration(clientJSON, attestation, body.Challenge, s.cfg.Origin, s.cfg.rpID())
	if err != nil {
		httpError(w, http.StatusForbidden, "passkey refused")
		return
	}
	if err = s.store.addPasskey(r.Context(), a.login, p); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: add passkey: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err = s.dao.Audit(r.Context(), a.login, "passkey_register", "admin:"+a.login, ""); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleLoginBegin(w http.ResponseWriter, r *http.Request, a *actor) {
	s.beginCeremony(w, r, a, "login", "")
}

// beginCeremony issues a challenge for a get() with the person's keys.
func (s *server) beginCeremony(w http.ResponseWriter, r *http.Request, a *actor, purpose, digest string) {
	credentials, err := s.store.passkeysOf(r.Context(), a.login)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: passkeys: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(credentials) == 0 {
		httpError(w, http.StatusForbidden, "no passkey registered")
		return
	}
	challenge, err := newChallenge()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err = s.store.putChallenge(r.Context(), challenge, a.login, purpose, digest); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: challenge: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"challengeHex": challenge, "publicKey": s.requestOptions(challenge, credentials)})
}

func (s *server) handleLoginFinish(w http.ResponseWriter, r *http.Request, a *actor) {
	var body assertionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad body")
		return
	}
	if err := s.verifyPasskey(r.Context(), a.login, "login", "", &body); err != nil {
		httpError(w, http.StatusForbidden, "passkey refused")
		return
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	value := b64(token)
	if err := s.store.createSession(r.Context(), value, a.login); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: session: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds())})
	if err := s.dao.Audit(r.Context(), a.login, "login", "admin:"+a.login, ""); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request, a *actor) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.deleteSession(r.Context(), c.Value); err != nil {
			logx.WithContext(r.Context()).Errorf("admin: logout: %v", err)
			httpError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, map[string]any{"ok": true})
}

// stepUpDigest binds an assertion to one exact action: the path and the
// body it is for.
func stepUpDigest(path, payload string) string {
	sum := sha256.Sum256([]byte(path + "\n" + payload))
	return hex.EncodeToString(sum[:])
}

func (s *server) handleStepUpBegin(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Path    string `json:"path"`
		Payload string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad body")
		return
	}
	s.beginCeremony(w, r, a, "stepup", stepUpDigest(body.Path, body.Payload))
}

// signedRequest is the body of an action: the action itself in payload,
// with the passkey assertion that authorizes exactly that payload where
// the action is dangerous.
type signedRequest struct {
	Payload   string         `json:"payload"`
	Assertion *assertionBody `json:"assertion"`
}

// readBody reads the body of an action. ok is false when the response is
// already written.
func (s *server) readBody(w http.ResponseWriter, r *http.Request) (*signedRequest, bool) {
	var req signedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad body")
		return nil, false
	}
	return &req, true
}

// confirm checks the passkey assertion of a dangerous action: it must be
// over a challenge issued for this very action. false means the response
// is already written.
func (s *server) confirm(w http.ResponseWriter, r *http.Request, a *actor, req *signedRequest) bool {
	if req.Assertion == nil {
		httpError(w, http.StatusUnauthorized, "confirm with the passkey")
		return false
	}
	if err := s.verifyPasskey(r.Context(), a.login, "stepup", stepUpDigest(r.URL.Path, req.Payload), req.Assertion); err != nil {
		httpError(w, http.StatusForbidden, "passkey refused")
		return false
	}
	return true
}

// readDangerous reads a dangerous action into out and confirms it.
func (s *server) readDangerous(w http.ResponseWriter, r *http.Request, a *actor, out any) bool {
	req, ok := s.readBody(w, r)
	if !ok {
		return false
	}
	if err := json.Unmarshal([]byte(req.Payload), out); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return false
	}
	return s.confirm(w, r, a, req)
}

func parseID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		httpError(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func (s *server) handleQueue(w http.ResponseWriter, r *http.Request, a *actor) {
	status := moderation.StatusOpen
	if r.URL.Query().Get("status") == "decided" {
		status = moderation.StatusDecided
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	rows, err := s.dao.Queue(r.Context(), status, after, queuePage)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: queue: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"reports": rows})
}

// handleReport shows a report, which is reading someone's personal content:
// the journal gets the view first.
func (s *server) handleReport(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	report, err := s.dao.Report(r.Context(), id)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: report %d: %v", id, err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if report == nil {
		httpError(w, http.StatusNotFound, "no such report")
		return
	}
	if err = s.dao.Audit(r.Context(), a.login, "view_report", fmt.Sprintf("report:%d", id), ""); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	snapshot, err := report.Snapshot()
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var (
		restriction *moderation.Restriction
		history     []moderation.Report
	)
	if report.TargetUserId != 0 {
		if restriction, err = s.dao.Restriction(r.Context(), report.TargetUserId); err != nil {
			logx.WithContext(r.Context()).Errorf("admin: restriction: %v", err)
			httpError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if history, err = s.dao.ReportsOfTarget(r.Context(), report.TargetUserId, 20); err != nil {
			logx.WithContext(r.Context()).Errorf("admin: history: %v", err)
			httpError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	writeJSON(w, map[string]any{"report": report, "snapshot": snapshot, "restriction": restriction, "history": history})
}

// safeMedia is the types the panel shows inline; anything else downloads,
// never renders.
var safeMedia = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true,
	"video/mp4": true, "video/webm": true,
}

// handleMedia streams the media of a reported message from dfs, the only
// content besides the snapshot a moderator can reach.
func (s *server) handleMedia(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	msgId, err := strconv.ParseInt(r.PathValue("msg"), 10, 32)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad message")
		return
	}
	report, err := s.dao.Report(r.Context(), id)
	if err != nil || report == nil {
		httpError(w, http.StatusNotFound, "no such report")
		return
	}
	snapshot, err := report.Snapshot()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var media *moderation.SnapshotMedia
	for _, m := range snapshot.Messages {
		if int64(m.Id) == msgId {
			media = m.Media
			break
		}
	}
	location := fileLocation(media)
	if location == nil {
		httpError(w, http.StatusNotFound, "no such media")
		return
	}
	if err = s.dao.Audit(r.Context(), a.login, "view_media", fmt.Sprintf("report:%d:%d", id, msgId), ""); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	contentType := media.Mime
	if safeMedia[contentType] {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
	}
	var offset int64
	for offset < mediaLimit {
		part, err := s.dfs.DfsDownloadFile(r.Context(), &dfs.TLDfsDownloadFile{Location: location, Offset: offset, Limit: mediaChunk})
		if err != nil {
			logx.WithContext(r.Context()).Errorf("admin: media of report %d: %v", id, err)
			if offset == 0 {
				httpError(w, http.StatusBadGateway, "the file could not be read")
			}
			return
		}
		data := part.GetBytes()
		if len(data) == 0 {
			return
		}
		if _, err = w.Write(data); err != nil {
			return
		}
		offset += int64(len(data))
		if len(data) < mediaChunk {
			return
		}
	}
}

// fileLocation builds the location of the snapshot's media, nil if it has
// none or it is no file.
func fileLocation(m *moderation.SnapshotMedia) *mtproto.InputFileLocation {
	switch {
	case m == nil:
		return nil
	case m.PhotoId != 0:
		return mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{
			Id:            m.PhotoId,
			AccessHash:    m.AccessHash,
			FileReference: m.FileRef,
			ThumbSize:     m.ThumbSize,
		}).To_InputFileLocation()
	case m.DocumentId != 0:
		return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
			Id:            m.DocumentId,
			AccessHash:    m.AccessHash,
			FileReference: m.FileRef,
		}).To_InputFileLocation()
	}
	return nil
}

// decideBody is the action of a decision.
type decideBody struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
	Until  int32  `json:"until"`
}

// dangerousAction: the decisions that change an account or remove content
// need the passkey again (ADR 0039).
func dangerousAction(action string) bool {
	return action != moderation.ActionDismiss
}

func (s *server) handleDecide(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	req, ok := s.readBody(w, r)
	if !ok {
		return
	}
	var dn decideBody
	if err := json.Unmarshal([]byte(req.Payload), &dn); err != nil {
		httpError(w, http.StatusBadRequest, "bad payload")
		return
	}
	if dangerousAction(dn.Action) && !s.confirm(w, r, a, req) {
		return
	}
	err := s.dao.Decide(r.Context(), moderation.Decision{ReportId: id, Actor: a.login, Action: dn.Action, Reason: dn.Reason, UntilDate: dn.Until})
	switch {
	case errors.Is(err, moderation.ErrReportNotFound):
		httpError(w, http.StatusNotFound, "no such report")
		return
	case errors.Is(err, moderation.ErrAlreadyDecided):
		httpError(w, http.StatusConflict, "already decided")
		return
	case errors.Is(err, moderation.ErrNoTarget), errors.Is(err, moderation.ErrNothingToDelete), errors.Is(err, moderation.ErrBadAction):
		httpError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		logx.WithContext(r.Context()).Errorf("admin: decide %d: %v", id, err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The worker takes over whatever this first attempt leaves.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := s.runner.Run(ctx, id); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: effects of report %d: %v", id, err)
		writeJSON(w, map[string]any{"ok": true, "effects_error": "steps are being retried"})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleRetry(w http.ResponseWriter, r *http.Request, a *actor) {
	id, ok := parseID(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.dao.RetryEffects(r.Context(), id, a.login); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: retry %d: %v", id, err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleLift(w http.ResponseWriter, r *http.Request, a *actor) {
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
	lifted, err := s.dao.Lift(r.Context(), id, body.Reason, a.login)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: lift %d: %v", id, err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"lifted": lifted})
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request, a *actor) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	rows, err := s.dao.AuditLog(r.Context(), before, auditPage)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"entries": rows})
}

func (s *server) handlePremiumList(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.premiumAccounts(r.Context())
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: premium list: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"accounts": rows})
}

// premiumBody is a grant or a revocation.
type premiumBody struct {
	Phone  string `json:"phone"`
	Months int32  `json:"months"`
	Grant  bool   `json:"grant"`
}

// handlePremiumSet grants or revokes Premium through the user service, the
// owner of the flag, which also tells the user's devices (task 014).
func (s *server) handlePremiumSet(w http.ResponseWriter, r *http.Request, a *actor) {
	var body premiumBody
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Months < 0 || body.Months > 120 {
		httpError(w, http.StatusBadRequest, "bad months")
		return
	}
	_, phone, err := phonenumber.CheckPhoneNumberInvalid(body.Phone)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad phone")
		return
	}
	account, err := s.store.accountByPhone(r.Context(), phone)
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: premium account: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if account == nil {
		httpError(w, http.StatusNotFound, "no such account")
		return
	}
	target := fmt.Sprintf("user:%d", account.Id)
	action, detail := "premium_revoke", ""
	if body.Grant {
		action, detail = "premium_grant", fmt.Sprintf("months=%d", body.Months)
	}
	if err = s.dao.Audit(r.Context(), a.login, action, target, detail); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	in := &userpb.TLUserUpdatePremium{UserId: account.Id, Premium: mtproto.ToBool(body.Grant)}
	if body.Grant && body.Months > 0 {
		in.Months = wrapperspb.Int32(body.Months)
	}
	if _, err = s.user.UserUpdatePremium(r.Context(), in); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: premium of user %d: %v", account.Id, err)
		if aerr := s.dao.Audit(r.Context(), a.login, action+"_failed", target, ""); aerr != nil {
			logx.WithContext(r.Context()).Errorf("admin: audit: %v", aerr)
		}
		httpError(w, http.StatusBadGateway, "the user service refused")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *server) handleAdminsList(w http.ResponseWriter, r *http.Request, a *actor) {
	rows, err := s.store.admins(r.Context())
	if err != nil {
		logx.WithContext(r.Context()).Errorf("admin: admins: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"admins": rows})
}

// adminBody adds a person or changes one.
type adminBody struct {
	Login    string `json:"login"`
	Role     int8   `json:"role"`
	Disabled bool   `json:"disabled"`
}

func (s *server) handleAdminSet(w http.ResponseWriter, r *http.Request, a *actor) {
	var body adminBody
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Login == "" || body.Role < roleObserver || body.Role > roleOwner {
		httpError(w, http.StatusBadRequest, "bad login or role")
		return
	}
	if body.Login == a.login && (body.Disabled || body.Role != roleOwner) {
		httpError(w, http.StatusBadRequest, "the owner cannot lock himself out")
		return
	}
	if err := s.dao.Audit(r.Context(), a.login, "admin_set", "admin:"+body.Login, fmt.Sprintf("role=%d disabled=%v", body.Role, body.Disabled)); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: audit: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.setAdmin(r.Context(), body.Login, body.Role, body.Disabled, a.login); err != nil {
		logx.WithContext(r.Context()).Errorf("admin: set admin: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
