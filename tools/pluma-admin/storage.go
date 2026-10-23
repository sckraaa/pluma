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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/garage"
)

// dfsStorage is the part of the dfs config storage init needs.
type dfsStorage struct {
	Minio struct {
		AccessKeyID     string
		SecretAccessKey string
		Bucket          struct {
			Documents      string `json:",default=documents"`      //nolint:staticcheck // a go-zero conf option, not encoding/json's
			Photos         string `json:",default=photos"`         //nolint:staticcheck // a go-zero conf option, not encoding/json's
			Videos         string `json:",default=videos"`         //nolint:staticcheck // a go-zero conf option, not encoding/json's
			EncryptedFiles string `json:",default=encryptedfiles"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
		}
	}
}

// storageCommand runs storage init: prepares Garage for dfs (ADR 0014).
// The config file is dfs's; the admin API comes from -admin and
// GARAGE_ADMIN_TOKEN.
func storageCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 || args[1] != "init" {
		return errUsage
	}
	flags := flag.NewFlagSet("storage init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	adminURL := flags.String("admin", "http://garage:3903", "Garage admin API")
	wait := flags.Duration("wait", 0, "wait this long for the admin API")
	capacity := flags.Int64("capacity", 1<<40, "capacity of the node in bytes (a weight for one node)")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	token := os.Getenv("GARAGE_ADMIN_TOKEN")
	if token == "" {
		return errors.New("GARAGE_ADMIN_TOKEN is not set")
	}
	var c dfsStorage
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	m := c.Minio

	ctx, cancel := context.WithTimeout(context.Background(), *wait+5*time.Minute)
	defer cancel()
	admin := garage.New(*adminURL, token)
	if err := admin.Wait(ctx, *wait); err != nil {
		return err
	}
	if changed, err := admin.EnsureLayout(ctx, "dc1", *capacity); err != nil {
		return err
	} else if changed {
		fmt.Fprintln(stdout, "storage: node layout applied")
	}
	if changed, err := admin.EnsureKey(ctx, m.AccessKeyID, m.SecretAccessKey, "dfs"); err != nil {
		return err
	} else if changed {
		fmt.Fprintf(stdout, "storage: key %s imported\n", m.AccessKeyID)
	}
	for _, b := range []string{m.Bucket.Documents, m.Bucket.Photos, m.Bucket.Videos, m.Bucket.EncryptedFiles} {
		created, err := admin.EnsureBucket(ctx, b, m.AccessKeyID)
		if err != nil {
			return fmt.Errorf("bucket %s: %w", b, err)
		}
		if created {
			fmt.Fprintf(stdout, "storage: bucket %s created\n", b)
		}
	}
	fmt.Fprintln(stdout, "storage: ready")
	return nil
}
