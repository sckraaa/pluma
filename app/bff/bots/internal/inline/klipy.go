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

package inline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/webfetch"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"golang.org/x/sync/singleflight"
)

// The GIFs of @gif found with KLIPY (task 099, part G, ADR 0062, p. 4.1).
// The API as docs.klipy.com describes it (read 2026-11-17):
// gifs-api/gifs-search-api.md and gifs-api/gifs-trending-api.md (GET
// api/v1/{app_key}/gifs/search and /trending, page from 1, per_page 8–50
// for search and 1–50 for trending, locale, content_filter off, low,
// medium or high, format_filter; the answer {result, data: {data,
// current_page, per_page, has_next}}, an item with id, slug, title, type
// and file of the sizes hd, md, sm, xs, each of gif, webp, jpg, mp4 and
// webm with url, width, height and size), gifs-api/gifs-format-sizes.md
// (median mp4: md 257 KB, sm 85 KB), getting-started.md (a key in testing
// mode has 100 requests an hour), network-requirements.md (api.klipy.com).
const (
	// KlipyURL is the API of KLIPY.
	KlipyURL = "https://api.klipy.com"
	// klipyProvider names the provider of the media it finds
	// (inline_media.provider).
	klipyProvider = "klipy"
	// klipyPageSize is the GIFs of a page, which is one page of KLIPY.
	klipyPageSize = 20
	// klipySearchCacheTime is the cache_time of the pages of a search, and
	// how long a page of KLIPY is kept in Redis.
	klipySearchCacheTime = 3600
	// klipyTrendingCacheTime is the same for the trending GIFs of the
	// empty query, which change through the day.
	klipyTrendingCacheTime = 600
	// klipyMaxPage is the last page of KLIPY asked for: 200 GIFs of a
	// query, as the popular list of the local provider.
	klipyMaxPage = 10
	// klipyWait bounds a request to KLIPY, the wait for its turn included.
	klipyWait = 6 * time.Second
	// klipyConcurrent bounds the requests to KLIPY at once from the
	// process.
	klipyConcurrent = 4
	// klipyMaxBody bounds the answer read: a page of 20 GIFs with the mp4
	// files only is a few KB, the example of the documentation with every
	// format about 3 KB a GIF.
	klipyMaxBody = 1 << 20
	// klipyContentFilter is the safety filter of the results: medium, as
	// the moderate safe search of @pic (decision to confirm).
	klipyContentFilter = "medium"
	// klipyRetryAfter is how long no request goes to KLIPY after it
	// answered 429 without a Retry-After; klipyRetryAfterMax bounds a
	// Retry-After. A testing key has 100 requests an hour.
	klipyRetryAfter    = time.Minute
	klipyRetryAfterMax = time.Hour
	// klipyMaxRef bounds the slug of a GIF, its key.
	klipyMaxRef = 128
	// klipyListKey prefixes the Redis key of a page of KLIPY; the rest is a
	// hash of the endpoint, the locale, the page and the text, so that the
	// key does not carry the text. The value is klipyListTag and the JSON
	// of the GIFs.
	klipyListKey = "inline_klipy_"
	klipyListTag = "v1:"
)

// klipySizes are the sizes of a GIF whose mp4 is imported, the first that
// fits: md (498 px in the example of the documentation, median 257 KB),
// then sm (320 px, 85 KB). hd is not smaller than md and xs (150 px) is a
// thumbnail.
var klipySizes = []string{"md", "sm"}

// klipyWords are the words KLIPY is asked for the emoji categories of the
// GIF tab (inlinebots.GifSearchEmojies): the clients send the emoji itself
// as the query (Android EmojiView.java:9015 searchEmoji, iOS and tdesktop
// the same keys of gif_search_emojies), and a search of KLIPY is by
// keyword or phrase (gifs-search-api.md).
var klipyWords = map[string]string{
	"👍": "thumbs up",
	"👎": "thumbs down",
	"😍": "love",
	"😂": "lol",
	"😮": "wow",
	"🙄": "eye roll",
	"😥": "sad",
	"😡": "angry",
	"🥳": "party",
	"😎": "cool",
}

// errKlipyLimited is a request not made: KLIPY answered 429 a moment ago.
var errKlipyLimited = errors.New("inline: klipy: rate limited, waiting before the next request")

// metricKlipyRequests counts the requests to KLIPY by how they ended: ok,
// limited (429, or none sent while waiting after one), refused (401 or 403:
// the key), error.
var metricKlipyRequests = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "inline",
	Name:      "klipy_requests_total",
	Help:      "Requests of @gif to KLIPY, by result: ok, limited, refused, error.",
	Labels:    []string{"result"},
})

// Klipy is the provider of @gif with KLIPY_API_KEY: the GIFs of KLIPY's
// search for a text and its trending GIFs for the empty query (the GIF tab
// of the emoji panel, Android EmojiView.java:8939), which the bots service
// imports into dfs as animations; no device reaches KLIPY. The pages keep
// KLIPY's order and only KLIPY's GIFs (integration-requirements.md, items 4
// and 5); a GIF without an mp4 that fits is left out, as it cannot be sent
// as an animation. The server's requests and its copies are what the owner
// asked KLIPY to allow (ADR 0062, p. 4.1).
type Klipy struct {
	base  *url.URL
	key   string
	cache Cache
	// client does not follow redirects: the API answers itself.
	client *http.Client
	slots  chan struct{}
	loads  singleflight.Group
	now    func() time.Time
	// until is the Unix time in nanoseconds before which no request goes
	// to KLIPY, after a 429.
	until atomic.Int64
}

// NewKlipy returns the provider on the KLIPY API at base (KlipyURL) with
// the app key and cache. The errors never carry the key.
func NewKlipy(base, key string, cache Cache) (*Klipy, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("inline: the KLIPY address is not an http(s) address")
	}
	if key == "" || len(key) > 256 || strings.IndexFunc(key, func(r rune) bool { return r <= ' ' || r == '/' || r > unicode.MaxASCII }) >= 0 {
		return nil, errors.New("inline: KLIPY_API_KEY is not a key of KLIPY")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return &Klipy{
		base:  u,
		key:   key,
		cache: cache,
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		slots: make(chan struct{}, klipyConcurrent),
		now:   time.Now,
	}, nil
}

// klipyGif is a GIF of a page of KLIPY: its slug, the key of the medium and
// what is kept for revalidation (inline_media.provider_ref), and its mp4.
type klipyGif struct {
	Slug   string `json:"s"`
	URL    string `json:"u"`
	Width  int32  `json:"w"`
	Height int32  `json:"h"`
}

// Search answers q: trending GIFs for an empty text, the search otherwise.
// An offset is k<page>.<index>: the page of KLIPY and the index in it of
// the first GIF of the page, as the searxng provider does; every page of
// KLIPY is kept in Redis for the cache time, so the pages that follow one
// another come from one answer. An offset this provider did not give is an
// empty page.
func (k *Klipy) Search(ctx context.Context, q Query) (Page, error) {
	text := klipyText(q.Text)
	endpoint, cacheTime := "search", int32(klipySearchCacheTime)
	if text == "" {
		endpoint, cacheTime = "trending", klipyTrendingCacheTime
	}
	page := Page{Items: []Item{}, CacheTime: cacheTime}
	pageno, from, ok := parseKlipyOffset(q.Offset)
	if !ok {
		return page, nil
	}
	locale := klipyLocale(q.Lang)
	var (
		list []klipyGif
		more bool
	)
	for {
		var err error
		if list, more, err = k.gifs(ctx, endpoint, text, locale, pageno, cacheTime); err != nil {
			return Page{}, err
		}
		// A page kept in Redis may have been asked again with fewer GIFs
		// since the offset was given: the next page goes on in its place.
		if from < len(list) || !more || pageno >= klipyMaxPage {
			break
		}
		pageno, from = pageno+1, 0
	}
	for i := from; i < len(list); i++ {
		g := list[i]
		page.Items = append(page.Items, Item{
			Source: &inlinemedia.Source{
				Provider:    klipyProvider,
				Ref:         g.Slug,
				ProviderRef: g.Slug,
				Kind:        inlinemedia.KindGif,
				URL:         g.URL,
				Width:       g.Width,
				Height:      g.Height,
			},
			Next: klipyNext(pageno, i+1, len(list), more),
		})
	}
	if len(page.Items) > 0 {
		page.Next = page.Items[len(page.Items)-1].Next
	}
	return page, nil
}

// gifs returns the GIFs of a page of KLIPY and whether it has a next page:
// from Redis, or asked again and kept there. A Redis that fails is logged
// and KLIPY asked, as the answer does not need the cache.
func (k *Klipy) gifs(ctx context.Context, endpoint, text, locale string, pageno int, cacheTime int32) ([]klipyGif, bool, error) {
	key := klipyListKeyOf(endpoint, text, locale, pageno)
	v, err := k.cache.GetCtx(ctx, key)
	if err != nil {
		logx.WithContext(ctx).Errorf("inline: klipy cache: %v", err)
	} else if p, ok := decodeKlipyPage(v); ok {
		return p.Gifs, p.More, nil
	}
	got := k.loads.DoChan(key, func() (interface{}, error) {
		// The request is shared by the queries that wait for it: the one
		// that started it going away must not fail the others.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), klipyWait)
		defer cancel()
		p, err := k.ask(lctx, endpoint, text, locale, pageno)
		if err != nil {
			return nil, err
		}
		if v, err := encodeKlipyPage(p); err != nil {
			logx.WithContext(lctx).Errorf("inline: klipy cache: %v", err)
		} else if err := k.cache.SetexCtx(lctx, key, v, int(cacheTime)); err != nil {
			logx.WithContext(lctx).Errorf("inline: klipy cache: %v", err)
		}
		return p, nil
	})
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case r := <-got:
		if r.Err != nil {
			return nil, false, r.Err
		}
		p := r.Val.(klipyPage)
		return p.Gifs, p.More, nil
	}
}

// klipyPage is what is kept of a page of KLIPY.
type klipyPage struct {
	Gifs []klipyGif `json:"g"`
	More bool       `json:"m"`
}

// klipyFile is a format of a size of a GIF.
type klipyFile struct {
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
	Size   int64  `json:"size"`
}

// klipyAnswer is what Search reads of an answer of the search and trending
// endpoints.
type klipyAnswer struct {
	Result bool `json:"result"`
	Data   struct {
		Data []struct {
			Id   json.RawMessage `json:"id"`
			Slug string          `json:"slug"`
			Type string          `json:"type"`
			// File is read per item: a GIF whose files are not of their
			// form is left out, not the page.
			File json.RawMessage `json:"file"`
		} `json:"data"`
		HasNext bool `json:"has_next"`
	} `json:"data"`
}

// ask asks KLIPY for a page. Errors never carry the address asked, which
// has the key and the text.
func (k *Klipy) ask(ctx context.Context, endpoint, text, locale string, pageno int) (klipyPage, error) {
	if until := k.until.Load(); until != 0 && k.now().UnixNano() < until {
		metricKlipyRequests.Inc("limited")
		return klipyPage{}, errKlipyLimited
	}
	select {
	case k.slots <- struct{}{}:
		defer func() { <-k.slots }()
	case <-ctx.Done():
		return klipyPage{}, fmt.Errorf("inline: klipy: waiting for a turn: %w", ctx.Err())
	}

	u := *k.base
	u.Path += "/api/v1/" + k.key + "/gifs/" + endpoint
	u.RawPath = ""
	query := url.Values{
		"page":           {strconv.Itoa(pageno)},
		"per_page":       {strconv.Itoa(klipyPageSize)},
		"content_filter": {klipyContentFilter},
		"format_filter":  {"mp4"},
	}
	if text != "" {
		query.Set("q", text)
	}
	if locale != "" {
		query.Set("locale", locale)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, errors.New("inline: klipy: the request cannot be made")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := k.client.Do(req)
	if err != nil {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, fmt.Errorf("inline: klipy: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Drained a little so the connection is kept.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			wait := retryAfter(resp.Header.Get("Retry-After"), k.now())
			k.until.Store(k.now().Add(wait).UnixNano())
			metricKlipyRequests.Inc("limited")
			return klipyPage{}, fmt.Errorf("inline: klipy: status 429, no request for %s", wait)
		case http.StatusUnauthorized, http.StatusForbidden:
			metricKlipyRequests.Inc("refused")
			return klipyPage{}, fmt.Errorf("inline: klipy: status %d: the key is refused", resp.StatusCode)
		}
		metricKlipyRequests.Inc("error")
		return klipyPage{}, fmt.Errorf("inline: klipy: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, klipyMaxBody+1))
	if err != nil {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, fmt.Errorf("inline: klipy: %w", redactURLError(err))
	}
	if len(body) > klipyMaxBody {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, fmt.Errorf("inline: klipy: an answer over %d bytes", klipyMaxBody)
	}
	var a klipyAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, errors.New("inline: klipy: the answer is not its JSON")
	}
	if !a.Result {
		metricKlipyRequests.Inc("error")
		return klipyPage{}, errors.New("inline: klipy: the answer has result false")
	}
	metricKlipyRequests.Inc("ok")

	p := klipyPage{Gifs: make([]klipyGif, 0, len(a.Data.Data)), More: a.Data.HasNext && pageno < klipyMaxPage}
	seen := make(map[string]bool, len(a.Data.Data))
	for _, item := range a.Data.Data {
		if len(p.Gifs) == klipyPageSize {
			// More than per_page asked for: a page keeps what was asked.
			break
		}
		// Only GIFs: an ad of the Ads API (asked for with its own
		// parameters, never sent here) or another type is not a medium.
		if item.Type != "" && item.Type != "gif" {
			continue
		}
		slug := item.Slug
		if slug == "" {
			// The id is a number in the documentation; a string is taken
			// as well.
			slug = strings.Trim(string(item.Id), `"`)
		}
		if slug == "" || slug == "null" || len(slug) > klipyMaxRef || seen[slug] {
			continue
		}
		var files map[string]map[string]*klipyFile
		if err := json.Unmarshal(item.File, &files); err != nil {
			continue
		}
		g, ok := klipyMp4(files)
		if !ok {
			continue
		}
		seen[slug] = true
		g.Slug = slug
		p.Gifs = append(p.Gifs, g)
	}
	return p, nil
}

// klipyMp4 picks the mp4 of a GIF to import: of the first of klipySizes
// with an http(s) address, sides within what clients take as a GIF
// (webfetch.MaxSide) and a size, when told, within inlinemedia.MaxGifBytes.
func klipyMp4(files map[string]map[string]*klipyFile) (klipyGif, bool) {
	for _, size := range klipySizes {
		f := files[size]["mp4"]
		if f == nil {
			continue
		}
		u, ok := searxngURL(f.URL)
		if !ok || f.Width <= 0 || f.Height <= 0 || f.Width > webfetch.MaxSide || f.Height > webfetch.MaxSide ||
			f.Size < 0 || f.Size > inlinemedia.MaxGifBytes {
			continue
		}
		return klipyGif{URL: u, Width: int32(f.Width), Height: int32(f.Height)}, true
	}
	return klipyGif{}, false
}

// retryAfter is the wait of a Retry-After of seconds (RFC 9110, 10.2.3) or
// of a date, klipyRetryAfter when there is none, at most klipyRetryAfterMax.
func retryAfter(v string, now time.Time) time.Duration {
	wait := klipyRetryAfter
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		wait = time.Duration(s) * time.Second
	} else if t, err := http.ParseTime(v); err == nil && t.After(now) {
		wait = t.Sub(now)
	}
	return min(wait, klipyRetryAfterMax)
}

// klipyText is the text searched for q: the words of an emoji category of
// the GIF tab (klipyWords; Android drops U+FE0F from the emoji of
// gif_search_emojies, MessagesController.java:3098, iOS and tdesktop keep
// what the config says), otherwise the text with its spaces collapsed.
func klipyText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if w, ok := klipyWords[strings.ReplaceAll(text, "️", "")]; ok {
		return w
	}
	return text
}

// klipyLocale is the locale of a request: the region of the app's language
// ("br" of "pt-br"), which is what KLIPY's locale is (ISO 3166-1 alpha-2,
// gifs-search-api.md), or none for a language without one. A language is
// not a country ("en", "uk"), so it is not sent (decision to confirm).
func klipyLocale(lang string) string {
	_, region, ok := strings.Cut(strings.ReplaceAll(strings.ToLower(lang), "_", "-"), "-")
	if !ok || len(region) != 2 {
		return ""
	}
	for _, c := range region {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	return region
}

// parseKlipyOffset reads an offset Search gave: "" for the first page, or
// k<page>.<index> in their decimal forms.
func parseKlipyOffset(s string) (pageno, index int, ok bool) {
	if s == "" {
		return 1, 0, true
	}
	rest, found := strings.CutPrefix(s, "k")
	if !found {
		return 0, 0, false
	}
	p, i, found := strings.Cut(rest, ".")
	if !found {
		return 0, 0, false
	}
	pageno, err1 := strconv.Atoi(p)
	index, err2 := strconv.Atoi(i)
	if err1 != nil || err2 != nil || pageno < 1 || pageno > klipyMaxPage || index < 0 || index >= klipyPageSize ||
		klipyOffset(pageno, index) != s {
		return 0, 0, false
	}
	return pageno, index, true
}

func klipyOffset(pageno, index int) string {
	return "k" + strconv.Itoa(pageno) + "." + strconv.Itoa(index)
}

// klipyNext is the offset after the GIF at index at-1 of a page of KLIPY of
// n GIFs: the next one of the page, or the start of the next page when
// KLIPY has one, or "", which ends the list for every client.
func klipyNext(pageno, at, n int, more bool) string {
	if at < n {
		return klipyOffset(pageno, at)
	}
	if !more || pageno >= klipyMaxPage {
		return ""
	}
	return klipyOffset(pageno+1, 0)
}

func klipyListKeyOf(endpoint, text, locale string, pageno int) string {
	h := sha256.New()
	for _, s := range []string{endpoint, locale, strconv.Itoa(pageno), text} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return klipyListKey + hex.EncodeToString(h.Sum(nil))
}

func encodeKlipyPage(p klipyPage) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	return klipyListTag + string(b), nil
}

// decodeKlipyPage reads a value of encodeKlipyPage; ok is false for a key
// that is not there or a value of another form, which is asked again.
func decodeKlipyPage(v string) (klipyPage, bool) {
	rest, ok := strings.CutPrefix(v, klipyListTag)
	if !ok {
		return klipyPage{}, false
	}
	var p klipyPage
	if err := json.Unmarshal([]byte(rest), &p); err != nil || len(p.Gifs) > klipyPageSize {
		return klipyPage{}, false
	}
	for _, g := range p.Gifs {
		if _, ok := searxngURL(g.URL); !ok || g.Slug == "" || g.Width <= 0 || g.Height <= 0 {
			return klipyPage{}, false
		}
	}
	if p.Gifs == nil {
		p.Gifs = []klipyGif{}
	}
	return p, true
}
