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

package langpack

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Invoker sends a request to Telegram and returns its serialized result;
// an rpc_error comes back as *mtclient.RPCError.
type Invoker interface {
	InvokeRaw(ctx context.Context, req mtproto.TLObject) ([]byte, error)
}

// Fetcher reads language packs from Telegram's langpack.* methods, which
// need no login (core.telegram.org/method/langpack.getLangPack: "can be
// invoked over an unauthenticated connection").
type Fetcher struct {
	Invoker Invoker
	// Delay is the pause between requests, to stay under Telegram's limits.
	Delay time.Duration
	// MaxFloodWait is the longest FLOOD_WAIT_X the fetcher waits out before
	// repeating a request; a longer one fails the fetch.
	MaxFloodWait time.Duration
	// RequestTimeout bounds one request (no bound when 0).
	RequestTimeout time.Duration

	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// Languages returns the languages of pack in Telegram's order
// (langpack.getLanguages).
func (f *Fetcher) Languages(ctx context.Context, pack string) ([]Language, error) {
	raw, err := f.invoke(ctx, &mtproto.TLLangpackGetLanguages{
		Constructor: mtproto.CRC32_langpack_getLanguages_42c6978f,
		LangPack:    pack,
	})
	if err != nil {
		return nil, fmt.Errorf("langpack.getLanguages(%s): %w", pack, err)
	}
	return ParseLanguages(pack, raw)
}

// Pack returns the pack in lang (langpack.getLangPack) as a snapshot.
func (f *Fetcher) Pack(ctx context.Context, pack string, lang Language) (*Snapshot, error) {
	raw, err := f.invoke(ctx, &mtproto.TLLangpackGetLangPack{
		Constructor: mtproto.CRC32_langpack_getLangPack_f2f2330a,
		LangPack:    pack,
		LangCode:    lang.LangCode,
	})
	if err != nil {
		return nil, fmt.Errorf("langpack.getLangPack(%s, %s): %w", pack, lang.LangCode, err)
	}
	snap, err := ParseLangPack(pack, lang, raw)
	if err != nil {
		return nil, err
	}
	snap.FetchedAt = f.clock().Unix()
	return snap, nil
}

// FetchPack fetches pack in each of langs (every language of the pack when
// langs is empty) and hands every snapshot to save as soon as it is read.
func (f *Fetcher) FetchPack(ctx context.Context, pack string, langs []string, save func(*Snapshot) error) error {
	all, err := f.Languages(ctx, pack)
	if err != nil {
		return err
	}
	byCode := make(map[string]Language, len(all))
	for _, l := range all {
		byCode[l.LangCode] = l
	}
	todo := all
	if len(langs) > 0 {
		todo = nil
		for _, langCode := range langs {
			l, ok := byCode[CanonicalLangCode(langCode)]
			if !ok {
				return fmt.Errorf("langpack: Telegram's pack %s has no language %q", pack, langCode)
			}
			todo = append(todo, l)
		}
	}
	for _, l := range todo {
		if err := f.pause(ctx, f.Delay); err != nil {
			return err
		}
		snap, err := f.Pack(ctx, pack, l)
		if err != nil {
			return err
		}
		if err = save(snap); err != nil {
			return err
		}
	}
	return nil
}

// invoke sends req, waiting out a FLOOD_WAIT_X of up to MaxFloodWait.
func (f *Fetcher) invoke(ctx context.Context, req mtproto.TLObject) ([]byte, error) {
	for {
		raw, err := f.invokeOnce(ctx, req)
		wait, flood := floodWait(err)
		if !flood || wait > f.MaxFloodWait {
			return raw, err
		}
		if err = f.pause(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (f *Fetcher) invokeOnce(ctx context.Context, req mtproto.TLObject) ([]byte, error) {
	if f.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.RequestTimeout)
		defer cancel()
	}
	return f.Invoker.InvokeRaw(ctx, req)
}

// floodWait returns the wait of a FLOOD_WAIT_X error.
func floodWait(err error) (time.Duration, bool) {
	var e *mtclient.RPCError
	if !errors.As(err, &e) || !strings.HasPrefix(e.Message, "FLOOD_WAIT_") {
		return 0, false
	}
	n, convErr := strconv.Atoi(strings.TrimPrefix(e.Message, "FLOOD_WAIT_"))
	if convErr != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

func (f *Fetcher) pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	if f.sleep != nil {
		return f.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (f *Fetcher) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// ParseLanguages decodes the result of langpack.getLanguages for pack.
func ParseLanguages(pack string, raw []byte) ([]Language, error) {
	objs, err := mtclient.DecodeVector(raw)
	if err != nil {
		return nil, fmt.Errorf("langpack.getLanguages(%s): %w", pack, err)
	}
	out := make([]Language, 0, len(objs))
	for i, o := range objs {
		l, ok := o.(interface {
			To_LangPackLanguage() *mtproto.LangPackLanguage
		})
		if !ok {
			return nil, fmt.Errorf("langpack.getLanguages(%s): element %d is %T", pack, i, o)
		}
		lang := LanguageFromTL(pack, l.To_LangPackLanguage())
		lang.SortOrder = int32(i)
		if lang.LangCode == "" || lang.PluralCode == "" {
			return nil, fmt.Errorf("langpack.getLanguages(%s): element %d without lang_code or plural_code", pack, i)
		}
		out = append(out, lang)
	}
	return out, nil
}

// ParseLangPack decodes the result of langpack.getLangPack for pack in
// lang. Telegram answers with the whole pack (from_version 0).
func ParseLangPack(pack string, lang Language, raw []byte) (*Snapshot, error) {
	d := mtproto.NewDecodeBuf(raw)
	o := d.Object()
	if err := d.GetError(); err != nil {
		return nil, fmt.Errorf("langpack.getLangPack(%s, %s): %w", pack, lang.LangCode, err)
	}
	diff, ok := o.(interface {
		To_LangPackDifference() *mtproto.LangPackDifference
	})
	if !ok {
		return nil, fmt.Errorf("langpack.getLangPack(%s, %s): got %T", pack, lang.LangCode, o)
	}
	ld := diff.To_LangPackDifference()
	if CanonicalLangCode(ld.GetLangCode()) != lang.LangCode {
		return nil, fmt.Errorf("langpack.getLangPack(%s, %s): answered for %q", pack, lang.LangCode, ld.GetLangCode())
	}
	if ld.GetFromVersion() != 0 {
		return nil, fmt.Errorf("langpack.getLangPack(%s, %s): from_version %d, not the whole pack", pack, lang.LangCode, ld.GetFromVersion())
	}
	snap := &Snapshot{
		LangPack: pack,
		LangCode: lang.LangCode,
		Version:  ld.GetVersion(),
		Language: lang,
		Strings:  make([]String, 0, len(ld.GetStrings())),
	}
	for _, o := range ld.GetStrings() {
		s, deleted, err := StringFromTL(o)
		if err != nil {
			return nil, fmt.Errorf("langpack.getLangPack(%s, %s): %w", pack, lang.LangCode, err)
		}
		if deleted {
			// A whole pack has nothing to delete.
			continue
		}
		snap.Strings = append(snap.Strings, s)
	}
	if err := snap.Validate(); err != nil {
		return nil, err
	}
	return snap, nil
}
