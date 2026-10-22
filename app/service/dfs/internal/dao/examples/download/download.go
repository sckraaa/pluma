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

// download is a manual tool for the dfs DAO: it reads back a file stored as
// upload parts (see ../upload), checks that the streaming reader and ReadAll
// return the same bytes and writes the file out. Run it against a dev
// environment:
//
//	go run ./app/service/dfs/internal/dao/examples/download -f etc/dfs.yaml -out test001.jpeg
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"io"
	"os"

	"github.com/teamgram/marmota/pkg/commands"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/config"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/dao"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	configFile = flag.String("f", "etc/dfs.yaml", "the config file")
	outputFile = flag.String("out", "test001.jpeg", "where to write the file")
	ownerId    = flag.Int64("owner", 7997959636588162716, "owner (creator) id of the file")
	fileId     = flag.Int64("id", -8695032368284712706, "file id")
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

	r, err := s.dao.OpenFile(context.Background(), *ownerId, *fileId, 1)
	if err != nil {
		logx.Errorf("open error: %v", err)
		return
	}

	b, err := io.ReadAll(r)
	if err != nil {
		logx.Errorf("read error: %v", err)
		return
	}

	b2, err := r.ReadAll(context.Background())
	if err != nil {
		logx.Errorf("readAll error: %v", err)
		return
	}

	if !bytes.Equal(b, b2) {
		logx.Errorf("stream and ReadAll differ: %d vs %d bytes", len(b), len(b2))
		return
	}
	logx.Infof("read %d bytes, sha256 %x", len(b), sha256.Sum256(b))
	if err = os.WriteFile(*outputFile, b, 0o600); err != nil {
		logx.Errorf("write error: %v", err)
	}
}

func (s *Server) Destroy() {
	// s.dao.Close()
}
