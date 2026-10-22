package test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamgram/teamgram-server/pkg/goffmpeg/transcoder"
)

// The transcoder drives the ffmpeg and ffprobe binaries, so these tests need
// them on PATH. The input samples are synthesized with ffmpeg's lavfi sources
// into a temporary directory, so no media fixtures are kept in the repository.

// sampleSources are the lavfi inputs every sample is generated from: one
// second of test pattern video and of a sine tone.
var sampleSources = []string{
	"-f", "lavfi", "-i", "testsrc=size=176x144:rate=25:duration=1",
	"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
}

// sampleOutputs lists, per input container, the ffmpeg output options used to
// produce the sample. Only encoders built into libavcodec are used (no
// libx264, libvpx, libmp3lame...), so the samples can be generated with any
// ffmpeg build. Audio-only containers map just the tone.
var sampleOutputs = map[string][]string{
	"3gp":  {"-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "aac", "-f", "3gp"},
	"avi":  {"-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "pcm_s16le", "-f", "avi"},
	"flv":  {"-map", "0:v", "-map", "1:a", "-c:v", "flv1", "-c:a", "aac", "-f", "flv"},
	"mkv":  {"-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "flac", "-f", "matroska"},
	"mov":  {"-map", "0:v", "-map", "1:a", "-c:v", "mpeg4", "-c:a", "aac", "-f", "mov"},
	"mpeg": {"-map", "0:v", "-map", "1:a", "-c:v", "mpeg1video", "-c:a", "mp2", "-f", "mpeg"},
	"ogg":  {"-map", "1:a", "-c:a", "flac", "-f", "ogg"},
	"wav":  {"-map", "1:a", "-c:a", "pcm_s16le", "-f", "wav"},
	"webm": {"-map", "1:a", "-c:a", "opus", "-strict", "-2", "-f", "webm"},
	"wmv":  {"-map", "0:v", "-map", "1:a", "-c:v", "wmv2", "-c:a", "wmav2", "-f", "asf"},
}

// requireFFmpeg skips the test when the binaries the transcoder runs are not
// installed: that is a missing external dependency, not a test failure. It
// returns the path of the ffmpeg binary.
func requireFFmpeg(t *testing.T) string {
	t.Helper()
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skipf("ffmpeg is not installed: %v", err)
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skipf("ffprobe is not installed: %v", err)
	}
	return ffmpegBin
}

// makeSample generates the sample for the given container in a temporary
// directory and returns its path.
func makeSample(t *testing.T, container string) string {
	t.Helper()
	ffmpegBin := requireFFmpeg(t)

	opts, ok := sampleOutputs[container]
	require.True(t, ok, "no sample definition for %q", container)

	path := filepath.Join(t.TempDir(), "sample."+container)
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
	args = append(args, sampleSources...)
	args = append(args, opts...)
	args = append(args, path)

	out, err := exec.Command(ffmpegBin, args...).CombinedOutput()
	require.NoError(t, err, "generating %s sample: %s", container, out)
	return path
}

// transcodeSample converts the sample of the given container to mp4 and checks
// that the output was written.
func transcodeSample(t *testing.T, container string) {
	t.Helper()
	inputPath := makeSample(t, container)
	outputPath := filepath.Join(t.TempDir(), container+".mp4")

	trans := new(transcoder.Transcoder)

	err := trans.Initialize(inputPath, outputPath)
	require.NoError(t, err)

	done := trans.Run(false)
	err = <-done
	require.NoError(t, err)

	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.NotZero(t, info.Size())
}

func TestInputNotFound(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	var inputPath = filepath.Join(dir, "nf")
	var outputPath = filepath.Join(dir, "out", "nf.mp4")

	trans := new(transcoder.Transcoder)

	err := trans.Initialize(inputPath, outputPath)
	assert.NotNil(t, err)
}

func TestTranscoding3GP(t *testing.T) {
	transcodeSample(t, "3gp")
}

func TestTranscodingAVI(t *testing.T) {
	transcodeSample(t, "avi")
}

func TestTranscodingFLV(t *testing.T) {
	transcodeSample(t, "flv")
}

func TestTranscodingMKV(t *testing.T) {
	transcodeSample(t, "mkv")
}

func TestTranscodingMOV(t *testing.T) {
	transcodeSample(t, "mov")
}

func TestTranscodingMPEG(t *testing.T) {
	transcodeSample(t, "mpeg")
}

func TestTranscodingOGG(t *testing.T) {
	transcodeSample(t, "ogg")
}

func TestTranscodingWAV(t *testing.T) {
	transcodeSample(t, "wav")
}

func TestTranscodingWEBM(t *testing.T) {
	transcodeSample(t, "webm")
}

func TestTranscodingWMV(t *testing.T) {
	transcodeSample(t, "wmv")
}

func TestTranscodingProgress(t *testing.T) {
	inputPath := makeSample(t, "avi")
	outputPath := filepath.Join(t.TempDir(), "avi.mp4")

	trans := new(transcoder.Transcoder)

	err := trans.Initialize(inputPath, outputPath)
	require.NoError(t, err)

	done := trans.Run(true)
	// Drain the progress stream: it ends when ffmpeg closes its stderr.
	for range trans.Output() {
	}

	err = <-done
	require.NoError(t, err)
}

func TestTranscodePipes(t *testing.T) {
	inputPath := makeSample(t, "mkv")
	input, err := os.Open(inputPath)
	require.NoError(t, err)
	defer input.Close()

	trans := new(transcoder.Transcoder)

	err = trans.InitializeEmptyTranscoder()
	require.NoError(t, err)

	w, err := trans.CreateInputPipe()
	require.NoError(t, err)

	r, err := trans.CreateOutputPipe("mp4")
	require.NoError(t, err)

	var (
		wg      sync.WaitGroup
		output  []byte
		readErr error
		copyErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		output, readErr = io.ReadAll(r)
		_ = r.Close()
	}()
	go func() {
		defer wg.Done()
		_, copyErr = io.Copy(w, input)
		_ = w.Close()
	}()

	done := trans.Run(false)
	err = <-done
	require.NoError(t, err)

	wg.Wait()
	require.NoError(t, readErr)
	require.NoError(t, copyErr)
	assert.NotEmpty(t, output)
}
