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
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/teamgram/teamgram-server/pkg/goffmpeg/models"
	"github.com/teamgram/teamgram-server/pkg/goffmpeg/transcoder"
	"github.com/teamgram/teamgram-server/pkg/goffmpeg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// FFmpegUtil runs the ffmpeg conversions dfs needs. It holds no state: every
// call drives its own transcoder, so the single instance kept in the service
// context is safe to use from concurrent requests.
type FFmpegUtil struct{}

func NewFFmpegUtil() *FFmpegUtil {
	return &FFmpegUtil{}
}

/*
	// ffmpeg -i safe_image.gif -f mp4 -movflags +faststart -pix_fmt yuv420p -vf scale=320:-1 -c:v libx264 -strict experimental -b:v 218k -bufsize 218k safe_image.gif.mp4
	// os.Setenv("PATH", "/usr/bin:/sbin:/usr/local/Cellar/ffmpeg/4.2.1_2/bin")
	o, err := exec.Command(
		"ffmpeg",
		"-f", "gif",
		"-i", gifPath,
		"-movflags", "+faststart",
		"-pix_fmt", "yuv420p",
		// "-s", "320x268",
		"-vf", "scale=320:-1",
		"-c:v", "libx264",
		// "-profile:v", "baseline",
		// "-x264opts", "cabac=0:bframes=0:ref=1:weightp=0:level=30:bitrate=700:vbv_maxrate=768:vbv_bufsize=1400",
		// "-pass", "1",
		"-strict", "experimental",
		"-b:v", "218k",
		"-bufsize", "218k",
		videoPath,
	).CombinedOutput()
*/

// newPipeTranscoder returns a fresh transcoder with an empty media file, to be
// fed through pipes.
func newPipeTranscoder() (*transcoder.Transcoder, error) {
	trans := new(transcoder.Transcoder)
	if err := trans.InitializeEmptyTranscoder(); err != nil {
		logx.Errorf("InitializeEmptyTranscoder - error: %v", err)
		return nil, err
	}
	return trans, nil
}

// readAllAsync reads r to the end in its own goroutine (ffmpeg blocks on a
// full output pipe, so it must be drained while the process runs). wait
// returns what was read.
func readAllAsync(r io.ReadCloser) (wait func() ([]byte, error)) {
	var (
		wg   sync.WaitGroup
		data []byte
		err  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer r.Close()
		data, err = io.ReadAll(r)
	}()
	return func() ([]byte, error) {
		wg.Wait()
		return data, err
	}
}

func (u *FFmpegUtil) ConvertToMp4(gifPath string) (err error) {
	trans := new(transcoder.Transcoder)
	videoPath := gifPath + ".mp4"

	// Initialize transcoder passing the input file path and output file path
	err = trans.Initialize(gifPath, videoPath)
	if err != nil {
		logx.Errorf("convertGifToMp4 - error: %v", err)
		return
	}

	// "-f gif"
	trans.MediaFile().SetOutputFormat("mp4")
	trans.MediaFile().SetMovFlags("+faststart")
	trans.MediaFile().SetPixFmt("yuv420p")

	w, h := GetWidthHeight(trans.MediaFile().GetMetadata())
	if w >= h {
		trans.MediaFile().SetVideoFilter("scale=320:-2")
	} else {
		trans.MediaFile().SetVideoFilter("scale=-2:320")
	}
	trans.MediaFile().SetVideoCodec("libx264")
	trans.MediaFile().SetStrict(-2)
	trans.MediaFile().SetVideoBitRate("218k")
	trans.MediaFile().SetBufferSize(218)

	done := trans.Run(true)
	// Drain the progress stream so that ffmpeg never blocks on stderr.
	for range trans.Output() {
	}

	// This channel is used to wait for the process to end
	err = <-done
	if err != nil {
		logx.Errorf("convertGifToMp4 - error: %v", err)
	}
	return
}

// convertToMp4ByPipe transcodes the file at path to an mp4 returned in
// memory; setup sets the encoding options. The duration (seconds) is taken
// from ffmpeg's progress reports.
func convertToMp4ByPipe(path string, setup func(mf *models.Mediafile)) ([]byte, int32, error) {
	trans, err := newPipeTranscoder()
	if err != nil {
		return nil, 0, err
	}

	trans.MediaFile().SetInputPath(path)
	trans.MediaFile().SetOutputFormat("mp4")
	setup(trans.MediaFile())

	r, err := trans.CreateOutputPipe("mp4")
	if err != nil {
		logx.Errorf("createOutputPipe error: %v", err)
		return nil, 0, err
	}
	output := readAllAsync(r)

	done := trans.Run(true)

	var duration int32
	for msg := range trans.Output() {
		duration = int32(math.Round(utils.DurToSec(msg.CurrentTime)))
	}

	// This channel is used to wait for the transcoding process to end
	if err = <-done; err != nil {
		logx.Errorf("transcoding error: %v", err)
		// The pipe is not closed when ffmpeg failed to start: unblock the reader.
		_ = r.Close()
		_, _ = output()
		return nil, 0, err
	}

	data, err := output()
	if err != nil {
		logx.Errorf("readPipe error: %v", err)
		return nil, 0, err
	}
	return data, duration, nil
}

// Settings of the H.264 videos dfs encodes itself: profile videos and GIFs
// converted to mp4.
//
// Quality is set by CRF instead of a bitrate: 18 is visually lossless for
// x264, where the conversions used the encoder default 23 before. The VBV cap
// bounds the size of noisy sources: 4 Mbit/s is about 1.5 times the highest
// bitrate an official client encodes a profile video with (Android 2.6 Mbit/s,
// iOS 2.4 Mbit/s), so ordinary sources stay well below it. The "slow" preset
// spends more time per frame for fewer bits at the same quality; the inputs are
// short (profile videos are at most 10 s) and small.
//
// A GIF keeps its own size up to gifMaxSide on its longer side (it was scaled
// to 320 before). Both outputs are silent, as Telegram's profile videos and
// GIFs are.
const (
	x264Preset   = "slow"
	x264CRF      = 18
	x264Profile  = "high"
	x264MaxRateK = 4000
	x264BufSizeK = 2 * x264MaxRateK
	x264PixFmt   = "yuv420p"

	gifMaxSide = 720

	// firstFrameQScale is the mjpeg quality scale of extracted first frames
	// (2 is the best useful value; ffmpeg's rate-controlled default is
	// visibly blocky). The frame is the source of the photo sizes of a profile
	// video and of the thumbnails of a GIF or a video, which are resized and
	// encoded again from it.
	firstFrameQScale = 2
)

// setupX264 sets the encoder options shared by the mp4 conversions.
func setupX264(mf *models.Mediafile) {
	mf.SetPixFmt(x264PixFmt)
	mf.SetVideoCodec("libx264")
	mf.SetPreset(x264Preset)
	mf.SetCRF(x264CRF)
	mf.SetVideoProfile(x264Profile)
	mf.SetVideoMaxBitrate(x264MaxRateK)
	mf.SetBufferSize(x264BufSizeK)
	mf.SetSkipAudio(true)
}

// gifScaleFilter keeps the size of a GIF, shrinking it only when its longer
// side exceeds gifMaxSide; both sides are made even, as yuv420p requires.
func gifScaleFilter() string {
	return fmt.Sprintf("scale='if(gte(iw,ih),trunc(min(iw,%[1]d)/2)*2,-2)':'if(gte(iw,ih),-2,trunc(min(ih,%[1]d)/2)*2)'", gifMaxSide)
}

// ConvertToMp4ByPipe transcodes the video at path to a silent H.264 mp4 of
// dstW x dstH (a negative side keeps the aspect ratio, as in ffmpeg's scale).
func (u *FFmpegUtil) ConvertToMp4ByPipe(path string, dstW, dstH int) (bytes []byte, duration int32, err error) {
	return convertToMp4ByPipe(path, func(mf *models.Mediafile) {
		setupX264(mf)
		mf.SetVideoFilter(fmt.Sprintf("scale=%d:%d", dstW, dstH))
	})
}

// ConvertGifToMp4ByPipe transcodes the GIF at path to a silent H.264 mp4 of
// the GIF's own size, its longer side limited to gifMaxSide.
func (u *FFmpegUtil) ConvertGifToMp4ByPipe(path string) (bytes []byte, duration int32, err error) {
	return convertToMp4ByPipe(path, func(mf *models.Mediafile) {
		setupX264(mf)
		mf.SetVideoFilter(gifScaleFilter())
	})
}

// getFirstFrame runs a prepared transcoder that outputs the first video frame
// as a JPEG image through a pipe; feed, when set, writes the input.
func getFirstFrame(trans *transcoder.Transcoder, feed func()) ([]byte, error) {
	trans.MediaFile().SetOutputFormat("image2")
	trans.MediaFile().SetVframes(1)
	trans.MediaFile().SetQScale(firstFrameQScale)

	r, err := trans.CreateOutputPipe("image2")
	if err != nil {
		logx.Errorf("createOutputPipe error: %v", err)
		return nil, err
	}
	output := readAllAsync(r)
	if feed != nil {
		feed()
	}

	// No progress is consumed: run without a stderr pipe.
	done := trans.Run(false)

	// This channel is used to wait for the transcoding process to end
	if err = <-done; err != nil {
		logx.Errorf("getFirstFrame error: %v", err)
		_ = r.Close()
		_, _ = output()
		return nil, err
	}

	data, err := output()
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		logx.Errorf("readPipe error: %v", err)
		return nil, err
	}
	return data, nil
}

// GetFirstFrame
// cmd := exec.Command("ffmpeg", "-i", filename, "-vframes", "1", "-s", fmt.Sprintf("%dx%d", width, height), "-f", "singlejpeg", "-")
func (u *FFmpegUtil) GetFirstFrame(iFilePath string) (bytes []byte, err error) {
	trans, err := newPipeTranscoder()
	if err != nil {
		return nil, err
	}
	trans.MediaFile().SetInputPath(iFilePath)

	return getFirstFrame(trans, nil)
}

// GetFirstFrameByPipe
// cmd := exec.Command("ffmpeg", "-i", filename, "-vframes", "1", "-s", fmt.Sprintf("%dx%d", width, height), "-f", "singlejpeg", "-")
func (u *FFmpegUtil) GetFirstFrameByPipe(iPipeData []byte) (bytes []byte, err error) {
	trans, err := newPipeTranscoder()
	if err != nil {
		return nil, err
	}

	// Create an input pipe to write to, which will return *io.PipeWriter
	w, err := trans.CreateInputPipe()
	if err != nil {
		logx.Errorf("createInputPipe error: %v", err)
		return nil, err
	}

	return getFirstFrame(trans, func() {
		go func() {
			defer w.Close()
			// ffmpeg stops reading once it has the first frame and the
			// transcoder then closes the pipe: ErrClosedPipe is expected.
			if _, err := w.Write(iPipeData); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				logx.Errorf("writePipe error: %v", err)
			}
		}()
	})
}

func (u *FFmpegUtil) GetMetadataByPipe(iPipeData []byte) (*models.Metadata, error) {
	trans := new(transcoder.Transcoder)
	if err := trans.GetMetadataByPipe(bytes.NewReader(iPipeData)); err != nil {
		return nil, err
	}
	return trans.MediaFile().GetMetadata(), nil
}

func (u *FFmpegUtil) GetMetadata(iFilePath string) (*models.Metadata, error) {
	trans := new(transcoder.Transcoder)
	if err := trans.GetMetadata(iFilePath); err != nil {
		return nil, err
	}
	return trans.MediaFile().GetMetadata(), nil
}

func GetDuration(md *models.Metadata) int {
	return int(math.Round(utils.DurToSec(md.Format.Duration)))
}

func GetWidthHeight(md *models.Metadata) (w, h int) {
	for i := 0; i < len(md.Streams); i++ {
		if md.Streams[i].CodecType == "video" {
			w = md.Streams[i].Width
			h = md.Streams[i].Height
			break
		}
	}
	return
}

func (u *FFmpegUtil) ConvertPngToMp4ByPipe(filePath string, dstW, dstH int) (bytes []byte, duration int32, err error) {
	return convertToMp4ByPipe(filePath, func(mf *models.Mediafile) {
		// mf.SetMovFlags("+faststart")
		mf.SetPixFmt("yuva420p")
		// mf.SetVideoFilter(fmt.Sprintf("scale=%d:%d", dstW, dstH))
		mf.SetVideoCodec("libx264")
		// mf.SetStrict(-2)
		// mf.SetVideoBitRate("218k")
		// mf.SetBufferSize(218)
		mf.SetFrameRate(30)
	})
}
