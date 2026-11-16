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
	"time"

	"github.com/teamgram/teamgram-server/pkg/inlinemedia"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/singleflight"
)

// The pictures of @pic (task 099, part E, ADR 0062).
const (
	// searxngProvider names the provider of the media it finds
	// (inline_media.provider).
	searxngProvider = "searxng"
	// searxngPageSize is the pictures of a page.
	searxngPageSize = 12
	// searxngCacheTime is the cache_time of every page, and how long a
	// page of SearXNG is kept in Redis.
	searxngCacheTime = 3600
	// searxngMaxPage is the last page of SearXNG asked for: the engines
	// give little past it, and a query does not page through the web.
	searxngMaxPage = 5
	// searxngListMax bounds the pictures kept of a page of SearXNG, which
	// has a few hundred of them from all its engines together: ten pages
	// of @pic, then the next page of SearXNG.
	searxngListMax = 120
	// searxngWait bounds a request to SearXNG, the wait for its turn
	// included; SearXNG gives its engines 3 s
	// (teamgramd/deploy/searxng/settings.yml).
	searxngWait = 6 * time.Second
	// searxngConcurrent bounds the requests to SearXNG at once from the
	// process.
	searxngConcurrent = 4
	// searxngMaxBody bounds the answer read: a page of a few hundred
	// pictures is about 400 KB (measured).
	searxngMaxBody = 4 << 20
	// searxngMaxURL bounds the address of a picture.
	searxngMaxURL = 2048
	// searxngListKey prefixes the Redis key of a page of SearXNG; the
	// rest is a hash of the language, the text and the page, so that the
	// key does not carry the text. The value is searxngListTag and the
	// JSON of the pictures.
	searxngListKey = "inline_searxng_"
	searxngListTag = "v1:"
)

// Searxng is the provider of @pic: pictures found by text with a SearXNG
// instance (docs.searxng.org/dev/search_api.html), which the bots service
// imports into dfs as photos. Android's picture search sends a found photo
// as a photo of its own and only ever asks with a text
// (PhotoPickerActivity.java:1636-1740), as @pic in a chat does.
type Searxng struct {
	base   *url.URL
	client *http.Client
	cache  Cache
	slots  chan struct{}
	loads  singleflight.Group
}

// NewSearxng returns the provider on the SearXNG at base (SEARXNG_URL,
// http://searxng:8080) and cache. base is an address on the network of the
// services, which bff calls as it is: the pictures it names are fetched by
// the importer, from public addresses only (pkg/webfetch).
func NewSearxng(base string, cache Cache) (*Searxng, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("inline: SearxngURL is not an http(s) address")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return &Searxng{
		base: u,
		client: &http.Client{
			// SearXNG answers the search itself: a redirect is an external
			// bang or a misconfigured address, never followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		cache: cache,
		slots: make(chan struct{}, searxngConcurrent),
	}, nil
}

// searxngPicture is a picture of a page of SearXNG: the address of the
// image, and of its thumbnail at the engine, the fallback of the import.
type searxngPicture struct {
	Src   string `json:"s"`
	Thumb string `json:"t,omitempty"`
}

// Search answers q. An offset is p<page>.<index>: the page of SearXNG and
// the index in its pictures of the first one of the page; every page of
// SearXNG is kept in Redis for the cache time, so the pages that follow
// one another come from one answer of the engines. An empty text, or an
// offset this provider did not give, is an empty page.
func (s *Searxng) Search(ctx context.Context, q Query) (Page, error) {
	page := Page{Items: []Item{}, CacheTime: searxngCacheTime}
	text := searxngText(q.Text)
	if text == "" {
		return page, nil
	}
	pageno, from, ok := parseSearxngOffset(q.Offset)
	if !ok {
		return page, nil
	}
	lang := searxngLang(q.Lang)
	var list []searxngPicture
	for {
		var err error
		if list, err = s.pictures(ctx, text, lang, pageno); err != nil {
			return Page{}, err
		}
		// A page kept in Redis may have ended, and been asked again with
		// fewer pictures, since the offset was given: the next page goes
		// on in its place, as an empty page would end the list in Android
		// (PhotoPickerActivity.java:1740).
		if from < len(list) || len(list) == 0 || pageno >= searxngMaxPage {
			break
		}
		pageno, from = pageno+1, 0
	}
	for i := from; i < len(list) && len(page.Items) < searxngPageSize; i++ {
		p := list[i]
		page.Items = append(page.Items, Item{
			Source: &inlinemedia.Source{
				Provider:    searxngProvider,
				Ref:         p.Src,
				Kind:        inlinemedia.KindPhoto,
				URL:         p.Src,
				FallbackURL: p.Thumb,
			},
			Next: searxngNext(pageno, i+1, len(list)),
		})
	}
	if len(page.Items) > 0 {
		page.Next = page.Items[len(page.Items)-1].Next
	}
	return page, nil
}

// pictures returns the pictures of a page of SearXNG: from Redis, or asked
// again and kept there. A Redis that fails is logged and SearXNG asked, as
// the answer does not need the cache.
func (s *Searxng) pictures(ctx context.Context, text, lang string, pageno int) ([]searxngPicture, error) {
	key := searxngListKeyOf(text, lang, pageno)
	v, err := s.cache.GetCtx(ctx, key)
	if err != nil {
		logx.WithContext(ctx).Errorf("inline: searxng cache: %v", err)
	} else if list, ok := decodeSearxngList(v); ok {
		return list, nil
	}
	got := s.loads.DoChan(key, func() (interface{}, error) {
		// The request is shared by the queries that wait for it: the one
		// that started it going away must not fail the others.
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), searxngWait)
		defer cancel()
		list, err := s.ask(lctx, text, lang, pageno)
		if err != nil {
			return nil, err
		}
		if v, err := encodeSearxngList(list); err != nil {
			logx.WithContext(lctx).Errorf("inline: searxng cache: %v", err)
		} else if err := s.cache.SetexCtx(lctx, key, v, searxngCacheTime); err != nil {
			logx.WithContext(lctx).Errorf("inline: searxng cache: %v", err)
		}
		return list, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-got:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]searxngPicture), nil
	}
}

// searxngAnswer is what Search reads of an answer of SearXNG's JSON format
// (searx/webutils.py get_json_response): the results, and the engines that
// did not answer, each a pair of its name and the error.
type searxngAnswer struct {
	Results []struct {
		ImgSrc       string `json:"img_src"`
		ThumbnailSrc string `json:"thumbnail_src"`
	} `json:"results"`
	UnresponsiveEngines [][]string `json:"unresponsive_engines"`
}

// ask asks SearXNG for a page of pictures. A page with no picture while
// engines failed is an error, not the end of the list: the engines
// blocked the address, or none answered in time. Errors never carry the
// address asked, which has the text.
func (s *Searxng) ask(ctx context.Context, text, lang string, pageno int) ([]searxngPicture, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, fmt.Errorf("inline: searxng: waiting for a turn: %w", ctx.Err())
	}

	u := *s.base
	u.Path += "/search"
	u.RawQuery = url.Values{
		"q":          {text},
		"categories": {"images"},
		"format":     {"json"},
		"pageno":     {strconv.Itoa(pageno)},
		// Moderate, as the settings of the instance.
		"safesearch": {"1"},
		"language":   {lang},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("inline: searxng: the request cannot be made")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inline: searxng: %w", redactURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Drained a little so the connection is kept.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode == http.StatusForbidden {
			return nil, errors.New("inline: searxng: status 403: the json format is not in search.formats")
		}
		return nil, fmt.Errorf("inline: searxng: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, searxngMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("inline: searxng: %w", redactURLError(err))
	}
	if len(body) > searxngMaxBody {
		return nil, fmt.Errorf("inline: searxng: an answer over %d bytes", searxngMaxBody)
	}
	var a searxngAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, errors.New("inline: searxng: the answer is not its JSON")
	}

	list := make([]searxngPicture, 0, min(len(a.Results), searxngListMax))
	seen := make(map[string]bool, len(a.Results))
	for _, r := range a.Results {
		if len(list) == searxngListMax {
			break
		}
		// Only a result with the image itself is a picture; the thumbnail
		// is the fallback of its import (inlinemedia.Source.FallbackURL).
		src, ok := searxngURL(r.ImgSrc)
		if !ok || seen[src] {
			continue
		}
		seen[src] = true
		thumb, _ := searxngURL(r.ThumbnailSrc)
		list = append(list, searxngPicture{Src: src, Thumb: thumb})
	}
	if len(list) == 0 && len(a.UnresponsiveEngines) > 0 {
		names := make([]string, 0, len(a.UnresponsiveEngines))
		for _, e := range a.UnresponsiveEngines {
			if len(e) > 0 {
				names = append(names, strings.Join(e, ": "))
			}
		}
		return nil, fmt.Errorf("inline: searxng: no picture, the engines failed: %s", strings.Join(names, "; "))
	}
	return list, nil
}

// searxngURL is raw when it is an absolute http(s) address of a host, of
// at most searxngMaxURL bytes.
func searxngURL(raw string) (string, bool) {
	if raw == "" || len(raw) > searxngMaxURL {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	return raw, true
}

// searxngText is the text searched for q: SearXNG reads a word that starts
// with "!" as an engine or a category ("!!" sends to another site), with
// ":" as a language and with "<" as a time limit
// (searx/query.py RawTextQuery), where for the user it is text. Those
// characters are dropped from the start of every word, and a word of
// nothing else with them, as search engines do not search punctuation.
func searxngText(text string) string {
	words := strings.Fields(text)
	kept := words[:0]
	for _, w := range words {
		if w = strings.TrimLeft(w, "!:<"); w != "" {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// searxngLang is the language of the search: the primary subtag of the
// app's language ("pt" of "pt-br"), which SearXNG takes as a language code
// (searx/webutils.py VALID_LANGUAGE_CODE, ^[a-z]{2,3}(-[a-zA-Z]{2})?$; it
// answers 400 to another form, such as "zh-hans"), or "all" when unknown.
func searxngLang(lang string) string {
	primary, _, _ := strings.Cut(strings.ToLower(lang), "-")
	primary, _, _ = strings.Cut(primary, "_")
	if l := len(primary); l < 2 || l > 3 {
		return "all"
	}
	for _, c := range primary {
		if c < 'a' || c > 'z' {
			return "all"
		}
	}
	return primary
}

// parseSearxngOffset reads an offset Search gave: "" for the first page,
// or p<page>.<index> in their decimal forms.
func parseSearxngOffset(s string) (pageno, index int, ok bool) {
	if s == "" {
		return 1, 0, true
	}
	p, i, found := strings.Cut(strings.TrimPrefix(s, "p"), ".")
	if !found || !strings.HasPrefix(s, "p") {
		return 0, 0, false
	}
	pageno, err1 := strconv.Atoi(p)
	index, err2 := strconv.Atoi(i)
	if err1 != nil || err2 != nil || pageno < 1 || pageno > searxngMaxPage || index < 0 || index >= searxngListMax ||
		searxngOffset(pageno, index) != s {
		return 0, 0, false
	}
	return pageno, index, true
}

func searxngOffset(pageno, index int) string {
	return "p" + strconv.Itoa(pageno) + "." + strconv.Itoa(index)
}

// searxngNext is the offset after the picture at index at-1 of a page of
// SearXNG of n pictures: the next one of the page, or the start of the next
// page, or "" after the last page, which ends the list for every client.
func searxngNext(pageno, at, n int) string {
	if at < n {
		return searxngOffset(pageno, at)
	}
	if pageno >= searxngMaxPage {
		return ""
	}
	return searxngOffset(pageno+1, 0)
}

func searxngListKeyOf(text, lang string, pageno int) string {
	h := sha256.New()
	h.Write([]byte(lang))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(pageno)))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return searxngListKey + hex.EncodeToString(h.Sum(nil))
}

func encodeSearxngList(list []searxngPicture) (string, error) {
	b, err := json.Marshal(list)
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	return searxngListTag + string(b), nil
}

// decodeSearxngList reads a value of encodeSearxngList; ok is false for a
// key that is not there or a value of another form, which is asked again.
func decodeSearxngList(v string) ([]searxngPicture, bool) {
	rest, ok := strings.CutPrefix(v, searxngListTag)
	if !ok {
		return nil, false
	}
	var list []searxngPicture
	if err := json.Unmarshal([]byte(rest), &list); err != nil {
		return nil, false
	}
	for _, p := range list {
		if _, ok := searxngURL(p.Src); !ok {
			return nil, false
		}
	}
	if list == nil {
		list = []searxngPicture{}
	}
	return list, true
}

// redactURLError drops the address from an error of net/http
// (*url.Error), which names it with the text of the query.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
