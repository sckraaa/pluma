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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// The CDN of large files (task 102, ADR 0065): bff pushes a file of 1 MiB
// and more, encrypted, to the node of the stand once a client that
// supports the CDN asks for it, and redirects the next requests there; the
// client downloads the cipher from the node as the official clients do and
// checks it against the hashes the home gives.

// Set by test/e2e/run.sh; empty with E2E_CDN=off.
var (
	cdnAddr      = os.Getenv("PLUMA_E2E_CDN_ADDR")       // the node's MTProto port, as this machine reaches it
	cdnControl   = os.Getenv("PLUMA_E2E_CDN_CONTROL")    // its control API, as this machine reaches it
	cdnCerts     = os.Getenv("PLUMA_E2E_CDN_CERTS")      // bff's certificates of the control API
	cdnDcAddress = os.Getenv("PLUMA_E2E_CDN_DC_ADDRESS") // the node in dc_options
	dc1Addresses = os.Getenv("PLUMA_E2E_DC1_ADDRESSES")  // DC 1 in dc_options
)

const (
	cdnDC = 201
	// cdnWithin is how long a redirect may take to start or stop: the
	// node is checked every 10 s and stays in the redirects 30 s after the
	// last good check (pkg/cdn prober.go), and a file of a few MB is
	// pushed in about a second at 20 Mbit/s.
	cdnWithin = 45 * time.Second
	// cdnMinSize is CDN_MIN_FILE_SIZE of the stand (the default).
	cdnMinSize = 1 << 20
)

func needCDN(t *testing.T) {
	t.Helper()
	if cdnAddr == "" {
		t.Skip("the stand runs without the CDN node (E2E_CDN=off)")
	}
}

// cdnDcOption returns the CDN DC of help.getConfig.
func cdnDcOption(ctx context.Context, t *testing.T, u *user) *mtproto.DcOption {
	t.Helper()
	for _, o := range dcOptions(ctx, t, u) {
		if o.GetCdn() && o.GetId() == cdnDC {
			return o
		}
	}
	t.Fatalf("help.getConfig names no CDN DC %d", cdnDC)
	return nil
}

func dcOptions(ctx context.Context, t *testing.T, u *user) []*mtproto.DcOption {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: u.c.Constructor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		t.Fatalf("help.getConfig: %v", err)
	}
	cfg, ok := o.(interface{ To_Config() *mtproto.Config })
	if !ok {
		t.Fatalf("help.getConfig returned %T", o)
	}
	return cfg.To_Config().GetDcOptions()
}

// cdnKey returns the node's key from help.getCdnConfig, as Android and
// tdesktop ask for it when resPQ of the CDN DC offers a key they do not
// know.
func cdnKey(ctx context.Context, t *testing.T, u *user) *rsa.PublicKey {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetCdnConfig{Constructor: u.c.Constructor(mtproto.Predicate_help_getCdnConfig)})
	if err != nil {
		t.Fatalf("help.getCdnConfig: %v", err)
	}
	cfg, ok := o.(interface{ To_CdnConfig() *mtproto.CdnConfig })
	if !ok {
		t.Fatalf("help.getCdnConfig returned %T", o)
	}
	pub, err := mtclient.CdnPublicKey(cfg.To_CdnConfig(), cdnDC)
	if err != nil {
		t.Fatal(err)
	}
	if serverkey.Fingerprint(pub) == serverkey.Fingerprint(publicKey(t)) {
		t.Fatal("the CDN node has the key of the home")
	}
	return pub
}

// uploadBig uploads data in parts of 512 KB and returns the input file.
func uploadBig(ctx context.Context, t *testing.T, u *user, data []byte, name string) *mtproto.InputFile {
	t.Helper()
	const part = 512 << 10
	id := randomID()
	parts := int32((len(data) + part - 1) / part)
	for n := int32(0); n < parts; n++ {
		if _, err := u.c.Invoke(ctx, &mtproto.TLUploadSaveBigFilePart{
			Constructor:    u.c.Constructor(mtproto.Predicate_upload_saveBigFilePart),
			FileId:         id,
			FilePart:       n,
			FileTotalParts: parts,
			Bytes:          data[int(n)*part : min(int(n+1)*part, len(data))],
		}); err != nil {
			t.Fatalf("upload.saveBigFilePart %d: %v", n, err)
		}
	}
	return mtproto.MakeTLInputFileBig(&mtproto.InputFile{Id_INT64: id, Parts: parts, Name: name}).To_InputFile()
}

// sendDocument sends data as a file to peer and returns the document as
// the sender has it.
func sendDocument(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, data []byte) *mtproto.Document {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peer,
		Media: mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
			File:     uploadBig(ctx, t, u, data, "data.bin"),
			MimeType: "application/octet-stream",
			Attributes: []*mtproto.DocumentAttribute{
				mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "data.bin"}).To_DocumentAttribute(),
			},
		}).To_InputMedia(),
		RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	for _, m := range sentMessages(t, o) {
		if d := m.GetMedia().GetDocument(); d.GetId() != 0 {
			if d.GetSize2_INT64() != int64(len(data)) {
				t.Fatalf("a document of %d bytes, %d sent", d.GetSize2_INT64(), len(data))
			}
			return d
		}
	}
	t.Fatal("messages.sendMedia returned no document")
	return nil
}

func documentLocation(d *mtproto.Document) *mtproto.InputFileLocation {
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
		Id: d.GetId(), AccessHash: d.GetAccessHash(), FileReference: d.GetFileReference(),
	}).To_InputFileLocation()
}

// getFile is upload.getFile; cdn sets cdn_supported, as Android and
// tdesktop do.
func getFile(ctx context.Context, u *user, loc *mtproto.InputFileLocation, offset int64, limit int32, cdn, precise bool) (*mtproto.Upload_File, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLUploadGetFile{
		Constructor:  u.c.Constructor(mtproto.Predicate_upload_getFile),
		CdnSupported: cdn,
		Precise:      precise,
		Location:     loc,
		Offset_INT64: offset,
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	f, ok := o.(interface{ To_Upload_File() *mtproto.Upload_File })
	if !ok {
		return nil, fmt.Errorf("upload.getFile returned %T", o)
	}
	return f.To_Upload_File(), nil
}

// downloadHome reads a file of size bytes from the home by 1 MB, without
// cdn_supported. A file that is still being stored is empty for a moment
// (dfs writes it after the send), so an empty first part is asked again.
func downloadHome(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputFileLocation, size int) []byte {
	t.Helper()
	var out []byte
	for off := 0; off < size; {
		f, err := getFile(ctx, u, loc, int64(off), 1<<20, false, false)
		if err != nil {
			t.Fatalf("upload.getFile at %d: %v", off, err)
		}
		if f.GetPredicateName() != mtproto.Predicate_upload_file {
			t.Fatalf("upload.getFile without cdn_supported: %s", f.GetPredicateName())
		}
		if len(f.GetBytes()) == 0 {
			if off != 0 {
				t.Fatalf("upload.getFile: the file ends at %d of %d", off, size)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		out = append(out, f.GetBytes()...)
		off += len(f.GetBytes())
	}
	return out
}

// waitGetFile asks upload.getFile with cdn_supported at offset until the
// answer is a redirect (want) or upload.file (!want), for cdnWithin.
func waitGetFile(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputFileLocation, offset int64, want bool) *mtproto.Upload_File {
	t.Helper()
	deadline := time.Now().Add(cdnWithin)
	for {
		f, err := getFile(ctx, u, loc, offset, 1<<20, true, false)
		if err != nil {
			t.Fatalf("upload.getFile with cdn_supported: %v", err)
		}
		if (f.GetPredicateName() == mtproto.Predicate_upload_fileCdnRedirect) == want {
			return f
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload.getFile still answers %s after %v", f.GetPredicateName(), cdnWithin)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// redirectOf waits for the redirect of a file the home has just served:
// the first answer with cdn_supported is the file itself (the copy is only
// queued then), the next ones the redirect once the copy is on the node.
func redirectOf(ctx context.Context, t *testing.T, u *user, loc *mtproto.InputFileLocation, offset int64) *mtclient.CdnRedirect {
	t.Helper()
	first, err := getFile(ctx, u, loc, offset, 1<<20, true, false)
	if err != nil {
		t.Fatalf("upload.getFile with cdn_supported: %v", err)
	}
	if first.GetPredicateName() != mtproto.Predicate_upload_file || len(first.GetBytes()) == 0 {
		t.Fatalf("the first request of a new file: %s of %d bytes, want the file from the home", first.GetPredicateName(), len(first.GetBytes()))
	}
	r, err := mtclient.ParseCdnRedirect(waitGetFile(ctx, t, u, loc, offset, true))
	if err != nil {
		t.Fatal(err)
	}
	if r.DC != cdnDC {
		t.Fatalf("redirected to DC %d, want %d", r.DC, cdnDC)
	}
	if len(r.Hashes) == 0 || hashOffset(r.Hashes[0]) != offset/mtclient.CdnHashChunk*mtclient.CdnHashChunk {
		t.Fatalf("the hashes of a redirect at %d start at %v", offset, r.Hashes)
	}
	return r
}

func hashOffset(fh *mtproto.FileHash) int64 {
	if fh.GetOffset_INT64() != 0 {
		return fh.GetOffset_INT64()
	}
	return int64(fh.GetOffset_INT32())
}

// cdnClient connects to the node with the secret of its dcOption and makes
// a key: a temporary one, never bound, as tdesktop does
// (session_private.cpp:2604-2630), or a permanent one, as Android and iOS
// do (Datacenter.cpp:1076, MTProto.m:909-912).
func cdnClient(ctx context.Context, t *testing.T, opt *mtproto.DcOption, pub *rsa.PublicKey, temp bool) *mtclient.Client {
	t.Helper()
	opts := mtclient.KeyOptions{DC: cdnDC}
	if temp {
		opts.Temp, opts.ExpiresIn = true, 24*3600
	}
	c, err := mtclient.DialNewKey(ctx, mtclient.Config{
		Addr:      cdnAddr,
		PublicKey: pub,
		Secret:    opt.GetSecret(),
		DC:        cdnDC,
		// tdesktop's initConnection to a CDN DC (session_private.cpp:671-707).
		DeviceModel:   "n/a",
		SystemVersion: "n/a",
		Logf:          t.Logf,
	}, opts)
	if err != nil {
		t.Fatalf("the key exchange with the CDN node (temporary %v): %v", temp, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func getCdnFileReq(c *mtclient.Client, token []byte, offset int64, limit int32) *mtproto.TLUploadGetCdnFile {
	return &mtproto.TLUploadGetCdnFile{
		Constructor:  c.Constructor(mtproto.Predicate_upload_getCdnFile),
		FileToken:    token,
		Offset_INT64: offset,
		Limit:        limit,
	}
}

func encodeQuery(t *testing.T, c *mtclient.Client, obj mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(128)
	if err := obj.Encode(x, c.Layer()); err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(x.GetBuf())
}

// getCdnFile asks the node for a part; the first request of a connection
// goes in invokeWithLayer(initConnection(...)), as the clients send it.
func getCdnFile(ctx context.Context, t *testing.T, c *mtclient.Client, token []byte, offset int64, limit int32, first bool) (mtproto.TLObject, error) {
	t.Helper()
	var req mtproto.TLObject = getCdnFileReq(c, token, offset, limit)
	if first {
		req = c.WrapInit(encodeQuery(t, c, req))
	}
	return c.Invoke(ctx, req)
}

// cdnFileHashes is upload.getCdnFileHashes at the home.
func cdnFileHashes(ctx context.Context, u *user, token []byte, offset int64) ([]*mtproto.FileHash, error) {
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUploadGetCdnFileHashes{
		Constructor:  u.c.Constructor(mtproto.Predicate_upload_getCdnFileHashes),
		FileToken:    token,
		Offset_INT64: offset,
	})
	if err != nil {
		return nil, err
	}
	return fileHashes(raw)
}

func fileHashes(raw []byte) ([]*mtproto.FileHash, error) {
	objs, err := mtclient.DecodeVector(raw)
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.FileHash, 0, len(objs))
	for _, o := range objs {
		h, ok := o.(interface{ To_FileHash() *mtproto.FileHash })
		if !ok {
			return nil, errors.New("not a fileHash in the vector")
		}
		out = append(out, h.To_FileHash())
	}
	return out, nil
}

// cdnDownload downloads the file of r from the node by parts of limit,
// decrypts each at its offset, checks it against the hashes (asking the
// home for the missing ones) and returns the file.
func cdnDownload(ctx context.Context, t *testing.T, c *mtclient.Client, home *user, r *mtclient.CdnRedirect, limit int32) []byte {
	t.Helper()
	hashes := mtclient.NewCdnHashes(r.Hashes)
	var out []byte
	for offset := int64(0); ; offset += int64(limit) {
		o, err := getCdnFile(ctx, t, c, r.FileToken, offset, limit, offset == 0)
		if err != nil {
			t.Fatalf("upload.getCdnFile(%d, %d): %v", offset, limit, err)
		}
		f, ok := o.(*mtproto.TLUploadCdnFile)
		if !ok {
			t.Fatalf("upload.getCdnFile(%d, %d) returned %T", offset, limit, o)
		}
		if len(f.GetBytes()) == 0 {
			return out
		}
		plain, err := mtclient.CdnDecrypt(r.Key, r.IV, offset, f.GetBytes())
		if err != nil {
			t.Fatal(err)
		}
		for {
			at, missing := hashes.Missing(offset, len(plain))
			if !missing {
				break
			}
			more, err := cdnFileHashes(ctx, home, r.FileToken, at)
			if err != nil || len(more) == 0 || hashOffset(more[0]) != at {
				t.Fatalf("upload.getCdnFileHashes at %d: %d hashes, %v", at, len(more), err)
			}
			hashes.Add(more)
		}
		if err = hashes.Check(offset, plain); err != nil {
			t.Fatal(err)
		}
		out = append(out, plain...)
	}
}

func randomFile(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestConfigDcOptions: help.getConfig lists DC 1 at every address of the
// stand, static, in their order, and the CDN DC with the flags and the
// secret the clients need (ADR 0065 p. 7): cdn and tcpo_only, so that
// tdesktop takes it for a CDN DC reached over TCP only
// (mtproto_dc_options.cpp:677-691), and the "dd" secret, with which
// Android tries no other port (Datacenter.cpp:337-345).
func TestConfigDcOptions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := mtclient.Dial(ctx, config(t, "e2e dc_options"))
	if err != nil {
		t.Fatal(err)
	}
	closeChecked(t, "client", c)
	options := dcOptions(ctx, t, &user{c: c})

	var want []string
	if dc1Addresses != "" {
		want = strings.Split(dc1Addresses, ",")
	}
	if cdnDcAddress != "" {
		want = append(want, cdnDcAddress)
	}
	if len(options) != len(want) {
		t.Fatalf("dc_options has %d entries, want %d (%v)", len(options), len(want), want)
	}
	for i, o := range options {
		addr := net.JoinHostPort(o.GetIpAddress(), strconv.Itoa(int(o.GetPort())))
		if addr != want[i] || o.GetIpv6() || o.GetMediaOnly() || o.GetThisPortOnly() {
			t.Errorf("dc_options[%d] = %v, want %s over IPv4", i, o, want[i])
		}
		if i < len(want)-1 || cdnDcAddress == "" {
			if o.GetId() != 1 || !o.GetStatic() || o.GetCdn() || o.GetTcpoOnly() || len(o.GetSecret()) != 0 {
				t.Errorf("dc_options[%d] = %v, want DC 1, static", i, o)
			}
			continue
		}
		if o.GetId() != cdnDC || !o.GetCdn() || !o.GetTcpoOnly() || o.GetStatic() {
			t.Errorf("dc_options[%d] = %v, want DC %d with cdn and tcpo_only", i, o, cdnDC)
		}
		if s := o.GetSecret(); len(s) != 17 || s[0] != 0xdd {
			t.Errorf("the secret of the CDN DC is %d bytes, want 0xdd and 16", len(s))
		}
	}
}

// TestGatewayObfuscated: the gateway still serves the obfuscated transport
// without a secret, and refuses a connection made with one: only the CDN
// node takes the secret of dc_options.
func TestGatewayObfuscated(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg := config(t, "e2e obfuscated")
	cfg.Obfuscated = true
	c, err := mtclient.Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("the obfuscated transport without a secret: %v", err)
	}
	closeChecked(t, "obfuscated", c)

	secret := append([]byte{0xdd}, randomFile(t, 16)...)
	cfg.Secret = secret
	rctx, rcancel := context.WithTimeout(ctx, 15*time.Second)
	defer rcancel()
	if c2, err := mtclient.DialNewKey(rctx, cfg, mtclient.KeyOptions{}); err == nil {
		_ = c2.Close()
		t.Fatal("the gateway made a key over a connection with a secret")
	}
}

// TestCdnDownload: Alice sends Bob a file of 3 MB and 5 bytes; Bob gets it
// from the home first, then a redirect to the node, whose key
// help.getCdnConfig gives; he downloads it from the node over a temporary
// key by 128 KB, as tdesktop, and over a permanent key by 1 MB, as
// Android, and it is the file sent. The logs of bff and the node hold none
// of the file's secrets.
func TestCdnDownload(t *testing.T) {
	needCDN(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	data := randomFile(t, 3<<20+5)
	sent := sendDocument(ctx, t, alice, peerOf(ctx, t, alice, bob), data)
	var doc *mtproto.Document
	if _, err := bob.c.WaitUpdate(ctx, func(u *mtproto.Updates) bool {
		for _, m := range newMessages(u) {
			if d := m.GetMedia().GetDocument(); d.GetId() == sent.GetId() {
				doc = d
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob's message with the file: %v", err)
	}
	loc := documentLocation(doc)
	if got := downloadHome(ctx, t, bob, loc, len(data)); !bytes.Equal(got, data) {
		t.Fatal("the file from the home differs")
	}

	// The redirect at 1 MB carries the hashes from 1 MB on; those before
	// come from upload.getCdnFileHashes.
	r := redirectOf(ctx, t, bob, loc, 1<<20)
	opt := cdnDcOption(ctx, t, bob)
	pub := cdnKey(ctx, t, bob)

	tdesktop := cdnClient(ctx, t, opt, pub, true)
	if got := cdnDownload(ctx, t, tdesktop, bob, r, 128<<10); !bytes.Equal(got, data) {
		t.Fatalf("by 128 KB over a temporary key: %d bytes, not the file", len(got))
	}
	android := cdnClient(ctx, t, opt, pub, false)
	if got := cdnDownload(ctx, t, android, bob, r, 1<<20); !bytes.Equal(got, data) {
		t.Fatalf("by 1 MB over a permanent key: %d bytes, not the file", len(got))
	}
	// Past the end: empty bytes, the end of the file for every client.
	o, err := getCdnFile(ctx, t, android, r.FileToken, 4<<20, 1<<20, false)
	if f, ok := o.(*mtproto.TLUploadCdnFile); err != nil || !ok || len(f.GetBytes()) != 0 {
		t.Fatalf("upload.getCdnFile past the end: %v, %v", o, err)
	}

	// Alice, who sent it, is redirected to the same copy.
	ra := waitGetFile(ctx, t, alice, documentLocation(sent), 0, true)
	if !bytes.Equal(ra.GetFileToken(), r.FileToken) {
		t.Error("the sender of the file was redirected to another copy")
	}

	notLogged(t, map[string][]byte{"file_token": r.FileToken, "key": r.Key, "iv": r.IV})
}

// notLogged fails the test if the logs of bff or the node hold one of
// secrets in hex, base64 or as Go prints a byte slice. The unpadded
// base64 forms are searched for, since each padded form contains its
// unpadded one.
func notLogged(t *testing.T, secrets map[string][]byte) {
	t.Helper()
	logs := stackOut(t, "logs", "--no-color", "bff", "cdn")
	for name, secret := range secrets {
		for _, form := range []string{
			hex.EncodeToString(secret), strings.ToUpper(hex.EncodeToString(secret)),
			base64.RawStdEncoding.EncodeToString(secret), base64.RawURLEncoding.EncodeToString(secret),
			strings.Trim(fmt.Sprint(secret), "[]"),
		} {
			if strings.Contains(logs, form) {
				t.Errorf("the logs of bff or the node hold the %s of a file", name)
			}
		}
	}
}

// TestCdnSecretChatFile: a file of a secret chat, of 2 MB, goes through
// the node too, once the home checked the receiver may read it.
func TestCdnSecretChatFile(t *testing.T) {
	needCDN(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	for _, u := range []*user{alice, bob} {
		getState(ctx, t, u)
	}
	toBob := peerOf(ctx, t, alice, bob)

	cfg, ok := dhConfig(ctx, t, alice, 0).(*mtproto.TLMessagesDhConfig)
	if !ok {
		t.Fatal("messages.getDhConfig returned no parameters")
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestEncryption{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestEncryption),
		UserId: &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, Constructor: alice.c.Constructor(mtproto.Predicate_inputUser),
			UserId: toBob.GetUserId(), AccessHash: toBob.GetAccessHash()},
		RandomId: int32(randomID()),
		GA:       secretG(t, cfg),
	})
	if err != nil {
		t.Fatalf("messages.requestEncryption: %v", err)
	}
	waiting, ok := o.(*mtproto.TLEncryptedChatWaiting)
	if !ok {
		t.Fatalf("messages.requestEncryption returned %T", o)
	}
	encryptionUpdate(ctx, t, bob, waiting.GetId(), mtproto.Predicate_encryptedChatRequested)
	peer := mtproto.MakeTLInputEncryptedChat(&mtproto.InputEncryptedChat{ChatId: waiting.GetId(), AccessHash: waiting.GetAccessHash()}).To_InputEncryptedChat()
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesAcceptEncryption{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_acceptEncryption), Peer: peer, GB: secretG(t, cfg), KeyFingerprint: 7,
	}); err != nil {
		t.Fatalf("messages.acceptEncryption: %v", err)
	}
	encryptionUpdate(ctx, t, alice, waiting.GetId(), mtproto.Predicate_encryptedChat)

	// The bytes are the clients' ciphertext; the server never reads them.
	data := randomFile(t, 2<<20)
	const part = 512 << 10
	fileId := randomID()
	for n := 0; n*part < len(data); n++ {
		if _, err = alice.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
			Constructor: alice.c.Constructor(mtproto.Predicate_upload_saveFilePart),
			FileId:      fileId, FilePart: int32(n), Bytes: data[n*part : (n+1)*part],
		}); err != nil {
			t.Fatalf("upload.saveFilePart %d: %v", n, err)
		}
	}
	sum := md5.Sum(data)
	randomId := randomID()
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendEncryptedFile{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendEncryptedFile),
		Peer:        peer,
		RandomId:    randomId,
		Data:        []byte("the message, encrypted by the clients"),
		File: mtproto.MakeTLInputEncryptedFileUploaded(&mtproto.InputEncryptedFile{
			Id: fileId, Parts: int32(len(data) / part), Md5Checksum: hex.EncodeToString(sum[:]), KeyFingerprint: 7,
		}).To_InputEncryptedFile(),
	}); err != nil {
		t.Fatalf("messages.sendEncryptedFile: %v", err)
	}
	m, _ := newEncryptedMessage(ctx, t, bob, randomId)
	file := m.GetFile()
	loc := mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: file.GetId(), AccessHash: file.GetAccessHash()}).To_InputFileLocation()
	if got := downloadHome(ctx, t, bob, loc, len(data)); !bytes.Equal(got, data) {
		t.Fatal("the file from the home differs")
	}

	// A wrong access hash is refused before any redirect.
	wrong := mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: file.GetId(), AccessHash: file.GetAccessHash() + 1}).To_InputFileLocation()
	if _, err = getFile(ctx, bob, wrong, 0, 1<<20, true, false); !mtclient.IsRPCError(err, "FILE_ID_INVALID") {
		t.Fatalf("a wrong access hash with cdn_supported: %v, want FILE_ID_INVALID", err)
	}

	r := redirectOf(ctx, t, bob, loc, 0)
	c := cdnClient(ctx, t, cdnDcOption(ctx, t, bob), cdnKey(ctx, t, bob), false)
	if got := cdnDownload(ctx, t, c, bob, r, 1<<20); !bytes.Equal(got, data) {
		t.Fatalf("from the node: %d bytes, not the file", len(got))
	}
}

// cdnNode is the control API of the stand's node, as bff reaches it.
func cdnNode(t *testing.T) *cdn.NodeClient {
	t.Helper()
	n, err := cdn.DialNode(&cdn.Settings{
		NodeControl: cdnControl,
		ServerName:  "cdn",
		CertFile:    filepath.Join(cdnCerts, "client.pem"),
		KeyFile:     filepath.Join(cdnCerts, "client-key.pem"),
		CAFile:      filepath.Join(cdnCerts, "server-ca.pem"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	return n
}

// TestCdnReupload: a node that lost a file asks the client to have it
// uploaded again (upload.cdnFileReuploadNeeded); upload.reuploadCdnFile at
// the home answers the hashes once the copy is back, and the node serves
// it again. A request_token the node did not sign and a file_token that is
// not the home's are refused, so that the clients go back to
// upload.getFile.
func TestCdnReupload(t *testing.T) {
	needCDN(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")
	self := &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	data := randomFile(t, 1<<20+300<<10)
	loc := documentLocation(sendDocument(ctx, t, u, self, data))
	downloadHome(ctx, t, u, loc, len(data))
	r := redirectOf(ctx, t, u, loc, 0)
	c := cdnClient(ctx, t, cdnDcOption(ctx, t, u), cdnKey(ctx, t, u), false)
	if got := cdnDownload(ctx, t, c, u, r, 1<<20); !bytes.Equal(got, data) {
		t.Fatal("the file from the node differs")
	}

	deleted, err := cdnNode(t).Delete(ctx, r.FileToken)
	if err != nil || !deleted {
		t.Fatalf("Delete on the node: %v, %v", deleted, err)
	}
	o, err := getCdnFile(ctx, t, c, r.FileToken, 0, 1<<20, false)
	if err != nil {
		t.Fatalf("upload.getCdnFile of a lost file: %v", err)
	}
	need, ok := o.(*mtproto.TLUploadCdnFileReuploadNeeded)
	if !ok || len(need.GetRequestToken()) == 0 {
		t.Fatalf("upload.getCdnFile of a lost file returned %T", o)
	}

	reupload := func(fileToken, requestToken []byte) ([]*mtproto.FileHash, error) {
		raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUploadReuploadCdnFile{
			Constructor:  u.c.Constructor(mtproto.Predicate_upload_reuploadCdnFile),
			FileToken:    fileToken,
			RequestToken: requestToken,
		})
		if err != nil {
			return nil, err
		}
		return fileHashes(raw)
	}
	forged := bytes.Clone(need.GetRequestToken())
	forged[len(forged)-1] ^= 1
	if _, err = reupload(r.FileToken, forged); !mtclient.IsRPCError(err, "REQUEST_TOKEN_INVALID") {
		t.Errorf("a forged request_token: %v, want REQUEST_TOKEN_INVALID", err)
	}
	foreign := randomFile(t, len(r.FileToken))
	if _, err = reupload(foreign, need.GetRequestToken()); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Errorf("reupload of a file_token not the home's: %v, want FILE_TOKEN_INVALID", err)
	}
	if _, err = cdnFileHashes(ctx, u, foreign, 0); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Errorf("hashes of a file_token not the home's: %v, want FILE_TOKEN_INVALID", err)
	}
	// upload.getCdnFile sent to the home sends the client back to getFile.
	if _, err = u.c.Invoke(ctx, getCdnFileReq(u.c, r.FileToken, 0, 1<<20)); !mtclient.IsRPCError(err, "FILE_TOKEN_INVALID") {
		t.Errorf("upload.getCdnFile at the home: %v, want FILE_TOKEN_INVALID", err)
	}

	hashes, err := reupload(r.FileToken, need.GetRequestToken())
	if err != nil {
		t.Fatalf("upload.reuploadCdnFile: %v", err)
	}
	if len(hashes) == 0 || hashOffset(hashes[0]) != 0 {
		t.Fatalf("upload.reuploadCdnFile answered %d hashes", len(hashes))
	}
	again := *r
	again.Hashes = hashes
	if got := cdnDownload(ctx, t, c, u, &again, 512<<10); !bytes.Equal(got, data) {
		t.Fatal("the file uploaded again differs")
	}
	notLogged(t, map[string][]byte{"file_token": r.FileToken, "request_token": need.GetRequestToken()})
}

// TestCdnNodeRestricted: the node serves upload.getCdnFile alone, to keys
// made with it, by the rules of the ranges of upload.getFile.
func TestCdnNodeRestricted(t *testing.T) {
	needCDN(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")
	opt, pub := cdnDcOption(ctx, t, u), cdnKey(ctx, t, u)
	c := cdnClient(ctx, t, opt, pub, false)

	query := c.WrapInit(encodeQuery(t, c, &mtproto.TLHelpGetConfig{Constructor: c.Constructor(mtproto.Predicate_help_getConfig)}))
	if _, err := c.Invoke(ctx, query); !mtclient.IsRPCError(err, "CDN_METHOD_INVALID") {
		t.Errorf("help.getConfig on the node: %v, want CDN_METHOD_INVALID", err)
	}
	token := randomFile(t, 32)
	for _, r := range []struct {
		offset int64
		limit  int32
		want   string
	}{
		{768 << 10, 512 << 10, "LIMIT_INVALID"}, // across 1 MB
		{0, 2 << 20, "LIMIT_INVALID"},
		{1000, 4096, "OFFSET_INVALID"},
	} {
		if _, err := getCdnFile(ctx, t, c, token, r.offset, r.limit, false); !mtclient.IsRPCError(err, r.want) {
			t.Errorf("upload.getCdnFile(%d, %d): %v, want %s", r.offset, r.limit, err, r.want)
		}
	}

	// A key the node never made: the transport error -404, after which the
	// clients make a new one.
	tr, err := mtclient.DialObfuscated(ctx, cdnAddr, opt.GetSecret(), cdnDC)
	if err != nil {
		t.Fatal(err)
	}
	unknown := &mtclient.AuthKey{ID: randomID(), Key: randomFile(t, 256)}
	stranger := mtclient.New(tr, unknown, mtclient.Config{Logf: t.Logf})
	defer func() { _ = stranger.Close() }()
	rctx, rcancel := context.WithTimeout(ctx, 20*time.Second)
	defer rcancel()
	_, err = getCdnFile(rctx, t, stranger, token, 0, 4096, true)
	if !mtclient.IsTransportError(err, -404) && !mtclient.IsTransportError(stranger.Err(), -404) {
		t.Errorf("a key the node does not know: %v (client: %v), want -404", err, stranger.Err())
	}
}

// TestCdnNoRedirect: no redirect without cdn_supported, with precise, for a
// file below 1 MiB or an avatar; none while the node is down, and again
// once it is back. Not parallel: the node is paused for every test meanwhile.
func TestCdnNoRedirect(t *testing.T) {
	needCDN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	u := login(ctx, t, testPhones(t, 1)[0], "Alice")
	self := &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}

	big := randomFile(t, 2<<20)
	loc := documentLocation(sendDocument(ctx, t, u, self, big))
	downloadHome(ctx, t, u, loc, len(big))
	redirectOf(ctx, t, u, loc, 0)

	home := func(name string, loc *mtproto.InputFileLocation, cdn, precise bool) {
		t.Helper()
		f, err := getFile(ctx, u, loc, 0, 1<<20, cdn, precise)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f.GetPredicateName() != mtproto.Predicate_upload_file || len(f.GetBytes()) == 0 {
			t.Errorf("%s: %s of %d bytes, want the file from the home", name, f.GetPredicateName(), len(f.GetBytes()))
		}
	}
	home("without cdn_supported", loc, false, false)
	home("with precise", loc, true, true)

	small := randomFile(t, cdnMinSize-124<<10)
	smallLoc := documentLocation(sendDocument(ctx, t, u, self, small))
	downloadHome(ctx, t, u, smallLoc, len(small))
	// Asked again after a push would have had its time: never queued.
	home("a file of 900 KB", smallLoc, true, false)
	time.Sleep(5 * time.Second)
	home("a file of 900 KB, again", smallLoc, true, false)

	photo := uploadPhoto(ctx, t, u)
	o, err := u.c.Invoke(ctx, &mtproto.TLPhotosUploadProfilePhoto{
		Constructor: u.c.Constructor(mtproto.Predicate_photos_uploadProfilePhoto),
		File:        photo.GetFile(),
	})
	if err != nil {
		t.Fatalf("photos.uploadProfilePhoto: %v", err)
	}
	p, ok := o.(interface{ To_Photos_Photo() *mtproto.Photos_Photo })
	if !ok {
		t.Fatalf("photos.uploadProfilePhoto returned %T", o)
	}
	avatar := mtproto.MakeTLInputPeerPhotoFileLocation(&mtproto.InputFileLocation{
		Big: true, Peer: self, PhotoId: p.To_Photos_Photo().GetPhoto().GetId(),
	}).To_InputFileLocation()
	home("an avatar", avatar, true, false)

	// The node down: the home serves the file within the time the node
	// stays in the redirects; back up, the redirects resume.
	stackOut(t, "pause", "cdn")
	paused := true
	t.Cleanup(func() {
		if paused {
			_, _ = exec.Command(runScript, "compose", "unpause", "cdn").CombinedOutput()
		}
	})
	if f := waitGetFile(ctx, t, u, loc, 0, false); len(f.GetBytes()) == 0 {
		t.Fatal("the node down: an empty file from the home")
	}
	stackOut(t, "unpause", "cdn")
	paused = false
	waitGetFile(ctx, t, u, loc, 0, true)
}
