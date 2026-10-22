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
// Author: teamgramio (teamgram.io@gmail.com)
//

package ffmpegutil

import (
	"bytes"
	"image/jpeg"
	"os/exec"
	"testing"
)

// TestConvertGifToMp4 runs the GIF upload pipeline used by dfs
// (uploadGifDocumentMedia, uploadProfilePhotoFileV2): GIF -> mp4 through
// pipes, first frame -> JPEG, mp4 metadata. It needs the ffmpeg and ffprobe
// binaries (with libx264, as in the production image).
func TestConvertGifToMp4(t *testing.T) {
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed: %v", bin, err)
		}
	}

	ffmpegUtil := NewFFmpegUtil()

	// safe_image.gif: 705x591, 5.4 s.
	data, duration, err := ffmpegUtil.ConvertToMp4ByPipe("./safe_image.gif", 320, -2)
	if err != nil {
		t.Fatalf("ConvertToMp4ByPipe: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ConvertToMp4ByPipe: empty mp4")
	}
	if duration < 5 || duration > 6 {
		t.Errorf("ConvertToMp4ByPipe: duration = %d, want 5..6", duration)
	}

	frame, err := ffmpegUtil.GetFirstFrameByPipe(data)
	if err != nil {
		t.Fatalf("GetFirstFrameByPipe: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("first frame is not a JPEG: %v", err)
	}
	if cfg.Width != 320 {
		t.Errorf("first frame width = %d, want 320", cfg.Width)
	}

	md, err := ffmpegUtil.GetMetadataByPipe(data)
	if err != nil {
		t.Fatalf("GetMetadataByPipe: %v", err)
	}
	w, h := GetWidthHeight(md)
	if w != 320 || h <= 0 || h%2 != 0 {
		t.Errorf("mp4 size = %dx%d, want 320 x even height", w, h)
	}
}
