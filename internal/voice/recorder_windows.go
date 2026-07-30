//go:build windows

package voice

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// eCapture is EDataFlow's capture endpoint value (mmdeviceapi.h). go-wca
// doesn't export this enum, only the ERole values (wca.EConsole etc).
const eCapture = 1

// maxRecordingDuration bounds a single recording so a forgotten stop can't
// grow memory unbounded.
const maxRecordingDuration = 5 * time.Minute

const (
	waveFormatPCM        = 1
	waveFormatIEEEFloat  = 3
	waveFormatExtensible = 0xFFFE
)

// Recorder captures the default input device via WASAPI shared-mode capture
// and produces a WAV file of whatever it heard. All COM calls happen on one
// goroutine locked to its OS thread for its whole lifetime (COM's apartment
// threading rules require the thread that called CoInitializeEx to be the
// one making subsequent calls).
type Recorder struct {
	stopCh chan struct{}
	result chan recordResult
	ready  chan error
}

type recordResult struct {
	wav []byte
	err error
}

// NewRecorder returns an idle recorder; no audio device is touched until
// Start is called. recorderExe is unused on Windows (capture happens
// in-process via WASAPI) — the parameter exists so every platform's
// NewRecorder shares one signature and callers don't need build tags.
func NewRecorder(recorderExe string) *Recorder {
	return &Recorder{}
}

// Start begins capturing from the default input device. It blocks until
// capture has actually started (or failed), so a failure (e.g. no
// microphone) surfaces immediately instead of silently recording nothing.
func (r *Recorder) Start() error {
	r.stopCh = make(chan struct{})
	r.result = make(chan recordResult, 1)
	r.ready = make(chan error, 1)
	go r.captureLoop()
	return <-r.ready
}

// Stop ends capture and returns the recorded audio as a WAV file.
func (r *Recorder) Stop() ([]byte, error) {
	if r.stopCh == nil {
		return nil, errors.New("recorder was not started")
	}
	close(r.stopCh)
	res := <-r.result
	return res.wav, res.err
}

func (r *Recorder) captureLoop() {
	// The thread that calls CoInitializeEx must be the one making every
	// subsequent COM call on these objects; LockOSThread for the entire
	// capture lifetime and let the OS thread die with the goroutine (Go's
	// documented pattern for this) rather than unlocking it back to the
	// scheduler in some COM-tainted state.
	runtime.LockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		r.ready <- fmt.Errorf("initialize COM: %w", err)
		return
	}
	defer ole.CoUninitialize()

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		r.ready <- fmt.Errorf("create device enumerator: %w", err)
		return
	}
	defer enumerator.Release()

	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(eCapture, wca.EConsole, &device); err != nil {
		r.ready <- fmt.Errorf("find default microphone: %w", err)
		return
	}
	defer device.Release()

	var client *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		r.ready <- fmt.Errorf("activate audio client: %w", err)
		return
	}
	defer client.Release()

	var mixFormat *wca.WAVEFORMATEX
	if err := client.GetMixFormat(&mixFormat); err != nil {
		r.ready <- fmt.Errorf("read microphone format: %w", err)
		return
	}

	const bufferDuration = wca.REFERENCE_TIME(3 * time.Second / 100) // 100ns units
	if err := client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, 0, bufferDuration, 0, mixFormat, nil); err != nil {
		r.ready <- fmt.Errorf("initialize capture stream: %w", err)
		return
	}

	var capture *wca.IAudioCaptureClient
	if err := client.GetService(wca.IID_IAudioCaptureClient, &capture); err != nil {
		r.ready <- fmt.Errorf("get capture client: %w", err)
		return
	}
	defer capture.Release()

	if err := client.Start(); err != nil {
		r.ready <- fmt.Errorf("start capture: %w", err)
		return
	}
	defer client.Stop()

	r.ready <- nil

	pcm := make([]byte, 0, 1<<20)
	deadline := time.Now().Add(maxRecordingDuration)
	blockAlign := uint32(mixFormat.NBlockAlign)

	for {
		select {
		case <-r.stopCh:
			r.result <- r.encode(pcm, mixFormat)
			return
		default:
		}
		if time.Now().After(deadline) {
			r.result <- r.encode(pcm, mixFormat)
			return
		}

		var packetLength uint32
		if err := capture.GetNextPacketSize(&packetLength); err != nil {
			r.result <- recordResult{err: fmt.Errorf("poll capture buffer: %w", err)}
			return
		}
		if packetLength == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		for packetLength != 0 {
			var data *byte
			var framesAvailable, flags uint32
			if err := capture.GetBuffer(&data, &framesAvailable, &flags, nil, nil); err != nil {
				r.result <- recordResult{err: fmt.Errorf("read capture buffer: %w", err)}
				return
			}
			if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 && framesAvailable > 0 {
				byteLen := framesAvailable * blockAlign
				chunk := unsafe.Slice(data, byteLen)
				pcm = append(pcm, chunk...)
			}
			if err := capture.ReleaseBuffer(framesAvailable); err != nil {
				r.result <- recordResult{err: fmt.Errorf("release capture buffer: %w", err)}
				return
			}
			if err := capture.GetNextPacketSize(&packetLength); err != nil {
				r.result <- recordResult{err: fmt.Errorf("poll capture buffer: %w", err)}
				return
			}
		}
	}
}

// encode wraps captured PCM in a standard RIFF/WAVE header. WASAPI shared
// mode very commonly reports the mix format as WAVE_FORMAT_EXTENSIBLE
// without exposing the sub-format tail (go-wca's WAVEFORMATEX doesn't model
// it); since we don't parse that tail, resolve it to the concrete PCM/float
// tag its bit depth implies (32-bit shared-mode mixers are essentially
// always IEEE float; 16-bit is essentially always integer PCM) so a plain
// WAV reader — including whisper.cpp's — knows how to interpret the samples
// without needing the extensible chunk at all.
func (r *Recorder) encode(pcm []byte, format *wca.WAVEFORMATEX) recordResult {
	if len(pcm) == 0 {
		return recordResult{err: errors.New("no audio captured — check your microphone")}
	}
	tag := format.WFormatTag
	if tag == waveFormatExtensible {
		if format.WBitsPerSample == 32 {
			tag = waveFormatIEEEFloat
		} else {
			tag = waveFormatPCM
		}
	}

	var buf []byte
	write := func(b []byte) { buf = append(buf, b...) }
	writeU32 := func(v uint32) { var b [4]byte; binary.LittleEndian.PutUint32(b[:], v); write(b[:]) }
	writeU16 := func(v uint16) { var b [2]byte; binary.LittleEndian.PutUint16(b[:], v); write(b[:]) }

	write([]byte("RIFF"))
	writeU32(uint32(36 + len(pcm)))
	write([]byte("WAVE"))
	write([]byte("fmt "))
	writeU32(16)
	writeU16(tag)
	writeU16(format.NChannels)
	writeU32(format.NSamplesPerSec)
	writeU32(format.NAvgBytesPerSec)
	writeU16(format.NBlockAlign)
	writeU16(format.WBitsPerSample)
	write([]byte("data"))
	writeU32(uint32(len(pcm)))
	write(pcm)

	return recordResult{wav: buf}
}
