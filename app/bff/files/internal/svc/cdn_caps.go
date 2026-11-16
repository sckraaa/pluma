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

package svc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/zeromicro/go-zero/core/collection"
)

// The cache of the pluma_cdn of the keys (task 107, "Пределы"): a client
// asks for a file a megabyte at a time, and authsession is asked once a
// minute per key. A client that starts or stops sending pluma_cdn is taken
// at its word within capsTTL.
const (
	capsKeys = 10000
	capsTTL  = time.Minute
)

// authStates reads what authsession keeps of a key, its client among it.
type authStates interface {
	AuthsessionGetAuthStateData(ctx context.Context, in *authsession.TLAuthsessionGetAuthStateData) (*authsession.AuthKeyStateData, error)
}

// cdnCaps is the cdn.Caps of the initConnection params the session stores
// for each permanent key in authsession (sess.MainAuthWrapper), read as the
// time zone of help.getPromoData is (configuration's clientZone).
type cdnCaps struct {
	auth  authStates
	cache *collection.Cache
}

var _ cdn.Caps = (*cdnCaps)(nil)

func newCdnCaps(auth authStates) (*cdnCaps, error) {
	cache, err := collection.NewCache(capsTTL, collection.WithLimit(capsKeys), collection.WithName("cdn_caps"))
	if err != nil {
		return nil, err
	}
	return &cdnCaps{auth: auth, cache: cache}, nil
}

// PlumaCdn returns the version of pluma_cdn of the client of the key. A
// failure is not cached: the next request asks again.
func (c *cdnCaps) PlumaCdn(ctx context.Context, permAuthKeyID int64) (int, error) {
	v, err := c.cache.Take(strconv.FormatInt(permAuthKeyID, 10), func() (any, error) {
		data, err := c.auth.AuthsessionGetAuthStateData(ctx, &authsession.TLAuthsessionGetAuthStateData{AuthKeyId: permAuthKeyID})
		if err != nil {
			return nil, fmt.Errorf("authsession.getAuthStateData: %w", err)
		}
		return cdn.PlumaCdnVersion(data.GetClient().GetParams()), nil
	})
	if err != nil {
		return 0, err
	}
	return v.(int), nil
}
