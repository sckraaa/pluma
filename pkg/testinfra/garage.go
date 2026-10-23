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
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Garage is a running Garage object store with the production config
// (teamgramd/deploy/garage/garage.toml), not yet prepared: the tests run
// pkg/garage on it as the storage-init service does.
type Garage struct {
	Container  *testcontainers.DockerContainer
	AdminURL   string
	AdminToken string
	S3Endpoint string // host:port
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// StartGarage starts the Garage image of docker-compose-env.yaml.
func StartGarage(ctx context.Context) (*Garage, error) {
	root, err := RepoRoot()
	if err != nil {
		return nil, err
	}
	g := &Garage{AdminToken: randomHex(32)}
	ctr, err := runService(ctx, "garage",
		testcontainers.WithExposedPorts("3900/tcp", "3903/tcp"),
		testcontainers.WithEnv(map[string]string{
			"GARAGE_RPC_SECRET":  randomHex(32),
			"GARAGE_ADMIN_TOKEN": g.AdminToken,
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      filepath.Join(root, "teamgramd/deploy/garage/garage.toml"),
			ContainerFilePath: "/etc/garage.toml",
			FileMode:          0o644,
		}),
		testcontainers.WithWaitStrategyAndDeadline(time.Minute,
			wait.ForListeningPort("3900/tcp"),
			wait.ForListeningPort("3903/tcp"),
		),
	)
	if err != nil {
		return nil, startError("garage", ctr, err)
	}
	g.Container = ctr
	admin, err := ctr.PortEndpoint(ctx, "3903/tcp", "http")
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	s3, err := ctr.PortEndpoint(ctx, "3900/tcp", "")
	if err != nil {
		_ = ctr.Terminate(ctx)
		return nil, err
	}
	g.AdminURL, g.S3Endpoint = admin, s3
	return g, nil
}

// NewS3Key returns an access key ID and secret in Garage's format.
func NewS3Key() (id, secret string) {
	return "GK" + randomHex(12), randomHex(32)
}

func (g *Garage) String() string {
	return fmt.Sprintf("garage(s3=%s admin=%s)", g.S3Endpoint, g.AdminURL)
}
