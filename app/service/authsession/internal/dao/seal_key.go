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

package dao

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
)

// Plain keys of before task 008 are sealed on their first read and by the
// key cleaner in rounds (SealPlainAuthKeys); a sealed key is never written
// back plain.

// sealPlainKey seals the plain body of authKeyId and stores it if the row
// still holds that body. It returns the sealed body.
func (d *Dao) sealPlainKey(ctx context.Context, authKeyId int64, plainBody string) (string, error) {
	key, sealed, err := d.keys.Open(authKeyId, plainBody)
	if err != nil {
		return "", err
	}
	if sealed {
		return plainBody, nil
	}
	body, err := d.keys.Seal(authKeyId, key)
	if err != nil {
		return "", err
	}
	if _, err = d.DB.Exec(ctx, "UPDATE auth_keys SET body = ? WHERE auth_key_id = ? AND body = ?", body, authKeyId, plainBody); err != nil {
		return "", err
	}
	return body, nil
}

type plainKeyRow struct {
	AuthKeyId int64  `db:"auth_key_id"`
	Body      string `db:"body"`
}

// SealPlainAuthKeys seals up to limit plain keys and returns how many it
// sealed.
func (d *Dao) SealPlainAuthKeys(ctx context.Context, limit int) (int, error) {
	var rows []plainKeyRow
	if err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT auth_key_id, body FROM auth_keys WHERE body NOT LIKE 'v1:%' LIMIT ?", limit); err != nil {
		return 0, err
	}
	var errs []error
	for _, r := range rows {
		if _, err := d.sealPlainKey(ctx, r.AuthKeyId, r.Body); err != nil {
			logx.WithContext(ctx).Errorf("sealPlainAuthKeys - key %d: %v", r.AuthKeyId, err)
			errs = append(errs, err)
		}
	}
	return len(rows) - len(errs), errors.Join(errs...)
}
