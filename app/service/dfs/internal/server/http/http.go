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

package http

import (
	"net/http"
	"time"

	"github.com/teamgram/teamgram-server/app/service/dfs/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// New starts the loopback HTTP server that gives dfs's own ffmpeg the
// uploads it transcodes (task 057). go-zero's timeout middleware would hold
// the whole response in memory until the handler returns, a whole video for
// ffmpeg's first read: it is off, and c.Timeout bounds each response as the
// server's write deadline instead, which streams.
func New(ctx *svc.ServiceContext, c rest.RestConf) *rest.Server {
	timeout := time.Duration(c.Timeout) * time.Millisecond
	c.Middlewares.Timeout = false
	srv := rest.MustNewServer(c)

	go func() {
		defer srv.Stop()

		srv.AddRoutes([]rest.Route{
			{
				Method:  http.MethodGet,
				Path:    "/dfs/file/:file",
				Handler: GetDfsFile(ctx),
			},
		})

		srv.StartWithOpts(func(s *http.Server) {
			s.ReadHeaderTimeout = 10 * time.Second
			s.WriteTimeout = timeout
		})
	}()
	return srv
}
