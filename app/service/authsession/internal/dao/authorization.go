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
	"net"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/dal/dataobject"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/mr"
)

// placeByIp is the place of a session: authorization.country in
// account.getAuthorizations, and from there the location of
// updateNewAuthorization and of the New login message.
//
// authorization.region stays empty. Web K and Web A show a session's place
// as region and country joined by ", " (tweb 346844bf
// src/components/sidebarLeft/tabs/session.tsx:28 and
// src/lib/appManagers/appAccountManager.ts:240; telegram-tt 6002824b
// src/components/left/settings/SettingsActiveSession.tsx:152-154), and the
// country here already names the city: with the country's ISO code as the
// region they showed a session in Prague as "CZ, Prague, Czechia". Android,
// iOS and Desktop read the country alone (DrKLO/Telegram aac1efcb
// ui/Cells/SessionCell.java:239-240 and ui/SessionBottomSheet.java:124-126;
// Telegram-iOS 6ad963e5 ItemListRecentSessionItem.swift:171-175; tdesktop
// a1e89e1f api/api_authorizations.cpp:62, 81). What Telegram sends as region
// is not verified.
func (d *Dao) placeByIp(ip string) string {
	// Without a database (geoIP logged why), or for a session whose address
	// is not known, there is no place.
	addr := net.ParseIP(ip)
	if addr == nil {
		return "UNKNOWN"
	}
	r, err := d.geoIP.city(addr)
	if errors.Is(err, errNoGeoIP) {
		return "UNKNOWN"
	}
	if err != nil {
		logx.Errorf("placeByIp - error: %v", err)
		return "UNKNOWN"
	}
	return placeOf(r)
}

// maxCityRadius is the largest location.accuracy_radius, in kilometres, at
// which placeOf names the city. MaxMind defines it as "the radius in
// kilometers around the specified location where the IP address is likely to
// be" (dev.maxmind.com/geoip/docs/databases/city-and-country, read
// 2026-10-30). A wider circle reaches other cities and regions, and the city
// would be a guess: MaxMind's own comparison for Czechia places only 28% of
// GeoLite's mobile addresses within 50 km of the true place
// (docs/tasks/017-rebranding.md, "Выбор базы GeoIP" in "Ход работы"). The
// country is shown then. The value is Pluma's choice; what Telegram does is
// not known.
const maxCityRadius = 100

// placeOf is "City, Country", or the country alone, as Telegram writes the
// place of a login (task 019): the country alone also when the database
// gives the city only within a radius over maxCityRadius. A database without
// accuracy_radius, such as DB-IP Lite, always names the city.
func placeOf(r *geoip2.City) string {
	city, country := r.City.Names["en"], r.Country.Names["en"]
	if r.Location.AccuracyRadius > maxCityRadius {
		city = ""
	}
	switch {
	case city != "" && country != "":
		return city + ", " + country
	case country != "":
		return country
	}
	return "UNKNOWN"
}

//func getAppNameByAppId(appId int32) string {
//	return "tdesktop"
//}

// The address of a session stays out of its authorization: authorization.ip
// is empty, as Telegram sends it. Its official clients showed no address in
// any session (owner's check, 2026-10-30). iOS never shows one
// (Telegram-iOS 6ad963e5
// submodules/SettingsUI/Sources/Privacy and Security/RecentSessionScreen.swift:438),
// Android, Desktop and Web K show it only when it is not empty
// (DrKLO/Telegram aac1efcb ui/SessionBottomSheet.java:154; tdesktop
// a1e89e1f settings/sections/settings_active_sessions.cpp:1328; tweb
// 346844bf src/components/sidebarLeft/tabs/activeSessions.tsx:322). The
// place is still found by the address (placeByIp).

func (d *Dao) GetAuthorization(ctx context.Context, authKeyId int64) (*mtproto.Authorization, error) {
	cData, err := d.GetCacheAuthData(ctx, authKeyId)
	if err != nil {
		return nil, err
	}

	country := d.placeByIp(cData.ClientIp())

	return mtproto.MakeTLAuthorization(&mtproto.Authorization{
		Current:         false,
		OfficialApp:     true,
		Hash:            0,
		PasswordPending: false,
		DeviceModel:     deviceModel(cData.LangPack(), cData.DeviceModel()),
		Platform:        platform(cData.LangPack(), cData.SystemVersion()),
		SystemVersion:   systemVersion(cData.LangPack(), cData.SystemVersion(), cData.DeviceModel()),
		ApiId:           cData.ApiId(),
		AppName:         appName(cData.LangPack(), cData.AppVersion()),
		AppVersion:      cData.AppVersion(),
		DateCreated:     0,
		DateActive:      0,
		Country:         country,
	}).To_Authorization(), nil
}

type idxId struct {
	idx int
	id  int64
}

func (d *Dao) GetAuthorizations(ctx context.Context, userId int64, excludeAuthKeyId int64) (authorizations []*mtproto.Authorization) {
	now := time.Now().Unix()
	doList, _ := d.AuthUsersDAO.SelectListByUserId(ctx, userId)
	if len(doList) == 0 {
		return
	}

	authorizations = make([]*mtproto.Authorization, len(doList)+1)
	mr.ForEach(
		func(source chan<- interface{}) {
			for i := 0; i < len(doList); i++ {
				source <- idxId{i, doList[i].AuthKeyId}
			}
		},
		func(item interface{}) {
			idx := item.(idxId)
			//kId := item.(int64)
			cData, _ := d.GetCacheAuthData(ctx, idx.id)
			if cData != nil {
				country := d.placeByIp(cData.ClientIp())
				authorization := mtproto.MakeTLAuthorization(&mtproto.Authorization{
					Current:         false,
					OfficialApp:     true,
					PasswordPending: false,
					Hash:            cData.Hash(),
					DeviceModel:     deviceModel(cData.LangPack(), cData.DeviceModel()),
					Platform:        platform(cData.LangPack(), cData.SystemVersion()),
					SystemVersion:   systemVersion(cData.LangPack(), cData.SystemVersion(), cData.DeviceModel()),
					ApiId:           cData.ApiId(),
					AppName:         appName(cData.LangPack(), cData.AppVersion()),
					AppVersion:      cData.AppVersion(),
					DateCreated:     int32(cData.DateCreated()),
					DateActive:      int32(cData.DateActivated()),
					Country:         country,
					// pluma: task 019
					Unconfirmed:               cData.Unconfirmed(now),
					EncryptedRequestsDisabled: cData.EncryptedRequestsDisabled(),
					CallRequestsDisabled:      cData.CallRequestsDisabled(),
				}).To_Authorization()

				if idx.id == excludeAuthKeyId {
					authorization.Current = true
					authorization.Hash = 0
					authorizations[0] = authorization
				} else {
					authorizations[idx.idx+1] = authorization
				}
			}
		})

	return removeAllNil(authorizations)
}

func (d *Dao) ResetAuthorization(ctx context.Context, userId int64, authKeyId, hash int64) []int64 {
	var (
		cacheKeyIdList []string
		hashList       []int64
		keyIdList      []int64
	)

	_, _ = d.AuthUsersDAO.SelectListByUserIdWithCB(
		ctx,
		userId,
		func(sz, i int, v *dataobject.AuthUsersDO) {
			if hash == 0 {
				if authKeyId == v.AuthKeyId {
					return
				}
				cacheKeyIdList = append(cacheKeyIdList, genAuthDataCacheKey(v.AuthKeyId))
				hashList = append(hashList, v.Id)
				keyIdList = append(keyIdList, v.AuthKeyId)
			} else {
				if hash == v.Hash && authKeyId != v.AuthKeyId {
					cacheKeyIdList = append(cacheKeyIdList, genAuthDataCacheKey(v.AuthKeyId))
					hashList = append(hashList, v.Id)
					keyIdList = append(keyIdList, v.AuthKeyId)
				}
			}
		})
	if len(keyIdList) == 0 {
		return keyIdList
	}

	_, _, _ = d.CachedConn.Exec(
		ctx,
		func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
			_, _ = d.AuthUsersDAO.DeleteByHashList(ctx, hashList)
			return 0, 0, nil
		},
		cacheKeyIdList...)

	return keyIdList
}
