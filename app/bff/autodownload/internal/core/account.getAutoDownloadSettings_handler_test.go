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

package core

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// TestAutoDownloadVideoUploadMaxBitrate checks the video upload quality the
// presets give the clients: the Android defaults, never 0 (which iOS turns
// into its 480p preset).
func TestAutoDownloadVideoUploadMaxBitrate(t *testing.T) {
	r, err := New(context.Background(), nil).AccountGetAutoDownloadSettings(&mtproto.TLAccountGetAutoDownloadSettings{})
	if err != nil {
		t.Fatalf("AccountGetAutoDownloadSettings: %v", err)
	}

	for _, tc := range []struct {
		name string
		set  *mtproto.AutoDownloadSettings
		want int32
	}{
		{"low", r.GetLow(), 50},
		{"medium", r.GetMedium(), 100},
		{"high", r.GetHigh(), 100},
	} {
		if got := tc.set.GetVideoUploadMaxbitrate(); got != tc.want {
			t.Errorf("%s: video_upload_maxbitrate = %d, want %d", tc.name, got, tc.want)
		}
	}

	// The value survives the TL encoding the clients read.
	x := mtproto.NewEncodeBuf(512)
	if err := r.Encode(x, 228); err != nil {
		t.Fatalf("encode: %v", err)
	}
	d := mtproto.NewDecodeBuf(x.GetBuf())
	back := &mtproto.Account_AutoDownloadSettings{}
	if err := back.Decode(d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := back.GetHigh().GetVideoUploadMaxbitrate(); got != 100 {
		t.Errorf("decoded high video_upload_maxbitrate = %d, want 100", got)
	}
}
