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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The map previews of geo points (task 100, ADR 0063): Android
// (MessagesController.java:5707-5712), tdesktop
// (storage/download_manager_mtproto.cpp:542-561) and Web K
// (helpers/getWebFileLocation.ts:3-23) ask for them with upload.getWebFile
// and inputWebFileGeoPointLocation at webfile_dc_id. The stand renders them
// with test/e2e/fakemaps, whose first pixel tells which of its requests a
// map came from. Every test uses points of its own, so the cache of maps in
// Redis is never shared between tests or runs.

// Set by test/e2e/run.sh: host:port of fakemaps.
var fakeMapsAddr = os.Getenv("PLUMA_E2E_FAKEMAPS")

// mapRequest is a request fakemaps got from bff (test/e2e/fakemaps).
type mapRequest struct {
	Seq    int     `json:"seq"`
	Style  string  `json:"style"`
	Lon    float64 `json:"lon"`
	Lat    float64 `json:"lat"`
	Zoom   float64 `json:"zoom"`
	W      int     `json:"w"`
	H      int     `json:"h"`
	Scale  int     `json:"scale"`
	Status int     `json:"status"`
}

func needFakeMaps(t *testing.T) {
	t.Helper()
	if fakeMapsAddr == "" {
		t.Skip("PLUMA_E2E_FAKEMAPS is not set: the stand has no maps service")
	}
}

// fakeMapsPort asks compose for the current port of fakemaps: a container
// that was recreated has a new one.
func fakeMapsPort() (string, error) {
	out, err := exec.Command(runScript, "compose", "port", "fakemaps", "8080").Output()
	return strings.TrimSpace(string(out)), err
}

// mapPoint is a point no other test and no earlier run asked a map of: six
// decimals, as bff rounds them, in the region of the maps.
func mapPoint(t *testing.T) (lat, long float64) {
	t.Helper()
	n := func(limit int64) int64 {
		v, err := rand.Int(rand.Reader, big.NewInt(limit))
		if err != nil {
			t.Fatal(err)
		}
		return v.Int64()
	}
	return float64(30_000_000+n(40_000_000)) / 1e6, float64(-100_000_000+n(150_000_000)) / 1e6
}

// mapRequests is what fakemaps got for the point, oldest first.
func mapRequests(t *testing.T, lat, long float64) []mapRequest {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://%s/e2e/requests?lon=%.6f&lat=%.6f", fakeMapsAddr, long, lat))
	if err != nil {
		t.Fatalf("fakemaps: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fakemaps: requests: status %d", resp.StatusCode)
	}
	var out []mapRequest
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("fakemaps: requests: %v", err)
	}
	return out
}

// planMap makes the next count requests for the point wait delay and answer
// status (200: the map).
func planMap(t *testing.T, lat, long float64, status int, delay time.Duration, count int) {
	t.Helper()
	resp, err := http.Post(fmt.Sprintf("http://%s/e2e/plan?lon=%.6f&lat=%.6f&status=%d&delay_ms=%d&count=%d",
		fakeMapsAddr, long, lat, status, delay.Milliseconds(), count), "", nil)
	if err != nil {
		t.Fatalf("fakemaps: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fakemaps: plan: status %d", resp.StatusCode)
	}
}

// samePoint: a point came back as it was sent, within the six decimals
// the maps are drawn at.
func samePoint(g *mtproto.GeoPoint, lat, long float64) bool {
	return math.Abs(g.GetLat()-lat) < 5e-7 && math.Abs(g.GetLong()-long) < 5e-7
}

func mapLocation(c *mtclient.Client, lat, long float64, accessHash int64, w, h, zoom, scale int32) *mtproto.InputWebFileLocation {
	return &mtproto.InputWebFileLocation{
		Constructor: c.Constructor(mtproto.Predicate_inputWebFileGeoPointLocation),
		GeoPoint: &mtproto.InputGeoPoint{
			Constructor: c.Constructor(mtproto.Predicate_inputGeoPoint),
			Lat:         lat,
			Long:        long,
		},
		AccessHash: accessHash,
		W:          w,
		H:          h,
		Zoom:       zoom,
		Scale:      scale,
	}
}

func geoMedia(lat, long float64) *mtproto.InputMedia {
	return mtproto.MakeTLInputMediaGeoPoint(&mtproto.InputMedia{
		GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint(),
	}).To_InputMedia()
}

// webMap is a map as a client loads it.
type webMap struct {
	bytes         []byte
	width, height int
	// seq is the request of fakemaps the map was rendered by.
	seq int
}

// fetchWebMap loads the map at loc in parts of part bytes, as the clients
// do (Android FileLoadOperation.java with parts of 32 KB, tdesktop
// file_download_mtproto.cpp), and checks what every part says of the file:
// one size (tdesktop cancels a load whose parts differ,
// file_download_mtproto.cpp:170-181), image/png (Android WebFile.java:43),
// storage.filePng. A part past the end has no bytes.
func fetchWebMap(ctx context.Context, u *user, loc *mtproto.InputWebFileLocation, part int32) (*webMap, error) {
	get := func(offset int32) (*mtproto.Upload_WebFile, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetWebFile{
			Constructor: u.c.Constructor(mtproto.Predicate_upload_getWebFile),
			Location:    loc,
			Offset:      offset,
			Limit:       part,
		})
		if err != nil {
			return nil, err
		}
		f, ok := o.(interface {
			To_Upload_WebFile() *mtproto.Upload_WebFile
		})
		if !ok {
			return nil, fmt.Errorf("upload.getWebFile returned %T", o)
		}
		wf := f.To_Upload_WebFile()
		if wf.GetMimeType() != "image/png" || wf.GetFileType().GetPredicateName() != mtproto.Predicate_storage_filePng {
			return nil, fmt.Errorf("a part of %s, %s", wf.GetMimeType(), wf.GetFileType().GetPredicateName())
		}
		return wf, nil
	}
	var (
		body []byte
		size int32 = -1
	)
	for offset := int32(0); size < 0 || offset < size; {
		wf, err := get(offset)
		if err != nil {
			return nil, err
		}
		switch {
		case size < 0:
			if size = wf.GetSize2(); size <= 0 {
				return nil, fmt.Errorf("the first part tells size %d", size)
			}
		case wf.GetSize2() != size:
			return nil, fmt.Errorf("the part at %d tells size %d, the first %d", offset, wf.GetSize2(), size)
		}
		n := int32(len(wf.GetBytes()))
		if n == 0 || n > part || (n < part && offset+n != size) {
			return nil, fmt.Errorf("the part at %d of %d has %d bytes", offset, size, n)
		}
		body = append(body, wf.GetBytes()...)
		offset += n
	}
	past, err := get(size)
	if err != nil {
		return nil, fmt.Errorf("the part past the end: %w", err)
	}
	if len(past.GetBytes()) != 0 || past.GetSize2() != size {
		return nil, fmt.Errorf("the part past the end: %d bytes, size %d", len(past.GetBytes()), past.GetSize2())
	}

	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("the map is not a PNG: %w", err)
	}
	c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA)
	return &webMap{
		bytes: body,
		width: img.Bounds().Dx(), height: img.Bounds().Dy(),
		seq: int(c.R)<<16 | int(c.G)<<8 | int(c.B),
	}, nil
}

func getWebMap(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputWebFileLocation, part int32) *webMap {
	t.Helper()
	m, err := fetchWebMap(ctx, u, loc, part)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	return m
}

// incomingGeo waits until the newest message with a point in u's dialog
// with peer has a point that match accepts, and returns the message.
func incomingGeo(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, match func(*mtproto.GeoPoint) bool) *mtproto.Message {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer,
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		for _, m := range messagesOf(t, o).GetMessages() {
			if g := m.GetMedia().GetGeo(); g.GetPredicateName() == mtproto.Predicate_geoPoint {
				if match(g) {
					return m
				}
				break
			}
		}
	}
	t.Fatal("the point did not reach the receiver")
	return nil
}

// TestWebMapGeo: Alice sends Bob a point; Bob's client loads the map of a
// bubble of Web K on a phone at devicePixelRatio 2 (277×195,
// components/wrappers/geo.ts:63-64, 152) at Android's zoom 15
// (ChatMessageCell.java:8828), in parts of 32 KB as Android loads and again
// in parts of 128 KB: the same PNG of 554×390, rendered once, in the style
// of Bob's language. Alice's client, in English, gets a map of its own
// style.
func TestWebMapGeo(t *testing.T) {
	needFakeMaps(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginIn(ctx, t, phones[0], "Alice", "en")
	bob := loginIn(ctx, t, phones[1], "Bob", "ru")
	lat, long := mapPoint(t)

	sendMedia(ctx, t, alice, inputPeerOf(ctx, t, alice, bob), geoMedia(lat, long))
	m := incomingGeo(ctx, t, bob, inputPeerOf(ctx, t, bob, alice), func(*mtproto.GeoPoint) bool { return true })
	g := m.GetMedia().GetGeo()
	if !samePoint(g, lat, long) {
		t.Fatalf("Bob got the point %v,%v, sent %v,%v", g.GetLat(), g.GetLong(), lat, long)
	}

	loc := mapLocation(bob.c, g.GetLat(), g.GetLong(), g.GetAccessHash(), 277, 195, 15, 2)
	small := getWebMap(ctx, t, bob, loc, 32<<10)
	if small.width != 554 || small.height != 390 {
		t.Fatalf("a map of %d×%d, want 554×390", small.width, small.height)
	}
	if len(small.bytes) <= 128<<10 {
		t.Fatalf("a map of %d bytes: the stand's maps must take several parts", len(small.bytes))
	}
	large := getWebMap(ctx, t, bob, loc, 128<<10)
	if !bytes.Equal(small.bytes, large.bytes) {
		t.Fatal("the parts of 32 KB and of 128 KB make different maps")
	}

	reqs := mapRequests(t, lat, long)
	want := fmt.Sprint([]mapRequest{{Seq: small.seq, Style: "ru", Lon: long, Lat: lat, Zoom: 15, W: 277, H: 195, Scale: 2, Status: 200}})
	if fmt.Sprint(reqs) != want {
		t.Fatalf("fakemaps got %+v, want %s", reqs, want)
	}

	en := getWebMap(ctx, t, alice, mapLocation(alice.c, lat, long, 0, 277, 195, 15, 2), 128<<10)
	reqs = mapRequests(t, lat, long)
	if len(reqs) != 2 || reqs[1].Style != "en" || reqs[1].Seq != en.seq || en.seq == small.seq {
		t.Fatalf("an English client: map of request %d, fakemaps got %+v", en.seq, reqs)
	}
}

// TestWebMapShared: two clients that ask for the same map at once, while
// the maps service is slow, get the same image of one render.
func TestWebMapShared(t *testing.T) {
	needFakeMaps(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	users := []*user{login(ctx, t, phones[0], "Carl"), login(ctx, t, phones[1], "Dave")}
	lat, long := mapPoint(t)
	planMap(t, lat, long, http.StatusOK, 1500*time.Millisecond, 1)

	maps := make([]*webMap, len(users))
	errs := make([]error, len(users))
	var wg sync.WaitGroup
	for i, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			maps[i], errs[i] = fetchWebMap(ctx, u, mapLocation(u.c, lat, long, 0, 320, 160, 15, 1), 128<<10)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
	}
	if !bytes.Equal(maps[0].bytes, maps[1].bytes) {
		t.Fatal("the two clients got different maps")
	}
	if reqs := mapRequests(t, lat, long); len(reqs) != 1 || reqs[0].Seq != maps[0].seq {
		t.Fatalf("fakemaps got %+v, want one request", reqs)
	}
}

// TestWebMapLive: a live location moves by an edit of its message (Android
// ChatMessageCell.java:8781-8784 then asks for the map of the new point):
// the new point is a new render, the old one still comes from the cache.
func TestWebMapLive(t *testing.T) {
	needFakeMaps(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob, toAlice := inputPeerOf(ctx, t, alice, bob), inputPeerOf(ctx, t, bob, alice)
	lat1, long1 := mapPoint(t)
	lat2, long2 := mapPoint(t)
	point := func(lat, long float64) *mtproto.InputGeoPoint {
		return mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: long}).To_InputGeoPoint()
	}

	id := sendMedia(ctx, t, alice, toBob, mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{
		GeoPoint: point(lat1, long1), Period: wrapperspb.Int32(900),
	}).To_InputMedia())
	g := incomingGeo(ctx, t, bob, toAlice, func(*mtproto.GeoPoint) bool { return true }).GetMedia().GetGeo()
	first := getWebMap(ctx, t, bob, mapLocation(bob.c, g.GetLat(), g.GetLong(), g.GetAccessHash(), 260, 150, 15, 2), 32<<10)

	// Android moves the location without the period.
	editedLocation(ctx, t, alice, toBob, id, mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{
		GeoPoint: point(lat2, long2),
	}).To_InputMedia())
	g = incomingGeo(ctx, t, bob, toAlice, func(g *mtproto.GeoPoint) bool { return samePoint(g, lat2, long2) }).GetMedia().GetGeo()
	moved := getWebMap(ctx, t, bob, mapLocation(bob.c, g.GetLat(), g.GetLong(), g.GetAccessHash(), 260, 150, 15, 2), 32<<10)
	if reqs := mapRequests(t, lat2, long2); len(reqs) != 1 || reqs[0].Seq != moved.seq || moved.seq == first.seq {
		t.Fatalf("the moved point: map of request %d, fakemaps got %+v", moved.seq, reqs)
	}

	again := getWebMap(ctx, t, bob, mapLocation(bob.c, lat1, long1, 0, 260, 150, 15, 2), 32<<10)
	if !bytes.Equal(again.bytes, first.bytes) {
		t.Fatal("the old point gave another map")
	}
	if reqs := mapRequests(t, lat1, long1); len(reqs) != 1 {
		t.Fatalf("the old point was rendered again: %+v", reqs)
	}
}

// TestWebMapRefusals: the errors of upload.getWebFile for maps
// (https://core.telegram.org/method/upload.getWebFile and the common rules
// of pkg/webfile) come before any render; a failing maps service is a 500
// that is not cached, so the client's next try renders again.
func TestWebMapRefusals(t *testing.T) {
	needFakeMaps(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	lat, long := mapPoint(t)

	part := func(loc *mtproto.InputWebFileLocation, offset, limit int32) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLUploadGetWebFile{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_getWebFile),
			Location:    loc,
			Offset:      offset,
			Limit:       limit,
		})
		return err
	}
	for _, c := range []struct {
		name          string
		loc           *mtproto.InputWebFileLocation
		offset, limit int32
		want          string
	}{
		{"width 0", mapLocation(alice.c, lat, long, 0, 0, 100, 15, 2), 0, 128 << 10, "LOCATION_INVALID"},
		{"longitude 200", mapLocation(alice.c, lat, 200, 0, 277, 195, 15, 2), 0, 128 << 10, "LOCATION_INVALID"},
		{"offset -1", mapLocation(alice.c, lat, long, 0, 277, 195, 15, 2), -1, 128 << 10, "OFFSET_INVALID"},
		{"a part of 2 MiB", mapLocation(alice.c, lat, long, 0, 277, 195, 15, 2), 0, 2 << 20, "LIMIT_INVALID"},
	} {
		if err := part(c.loc, c.offset, c.limit); !mtclient.IsRPCError(err, c.want) {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	if reqs := mapRequests(t, lat, long); len(reqs) != 0 {
		t.Fatalf("refused requests reached the maps service: %+v", reqs)
	}

	planMap(t, lat, long, http.StatusServiceUnavailable, 0, 1)
	loc := mapLocation(alice.c, lat, long, 0, 277, 195, 15, 2)
	var rpcErr *mtclient.RPCError
	if err := part(loc, 0, 128<<10); !errors.As(err, &rpcErr) || rpcErr.Code != 500 || rpcErr.Message != "INTERNAL_SERVER_ERROR" {
		t.Fatalf("the maps service answered 503: %v, want 500 INTERNAL_SERVER_ERROR", err)
	}
	m := getWebMap(ctx, t, alice, loc, 128<<10)
	reqs := mapRequests(t, lat, long)
	if len(reqs) != 2 || reqs[0].Status != http.StatusServiceUnavailable || reqs[1].Status != http.StatusOK || reqs[1].Seq != m.seq {
		t.Fatalf("after a failed render: map of request %d, fakemaps got %+v", m.seq, reqs)
	}
}

// TestWebMapScale: what tdesktop asks for when cScale·DPR reaches 600, such
// as 300 % on a 2x screen (scale = 1 + cScale·DPR/200, zoom = 13 + scale − 1,
// data/data_location.cpp:95-110), is rendered, scale 4 at most, and every
// side within 2048 pixels; the other fields are brought into the documented
// ranges
// (https://core.telegram.org/constructor/inputWebFileGeoPointLocation),
// not refused (ADR 0063, p. 2).
func TestWebMapScale(t *testing.T) {
	needFakeMaps(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")

	for _, c := range []struct {
		name                  string
		w, h, zoom, scale     int32
		wantW, wantH          int
		wantZoom              float64
		wantScale             int
		wantWidth, wantHeight int
	}{
		{"tdesktop at 300 % on a 2x screen", 200, 150, 16, 4, 200, 150, 16, 4, 800, 600},
		{"scale 5", 200, 150, 17, 5, 200, 150, 17, 4, 800, 600},
		{"1024×1024 at 4x", 1024, 1024, 15, 4, 1024, 1024, 15, 2, 2048, 2048},
		{"out of every range", 2000, 10, 25, 0, 1024, 16, 20, 1, 1024, 16},
	} {
		t.Run(c.name, func(t *testing.T) {
			lat, long := mapPoint(t)
			m := getWebMap(ctx, t, alice, mapLocation(alice.c, lat, long, 0, c.w, c.h, c.zoom, c.scale), 512<<10)
			if m.width != c.wantWidth || m.height != c.wantHeight {
				t.Errorf("a map of %d×%d, want %d×%d", m.width, m.height, c.wantWidth, c.wantHeight)
			}
			want := fmt.Sprint([]mapRequest{{Seq: m.seq, Style: "en", Lon: long, Lat: lat, Zoom: c.wantZoom,
				W: c.wantW, H: c.wantH, Scale: c.wantScale, Status: 200}})
			if reqs := mapRequests(t, lat, long); fmt.Sprint(reqs) != want {
				t.Errorf("fakemaps got %+v, want %s", reqs, want)
			}
		})
	}
}

// TestConfigWebfileDc: the clients send upload.getWebFile to webfile_dc_id
// (Android MessagesController.java:5696, tdesktop mtproto_config.cpp:257,
// Web K appManagers/apiFileManager.ts:200), 4 by default, and the patched clients know
// only DC 1 (clients/patches/android.md, desktop.md, web.md).
func TestConfigWebfileDc(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	o, err := alice.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: alice.c.Constructor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}
	conf, ok := o.(*mtproto.TLConfig)
	if !ok {
		t.Fatalf("help.getConfig returned %T", o)
	}
	if conf.GetWebfileDcId() != 1 {
		t.Fatalf("webfile_dc_id %d, want 1", conf.GetWebfileDcId())
	}
}
