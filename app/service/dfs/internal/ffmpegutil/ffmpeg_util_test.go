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
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/goffmpeg/models"
)

// needFFmpeg skips the test when the ffmpeg and ffprobe binaries (with
// libx264, as in the production image) are not installed.
func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed: %v", bin, err)
		}
	}
}

// makeInput renders a test input with ffmpeg's lavfi sources.
func makeInput(t *testing.T, name string, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("ffmpeg", append(append([]string{"-v", "error", "-y"}, args...), path)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make %s: %v\n%s", name, err, out)
	}
	return path
}

// checkX264 checks that md is a silent H.264 High profile, yuv420p mp4 of
// w x h.
func checkX264(t *testing.T, md *models.Metadata, w, h int) {
	t.Helper()
	var video int
	for _, s := range md.Streams {
		switch s.CodecType {
		case "video":
			video++
			if s.CodecName != "h264" || s.Profile != "High" || s.PixFmt != "yuv420p" {
				t.Errorf("video stream = %s/%s/%s, want h264/High/yuv420p", s.CodecName, s.Profile, s.PixFmt)
			}
			if s.Width != w || s.Height != h {
				t.Errorf("video size = %dx%d, want %dx%d", s.Width, s.Height, w, h)
			}
		case "audio":
			t.Errorf("audio stream %s present, want a silent mp4", s.CodecName)
		}
	}
	if video != 1 {
		t.Errorf("%d video streams, want 1", video)
	}
}

// TestConvertGifToMp4 runs the GIF upload pipeline used by dfs
// (uploadGifDocumentMedia): GIF -> mp4 through pipes, first frame -> JPEG,
// mp4 metadata.
func TestConvertGifToMp4(t *testing.T) {
	needFFmpeg(t)
	ffmpegUtil := NewFFmpegUtil()

	// safe_image.gif: 705x591, 5.4 s. It fits gifMaxSide, so it keeps its
	// size, made even.
	data, duration, err := ffmpegUtil.ConvertGifToMp4ByPipe("./safe_image.gif")
	if err != nil {
		t.Fatalf("ConvertGifToMp4ByPipe: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ConvertGifToMp4ByPipe: empty mp4")
	}
	if duration < 5 || duration > 6 {
		t.Errorf("ConvertGifToMp4ByPipe: duration = %d, want 5..6", duration)
	}

	frame, err := ffmpegUtil.GetFirstFrameByPipe(data)
	if err != nil {
		t.Fatalf("GetFirstFrameByPipe: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("first frame is not a JPEG: %v", err)
	}
	if cfg.Width != 704 || cfg.Height != 590 {
		t.Errorf("first frame size = %dx%d, want 704x590", cfg.Width, cfg.Height)
	}

	md, err := ffmpegUtil.GetMetadataByPipe(data)
	if err != nil {
		t.Fatalf("GetMetadataByPipe: %v", err)
	}
	checkX264(t, md, 704, 590)
}

// TestConvertGifToMp4MaxSide checks that a GIF larger than gifMaxSide is
// shrunk to it on its longer side, in both orientations, with even sides.
func TestConvertGifToMp4MaxSide(t *testing.T) {
	needFFmpeg(t)
	ffmpegUtil := NewFFmpegUtil()

	for _, tc := range []struct {
		name string
		size string
		w, h int
	}{
		{"landscape", "1000x400", 720, 288},
		{"portrait", "301x1001", 216, 720},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gif := makeInput(t, "in.gif", "-f", "lavfi", "-i", "testsrc=size="+tc.size+":rate=10:duration=1")
			data, _, err := ffmpegUtil.ConvertGifToMp4ByPipe(gif)
			if err != nil {
				t.Fatalf("ConvertGifToMp4ByPipe: %v", err)
			}
			md, err := ffmpegUtil.GetMetadataByPipe(data)
			if err != nil {
				t.Fatalf("GetMetadataByPipe: %v", err)
			}
			checkX264(t, md, tc.w, tc.h)
		})
	}
}

// TestConvertProfileVideo runs the profile video conversion of
// uploadProfilePhotoFileV2: any video -> a silent 800x800 H.264 mp4.
func TestConvertProfileVideo(t *testing.T) {
	needFFmpeg(t)
	ffmpegUtil := NewFFmpegUtil()

	src := makeInput(t, "in.mp4",
		"-f", "lavfi", "-i", "testsrc2=size=640x640:rate=30:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")

	data, duration, err := ffmpegUtil.ConvertToMp4ByPipe(src, 800, 800)
	if err != nil {
		t.Fatalf("ConvertToMp4ByPipe: %v", err)
	}
	if duration != 2 {
		t.Errorf("duration = %d, want 2", duration)
	}
	md, err := ffmpegUtil.GetMetadataByPipe(data)
	if err != nil {
		t.Fatalf("GetMetadataByPipe: %v", err)
	}
	checkX264(t, md, 800, 800)
}

// TestX264Command pins the encoder options of the conversions: CRF-driven
// quality with a VBV cap instead of a fixed bitrate.
func TestX264Command(t *testing.T) {
	mf := &models.Mediafile{}
	setupX264(mf)
	got := " " + strings.Join(mf.ToStrCommand(), " ") + " "
	for _, want := range []string{
		" -c:v libx264 ", " -maxrate 4000k ", " -profile:v high ", " -an ",
		" -crf 18 ", " -bufsize 8000k ", " -preset slow ", " -pix_fmt yuv420p ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("command %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, " -b:v ") {
		t.Errorf("command %q sets a fixed bitrate", got)
	}
}

// TestFirstFrameQuality checks that the extracted first frame is close to the
// lossless frame: it is the source the thumbnails and the profile photo sizes
// are encoded from again.
func TestFirstFrameQuality(t *testing.T) {
	needFFmpeg(t)
	ffmpegUtil := NewFFmpegUtil()

	frame, err := ffmpegUtil.GetFirstFrame("./safe_image.gif")
	if err != nil {
		t.Fatalf("GetFirstFrame: %v", err)
	}
	got, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("first frame is not a JPEG: %v", err)
	}

	ref := makeInput(t, "ref.png", "-i", "./safe_image.gif", "-vframes", "1", "-c:v", "png", "-f", "image2")
	want, err := decodePNG(ref)
	if err != nil {
		t.Fatalf("decode reference frame: %v", err)
	}

	// ffmpeg's default mjpeg quality gives about 43 dB on this frame, the
	// best quality scale about 48 dB.
	if p := psnr(t, got, want); p < 46 {
		t.Errorf("first frame PSNR = %.1f dB, want >= 46", p)
	}
}

func decodePNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// psnr is the peak signal-to-noise ratio of a against b over the RGB
// channels, in dB.
func psnr(t *testing.T, a, b image.Image) float64 {
	t.Helper()
	if a.Bounds().Size() != b.Bounds().Size() {
		t.Fatalf("frame sizes differ: %v vs %v", a.Bounds(), b.Bounds())
	}
	var sum, n float64
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			r1, g1, b1, _ := a.At(a.Bounds().Min.X+x, a.Bounds().Min.Y+y).RGBA()
			r2, g2, b2, _ := b.At(b.Bounds().Min.X+x, b.Bounds().Min.Y+y).RGBA()
			for _, d := range []float64{
				float64(r1>>8) - float64(r2>>8),
				float64(g1>>8) - float64(g2>>8),
				float64(b1>>8) - float64(b2>>8),
			} {
				sum += d * d
				n++
			}
		}
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/n))
}
