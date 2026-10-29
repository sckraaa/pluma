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

package main

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/teamgram/marmota/pkg/net/rpcx"
	"github.com/teamgram/proto/mtproto"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/conf"

	"github.com/zeromicro/go-zero/zrpc"
)

// serviceUserId is the service notifications account, "Pluma" (z_init.sql,
// z_pluma-service-account.sql).
const serviceUserId = 777000

// servicePhoto is Pluma's photo of the account (service-photo.svg).
//
//go:embed service-photo.jpg
var servicePhoto []byte

// photoPartSize is the size of the parts of an upload, as clients send them.
const photoPartSize = 512 * 1024

// photoServices is the way of a client's photos.uploadProfilePhoto: the
// parts of the file to dfs, the photo and its sizes made by media, the
// account's main photo changed by biz.
type photoServices interface {
	WritePart(ctx context.Context, owner, fileId int64, part int32, bytes []byte) error
	UploadProfilePhoto(ctx context.Context, owner int64, file *mtproto.InputFile) (*mtproto.Photo, error)
	SetProfilePhoto(ctx context.Context, userId, photoId int64) error
}

// photoConfig is the part of the bff config the command needs.
type photoConfig struct {
	BizServiceClient zrpc.RpcClientConf
	MediaClient      zrpc.RpcClientConf
	DfsClient        zrpc.RpcClientConf
}

type rpcPhotoServices struct {
	dfs   dfs_client.DfsClient
	media media_client.MediaClient
	user  user_client.UserClient
}

func (s *rpcPhotoServices) WritePart(ctx context.Context, owner, fileId int64, part int32, bytes []byte) error {
	_, err := s.dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{
		Creator:  owner,
		FileId:   fileId,
		FilePart: part,
		Bytes:    bytes,
	})
	return err
}

func (s *rpcPhotoServices) UploadProfilePhoto(ctx context.Context, owner int64, file *mtproto.InputFile) (*mtproto.Photo, error) {
	return s.media.MediaUploadProfilePhotoFile(ctx, &media.TLMediaUploadProfilePhotoFile{
		OwnerId: owner,
		File:    file,
	})
}

func (s *rpcPhotoServices) SetProfilePhoto(ctx context.Context, userId, photoId int64) error {
	_, err := s.user.UserUpdateProfilePhoto(ctx, &userpb.TLUserUpdateProfilePhoto{
		UserId:   userId,
		Id:       photoId,
		Uploaded: true,
	})
	return err
}

// servicePhotoCommand: service-photo [file] sets the profile photo of the
// service notifications account. Clients draw an account without a photo
// as a coloured letter, and Telegram's own account has a photo; without a
// file the command sets Pluma's. The new photo becomes the main one and the
// earlier ones stay in the account's album, as when a user sets a photo;
// biz sends updateUser to everyone who has the account among their chats.
// Run it in the bff container, where etcd finds the services.
func servicePhotoCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) > 2 {
		return errUsage
	}
	image, name := servicePhoto, "pluma.jpg"
	if len(args) == 2 {
		var err error
		if image, err = os.ReadFile(args[1]); err != nil {
			return err
		}
		name = "photo"
	}
	var c photoConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	services := &rpcPhotoServices{
		dfs:   dfs_client.NewDfsClient(rpcx.GetCachedRpcClient(c.DfsClient)),
		media: media_client.NewMediaClient(rpcx.GetCachedRpcClient(c.MediaClient)),
		user:  user_client.NewUserClient(rpcx.GetCachedRpcClient(c.BizServiceClient)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	photoId, err := setServicePhoto(ctx, services, image, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "The service account (%d) has photo %d now.\n", serviceUserId, photoId)
	return nil
}

// setServicePhoto uploads image as an anonymous owner, one id for this
// upload alone, and makes it the account's photo; it returns the photo id.
func setServicePhoto(ctx context.Context, s photoServices, image []byte, name string) (int64, error) {
	switch http.DetectContentType(image) {
	case "image/jpeg", "image/png":
	default:
		return 0, errors.New("service-photo: the file is not a JPEG or PNG image")
	}
	owner, err := randomId()
	if err != nil {
		return 0, err
	}
	fileId, err := randomId()
	if err != nil {
		return 0, err
	}
	parts := int32(0)
	for off := 0; off < len(image); off += photoPartSize {
		end := min(off+photoPartSize, len(image))
		if err := s.WritePart(ctx, owner, fileId, parts, image[off:end]); err != nil {
			return 0, fmt.Errorf("dfs: part %d: %w", parts, err)
		}
		parts++
	}
	sum := md5.Sum(image)
	photo, err := s.UploadProfilePhoto(ctx, owner, mtproto.MakeTLInputFile(&mtproto.InputFile{
		Id_INT64:    fileId,
		Parts:       parts,
		Name:        name,
		Md5Checksum: hex.EncodeToString(sum[:]),
	}).To_InputFile())
	if err != nil {
		return 0, fmt.Errorf("media: %w", err)
	}
	if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
		return 0, errors.New("media: no photo made of the file")
	}
	if err := s.SetProfilePhoto(ctx, serviceUserId, photo.GetId()); err != nil {
		return 0, fmt.Errorf("biz: %w", err)
	}
	return photo.GetId(), nil
}

// randomId is a positive random int64, as clients make file ids.
func randomId() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:]) >> 1), nil
}
