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
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
)

// RegionClient is the part of the authsession client Regions uses.
type RegionClient interface {
	AuthsessionGetIpRegion(ctx context.Context, in *authsession.TLAuthsessionGetIpRegion) (*mtproto.String, error)
}

// AuthsessionRegions names regions by the GeoIP database of authsession,
// as the apps' own sessions are named (task 106).
type AuthsessionRegions struct {
	Client RegionClient
}

// Region implements Regions.
func (a AuthsessionRegions) Region(ctx context.Context, ip string) (string, error) {
	r, err := a.Client.AuthsessionGetIpRegion(ctx, &authsession.TLAuthsessionGetIpRegion{Ip: ip})
	if err != nil {
		return "", err
	}
	return r.GetV(), nil
}
