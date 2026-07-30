//go:build darwin

package voice

import (
	"encoding/binary"
	"testing"
)

func TestFinalizeLiveWAVRepairsGrowingDataChunk(t *testing.T) {
	wav := []byte("RIFF\x00\x00\x00\x00WAVE")
	appendChunk := func(name string, declared uint32, payload []byte) {
		wav = append(wav, name...)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], declared)
		wav = append(wav, size[:]...)
		wav = append(wav, payload...)
		if len(payload)%2 != 0 {
			wav = append(wav, 0)
		}
	}
	appendChunk("JUNK", 4, []byte{0, 0, 0, 0})
	appendChunk("data", 0, []byte{1, 2, 3, 4, 5, 6})

	if err := finalizeLiveWAV(wav); err != nil {
		t.Fatalf("finalizeLiveWAV: %v", err)
	}
	if got, want := binary.LittleEndian.Uint32(wav[4:8]), uint32(len(wav)-8); got != want {
		t.Fatalf("RIFF size = %d, want %d", got, want)
	}
	const dataSizeOffset = 12 + 8 + 4 + 4
	if got, want := binary.LittleEndian.Uint32(wav[dataSizeOffset:dataSizeOffset+4]), uint32(6); got != want {
		t.Fatalf("data size = %d, want %d", got, want)
	}
}
