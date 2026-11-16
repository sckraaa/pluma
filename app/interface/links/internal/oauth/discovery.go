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

package oauth

import (
	"encoding/json"
	"net/http"
)

// discoveryDoc is the provider's metadata (OpenID Connect Discovery 1.0,
// 3): what a site's library needs to log users in with it.
type discoveryDoc struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ClaimsSupported                   []string `json:"claims_supported"`
}

func (p *Provider) discovery(w http.ResponseWriter) {
	writeJSON(w, &discoveryDoc{
		Issuer:                            p.Issuer,
		AuthorizationEndpoint:             p.Issuer + "/auth",
		TokenEndpoint:                     p.Issuer + "/token",
		JwksURI:                           p.Issuer + "/.well-known/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               []string{"authorization_code"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"RS256"},
		ScopesSupported:                   knownScopes,
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ClaimsSupported: []string{
			"iss", "aud", "sub", "iat", "exp", "auth_time", "nonce",
			"id", "name", "given_name", "family_name", "preferred_username", "picture",
			"phone_number", "phone_number_verified",
		},
	})
}

func (p *Provider) jwks(w http.ResponseWriter) {
	b, err := p.Keys.JWKS()
	if err != nil {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	writeRaw(w, b)
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	writeRaw(w, b)
}

// writeRaw answers with a public document: any site's script may read it,
// and caches may keep it an hour (a key is published before it signs and
// for a while after, docs/ops/bots.md).
func writeRaw(w http.ResponseWriter, b []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "public, max-age=3600")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
