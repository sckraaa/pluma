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

package testinfra

import (
	"bytes"
	"context"
	"path"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TileserverGLImage is the renderer the maps service is built on
// (teamgramd/deploy/maps/Dockerfile, ADR 0063, p. 3), pinned by the digest of
// its multi-arch index. It is not part of ComposeFile: production runs an
// image built on it. Keep it equal to the FROM of that Dockerfile.
const TileserverGLImage = "maptiler/tileserver-gl:v5.7.0@sha256:24d0364e1d40da0ae3fb3269f617c26e36a883ad4b7454e5c4064a8795bfc807"

// TileserverGL is a running tileserver-gl.
type TileserverGL struct {
	Container *testcontainers.DockerContainer
	// URL is the base of its HTTP API, as http://host:port.
	URL string
}

// StartTileserverGL starts tileserver-gl with files (name → content) in its
// data directory /data, among them config.json, which it is started with.
// Paths in the config are relative to /data.
func StartTileserverGL(ctx context.Context, files map[string][]byte) (*TileserverGL, error) {
	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithCmd("--config", "/data/config.json"),
		testcontainers.WithWaitStrategyAndDeadline(2*time.Minute,
			wait.ForHTTP("/health").WithPort("8080/tcp"),
		),
	}
	for name, content := range files {
		opts = append(opts, testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            bytes.NewReader(content),
			ContainerFilePath: path.Join("/data", name),
			FileMode:          0o644,
		}))
	}
	ctr, err := runImage(ctx, TileserverGLImage, opts...)
	if err != nil {
		return nil, startError("tileserver-gl", ctr, err)
	}
	t := &TileserverGL{Container: ctr}
	if t.URL, err = ctr.PortEndpoint(ctx, "8080/tcp", "http"); err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	return t, nil
}

// Terminate stops and removes the container.
func (t *TileserverGL) Terminate(ctx context.Context) error {
	if t == nil || t.Container == nil {
		return nil
	}
	return t.Container.Terminate(ctx)
}
