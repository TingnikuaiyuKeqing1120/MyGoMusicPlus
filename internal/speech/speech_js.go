//go:build js

package speech

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Browsers have no sherpa-onnx to load: captions are unavailable on the web
// build, and the model is neither downloaded nor run there.

// ErrUnavailable says there is no speech recognition for this platform.
var ErrUnavailable = errors.New("speech: no sherpa-onnx for this platform")

// SampleRate is the rate of the audio the models hear.
const SampleRate = 16000

// A Segment is what is said between two pauses, and when.
type Segment struct {
	Start, End time.Duration
	Text       string
}

// A Source is audio to transcribe: interleaved samples of its channels, at
// its rate.
type Source interface {
	SampleRate() int
	Channels() int
	Read(dst []float32) (int, error)
}

// A Recognizer transcribes audio; on the web there is none.
type Recognizer struct{}

// NewRecognizer reports that captions are unavailable here.
func NewRecognizer(root string) (*Recognizer, error) { return nil, ErrUnavailable }

// Close lets the model go; there is none.
func (r *Recognizer) Close() {}

// Transcribe reports that captions are unavailable here.
func (r *Recognizer) Transcribe(ctx context.Context, src Source, from, until time.Duration, said func(Segment), heard func(time.Duration)) error {
	return ErrUnavailable
}

// Available reports whether there is speech recognition for this platform.
func Available() bool { return false }

func platform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// libDir returns the directory holding the libraries and the VAD.
func libDir(root string) string {
	if dir := os.Getenv("SHERPA_ONNX_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(root, "sherpa-onnx-"+libVersion)
}