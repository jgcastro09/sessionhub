//go:build !windows && !darwin

package voice

import "errors"

// ErrUnsupportedPlatform is returned by Recorder on platforms without a
// capture backend yet (Windows/WASAPI and macOS/AVFoundation are the ones
// implemented so far).
var ErrUnsupportedPlatform = errors.New("voice dictation isn't supported on this platform yet")

// Recorder is a no-op stand-in outside Windows, keeping the package
// importable (and SessionHub's CGO_ENABLED=0 cross-build intact) on
// platforms without a capture backend yet.
type Recorder struct{}

func NewRecorder(recorderExe string) *Recorder { return &Recorder{} }

func (r *Recorder) Start() error { return ErrUnsupportedPlatform }

func (r *Recorder) Stop() ([]byte, error) { return nil, ErrUnsupportedPlatform }
