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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/webfile"
)

// GeoSource serves the maps of inputWebFileGeoPointLocation (task 100, ADR
// 0063) with the maps service: the map preview of a geo point, a venue, a
// live location, the point of a poll and a business address, which
// Android (MessagesController.java:5707-5712, with static_maps_provider
// "telegram"), tdesktop (storage/download_manager_mtproto.cpp:542-561) and
// Web K (helpers/getWebFileLocation.ts:3-23) ask for by upload.getWebFile.
//
// Every part of a map is cut from the one image staticmap keeps in Redis,
// so the source is Cached and the Server keeps no copy of its own:
// tdesktop cancels a load whose parts tell different sizes
// (file_download_mtproto.cpp:170-181).
type GeoSource struct {
	maps *staticmap.Service
}

// NewGeoSource returns the source of maps rendered by maps, which must not
// be nil.
func NewGeoSource(maps *staticmap.Service) *GeoSource {
	return &GeoSource{maps: maps}
}

var _ webfile.Source = (*GeoSource)(nil)

// geoLocation is a map request brought into range (staticmap.Normalize)
// and the language of the session, which picks the style of the labels.
type geoLocation struct {
	params   staticmap.Params
	langCode string
}

// Key names the map by every field of the request and the language: the
// style is a function of the language, so equal keys are the same image.
// The Server does not use it for a Cached source; it is kept exact all the
// same.
func (l geoLocation) Key() string {
	return staticmap.Key("lang:"+l.langCode, l.params)
}

// Resolve brings the location into the ranges the maps service renders:
// LOCATION_INVALID for a point that is no point or a side of 0 or less
// (ADR 0063, p. 2).
func (s *GeoSource) Resolve(_ context.Context, r webfile.Request, loc *mtproto.InputWebFileLocation) (webfile.Location, error) {
	p, err := staticmap.Normalize(loc)
	if err != nil {
		return nil, err
	}
	return geoLocation{params: p, langCode: r.LangCode}, nil
}

// Open returns the map, from the cache or rendered for the user, whose
// renders count against the limit per user. The errors are staticmap's RPC
// errors (FLOOD_WAIT_X, INTERNAL_SERVER_ERROR) or the error of ctx; neither
// carries the point.
func (s *GeoSource) Open(ctx context.Context, r webfile.Request, l webfile.Location) (webfile.File, error) {
	gl, ok := l.(geoLocation)
	if !ok {
		return webfile.File{}, mtproto.ErrLocationInvalid
	}
	b, err := s.maps.GetParams(ctx, r.UserId, gl.langCode, gl.params)
	if err != nil {
		return webfile.File{}, err
	}
	return webfile.File{Bytes: b.Bytes, MimeType: staticmap.MimeType, Mtime: b.Mtime}, nil
}

// Cached is true: staticmap keeps every map in Redis.
func (s *GeoSource) Cached() bool { return true }
