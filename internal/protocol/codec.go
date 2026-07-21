package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
)

const (
	HeaderSize   = 10
	MaxFrameSize = 64 * 1024
)

var (
	ErrFrameTooLarge = errors.New("frame exceeds application limit")
	ErrInvalidHeader = errors.New("invalid frame header")
)

type Message struct {
	MsgID uint16
	Seq   uint32
	Body  []byte
}

// Encode serializes a protobuf payload into the sequenced little-endian envelope.
func Encode(msgID uint16, seq uint32, payload proto.Message) ([]byte, error) {
	if payload == nil {
		return nil, errors.New("protobuf payload is nil")
	}
	body, err := proto.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal protobuf payload: %w", err)
	}
	totalLen := HeaderSize + len(body)
	if totalLen > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	frame := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(frame[:4], uint32(totalLen))
	binary.LittleEndian.PutUint16(frame[4:6], msgID)
	binary.LittleEndian.PutUint32(frame[6:10], seq)
	copy(frame[HeaderSize:], body)
	return frame, nil
}

func Decode(data []byte) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, ErrInvalidHeader
	}
	totalLen := binary.LittleEndian.Uint32(data[:4])
	if int(totalLen) != len(data) {
		return nil, fmt.Errorf("frame length mismatch: declared=%d actual=%d", totalLen, len(data))
	}
	if totalLen > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	body := make([]byte, len(data)-HeaderSize)
	copy(body, data[HeaderSize:])
	return &Message{
		MsgID: binary.LittleEndian.Uint16(data[4:6]),
		Seq:   binary.LittleEndian.Uint32(data[6:10]),
		Body:  body,
	}, nil
}
