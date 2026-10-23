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

//go:build integration

package garage_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/garage"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// TestPrepareAndUse prepares a fresh Garage as storage-init does, twice,
// then makes the S3 calls dfs makes: put, ranged get, multipart upload.
func TestPrepareAndUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	g, err := testinfra.StartGarage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Container.Terminate(context.Background()) })

	admin := garage.New(g.AdminURL, g.AdminToken)
	if err := admin.Wait(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	id, secret := testinfra.NewS3Key()
	buckets := []string{"documents", "photos", "videos", "encryptedfiles"}
	for round := 0; round < 2; round++ {
		changed, err := admin.EnsureLayout(ctx, "dc1", 1<<40)
		if err != nil || changed != (round == 0) {
			t.Fatalf("round %d layout: changed %v, %v", round, changed, err)
		}
		changed, err = admin.EnsureKey(ctx, id, secret, "dfs")
		if err != nil || changed != (round == 0) {
			t.Fatalf("round %d key: changed %v, %v", round, changed, err)
		}
		for _, b := range buckets {
			created, err := admin.EnsureBucket(ctx, b, id)
			if err != nil || created != (round == 0) {
				t.Fatalf("round %d bucket %s: created %v, %v", round, b, created, err)
			}
		}
	}
	if _, err := admin.EnsureKey(ctx, id, strings.Repeat("0", 64), "dfs"); err == nil {
		t.Fatal("the key with another secret accepted")
	}

	// The calls of dfs (app/service/dfs/internal/minio_util), region as in
	// the dfs config.
	core, err := minio.NewCore(g.S3Endpoint, &minio.Options{Creds: credentials.NewStaticV4(id, secret, ""), Region: "garage"})
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("pluma"), 1000)
	if _, err := core.PutObject(ctx, "photos", "a/b.jpg", bytes.NewReader(data), int64(len(data)), "", "", minio.PutObjectOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(5, 9); err != nil {
		t.Fatal(err)
	}
	r, _, _, err := core.GetObject(ctx, "photos", "a/b.jpg", opts)
	if err != nil {
		t.Fatalf("ranged get: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "pluma" {
		t.Fatalf("ranged get returned %q", got)
	}

	uploadID, err := core.NewMultipartUpload(ctx, "videos", "v.mp4", minio.PutObjectOptions{})
	if err != nil {
		t.Fatalf("new multipart: %v", err)
	}
	part := bytes.Repeat([]byte{7}, 5<<20)
	var parts []minio.CompletePart
	for i := 1; i <= 2; i++ {
		p, err := core.PutObjectPart(ctx, "videos", "v.mp4", uploadID, i, bytes.NewReader(part), int64(len(part)), "", "", nil)
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		parts = append(parts, minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	if _, err := core.CompleteMultipartUpload(ctx, "videos", "v.mp4", uploadID, parts, minio.PutObjectOptions{}); err != nil {
		t.Fatalf("complete multipart: %v", err)
	}
	info, err := core.StatObject(ctx, "videos", "v.mp4", minio.StatObjectOptions{})
	if err != nil || info.Size != int64(2*len(part)) {
		t.Fatalf("multipart object: size %d, %v", info.Size, err)
	}
}
