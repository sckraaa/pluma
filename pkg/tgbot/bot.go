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

// Package tgbot is a bot of Telegram that the server reads sticker sets and
// their files with (task 030): it logs in with the bot's token
// (auth.importBotAuthorization), which involves no phone, code or person's
// account, and asks only what bots may ask (messages.getStickerSet,
// upload.getFile: "Both users and bots can use this method",
// core.telegram.org, 2026-11-02). Answers come in the server's layer and
// decode with its own codec.
package tgbot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Config is the bot and the application it logs in through.
type Config struct {
	APIID    int32  // my.telegram.org
	APIHash  string // of that application
	Token    string // from @BotFather
	Addr     string // the first datacenter, default langpack.TelegramAddr (DC 2)
	DC       int32  // its number, default langpack.TelegramDC
	MaxWait  time.Duration
	DialFunc func(ctx context.Context, cfg mtclient.Config, dc int32) (Conn, error) // tests
}

// Conn is a connection to one datacenter of Telegram.
type Conn interface {
	InvokeRaw(ctx context.Context, req mtproto.TLObject) ([]byte, error)
	Close() error
}

// Settings are the bot in a service's config: all empty, no bot. The
// api_id is a string, so that an unset one (an empty environment variable)
// is no error of the config.
type Settings struct {
	ApiId    string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	ApiHash  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
	BotToken string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json\'s
}

// FromSettings returns the bot of s, nil and no error if s has none.
func FromSettings(s Settings) (*Bot, error) {
	if s.ApiId == "" && s.ApiHash == "" && s.BotToken == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(s.ApiId, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("tgbot: api_id %q: %w", s.ApiId, err)
	}
	return New(Config{APIID: int32(id), APIHash: s.ApiHash, Token: s.BotToken})
}

// ErrNotConfigured is a bot without a token or an application.
var ErrNotConfigured = errors.New("tgbot: no bot configured")

// Bot is a logged-in bot with a connection per datacenter it needs.
type Bot struct {
	cfg Config

	mu    sync.Mutex
	home  int32            // the bot's datacenter
	conns map[int32]Conn   // by datacenter, logged in
	addrs map[int32]string // from help.getConfig
}

// New returns a bot that logs in on its first request.
func New(cfg Config) (*Bot, error) {
	if cfg.APIID <= 0 || cfg.APIHash == "" || cfg.Token == "" {
		return nil, ErrNotConfigured
	}
	if cfg.Addr == "" {
		cfg.Addr = langpack.TelegramAddr
	}
	if cfg.DC == 0 {
		cfg.DC = langpack.TelegramDC
	}
	if cfg.MaxWait == 0 {
		cfg.MaxWait = 30 * time.Second
	}
	if cfg.DialFunc == nil {
		cfg.DialFunc = dial
	}
	return &Bot{cfg: cfg, conns: map[int32]Conn{}, addrs: map[int32]string{cfg.DC: cfg.Addr}}, nil
}

// conn is a connection to Telegram that wraps every request in
// invokeWithLayer(initConnection(...)), as langpack.TelegramClient does.
type conn struct {
	c *mtclient.Client
}

func (c conn) InvokeRaw(ctx context.Context, req mtproto.TLObject) ([]byte, error) {
	x := mtproto.NewEncodeBuf(512)
	if err := req.Encode(x, c.c.Layer()); err != nil {
		return nil, err
	}
	return c.c.InvokeRaw(ctx, c.c.WrapInit(x.GetBuf()))
}

func (c conn) Close() error { return c.c.Close() }

func dial(ctx context.Context, cfg mtclient.Config, dc int32) (Conn, error) {
	c, err := mtclient.DialNewKey(ctx, cfg, mtclient.KeyOptions{DC: dc})
	if err != nil {
		return nil, err
	}
	return conn{c: c}, nil
}

func (b *Bot) dialDC(ctx context.Context, dc int32) (Conn, error) {
	addr, ok := b.addrs[dc]
	if !ok {
		return nil, fmt.Errorf("tgbot: no address of datacenter %d", dc)
	}
	return b.cfg.DialFunc(ctx, mtclient.Config{
		Addr:           addr,
		PublicKey:      langpack.TelegramPublicKey(),
		DC:             dc,
		APIID:          b.cfg.APIID,
		DeviceModel:    "Pluma",
		SystemVersion:  "server",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangCode:       "en",
	}, dc)
}

// homeConn is the logged-in connection to the bot's datacenter.
func (b *Bot) homeConn(ctx context.Context) (Conn, error) {
	if c := b.conns[b.home]; b.home != 0 && c != nil {
		return c, nil
	}
	dc := b.cfg.DC
	for range 3 {
		c, err := b.dialDC(ctx, dc)
		if err != nil {
			return nil, err
		}
		_, err = c.InvokeRaw(ctx, &mtproto.TLAuthImportBotAuthorization{
			Constructor:  ctor(mtproto.Predicate_auth_importBotAuthorization),
			ApiId:        b.cfg.APIID,
			ApiHash:      b.cfg.APIHash,
			BotAuthToken: b.cfg.Token,
		})
		if to, ok := migrateTo(err, "USER_MIGRATE_"); ok {
			c.Close()
			if err := b.learnAddrs(ctx, dc); err != nil {
				return nil, err
			}
			dc = to
			continue
		}
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("tgbot: login: %w", err)
		}
		b.home, b.conns[dc] = dc, c
		if len(b.addrs) == 1 {
			if err := b.learnAddrsOn(ctx, c); err != nil {
				return nil, err
			}
		}
		return c, nil
	}
	return nil, errors.New("tgbot: login: too many migrations")
}

// learnAddrs reads the addresses of the datacenters through a connection
// without login to dc.
func (b *Bot) learnAddrs(ctx context.Context, dc int32) error {
	c, err := b.dialDC(ctx, dc)
	if err != nil {
		return err
	}
	defer c.Close()
	return b.learnAddrsOn(ctx, c)
}

func (b *Bot) learnAddrsOn(ctx context.Context, c Conn) error {
	raw, err := c.InvokeRaw(ctx, &mtproto.TLHelpGetConfig{Constructor: ctor(mtproto.Predicate_help_getConfig)})
	if err != nil {
		return fmt.Errorf("tgbot: help.getConfig: %w", err)
	}
	o, err := decode(raw)
	if err != nil {
		return err
	}
	cfg, ok := o.(interface{ To_Config() *mtproto.Config })
	if !ok {
		return fmt.Errorf("tgbot: help.getConfig: %T", o)
	}
	for _, opt := range cfg.To_Config().GetDcOptions() {
		// IPv4, not CDN, not only for tests; a media-only address is fine
		// for files but the general one is taken first.
		if opt.GetIpv6() || opt.GetCdn() || opt.GetTcpoOnly() || strings.Contains(opt.GetIpAddress(), ":") {
			continue
		}
		if _, ok := b.addrs[opt.GetId()]; ok && opt.GetMediaOnly() {
			continue
		}
		b.addrs[opt.GetId()] = net.JoinHostPort(opt.GetIpAddress(), strconv.Itoa(int(opt.GetPort())))
	}
	return nil
}

// connTo is a logged-in connection to datacenter dc: the home one, or one
// the bot's authorization was carried to (auth.exportAuthorization).
func (b *Bot) connTo(ctx context.Context, dc int32) (Conn, error) {
	home, err := b.homeConn(ctx)
	if err != nil {
		return nil, err
	}
	if dc == b.home || dc == 0 {
		return home, nil
	}
	if c := b.conns[dc]; c != nil {
		return c, nil
	}
	raw, err := home.InvokeRaw(ctx, &mtproto.TLAuthExportAuthorization{Constructor: ctor(mtproto.Predicate_auth_exportAuthorization), DcId: dc})
	if err != nil {
		return nil, fmt.Errorf("tgbot: auth.exportAuthorization: %w", err)
	}
	o, err := decode(raw)
	if err != nil {
		return nil, err
	}
	ex, ok := o.(interface {
		To_Auth_ExportedAuthorization() *mtproto.Auth_ExportedAuthorization
	})
	if !ok {
		return nil, fmt.Errorf("tgbot: auth.exportAuthorization: %T", o)
	}
	c, err := b.dialDC(ctx, dc)
	if err != nil {
		return nil, err
	}
	a := ex.To_Auth_ExportedAuthorization()
	if _, err := c.InvokeRaw(ctx, &mtproto.TLAuthImportAuthorization{
		Constructor: ctor(mtproto.Predicate_auth_importAuthorization),
		Id:          a.GetId(),
		Bytes:       a.GetBytes(),
	}); err != nil {
		c.Close()
		return nil, fmt.Errorf("tgbot: auth.importAuthorization on %d: %w", dc, err)
	}
	b.conns[dc] = c
	return c, nil
}

// invoke sends req to dc, waits out a short FLOOD_WAIT, and drops a
// connection that failed so that the next request dials again.
func (b *Bot) invoke(ctx context.Context, dc int32, req mtproto.TLObject) ([]byte, error) {
	for {
		// The connections are the lock's; requests on one run at once.
		b.mu.Lock()
		c, err := b.connTo(ctx, dc)
		b.mu.Unlock()
		if err != nil {
			return nil, err
		}
		raw, err := c.InvokeRaw(ctx, req)
		if wait, ok := floodWait(err); ok {
			if wait > b.cfg.MaxWait {
				return nil, fmt.Errorf("tgbot: FLOOD_WAIT of %s", wait)
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		var rpc *mtclient.RPCError
		if err != nil && !errors.As(err, &rpc) && ctx.Err() == nil {
			// The connection broke: dial again next time.
			b.mu.Lock()
			c.Close()
			for k, v := range b.conns {
				if v == c {
					delete(b.conns, k)
					if k == b.home {
						b.home = 0
					}
				}
			}
			b.mu.Unlock()
		}
		return raw, err
	}
}

// StickerSet returns the set Telegram has for in (by short name or id), and
// mtproto.ErrStickersetInvalid for one it does not give out.
func (b *Bot) StickerSet(ctx context.Context, in *mtproto.InputStickerSet) (*mtproto.Messages_StickerSet, error) {
	raw, err := b.invoke(ctx, 0, &mtproto.TLMessagesGetStickerSet{
		Constructor: ctor(mtproto.Predicate_messages_getStickerSet),
		Stickerset:  in,
		Hash:        0,
	})
	if mtclient.IsRPCError(err, "STICKERSET_INVALID") {
		return nil, mtproto.ErrStickersetInvalid
	}
	if err != nil {
		return nil, err
	}
	o, err := decode(raw)
	if err != nil {
		return nil, err
	}
	s, ok := o.(interface {
		To_Messages_StickerSet() *mtproto.Messages_StickerSet
	})
	if !ok {
		return nil, fmt.Errorf("tgbot: messages.getStickerSet: %T", o)
	}
	return s.To_Messages_StickerSet(), nil
}

// partSize is the part of upload.getFile: 1 MiB, within one 1 MiB block
// as core.telegram.org/api/files allows without precise.
const partSize = 1 << 20

// File returns the file of doc, or of its thumbnail of type thumb, at most
// max bytes. Files download in parallel.
func (b *Bot) File(ctx context.Context, doc *mtproto.Document, thumb string, max int) ([]byte, error) {
	dc := doc.GetDcId()
	var out []byte
	for offset := int64(0); ; offset += partSize {
		raw, err := b.invoke(ctx, dc, &mtproto.TLUploadGetFile{
			Constructor: ctor(mtproto.Predicate_upload_getFile),
			Location: mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
				Id:            doc.GetId(),
				AccessHash:    doc.GetAccessHash(),
				FileReference: doc.GetFileReference(),
				ThumbSize:     thumb,
			}).To_InputFileLocation(),
			Offset_INT64: offset,
			Limit:        partSize,
		})
		if to, ok := migrateTo(err, "FILE_MIGRATE_"); ok {
			dc, offset = to, offset-partSize
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("tgbot: upload.getFile: %w", err)
		}
		o, err := decode(raw)
		if err != nil {
			return nil, err
		}
		f, ok := o.(interface{ To_Upload_File() *mtproto.Upload_File })
		if !ok {
			return nil, fmt.Errorf("tgbot: upload.getFile: %T", o)
		}
		part := f.To_Upload_File().GetBytes()
		out = append(out, part...)
		if len(out) > max {
			return nil, fmt.Errorf("tgbot: a file over %d bytes", max)
		}
		if len(part) < partSize {
			return out, nil
		}
	}
}

// Close closes every connection.
func (b *Bot) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for dc, c := range b.conns {
		c.Close()
		delete(b.conns, dc)
	}
	b.home = 0
	return nil
}

// ctor is the constructor of a request in the server's layer.
func ctor(predicate string) mtproto.TLConstructor {
	return mtclient.Constructor(predicate, mtclient.DefaultLayer)
}

func decode(raw []byte) (mtproto.TLObject, error) {
	d := mtproto.NewDecodeBuf(raw)
	o := d.Object()
	if err := d.GetError(); err != nil {
		return nil, fmt.Errorf("tgbot: decode: %w", err)
	}
	return o, nil
}

// migrateTo reads the datacenter of an error such as USER_MIGRATE_4.
func migrateTo(err error, prefix string) (int32, bool) {
	var rpc *mtclient.RPCError
	if !errors.As(err, &rpc) || !strings.HasPrefix(rpc.Message, prefix) {
		return 0, false
	}
	n, perr := strconv.ParseInt(strings.TrimPrefix(rpc.Message, prefix), 10, 32)
	if perr != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}

// floodWait reads the wait of a FLOOD_WAIT_X error.
func floodWait(err error) (time.Duration, bool) {
	var rpc *mtclient.RPCError
	if !errors.As(err, &rpc) || !strings.HasPrefix(rpc.Message, "FLOOD_WAIT_") {
		return 0, false
	}
	n, perr := strconv.Atoi(strings.TrimPrefix(rpc.Message, "FLOOD_WAIT_"))
	if perr != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}
