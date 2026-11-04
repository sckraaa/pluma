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

// Package webpage builds the cards of links (task 023, ADR 0027): the
package webpage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// youtubeWatch is the base the pictures of a YouTube card are taken from.
var youtubeWatch = &url.URL{Scheme: "https", Host: "www.youtube.com", Path: "/watch"}

// youtubeID is a video's id on YouTube.
var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youtubeHosts are the hosts of YouTube's pages of a video.
var youtubeHosts = map[string]bool{
	"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true, "music.youtube.com": true,
}

// ytVideo is a video on YouTube and where the link starts it (seconds).
type ytVideo struct {
	id    string
	start int
}

// youtubeVideo reads the video of a YouTube link: youtu.be/<id>, or
// /watch?v=<id>, /shorts/<id>, /embed/<id>, /live/<id> on YouTube's hosts;
// t= or start= is where it starts (seconds, or 1h2m3s).
func youtubeVideo(u *url.URL) (ytVideo, bool) {
	host := strings.ToLower(u.Hostname())
	var id string
	switch {
	case host == "youtu.be" || host == "www.youtu.be":
		id = strings.Trim(u.Path, "/")
	case youtubeHosts[host]:
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case len(parts) == 1 && parts[0] == "watch":
			id = u.Query().Get("v")
		case len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live"):
			id = parts[1]
		}
	}
	if !youtubeID.MatchString(id) {
		return ytVideo{}, false
	}
	q := u.Query()
	t := q.Get("t")
	if t == "" {
		t = q.Get("start")
	}
	return ytVideo{id: id, start: seconds(t)}, true
}

// seconds reads "183", "183s" or "1h2m3s"; 0 for anything else.
func seconds(t string) int {
	if n, err := strconv.Atoi(t); err == nil && n > 0 {
		return n
	}
	total, num := 0, 0
	digits := false
	for _, r := range t {
		switch {
		case r >= '0' && r <= '9':
			num, digits = num*10+int(r-'0'), true
		case digits && (r == 'h' || r == 'm' || r == 's'):
			total += num * map[rune]int{'h': 3600, 'm': 60, 's': 1}[r]
			num, digits = 0, false
		default:
			return 0
		}
	}
	if digits || total > 24*3600 {
		return 0
	}
	return total
}

// youtubeOEmbed is what YouTube's oEmbed tells of a video
// (oembed.com, "type": "video").
type youtubeOEmbed struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ProviderName string `json:"provider_name"`
}

// youtubeMeta makes the card of a YouTube video from its oEmbed, as
// Telegram's card of one is: a video, with YouTube's player and the
// video's picture; false if oEmbed does not answer.
func (s *Service) youtubeMeta(ctx context.Context, v ytVideo) (Meta, bool) {
	base, images := s.youtube, s.ytimg
	if base == "" {
		base, images = "https://www.youtube.com", "https://i.ytimg.com"
	}
	q := url.Values{"url": {"https://www.youtube.com/watch?v=" + v.id}, "format": {"json"}}
	u, err := url.Parse(base + "/oembed?" + q.Encode())
	if err != nil {
		return Meta{}, false
	}
	var o youtubeOEmbed
	if err := s.fetch.json(ctx, u, &o); err != nil {
		s.logger.Infof("webpage: YouTube's oEmbed not read: %v", errKind(err))
		return Meta{}, false
	}
	if o.Title == "" {
		return Meta{}, false
	}
	embed := "https://www.youtube.com/embed/" + v.id
	if v.start > 0 {
		embed += "?start=" + strconv.Itoa(v.start)
	}
	m := Meta{
		Title:       clip(o.Title, maxTitle),
		SiteName:    "YouTube",
		Type:        "video.other",
		Author:      clip(o.AuthorName, maxTitle),
		Image:       images + "/vi/" + v.id + "/maxresdefault.jpg",
		Video:       embed,
		VideoType:   "text/html",
		VideoWidth:  1280,
		VideoHeight: 720,
		Tagged:      true,
	}
	// maxresdefault is not made for every video: hqdefault (oEmbed's
	// thumbnail_url) is.
	m.fallbackImage = images + "/vi/" + v.id + "/hqdefault.jpg"
	return m, true
}

// maxJSON bounds an answer of JSON.
const maxJSON = 64 << 10

// json fetches a JSON answer into v, at most maxJSON bytes.
func (f *fetcher) json(ctx context.Context, u *url.URL, v any) error {
	resp, err := f.get(ctx, u, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "application/json" {
		return fmt.Errorf("%w: %s", errNoPage, mt)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxJSON)).Decode(v)
}
