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
	"math"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/autodownload"
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
		{"medium", r.GetMedium(), 85},
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

// The presets give 720p on mobile data and 1080p on Wi-Fi in both clients that
// compress: iOS preset v*5/100 (4 is 720p, 5 is 1080p), Android step
// round(v / (100 / steps)) - 1 of the steps a source allows.
func TestVideoUploadPresets(t *testing.T) {
	ios := func(v int) int { return v * 5 / 100 }
	android := func(v, steps int) int { return int(math.Round(float64(v)/(100/float64(steps)))) - 1 }
	presets := autodownload.Presets()
	videoUploadMaxBitrateMedium := int(presets[autodownload.Medium].VideoUploadMaxbitrate)
	videoUploadMaxBitrateHigh := int(presets[autodownload.High].VideoUploadMaxbitrate)
	if got := ios(videoUploadMaxBitrateMedium); got != 4 {
		t.Errorf("iOS on mobile data: preset %d, want 4 (720p)", got)
	}
	if got := ios(videoUploadMaxBitrateHigh); got != 5 {
		t.Errorf("iOS on Wi-Fi: preset %d, want 5 (1080p)", got)
	}
	// Steps 360, 480, 720, 1080 of a 1080p source; fewer for smaller ones.
	for steps, want := range map[int]int{4: 2, 3: 2, 2: 1} {
		if got := android(videoUploadMaxBitrateMedium, steps); got != want {
			t.Errorf("Android on mobile data, %d steps: step %d, want %d", steps, got, want)
		}
	}
	if got := android(videoUploadMaxBitrateHigh, 4); got != 3 {
		t.Errorf("Android on Wi-Fi: step %d, want 3 (1080p)", got)
	}
}
