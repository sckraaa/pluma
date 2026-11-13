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

package groupcalls

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// E2EDiag is what the server sees of the encryption of one participant
// (ADR 0060 §5): how it polls the chain, the epochs the SFU sees in its
// sound, the last status report of its client and the alarms it is in.
// Heights, prefixes of hashes and counters only; never a key, a nonce or a
// phone number. bff writes it, pluma-admin calls show reads it.
type E2EDiag struct {
	// Poll is the last phone.getGroupCallChainBlocks of each sub-chain.
	Poll [2]E2EPoll `json:"poll"`
	// SendEpochs are the epochs of the last sound frame the SFU saw, at
	// SendAt (unix ms).
	SendEpochs []string `json:"send_epochs,omitempty"`
	SendAt     int64    `json:"send_at,omitempty"`
	// Report is the last status report, at ReportAt (unix ms); ReportsLost
	// counts the reports missing by their seq since the join.
	Report      *tgcallsjson.E2EStatus `json:"report,omitempty"`
	ReportAt    int64                  `json:"report_at,omitempty"`
	ReportsLost int64                  `json:"reports_lost,omitempty"`
	// Rejoins is how many times the user joined in the last 5 minutes while
	// in the group state already: a new connection or a new key after a
	// failure (ADR 0060 §5).
	Rejoins int `json:"rejoins"`
	// Alarms are the conditions of ADR 0060 §5 the participant is in now.
	Alarms []string `json:"alarms,omitempty"`
}

// E2EPoll is one poll of a sub-chain: the offset asked and when (unix ms).
type E2EPoll struct {
	Offset int32 `json:"offset"`
	At     int64 `json:"at,omitempty"`
}

// SaveDiag stores the diagnostics of a participant of a call.
func (s *Store) SaveDiag(ctx context.Context, callId, userId int64, at time.Time, d *E2EDiag) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, "insert into group_call_e2e_diag (call_id, user_id, updated_at, diag) values (?, ?, ?, ?)"+
		" on duplicate key update updated_at = values(updated_at), diag = values(diag)", callId, userId, at.UnixMilli(), string(b))
	return err
}

// StoredDiag is the diagnostics of a participant as stored.
type StoredDiag struct {
	UserId int64
	// UpdatedAt is when bff wrote it (unix ms).
	UpdatedAt int64
	Diag      *E2EDiag
}

// Diags returns the diagnostics of the participants of a call, by user id.
func (s *Store) Diags(ctx context.Context, callId int64) ([]StoredDiag, error) {
	var rows []struct {
		UserId    int64  `db:"user_id"`
		UpdatedAt int64  `db:"updated_at"`
		Diag      string `db:"diag"`
	}
	err := s.db.QueryRowsPartial(ctx, &rows, "select user_id, updated_at, diag from group_call_e2e_diag where call_id = ? order by user_id", callId)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]StoredDiag, 0, len(rows))
	for _, r := range rows {
		d := new(E2EDiag)
		if err := json.Unmarshal([]byte(r.Diag), d); err != nil {
			return nil, err
		}
		out = append(out, StoredDiag{UserId: r.UserId, UpdatedAt: r.UpdatedAt, Diag: d})
	}
	return out, nil
}
