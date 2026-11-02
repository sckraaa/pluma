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
	"errors"
	"fmt"
	"image"
	_ "image/gif" // pictures of pages: decoders
	"image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/teamgram/proto/mtproto"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The card of a page, as Telegram builds it (messages.getWebPagePreview of
// telegram.org, github.com, en.wikipedia.org, youtube.com, example.com and
// news.ycombinator.com, 2026-11-02, docs/tasks/023):
//   - url is the address normalized, display_url the same without the
//     scheme, "www." and a lone "/";
//   - a page with neither a description nor a picture has no card:
//     webPageEmpty (example.com and news.ycombinator.com have only a title);
//   - type is "video" for a video page (og:type video.*), "photo" for a page
//     with a large picture (twitter:card summary_large_image: github.com,
//     with has_large_media), otherwise "article" (telegram.org's square
//     256 px picture, Wikipedia's);
//   - a video page has its player: embed_url, embed_type "iframe" and its
//     size (YouTube), and has_large_media.

// normalize returns the address Telegram answers with: lower-case scheme
// and host, no "www.", no fragment, and "/" for an empty path.
func normalize(u *url.URL) *url.URL {
	n := *u
	n.Scheme = strings.ToLower(n.Scheme)
	n.Host = strings.TrimPrefix(strings.ToLower(n.Host), "www.")
	n.Fragment, n.RawFragment = "", ""
	if n.Path == "" {
		n.Path, n.RawPath = "/", ""
	}
	return &n
}

// displayURL is the address as the card shows it: no scheme, and no
// trailing "/" of the root.
func displayURL(u *url.URL) string {
	s := u.Host + u.EscapedPath()
	if u.RawQuery != "" {
		s += "?" + u.RawQuery
	}
	return strings.TrimSuffix(s, "/")
}

// parseLink parses the link of a message: with no scheme it is http, as
// clients make it of a bare "example.com".
func parseLink(link string) (*url.URL, error) {
	link = strings.TrimSpace(link)
	if !strings.Contains(link, "://") {
		link = "http://" + link
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	if err := fetchable(u); err != nil {
		return nil, err
	}
	return u, nil
}

// hasCard reports whether a page gets a card at all.
func (m Meta) hasCard() bool {
	return m.Description != "" || m.Image != ""
}

// pageType is the type of the card.
func (m Meta) pageType() string {
	switch {
	case strings.HasPrefix(m.Type, "video"):
		return "video"
	case m.Card == "summary_large_image":
		return "photo"
	default:
		return "article"
	}
}

// largeMedia reports whether clients show the picture large.
func (m Meta) largeMedia() bool {
	return m.pageType() != "article"
}

// buildPage makes the webPage of a page; photo is its picture, nil if it
// has none.
func buildPage(id int64, u *url.URL, m Meta, photo *mtproto.Photo) *mtproto.WebPage {
	p := &mtproto.WebPage{
		Id:            id,
		Url_STRING:    u.String(),
		DisplayUrl:    displayURL(u),
		Hash:          0,
		HasLargeMedia: m.largeMedia(),
		Type:          str(m.pageType()),
		SiteName:      str(m.SiteName),
		Title:         str(m.Title),
		Description:   str(m.Description),
		Author:        str(m.Author),
		Photo:         photo,
	}
	if m.pageType() == "video" && strings.HasPrefix(m.Video, "https://") && (m.VideoType == "" || strings.Contains(m.VideoType, "html")) {
		p.EmbedUrl = str(m.Video)
		p.EmbedType = str("iframe")
		if m.VideoWidth > 0 && m.VideoHeight > 0 {
			p.EmbedWidth = i32(int32(m.VideoWidth))
			p.EmbedHeight = i32(int32(m.VideoHeight))
		}
	}
	return mtproto.MakeTLWebPage(p).To_WebPage()
}

// emptyPage is the card of a page without one.
func emptyPage(id int64, u *url.URL) *mtproto.WebPage {
	return mtproto.MakeTLWebPageEmpty(&mtproto.WebPage{
		Id:             id,
		Url_FLAGSTRING: mtproto.MakeFlagsString(u.String()),
	}).To_WebPage()
}

// maxSide is the longest side of the picture stored: the largest size
// Telegram makes of one is 1280 px ("y", github.com's 1200 x 600).
const maxSide = 1280

// maxPixels guards against a picture that decodes to gigabytes.
const maxPixels = 40_000_000

// pictureJPEG turns a page's picture into the JPEG the server stores: any
// of JPEG, PNG, GIF (its first frame) or WebP, at most maxSide on its
// longer side.
func pictureJPEG(b []byte) ([]byte, int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, 0, 0, fmt.Errorf("webpage: picture of %d x %d", cfg.Width, cfg.Height)
	}
	if cfg.Width < 32 || cfg.Height < 32 {
		return nil, 0, 0, errors.New("webpage: picture too small for a card")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width > maxSide || cfg.Height > maxSide {
		img = imaging.Fit(img, maxSide, maxSide, imaging.Lanczos)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 87}); err != nil {
		return nil, 0, 0, err
	}
	bounds := img.Bounds()
	return out.Bytes(), bounds.Dx(), bounds.Dy(), nil
}

func str(s string) *wrapperspb.StringValue {
	if s == "" {
		return nil
	}
	return mtproto.MakeFlagsString(s)
}

func i32(v int32) *wrapperspb.Int32Value {
	return mtproto.MakeFlagsInt32(v)
}
