package remote

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	protocolVersion = 1
	maxFrameSize    = 8 << 20
)

type Frame struct {
	ID         string          `json:"id,omitempty"`
	Type       string          `json:"type"`
	InstanceID string          `json:"instance_id,omitempty"`
	Sequence   uint64          `json:"sequence,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func WriteFrame(writer io.Writer, frame Frame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("encode remote frame: %w", err)
	}
	if len(data) > maxFrameSize {
		return fmt.Errorf("remote frame is %d bytes; maximum is %d", len(data), maxFrameSize)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := writer.Write(header[:]); err != nil {
		return fmt.Errorf("write remote frame header: %w", err)
	}
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("write remote frame body: %w", err)
	}
	return nil
}

func ReadFrame(reader *bufio.Reader) (Frame, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Frame{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxFrameSize {
		return Frame{}, fmt.Errorf("invalid remote frame size %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return Frame{}, fmt.Errorf("read remote frame: %w", err)
	}
	var frame Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		return Frame{}, fmt.Errorf("decode remote frame: %w", err)
	}
	if frame.Type == "" {
		return Frame{}, fmt.Errorf("remote frame type is required")
	}
	return frame, nil
}

func payload(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
