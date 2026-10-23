// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

// upload is a manual tool for the dfs DAO: it writes a local file as upload
// parts to the temporary part storage (Redis) and records its file info, the
// way upload.saveFilePart does. Run it against a dev environment:
//
//	go run ./app/service/dfs/internal/dao/examples/upload -f etc/dfs.yaml -in image.jpeg
package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"github.com/teamgram/marmota/pkg/commands"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/config"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

const filePartSize = 10240

var (
	configFile = flag.String("f", "etc/dfs.yaml", "the config file")
	inputFile  = flag.String("in", "test001.jpeg", "the file to upload")
	ownerId    = flag.Int64("owner", 100, "owner (creator) id of the file")
	fileId     = flag.Int64("id", 1, "file id")
)

func main() {
	commands.Run(New())
}

type Server struct {
	dao *dao.Dao
}

func New() *Server {
	s := new(Server)
	return s
}

func (s *Server) Initialize() error {
	var c config.Config
	conf.MustLoad(*configFile, &c)

	// The config carries secrets (passwords, keys): never log it whole.
	logx.Infof("loaded config %s", *configFile)
	s.dao = dao.New(c)
	return nil
}

func (s *Server) RunLoop() {
	defer func() {
		s.Destroy()
		os.Exit(0)
	}()

	buf, err := os.ReadFile(*inputFile)
	if err != nil {
		logx.Errorf("open error: %v", err)
		return
	}

	szParts := len(buf) / filePartSize
	lastSize := len(buf) % filePartSize
	logx.Infof("uploading %s: size = %d, full parts = %d, last part = %d", *inputFile, len(buf), szParts, lastSize)

	ctx := context.Background()
	for i := 0; i < szParts; i++ {
		if err = s.dao.WriteFilePartData(ctx, *ownerId, *fileId, int32(i), buf[i*filePartSize:(i+1)*filePartSize]); err != nil {
			logx.Errorf("write part %d error: %v", i, err)
			return
		}
	}
	totalParts := szParts
	if lastSize > 0 {
		if err = s.dao.WriteFilePartData(ctx, *ownerId, *fileId, int32(szParts), buf[szParts*filePartSize:]); err != nil {
			logx.Errorf("write part %d error: %v", szParts, err)
			return
		}
		totalParts++
	}

	if err = s.dao.SetFileInfo(ctx, &model.DfsFileInfo{
		Creator:        *ownerId,
		FileId:         *fileId,
		Big:            false,
		FileName:       filepath.Base(*inputFile),
		FileTotalParts: totalParts,
		FilePartSize:   filePartSize,
	}); err != nil {
		logx.Errorf("set file info error: %v", err)
		return
	}
	logx.Infof("uploaded %s as (%d, %d) in %d parts", *inputFile, *ownerId, *fileId, totalParts)
}

func (s *Server) Destroy() {
	// s.dao.Close()
}
