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

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// upload.getWebFile (task 099, ADR 0062, p. 5): clients send it to
// webfile_dc_id, the server's own DC, and get the errors of the method's
// documentation instead of the enterprise stub: an address the server did
// not sign and a map of no size are LOCATION_INVALID (the latter whether
// the stand runs the maps or not: staticmap.Normalize refuses it before
// any render, and without the maps no source serves maps), an album cover
// WEBFILE_NOT_AVAILABLE, a part over 1 MiB LIMIT_INVALID. The maps
// themselves are the TestWebMap tests of task 100.
func TestWebFileLocations(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	o, err := alice.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: alice.c.Constructor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		t.Fatal(err)
	}
	conf, ok := o.(*mtproto.TLConfig)
	if !ok {
		t.Fatalf("help.getConfig: %T", o)
	}
	if conf.GetWebfileDcId() != conf.GetThisDc() {
		t.Errorf("webfile_dc_id %d, this_dc %d", conf.GetWebfileDcId(), conf.GetThisDc())
	}

	webFile := func(loc *mtproto.InputWebFileLocation, limit int32) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLUploadGetWebFile{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_getWebFile),
			Location:    loc,
			Offset:      0,
			Limit:       limit,
		})
		return err
	}
	unsigned := &mtproto.InputWebFileLocation{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputWebFileLocation),
		Url:         "https://bot.example/thumb.jpg",
		AccessHash:  12345,
	}
	if err := webFile(unsigned, 128<<10); !mtclient.IsRPCError(err, "LOCATION_INVALID") {
		t.Errorf("an unsigned address: %v", err)
	}
	if err := webFile(unsigned, 2<<20); !mtclient.IsRPCError(err, "LIMIT_INVALID") {
		t.Errorf("a part of 2 MiB: %v", err)
	}
	geo := &mtproto.InputWebFileLocation{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputWebFileGeoPointLocation),
		GeoPoint: &mtproto.InputGeoPoint{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPoint),
			Lat:         52.52,
			Long:        13.40,
		},
		AccessHash: 0, W: 0, H: 100, Zoom: 15, Scale: 2,
	}
	if err := webFile(geo, 128<<10); !mtclient.IsRPCError(err, "LOCATION_INVALID") {
		t.Errorf("a map of width 0: %v", err)
	}
	album := &mtproto.InputWebFileLocation{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputWebFileAudioAlbumThumbLocation),
		Small:       true,
		Title:       mtproto.MakeFlagsString("Song"),
		Performer:   mtproto.MakeFlagsString("Band"),
	}
	if err := webFile(album, 128<<10); !mtclient.IsRPCError(err, "WEBFILE_NOT_AVAILABLE") {
		t.Errorf("an album cover: %v", err)
	}
}
