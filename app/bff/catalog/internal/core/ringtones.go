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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
)

// The notification sounds of an account (task 084, RPCRingtone): the list
// is in pkg/ringtones/saved, the documents in media.

// savedRingtones is the list of the account as account.savedRingtones: the
// documents in the order of the list, the newest first, and the hash of
// their ids (core.telegram.org/api/offsets#hash-generation). A document
// media no longer has is left out.
func (c *CatalogCore) savedRingtones() (*mtproto.Account_SavedRingtones, error) {
	entries, err := c.svcCtx.Ringtones.Saved(c.ctx, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	documents := make([]*mtproto.Document, 0, len(entries))
	if len(entries) > 0 {
		ids := make([]int64, len(entries))
		for i, e := range entries {
			ids[i] = e.DocumentID
		}
		list, err := c.svcCtx.Media.MediaGetDocumentList(c.ctx, &media.TLMediaGetDocumentList{IdList: ids})
		if err != nil {
			return nil, err
		}
		byId := make(map[int64]*mtproto.Document, len(list.GetDatas()))
		for _, d := range list.GetDatas() {
			if d.GetPredicateName() == mtproto.Predicate_document {
				byId[d.GetId()] = d
			}
		}
		for _, id := range ids {
			if d, ok := byId[id]; ok {
				documents = append(documents, d)
			}
		}
	}
	var hash int64
	for _, d := range documents {
		hash = appearance.CalcHash(hash, d.GetId())
	}
	return mtproto.MakeTLAccountSavedRingtones(&mtproto.Account_SavedRingtones{
		Hash:      hash,
		Ringtones: documents,
	}).To_Account_SavedRingtones(), nil
}

// ringtoneDocument is the document id names if it may be a notification
// sound as it is: an MP3 within the limits, as Android checks before it
// offers to save a sound of a chat (MediaDataController.java:8801-8811).
// RINGTONE_INVALID otherwise, also for a document the id and access hash
// do not name.
func (c *CatalogCore) ringtoneDocument(id *mtproto.InputDocument) (*mtproto.Document, error) {
	if id.GetPredicateName() != mtproto.Predicate_inputDocument || id.GetId() == 0 {
		return nil, ringtones.ErrInvalid
	}
	d, err := c.svcCtx.Media.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: id.GetId()})
	if err != nil {
		return nil, err
	}
	if d.GetPredicateName() != mtproto.Predicate_document || d.GetAccessHash() != id.GetAccessHash() {
		return nil, ringtones.ErrInvalid
	}
	size := max(d.GetSize2(), d.GetSize2_INT64(), int64(d.GetSize2_INT32()))
	if !ringtones.IsMP3(d.GetMimeType()) || size <= 0 || size > ringtones.SizeMax {
		return nil, ringtones.ErrInvalid
	}
	audio := false
	for _, a := range d.GetAttributes() {
		if a.GetPredicateName() != mtproto.Predicate_documentAttributeAudio {
			continue
		}
		audio = true
		if max(a.GetDuration(), a.GetDuration_FLOAT64(), float64(a.GetDuration_INT32())) > ringtones.DurationMax {
			return nil, ringtones.ErrInvalid
		}
	}
	if !audio {
		return nil, ringtones.ErrInvalid
	}
	return d, nil
}
