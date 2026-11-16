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

package business

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// Location is the business location of an account: an address and maybe a
// point on the map.
type Location struct {
	Address string
	HasGeo  bool
	Lat     float64
	Long    float64
}

// ToTL is l as businessLocation.
func (l *Location) ToTL() *mtproto.BusinessLocation {
	out := &mtproto.BusinessLocation{Address: l.Address}
	if l.HasGeo {
		out.GeoPoint = mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Long: l.Long, Lat: l.Lat}).To_GeoPoint()
	}
	return mtproto.MakeTLBusinessLocation(out).To_BusinessLocation()
}

// ValidAddress tells whether address fits AddressLengthMax.
func ValidAddress(address string) bool {
	return utf8.ValidString(address) && utf8.RuneCountInString(address) <= AddressLengthMax
}

// ValidPoint tells whether lat and long are a point on the map.
func ValidPoint(lat, long float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(long) && lat >= -90 && lat <= 90 && long >= -180 && long <= 180
}

// Intro is the intro of an account, what the empty chat with it shows in
// place of the default one: a title, a description and maybe a sticker.
type Intro struct {
	Title       string
	Description string
	StickerID   int64
}

// ToTL is i as businessIntro, with sticker, the document of StickerID, nil
// for none.
func (i *Intro) ToTL(sticker *mtproto.Document) *mtproto.BusinessIntro {
	return mtproto.MakeTLBusinessIntro(&mtproto.BusinessIntro{
		Title:       i.Title,
		Description: i.Description,
		Sticker:     sticker,
	}).To_BusinessIntro()
}

// UTF16Length is the length of s in UTF-16 code units, as the clients
// count the limits of an intro.
func UTF16Length(s string) int {
	n := 0
	for _, r := range s {
		if k := utf16.RuneLen(r); k > 0 {
			n += k
		} else {
			n++
		}
	}
	return n
}

// Profile is what the profile of an account shows of Business; a nil part
// is not set.
type Profile struct {
	Hours    *Hours
	Location *Location
	Intro    *Intro
}

// Store reads and writes the Business settings of accounts.
type Store struct {
	db *sqlx.DB
}

// NewStore returns a Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

type profileRow struct {
	HoursTimezone    string  `db:"hours_timezone"`
	HoursIntervals   string  `db:"hours_intervals"`
	LocationSet      bool    `db:"location_set"`
	LocationAddress  string  `db:"location_address"`
	LocationHasGeo   bool    `db:"location_has_geo"`
	LocationLat      float64 `db:"location_lat"`
	LocationLong     float64 `db:"location_long"`
	IntroSet         bool    `db:"intro_set"`
	IntroTitle       string  `db:"intro_title"`
	IntroDescription string  `db:"intro_description"`
	IntroStickerID   int64   `db:"intro_sticker_id"`
}

const selectProfile = "SELECT hours_timezone, hours_intervals, location_set, location_address, location_has_geo, location_lat, location_long," +
	" intro_set, intro_title, intro_description, intro_sticker_id FROM business_profiles WHERE user_id = ?"

// Profile returns the Business profile of userID; an account that set
// nothing has an empty one.
func (s *Store) Profile(ctx context.Context, userID int64) (Profile, error) {
	var rows []profileRow
	if err := s.db.QueryRowsPartial(ctx, &rows, selectProfile, userID); err != nil {
		return Profile{}, err
	}
	if len(rows) == 0 {
		return Profile{}, nil
	}
	return rows[0].profile(userID)
}

func (r profileRow) profile(userID int64) (Profile, error) {
	var p Profile
	if r.HoursTimezone != "" {
		h := &Hours{TimezoneID: r.HoursTimezone}
		if err := json.Unmarshal([]byte(r.HoursIntervals), &h.Intervals); err != nil {
			return Profile{}, fmt.Errorf("business: the work hours of %d: %w", userID, err)
		}
		p.Hours = h
	}
	if r.LocationSet {
		p.Location = &Location{Address: r.LocationAddress, HasGeo: r.LocationHasGeo, Lat: r.LocationLat, Long: r.LocationLong}
	}
	if r.IntroSet {
		p.Intro = &Intro{Title: r.IntroTitle, Description: r.IntroDescription, StickerID: r.IntroStickerID}
	}
	return p, nil
}

// SetHoursTx keeps h, normalized by NormalizeIntervals, as the work hours
// of userID in tx; nil removes them. now is unix ms.
func SetHoursTx(tx *sqlx.Tx, userID int64, h *Hours, now int64) error {
	timezone, intervals := "", ""
	if h != nil {
		data, err := json.Marshal(h.Intervals)
		if err != nil {
			return fmt.Errorf("business: the work hours of %d: %w", userID, err)
		}
		timezone, intervals = h.TimezoneID, string(data)
	}
	return upsertProfile(tx, userID, now, "hours_timezone = new.hours_timezone, hours_intervals = new.hours_intervals",
		[]string{"hours_timezone", "hours_intervals"}, timezone, intervals)
}

// SetLocationTx keeps l as the location of userID in tx; nil removes it.
func SetLocationTx(tx *sqlx.Tx, userID int64, l *Location, now int64) error {
	var v Location
	if l != nil {
		v = *l
		if !v.HasGeo {
			v.Lat, v.Long = 0, 0
		}
	}
	return upsertProfile(tx, userID, now,
		"location_set = new.location_set, location_address = new.location_address, location_has_geo = new.location_has_geo,"+
			" location_lat = new.location_lat, location_long = new.location_long",
		[]string{"location_set", "location_address", "location_has_geo", "location_lat", "location_long"},
		l != nil, v.Address, v.HasGeo, v.Lat, v.Long)
}

// SetIntroTx keeps i as the intro of userID in tx; nil removes it.
func SetIntroTx(tx *sqlx.Tx, userID int64, i *Intro, now int64) error {
	var v Intro
	if i != nil {
		v = *i
	}
	return upsertProfile(tx, userID, now,
		"intro_set = new.intro_set, intro_title = new.intro_title, intro_description = new.intro_description, intro_sticker_id = new.intro_sticker_id",
		[]string{"intro_set", "intro_title", "intro_description", "intro_sticker_id"},
		i != nil, v.Title, v.Description, v.StickerID)
}

// upsertProfile writes columns of the profile row of userID, made if
// missing, and removes the row when nothing is left set in it.
func upsertProfile(tx *sqlx.Tx, userID, now int64, update string, columns []string, values ...any) error {
	query := "INSERT INTO business_profiles (user_id, updated_at"
	placeholders := "?, ?"
	for _, c := range columns {
		query += ", " + c
		placeholders += ", ?"
	}
	query += ") VALUES (" + placeholders + ") AS new ON DUPLICATE KEY UPDATE " + update + ", updated_at = new.updated_at"
	args := append([]any{userID, now}, values...)
	if _, err := tx.Exec(query, args...); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM business_profiles WHERE user_id = ? AND hours_timezone = '' AND location_set = 0 AND intro_set = 0", userID)
	return err
}
