package video

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	// frame layout must match encodeFrame in video-feed.go:
	// id (4 bytes) | timestamp ns (8 bytes) | payload length (8 bytes) | payload
	HEADER_SIZE       = 4 + 8 + 8
	MAX_PAYLOAD_BYTES = 1 << 20 // reject corrupt headers before allocating
)

type Frame struct {
	ID        uint32
	Timestamp time.Time
	Payload   []byte
}

// encodeFrame packs a frame for the wire:
// id (4 bytes) | timestamp ns (8 bytes) | payload length (8 bytes) | payload
func EncodeFrame(f Frame) []byte {
	buf := make([]byte, 0, HEADER_SIZE+len(f.Payload))
	buf = binary.BigEndian.AppendUint32(buf, f.ID)
	buf = binary.BigEndian.AppendUint64(buf, uint64(f.Timestamp.UnixNano()))
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(f.Payload)))
	return append(buf, f.Payload...)
}

// ReadFrame reads exactly one frame from r.
// Returns io.EOF only if the stream ended cleanly between frames;
// a stream that stops mid-frame returns io.ErrUnexpectedEOF.
func ReadFrame(r io.Reader) (Frame, error) {
	hdr := make([]byte, HEADER_SIZE)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return Frame{}, err
	}

	length := binary.BigEndian.Uint64(hdr[12:20])
	if length > MAX_PAYLOAD_BYTES {
		return Frame{}, fmt.Errorf("payload length %d exceeds max %d", length, MAX_PAYLOAD_BYTES)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF // got a header but no payload
		}
		return Frame{}, err
	}

	return Frame{
		ID:        binary.BigEndian.Uint32(hdr[0:4]),
		Timestamp: time.Unix(0, int64(binary.BigEndian.Uint64(hdr[4:12]))),
		Payload:   payload,
	}, nil
}
