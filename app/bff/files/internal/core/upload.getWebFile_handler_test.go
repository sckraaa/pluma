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

package core

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/files/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/webfetch"
	"github.com/teamgram/teamgram-server/pkg/webfile"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func webFileCore(t *testing.T, c webfile.Config) *FilesCore {
	t.Helper()
	s, err := webfile.New(c, webfetch.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	return &FilesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{WebFiles: s},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 7, LangCode: "en"},
	}
}

// upload.getWebFile answers with the errors of its documentation instead of
// the enterprise stub: a map is LOCATION_INVALID until a source serves it
// (task 100), an album cover WEBFILE_NOT_AVAILABLE, an address the server
// did not sign LOCATION_INVALID, and the range is checked.
func TestUploadGetWebFile(t *testing.T) {
	const key = "an-example-key-of-32-bytes-or-more"
	c := webFileCore(t, webfile.Config{HMACKey: key, CacheDir: t.TempDir(), CacheBytes: 1 << 20})
	for _, tc := range []struct {
		name string
		loc  *mtproto.InputWebFileLocation
		lim  int32
		want error
	}{
		{"map", mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{
			GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: 52.5, Long: 13.4}).To_InputGeoPoint(),
			W:        100, H: 100, Zoom: 15, Scale: 2,
		}).To_InputWebFileLocation(), 4096, mtproto.ErrLocationInvalid},
		{"album cover", mtproto.MakeTLInputWebFileAudioAlbumThumbLocation(&mtproto.InputWebFileLocation{}).To_InputWebFileLocation(), 4096, ErrWebfileNotAvailable},
		{"unsigned address", mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{Url: "https://bot.example/a.jpg", AccessHash: 1}).To_InputWebFileLocation(), 4096, mtproto.ErrLocationInvalid},
		{"internal address", mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{
			Url: "http://10.0.0.1/a.jpg", AccessHash: webfile.Sign([]byte(key), "http://10.0.0.1/a.jpg"),
		}).To_InputWebFileLocation(), 4096, ErrWebfileNotAvailable},
		{"limit", mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{Url: "https://bot.example/a.jpg", AccessHash: 1}).To_InputWebFileLocation(), 2 << 20, mtproto.ErrLimitInvalid},
	} {
		_, err := c.UploadGetWebFile(&mtproto.TLUploadGetWebFile{Location: tc.loc, Offset: 0, Limit: tc.lim})
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

// styleRenderer draws blank maps and records the styles it is asked for.
type styleRenderer struct{ styles []string }

func (r *styleRenderer) Render(_ context.Context, style string, p staticmap.Params) ([]byte, error) {
	r.styles = append(r.styles, style)
	var b bytes.Buffer
	err := png.Encode(&b, image.NewGray(image.Rect(0, 0, p.PixelWidth(), p.PixelHeight())))
	return b.Bytes(), err
}

// With the maps on, upload.getWebFile serves a map for the session that
// asks: the labels in the language of its connection.
func TestUploadGetWebFileMap(t *testing.T) {
	mr := miniredis.RunT(t)
	store := kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
	r := &styleRenderer{}
	maps, err := staticmap.NewService(staticmap.Config{URL: "http://maps:8080"}, store, r)
	if err != nil {
		t.Fatal(err)
	}
	c := webFileCore(t, webfile.Config{})
	c.svcCtx.WebFiles.Register(mtproto.Predicate_inputWebFileGeoPointLocation, svc.NewGeoSource(maps))
	c.MD.LangCode = "ru"

	f, err := c.UploadGetWebFile(&mtproto.TLUploadGetWebFile{
		Location: mtproto.MakeTLInputWebFileGeoPointLocation(&mtproto.InputWebFileLocation{
			GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: 52.5, Long: 13.4}).To_InputGeoPoint(),
			W:        100, H: 100, Zoom: 15, Scale: 2,
		}).To_InputWebFileLocation(),
		Offset: 0,
		Limit:  1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(f.GetBytes()))
	if err != nil || cfg.Width != 200 || cfg.Height != 200 || f.GetMimeType() != "image/png" || int(f.GetSize2()) != len(f.GetBytes()) {
		t.Errorf("a map of %d×%d (%v), type %q, size %d of %d bytes", cfg.Width, cfg.Height, err, f.GetMimeType(), f.GetSize2(), len(f.GetBytes()))
	}
	if len(r.styles) != 1 || r.styles[0] != "ru" {
		t.Errorf("styles %q, want [ru]", r.styles)
	}
}
