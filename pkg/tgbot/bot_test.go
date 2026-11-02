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

package tgbot

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// fakeTelegram answers as Telegram for a bot whose home is DC 4, with a
// file on DC 5 that DC 4 sends away with FILE_MIGRATE_5.
type fakeTelegram struct {
	mu       sync.Mutex
	dials    []int32
	imported map[int32]bool // DCs the authorization was carried to
	flood    int            // FLOOD_WAIT_1 answers left for getStickerSet
	timeouts int            // -503 Timeout answers left for upload.getFile
	file     []byte
}

type fakeConn struct {
	t  *fakeTelegram
	dc int32
}

func encode(t *testing.T, o mtproto.TLObject) []byte {
	x := mtproto.NewEncodeBuf(512)
	if err := o.Encode(x, mtclient.DefaultLayer); err != nil {
		t.Fatal(err)
	}
	return x.GetBuf()
}

var testT *testing.T

func (c *fakeConn) InvokeRaw(_ context.Context, req mtproto.TLObject) ([]byte, error) {
	c.t.mu.Lock()
	defer c.t.mu.Unlock()
	switch r := req.(type) {
	case *mtproto.TLAuthImportBotAuthorization:
		if r.BotAuthToken != "123:abc" || r.ApiHash != "hash" {
			return nil, &mtclient.RPCError{Code: 401, Message: "ACCESS_TOKEN_INVALID"}
		}
		if c.dc != 4 {
			return nil, &mtclient.RPCError{Code: 303, Message: "USER_MIGRATE_4"}
		}
		return encode(testT, mtproto.MakeTLBoolTrue(nil)), nil
	case *mtproto.TLHelpGetConfig:
		return encode(testT, mtproto.MakeTLConfig(&mtproto.Config{TestMode: mtproto.BoolFalse, BlockedMode: false, DcOptions: []*mtproto.DcOption{
			mtproto.MakeTLDcOption(&mtproto.DcOption{Id: 4, IpAddress: "10.0.0.4", Port: 443}).To_DcOption(),
			mtproto.MakeTLDcOption(&mtproto.DcOption{Id: 5, IpAddress: "10.0.0.5", Port: 443}).To_DcOption(),
			mtproto.MakeTLDcOption(&mtproto.DcOption{Id: 5, IpAddress: "2001:db8::5", Port: 443, Ipv6: true}).To_DcOption(),
		}})), nil
	case *mtproto.TLMessagesGetStickerSet:
		if c.dc != 4 {
			return nil, errors.New("not logged in")
		}
		if c.t.flood > 0 {
			c.t.flood--
			return nil, &mtclient.RPCError{Code: 420, Message: "FLOOD_WAIT_1"}
		}
		if r.Stickerset.GetShortName() != "Free420" {
			return nil, &mtclient.RPCError{Code: 406, Message: "STICKERSET_INVALID"}
		}
		return encode(testT, mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 9, ShortName: "Free420", Title: "Free"}).To_StickerSet(),
		})), nil
	case *mtproto.TLAuthExportAuthorization:
		return encode(testT, mtproto.MakeTLAuthExportedAuthorization(&mtproto.Auth_ExportedAuthorization{Id: 1, Bytes: []byte{7}})), nil
	case *mtproto.TLAuthImportAuthorization:
		c.t.imported[c.dc] = true
		return encode(testT, mtproto.MakeTLBoolTrue(nil)), nil
	case *mtproto.TLUploadGetFile:
		if c.dc == 4 {
			return nil, &mtclient.RPCError{Code: 303, Message: "FILE_MIGRATE_5"}
		}
		if !c.t.imported[c.dc] {
			return nil, &mtclient.RPCError{Code: 401, Message: "AUTH_KEY_UNREGISTERED"}
		}
		if c.t.timeouts > 0 {
			c.t.timeouts--
			return nil, &mtclient.RPCError{Code: -503, Message: "Timeout"}
		}
		end := min(int(r.Offset_INT64)+partSize, len(c.t.file))
		return encode(testT, mtproto.MakeTLUploadFile(&mtproto.Upload_File{
			Type:  mtproto.MakeTLStorageFilePartial(nil).To_Storage_FileType(),
			Bytes: c.t.file[r.Offset_INT64:end],
		})), nil
	}
	return nil, errors.New("unexpected request")
}

func (c *fakeConn) Close() error { return nil }

func newTestBot(t *testing.T, ft *fakeTelegram) *Bot {
	t.Helper()
	testT = t
	b, err := New(Config{APIID: 1, APIHash: "hash", Token: "123:abc", MaxWait: 2 * time.Second,
		DialFunc: func(_ context.Context, cfg mtclient.Config, dc int32) (Conn, error) {
			ft.mu.Lock()
			ft.dials = append(ft.dials, dc)
			ft.mu.Unlock()
			return &fakeConn{t: ft, dc: dc}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStickerSetAndFile(t *testing.T) {
	ft := &fakeTelegram{imported: map[int32]bool{}, flood: 1, timeouts: 1, file: bytes.Repeat([]byte("s"), partSize+10)}
	b := newTestBot(t, ft)
	ctx := context.Background()

	set, err := b.StickerSet(ctx, mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "Free420"}).To_InputStickerSet())
	if err != nil || set.GetSet().GetShortName() != "Free420" {
		t.Fatalf("set %v, %v", set, err)
	}
	if b.home != 4 || b.addrs[5] != "10.0.0.5:443" {
		t.Fatalf("home %d, addresses %v", b.home, b.addrs)
	}
	if _, err := b.StickerSet(ctx, mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "none"}).To_InputStickerSet()); !errors.Is(err, mtproto.ErrStickersetInvalid) {
		t.Fatalf("an unknown set: %v", err)
	}

	doc := mtproto.MakeTLDocument(&mtproto.Document{Id: 1, AccessHash: 2, DcId: 4}).To_Document()
	f, err := b.File(ctx, doc, "", 4*partSize)
	if err != nil || !bytes.Equal(f, ft.file) {
		t.Fatalf("file of %d bytes, %v", len(f), err)
	}
	if !ft.imported[5] {
		t.Fatal("the authorization was not carried to DC 5")
	}
	if _, err := b.File(ctx, doc, "", partSize); err == nil {
		t.Fatal("a file over the limit")
	}
}

// Server errors are sent again a few times, then given up.
func TestServerErrors(t *testing.T) {
	ft := &fakeTelegram{imported: map[int32]bool{5: true}, timeouts: maxServerFailures + 1, file: []byte("s")}
	b := newTestBot(t, ft)
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	doc := mtproto.MakeTLDocument(&mtproto.Document{Id: 1, AccessHash: 2, DcId: 5}).To_Document()
	if _, err := b.File(ctx, doc, "", partSize); err == nil || ctx.Err() == nil {
		t.Fatalf("timeouts past the deadline: %v", err)
	}
	if ft.timeouts != maxServerFailures-1 {
		t.Fatalf("%d timeouts left, want %d", ft.timeouts, maxServerFailures-1)
	}
}

func TestFloodWaitTooLong(t *testing.T) {
	ft := &fakeTelegram{imported: map[int32]bool{}, flood: 1}
	b := newTestBot(t, ft)
	b.cfg.MaxWait = 0
	b.cfg.MaxWait = time.Millisecond
	if _, err := b.StickerSet(context.Background(), mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "Free420"}).To_InputStickerSet()); err == nil {
		t.Fatal("a FLOOD_WAIT over the limit was waited out")
	}
}

func TestNotConfigured(t *testing.T) {
	if _, err := New(Config{APIID: 1, APIHash: "h"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("%v", err)
	}
}
