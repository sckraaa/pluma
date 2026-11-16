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

package core

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// AccountUpdateBusinessLocation
// account.updateBusinessLocation#9e6b131a flags:# geo_point:flags.1?InputGeoPoint address:flags.0?string = Bool;
//
// Sets the business location of the account: an address of up to 96
// characters and maybe a point on the map; "an empty call" removes it
// (core.telegram.org/api/business). The address is what makes a location:
// tdesktop holds a location only with an address (BusinessLocation::operator
// bool, data/business/data_business_common.h:198-204) and sends a point
// only beside one (data/business/data_business_info.cpp:144-160), Android
// sends the address with every location it sets
// (ui/Business/LocationActivity.java:382-391), and iOS refuses to save a
// location without one (BusinessLocationSetupScreen.swift:252-259). So a
// call without an address removes the location, with or without a point.
// A point without the flag, or inputGeoPointEmpty, is none.
func (c *BusinessCore) AccountUpdateBusinessLocation(in *mtproto.TLAccountUpdateBusinessLocation) (*mtproto.Bool, error) {
	const method = "account.updateBusinessLocation"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	var location *business.Location
	if address := in.GetAddress().GetValue(); address != "" {
		if !business.ValidAddress(address) {
			err := mtproto.ErrAddressInvalid
			c.Logger.Errorf("%s - error: %v", method, err)
			return nil, err
		}
		location = &business.Location{Address: address}
		switch geo := in.GetGeoPoint(); geo.GetPredicateName() {
		case "", mtproto.Predicate_inputGeoPointEmpty:
		case mtproto.Predicate_inputGeoPoint:
			if !business.ValidPoint(geo.GetLat(), geo.GetLong()) {
				err := mtproto.ErrGeoPointInvalid
				c.Logger.Errorf("%s - error: %v", method, err)
				return nil, err
			}
			location.HasGeo, location.Lat, location.Long = true, geo.GetLat(), geo.GetLong()
		default:
			err := mtproto.ErrGeoPointInvalid
			c.Logger.Errorf("%s - error: %v", method, err)
			return nil, err
		}
	}
	if err := c.saveProfile(method, func(tx *sqlx.Tx, now int64) error {
		return business.SetLocationTx(tx, c.MD.UserId, location, now)
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
