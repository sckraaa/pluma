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
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"

	"github.com/teamgram/teamgram-server/app/bff/files/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/files/internal/svc"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// fakeDfs answers dfs.downloadFile with the bytes asked for; the other
// methods are not called.
type fakeDfs struct {
	dfs_client.DfsClient
	calls int
}

func (d *fakeDfs) DfsDownloadFile(_ context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error) {
	d.calls++
	return mtproto.MakeTLUploadFile(&mtproto.Upload_File{
		Type:  mtproto.MakeTLStorageFilePartial(nil).To_Storage_FileType(),
		Bytes: make([]byte, in.GetLimit()),
	}).To_Upload_File(), nil
}

// noSizes is a Sizer for files that all have rows already.
type noSizes struct{}

func (noSizes) FileSize(context.Context, *mtproto.InputFileLocation) (int64, error) {
	return 0, cdn.ErrNoFile
}

const cdnTestDc = 201

// readyCdn returns a Home whose delivery is healthy and for whose origin
// document id of size bytes is prepared, and the secrets of that file.
func readyCdn(t *testing.T, id, size int64) (*cdn.Home, cdn.Secrets) {
	t.Helper()
	ctx := context.Background()
	store := cdn.NewMemStore()
	now := time.Now().UnixMilli()
	f, err := store.Ensure(ctx, &cdn.File{Key: cdn.FileKey{Kind: cdn.KindDocument, Id: id}, Size: size, Eligible: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	sec, err := cdn.NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if f, err = store.SetKey(ctx, f.RowId, sec); err != nil {
		t.Fatal(err)
	}
	if err = store.Enqueue(ctx, f.RowId, cdn.TargetHomeOrigin, 0, now); err != nil {
		t.Fatal(err)
	}
	c, err := store.Claim(ctx, cdn.TargetHomeOrigin, time.Minute, now)
	if err != nil || c == nil {
		t.Fatalf("claim: %v, %v", c, err)
	}
	plain := make([]byte, size)
	if _, err = rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	h := cdn.NewHasher()
	_, _ = h.Write(plain)
	if ok, err := store.MarkReady(ctx, c, h.Sum(), now); !ok || err != nil {
		t.Fatalf("ready: %v, %v", ok, err)
	}
	if err = store.PutDeliveryHealth(ctx, &cdn.DeliveryHealth{DcId: cdnTestDc, HealthyUntil: now + time.Minute.Milliseconds(), UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	set, err := cdn.Config{
		Deliveries: []cdn.DeliveryConfig{{Id: cdnTestDc, Name: "gcore", BaseURL: "https://cdn.example.com", Key: randomHex(t, 16)}},
		Origin:     cdn.OriginConfig{AuthValue: randomHex(t, 32)},
	}.Parse()
	if err != nil {
		t.Fatal(err)
	}
	home, err := cdn.NewHome(store, set, noSizes{})
	if err != nil {
		t.Fatal(err)
	}
	return home, f.Secrets
}

// randomHex is n throwaway random bytes in hex.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func cdnCore(home *cdn.Home, d *fakeDfs) *FilesCore {
	ctx := context.Background()
	return &FilesCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: &dao.Dao{DfsClient: d}, CDN: home},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: 7},
	}
}

func docLocation(id int64) *mtproto.InputFileLocation {
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: id, AccessHash: 1, FileReference: []byte{}}).To_InputFileLocation()
}

func sameError(err, want error) bool {
	return status.Code(err) == status.Code(want) && status.Convert(err).Message() == status.Convert(want).Message()
}

// TestGetFileRedirect: a client that supports the CDN is redirected to a
// prepared file; one that does not, or asks for precise ranges, or a server
// with the CDN off, is served from dfs.
func TestGetFileRedirect(t *testing.T) {
	home, sec := readyCdn(t, 10, 3<<20+5)
	req := func(cdnSupported, precise bool) *mtproto.TLUploadGetFile {
		return &mtproto.TLUploadGetFile{CdnSupported: cdnSupported, Precise: precise, Location: docLocation(10), Offset_INT64: 1 << 20, Limit: 1 << 20}
	}

	d := &fakeDfs{}
	res, err := cdnCore(home, d).UploadGetFile(req(true, false))
	if err != nil {
		t.Fatal(err)
	}
	if res.GetPredicateName() != mtproto.Predicate_upload_fileCdnRedirect || d.calls != 0 {
		t.Fatalf("%s after %d dfs calls, want a redirect", res.GetPredicateName(), d.calls)
	}
	id, _, _, err := cdn.ParseRedirectToken(res.GetFileToken())
	if err != nil || res.GetDcId() != cdnTestDc || string(id) != string(sec.Token) ||
		len(res.GetEncryptionKey()) != 32 || len(res.GetEncryptionIv()) != 16 ||
		len(res.GetFileHashes()) != 17 || res.GetFileHashes()[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("redirect dc %d, %d hashes", res.GetDcId(), len(res.GetFileHashes()))
	}

	for name, c := range map[string]*FilesCore{
		"no cdn_supported": cdnCore(home, d),
		"precise":          cdnCore(home, d),
		"cdn off":          cdnCore(nil, d),
	} {
		in := req(name != "no cdn_supported", name == "precise")
		before := d.calls
		res, err := c.UploadGetFile(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.GetPredicateName() != mtproto.Predicate_upload_file || d.calls != before+1 {
			t.Fatalf("%s: %s, want upload.file from dfs", name, res.GetPredicateName())
		}
	}
}

// TestCdnMethods: upload.getCdnFile at home, upload.getCdnFileHashes,
// upload.reuploadCdnFile and help.getCdnConfig, with the CDN on and off.
func TestCdnMethods(t *testing.T) {
	home, sec := readyCdn(t, 11, 2<<20)
	on, off := cdnCore(home, &fakeDfs{}), cdnCore(nil, &fakeDfs{})

	for name, c := range map[string]*FilesCore{"on": on, "off": off} {
		if _, err := c.UploadGetCdnFile(&mtproto.TLUploadGetCdnFile{FileToken: sec.Token, Limit: 4096}); !sameError(err, mtproto.ErrFileTokenInvalid) {
			t.Errorf("%s: upload.getCdnFile: %v", name, err)
		}
	}

	token, err := cdn.EncodeRedirectToken(sec.Token, time.Now().Unix()+3600, "https://cdn.example.com"+cdn.ObjectPath(sec.Token))
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := on.UploadGetCdnFileHashes(&mtproto.TLUploadGetCdnFileHashes{FileToken: token, Offset_INT64: 1 << 20})
	if err != nil || len(hashes.GetDatas()) != 8 || hashes.GetDatas()[0].GetOffset_INT64() != 1<<20 {
		t.Fatalf("upload.getCdnFileHashes: %v, %v", hashes, err)
	}
	// The bare id of the file is not a token of a redirect.
	if _, err = on.UploadGetCdnFileHashes(&mtproto.TLUploadGetCdnFileHashes{FileToken: sec.Token}); !sameError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("upload.getCdnFileHashes by the id: %v", err)
	}
	if _, err = off.UploadGetCdnFileHashes(&mtproto.TLUploadGetCdnFileHashes{FileToken: token}); !sameError(err, mtproto.ErrFileTokenInvalid) {
		t.Fatalf("upload.getCdnFileHashes off: %v", err)
	}

	for name, c := range map[string]*FilesCore{"on": on, "off": off} {
		if _, err = c.UploadReuploadCdnFile(&mtproto.TLUploadReuploadCdnFile{FileToken: token, RequestToken: []byte("any")}); !sameError(err, mtproto.ErrFileTokenInvalid) {
			t.Errorf("%s: upload.reuploadCdnFile: %v", name, err)
		}
		cfg, err := c.HelpGetCdnConfig(&mtproto.TLHelpGetCdnConfig{})
		if err != nil || cfg.GetPredicateName() != mtproto.Predicate_cdnConfig || len(cfg.GetPublicKeys()) != 0 {
			t.Errorf("%s: help.getCdnConfig: %v, %v", name, cfg, err)
		}
	}
}
