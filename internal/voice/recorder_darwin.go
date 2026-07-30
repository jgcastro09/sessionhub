//go:build darwin

package voice

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Recorder shells out to the sessionhub-voice-recorder helper (see
// native/macos/recorder.m), the same way Manager shells out to
// whisper-server. There's no pure-Go CoreAudio capture library the way
// go-wca covers WASAPI on Windows, and hand-rolling cgo bindings inside the
// sessionhub binary itself would force cross-compiling darwin with cgo —
// see the package doc comment in install.go for why that's avoided.
type Recorder struct {
	recorderExe string
	cmd         *exec.Cmd
	wavPath     string
	exited      chan error
	stderr      *bytes.Buffer
}

// NewRecorder returns an idle recorder; recorderExe is the path resolved by
// Manager.RecorderExe() after a successful Ensure.
func NewRecorder(recorderExe string) *Recorder {
	return &Recorder{recorderExe: recorderExe}
}

// Start launches the helper, which begins recording immediately. It waits
// briefly to catch an immediate failure (no microphone, permission denied)
// so that shows up here rather than only being discovered on Stop.
func (r *Recorder) Start() error {
	if r.recorderExe == "" {
		return fmt.Errorf("voice transcription isn't set up yet")
	}
	wavPath := filepath.Join(os.TempDir(), fmt.Sprintf("sessionhub-voice-%d.wav", time.Now().UnixNano()))
	cmd := exec.Command(r.recorderExe, wavPath)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start microphone recorder: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	select {
	case err := <-exited:
		return fmt.Errorf("microphone recorder exited immediately: %v (stderr: %s)", err, stderr.String())
	case <-time.After(300 * time.Millisecond):
	}

	r.cmd = cmd
	r.wavPath = wavPath
	r.exited = exited
	r.stderr = stderr
	return nil
}

// Stop signals the helper to finish, waits for it to flush and exit, then
// reads back the WAV it wrote.
func (r *Recorder) Stop() ([]byte, error) {
	if r.cmd == nil {
		return nil, errors.New("recorder was not started")
	}
	defer os.Remove(r.wavPath)

	if err := r.cmd.Process.Signal(syscall.SIGINT); err != nil {
		_ = r.cmd.Process.Kill()
	}

	select {
	case err := <-r.exited:
		if err != nil {
			return nil, fmt.Errorf("microphone recorder exited with an error: %w (stderr: %s)", err, r.stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = r.cmd.Process.Kill()
		return nil, errors.New("timed out waiting for the microphone recorder to stop")
	}

	wav, err := os.ReadFile(r.wavPath)
	if err != nil {
		return nil, fmt.Errorf("read recorded audio: %w", err)
	}
	if len(wav) == 0 {
		return nil, errors.New("no audio captured — check this terminal app has microphone access in System Settings > Privacy & Security > Microphone")
	}
	return wav, nil
}
