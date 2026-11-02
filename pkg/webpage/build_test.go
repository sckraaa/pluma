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

package webpage

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/url"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestNormalizeAndDisplayURL(t *testing.T) {
	for _, tc := range []struct{ in, url, display string }{
		{"https://telegram.org", "https://telegram.org/", "telegram.org"},
		{"HTTPS://WWW.YouTube.com/watch?v=abc#t=1", "https://youtube.com/watch?v=abc", "youtube.com/watch?v=abc"},
		{"https://en.wikipedia.org/wiki/Go_(programming_language)", "https://en.wikipedia.org/wiki/Go_(programming_language)",
			"en.wikipedia.org/wiki/Go_(programming_language)"},
		{"http://github.com/teamgram/", "http://github.com/teamgram/", "github.com/teamgram"},
	} {
		n := normalize(mustURL(t, tc.in))
		if n.String() != tc.url || displayURL(n) != tc.display {
			t.Errorf("%s: %s, %s; want %s, %s", tc.in, n, displayURL(n), tc.url, tc.display)
		}
	}
}

func TestParseLink(t *testing.T) {
	u, err := parseLink(" example.com/a ")
	if err != nil || u.String() != "http://example.com/a" {
		t.Fatalf("bare link: %v, %v", u, err)
	}
	for _, bad := range []string{"http://192.168.0.1/", "localhost:8080", "mailto:a@b.example", "http://%zz"} {
		if _, err := parseLink(bad); err == nil {
			t.Errorf("parseLink(%q) accepted", bad)
		}
	}
}

func TestPageType(t *testing.T) {
	for _, tc := range []struct {
		m     Meta
		typ   string
		large bool
	}{
		{Meta{Type: "website"}, "article", false},
		{Meta{Type: "article", Card: "summary"}, "article", false},
		{Meta{Type: "object", Card: "summary_large_image"}, "photo", true},
		{Meta{Type: "video.other", Card: "player"}, "video", true},
		{Meta{}, "article", false},
	} {
		if got := tc.m.pageType(); got != tc.typ || tc.m.largeMedia() != tc.large {
			t.Errorf("%+v: %s, %v; want %s, %v", tc.m, got, tc.m.largeMedia(), tc.typ, tc.large)
		}
	}
}

func TestHasCard(t *testing.T) {
	if (Meta{Title: "Only a title"}).hasCard() {
		t.Error("a title alone makes a card (example.com has none in Telegram)")
	}
	if !(Meta{Description: "d"}).hasCard() || !(Meta{Image: "/i.png"}).hasCard() {
		t.Error("a description or a picture makes no card")
	}
}

func TestBuildPage(t *testing.T) {
	u := mustURL(t, "https://youtube.com/watch?v=abc")
	photo := mtproto.MakeTLPhoto(&mtproto.Photo{Id: 7}).To_Photo()
	p := buildPage(42, u, Meta{
		Title: "A video", Description: "About it", SiteName: "YouTube", Type: "video.other",
		Video: "https://www.youtube.com/embed/abc", VideoType: "text/html", VideoWidth: 1280, VideoHeight: 720,
	}, photo)
	if p.GetPredicateName() != mtproto.Predicate_webPage || p.GetId() != 42 || p.GetUrl_STRING() != u.String() ||
		p.GetDisplayUrl() != "youtube.com/watch?v=abc" || p.GetHash() != 0 {
		t.Fatalf("page %v", p)
	}
	if p.GetType().GetValue() != "video" || !p.GetHasLargeMedia() || p.GetPhoto().GetId() != 7 {
		t.Fatalf("type %v, large %v, photo %v", p.GetType(), p.GetHasLargeMedia(), p.GetPhoto())
	}
	if p.GetEmbedUrl().GetValue() != "https://www.youtube.com/embed/abc" || p.GetEmbedType().GetValue() != "iframe" ||
		p.GetEmbedWidth().GetValue() != 1280 || p.GetEmbedHeight().GetValue() != 720 {
		t.Fatalf("embed %v %v %v %v", p.GetEmbedUrl(), p.GetEmbedType(), p.GetEmbedWidth(), p.GetEmbedHeight())
	}
	if p.GetAuthor() != nil {
		t.Fatalf("an author that the page did not name: %v", p.GetAuthor())
	}
	// A player over plain http, or a video file, is no embed.
	for _, m := range []Meta{
		{Description: "d", Type: "video", Video: "http://site.example/embed"},
		{Description: "d", Type: "video", Video: "https://site.example/v.mp4", VideoType: "video/mp4"},
	} {
		if p := buildPage(1, u, m, nil); p.GetEmbedUrl() != nil {
			t.Errorf("%+v: embed %v", m, p.GetEmbedUrl())
		}
	}
	e := emptyPage(5, u)
	if e.GetPredicateName() != mtproto.Predicate_webPageEmpty || e.GetId() != 5 || e.GetUrl_FLAGSTRING().GetValue() != u.String() {
		t.Fatalf("empty page %v", e)
	}
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPictureJPEG(t *testing.T) {
	out, w, h, err := pictureJPEG(testPNG(t, 2400, 1200))
	if err != nil {
		t.Fatal(err)
	}
	if w != maxSide || h != maxSide/2 {
		t.Fatalf("%d x %d, want %d x %d", w, h, maxSide, maxSide/2)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != w || cfg.Height != h {
		t.Fatalf("the JPEG: %+v, %v", cfg, err)
	}
	if _, w, h, err = pictureJPEG(testPNG(t, 256, 256)); err != nil || w != 256 || h != 256 {
		t.Fatalf("a small picture: %d x %d, %v", w, h, err)
	}
	if _, _, _, err = pictureJPEG(testPNG(t, 16, 16)); err == nil {
		t.Error("an icon became a card's picture")
	}
	if _, _, _, err = pictureJPEG([]byte("<svg/>")); err == nil {
		t.Error("not a picture decoded")
	}
	// A header that claims a huge picture is refused before decoding.
	huge := testPNG(t, 64, 64)
	huge[16], huge[17], huge[18], huge[19] = 0, 0, 0x7f, 0xff // width 32767
	huge[20], huge[21], huge[22], huge[23] = 0, 0, 0x7f, 0xff // height 32767
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(huge)); err != nil || cfg.Width != 32767 {
		t.Fatalf("the forged header: %+v, %v", cfg, err)
	}
	if _, _, _, err = pictureJPEG(huge); err == nil {
		t.Error("a picture of a billion pixels accepted")
	}
}
