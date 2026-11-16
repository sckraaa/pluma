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

// Package nodekey keeps the MTProto RSA key of a CDN node (ADR 0065 §5):
// 2048 bits, made on the node at its first start in its data directory
// (mode 0600) and never sent anywhere but its public part. The home's own
// server key is never put on the node.
package nodekey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// FileName is the key's file in the data directory.
const FileName = "rsa.key"

// Key is the node's key with its public part as help.getCdnConfig hands it
// to clients.
type Key struct {
	*serverkey.Key
	// PublicPEM is the public key, PKCS#1 PEM: what Android reads with
	// PEM_read_bio_RSAPublicKey (Handshake.cpp:1032) and tdesktop and iOS
	// take from cdnPublicKey.public_key.
	PublicPEM []byte
}

// LoadOrCreate loads the key from dir, or makes it there when the file does
// not exist. An existing file is checked as the gateway checks its key
// (serverkey.Load: mode, size, format); a damaged one stops the node rather
// than being replaced, since clients pinned to the old key would need the
// home to learn the new one first.
func LoadOrCreate(dir string) (*Key, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := create(path); err != nil {
			return nil, err
		}
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("nodekey: %w", err)
	}
	priv, err := serverkey.ParsePrivateKeyPEM(data)
	if err != nil {
		return nil, fmt.Errorf("nodekey: %s: %w", path, err)
	}
	k, err := serverkey.Load(path, strconv.FormatUint(serverkey.Fingerprint(&priv.PublicKey), 10))
	if err != nil {
		return nil, fmt.Errorf("nodekey: %w", err)
	}
	return &Key{
		Key:       k,
		PublicPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&k.Private.PublicKey)}),
	}, nil
}

// create writes a new key to path, never over an existing file.
func create(path string) error {
	key, err := rsa.GenerateKey(rand.Reader, serverkey.Bits)
	if err != nil {
		return fmt.Errorf("nodekey: generate: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("nodekey: %w", err)
	}
	// A temporary file renamed in place: a crash never leaves a half-written
	// key that the next start would refuse.
	f, err := os.CreateTemp(filepath.Dir(path), FileName+".*")
	if err != nil {
		return fmt.Errorf("nodekey: %w", err)
	}
	tmp := f.Name()
	err = f.Chmod(0o600)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Link fails if a key appeared meanwhile (another process): that
		// key wins.
		err = os.Link(tmp, path)
		if errors.Is(err, fs.ErrExist) {
			err = nil
		}
	}
	_ = os.Remove(tmp)
	if err != nil {
		return fmt.Errorf("nodekey: write %s: %w", path, err)
	}
	return nil
}
