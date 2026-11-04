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
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestYoutubeVideo(t *testing.T) {
	for _, c := range []struct {
		link  string
		id    string
		start int
	}{
		{"https://youtu.be/OBzWl3Bw1JY?t=183&is=vfoAUXMoA-eib_pw", "OBzWl3Bw1JY", 183},
		{"https://www.youtube.com/watch?v=OBzWl3Bw1JY&t=1h2m3s", "OBzWl3Bw1JY", 3723},
		{"https://m.youtube.com/shorts/OBzWl3Bw1JY", "OBzWl3Bw1JY", 0},
		{"https://youtube.com/embed/OBzWl3Bw1JY?start=7", "OBzWl3Bw1JY", 7},
		{"https://www.youtube.com/live/OBzWl3Bw1JY?t=5s", "OBzWl3Bw1JY", 5},
		{"https://www.youtube.com/watch?v=short", "", 0},
		{"https://www.youtube.com/@channel", "", 0},
		{"https://example.com/watch?v=OBzWl3Bw1JY", "", 0},
		{"https://youtu.be/OBzWl3Bw1JY?t=x", "OBzWl3Bw1JY", 0},
	} {
		u, err := url.Parse(c.link)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := youtubeVideo(u)
		if ok != (c.id != "") || v.id != c.id || v.start != c.start {
			t.Errorf("%s: %+v %v", c.link, v, ok)
		}
	}
}

// TestPreviewYoutube: YouTube serves the server an empty page (no video
// tags, the title " - YouTube"), which made the card "- YouTube" with
// YouTube's own description and no picture (the owner's report, task 055):
// the card comes from oEmbed instead, a video with YouTube's player from
// the link's start and the video's picture (hqdefault where maxresdefault
// is missing).
func TestPreviewYoutube(t *testing.T) {
	var jpeg []byte
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/OBzWl3Bw1JY":
			serveHTML(w, `<head><title> - YouTube</title><meta name="description" content="Enjoy the videos and music you love."></head>`)
		case "/oembed":
			if r.URL.Query().Get("url") != "https://www.youtube.com/watch?v=OBzWl3Bw1JY" || r.URL.Query().Get("format") != "json" {
				t.Errorf("oEmbed query %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"title":"A video","author_name":"Its author","type":"video","provider_name":"YouTube","thumbnail_url":"x"}`))
		case "/vi/OBzWl3Bw1JY/hqdefault.jpg":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(jpeg)
		default:
			http.NotFound(w, r)
		}
	}))
	env.s.youtube, env.s.ytimg = "http://yt.example", "http://img.example"
	jpeg = testPNG(t, 480, 360)
	p, err := env.s.Preview(context.Background(), "http://youtu.be/OBzWl3Bw1JY?t=183")
	if err != nil {
		t.Fatal(err)
	}
	if p.GetPredicateName() != mtproto.Predicate_webPage || p.GetTitle().GetValue() != "A video" || p.GetAuthor().GetValue() != "Its author" ||
		p.GetSiteName().GetValue() != "YouTube" || p.GetType().GetValue() != "video" || !p.GetHasLargeMedia() ||
		p.GetEmbedUrl().GetValue() != "https://www.youtube.com/embed/OBzWl3Bw1JY?start=183" || p.GetEmbedType().GetValue() != "iframe" ||
		p.GetPhoto() == nil || p.GetDescription().GetValue() != "" {
		t.Fatalf("card %v", p)
	}
	if c, _ := env.cache.get(context.Background(), "http://youtu.be/OBzWl3Bw1JY?t=183"); c == nil || c.expires != env.clock.Add(pageTTL).Unix() {
		t.Errorf("cached until %v", c)
	}
}

// TestPreviewYoutubeNoOEmbed: when oEmbed does not answer, YouTube's empty
// page is no card: none, asked again soon.
func TestPreviewYoutubeNoOEmbed(t *testing.T) {
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/OBzWl3Bw1JY" {
			serveHTML(w, `<head><title> - YouTube</title><meta name="description" content="Enjoy the videos and music you love."></head>`)
			return
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	env.s.youtube, env.s.ytimg = "http://yt.example", "http://img.example"
	p, err := env.s.Preview(context.Background(), "http://youtu.be/OBzWl3Bw1JY")
	if err != nil {
		t.Fatal(err)
	}
	if p.GetPredicateName() != mtproto.Predicate_webPageEmpty {
		t.Fatalf("card %v", p)
	}
	if c, _ := env.cache.get(context.Background(), "http://youtu.be/OBzWl3Bw1JY"); c == nil || time.Unix(c.expires, 0) != env.clock.Add(failedTTL) {
		t.Errorf("cached %v", c)
	}
}

// TestPreviewUntagged: a card of the plain head (no Open Graph, no Twitter
// Cards) is asked again sooner: it may be a page served in place of the
// real one.
func TestPreviewUntagged(t *testing.T) {
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveHTML(w, `<head><title>Plain</title><meta name="description" content="A page"></head>`)
	}))
	if _, err := env.s.Preview(context.Background(), "http://plain.example/"); err != nil {
		t.Fatal(err)
	}
	c, _ := env.cache.get(context.Background(), "http://plain.example/")
	if c == nil || time.Unix(c.expires, 0) != env.clock.Add(emptyTTL) {
		t.Errorf("cached %v", c)
	}
}
