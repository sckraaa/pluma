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

// Package data gives the pages of links what anybody may see of the
package data

import (
	"context"

	"github.com/teamgram/teamgram-server/app/interface/links/internal/oauth"
)

// Account implements oauth.Accounts: a user of the login on a site
// (task 106), the bot of the site or the user who logs in. The photo is
// the one anybody may see, the avatar the site's pages serve; the phone
// number goes to a site only with the user's consent in the app.
func (s *Source) Account(ctx context.Context, id int64) (*oauth.Account, error) {
	u, err := s.user(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	return &oauth.Account{
		Id:        u.Id(),
		Bot:       u.IsBot(),
		FirstName: u.FirstName(),
		LastName:  u.LastName(),
		Username:  u.Username(),
		Phone:     u.Phone(),
		PhotoId:   userPhoto(u),
	}, nil
}
