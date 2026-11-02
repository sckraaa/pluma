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
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Meta is what a page says about itself in its head: Open Graph
// (ogp.me), Twitter Cards, the title and the description.
type Meta struct {
	Title       string
	Description string
	SiteName    string
	Type        string // og:type
	Card        string // twitter:card
	Image       string // og:image, or twitter:image
	ImageWidth  int
	ImageHeight int
	Video       string // og:video:url, the embed of a video page
	VideoWidth  int
	VideoHeight int
	VideoType   string // og:video:type
	Author      string
	Canonical   string // og:url
}

// parseMeta reads the head of an HTML document in the encoding its
// Content-Type or meta tags give. It stops at the body: everything it needs
// is in the head.
func parseMeta(body io.Reader, contentType string) (Meta, error) {
	r, err := charset.NewReader(body, contentType)
	if err != nil {
		return Meta{}, err
	}
	var (
		m        Meta
		title    strings.Builder
		inTitle  bool
		metaDesc string
		twTitle  string
		twDesc   string
		twImage  string
	)
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return m.finish(title.String(), metaDesc, twTitle, twDesc, twImage), nil
			}
			return m.finish(title.String(), metaDesc, twTitle, twDesc, twImage), z.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				return m.finish(title.String(), metaDesc, twTitle, twDesc, twImage), nil
			case "title":
				inTitle = tt == html.StartTagToken
			case "meta":
				if !hasAttr {
					continue
				}
				var key, content string
				for {
					k, v, more := z.TagAttr()
					switch string(k) {
					case "property", "name", "itemprop":
						if key == "" {
							key = strings.ToLower(strings.TrimSpace(string(v)))
						}
					case "content":
						content = strings.TrimSpace(string(v))
					}
					if !more {
						break
					}
				}
				switch key {
				case "og:title":
					m.Title = content
				case "og:description":
					m.Description = content
				case "og:site_name":
					m.SiteName = content
				case "og:type":
					m.Type = strings.ToLower(content)
				case "og:url":
					m.Canonical = content
				case "og:image", "og:image:url", "og:image:secure_url":
					if m.Image == "" || key == "og:image:secure_url" && strings.HasPrefix(content, "https:") {
						m.Image = content
					}
				case "og:image:width":
					m.ImageWidth = atoi(content)
				case "og:image:height":
					m.ImageHeight = atoi(content)
				case "og:video", "og:video:url", "og:video:secure_url":
					if m.Video == "" || key == "og:video:secure_url" && strings.HasPrefix(content, "https:") {
						m.Video = content
					}
				case "og:video:width":
					m.VideoWidth = atoi(content)
				case "og:video:height":
					m.VideoHeight = atoi(content)
				case "og:video:type":
					m.VideoType = strings.ToLower(content)
				case "article:author", "author":
					if m.Author == "" && !strings.Contains(content, "://") {
						m.Author = content
					}
				case "twitter:card":
					m.Card = strings.ToLower(content)
				case "twitter:title":
					twTitle = content
				case "twitter:description":
					twDesc = content
				case "twitter:image", "twitter:image:src":
					twImage = content
				case "description":
					metaDesc = content
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "head":
				return m.finish(title.String(), metaDesc, twTitle, twDesc, twImage), nil
			}
		case html.TextToken:
			if inTitle && title.Len() < maxTitle*4 {
				title.Write(z.Text())
			}
		}
	}
}

// finish fills what Open Graph left out from Twitter Cards and the plain
// head, and trims every text to its limit.
func (m Meta) finish(title, metaDesc, twTitle, twDesc, twImage string) Meta {
	if m.Title == "" {
		m.Title = twTitle
	}
	if m.Title == "" {
		m.Title = strings.Join(strings.Fields(title), " ")
	}
	if m.Description == "" {
		m.Description = twDesc
	}
	if m.Description == "" {
		m.Description = metaDesc
	}
	if m.Image == "" {
		m.Image = twImage
	}
	m.Title = clip(m.Title, maxTitle)
	m.Description = clip(m.Description, maxDescription)
	m.SiteName = clip(m.SiteName, maxTitle)
	m.Author = clip(m.Author, maxTitle)
	return m
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 100000 {
		return 0
	}
	return n
}

// clip cuts s to at most n runes, at a rune boundary.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}
