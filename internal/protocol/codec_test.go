package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

func TestLoginReqGoldenSequencedFrame(t *testing.T) {
	frame, err := Encode(MsgID_LoginReq, 1, &protocolpb.LoginReq{Code: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0f, 0, 0, 0, 0xe9, 0x03, 1, 0, 0, 0, 0x0a, 0x03, 'a', 'b', 'c'}
	if !bytes.Equal(frame, want) {
		t.Fatalf("frame=%x want=%x", frame, want)
	}
	if got := binary.LittleEndian.Uint32(frame[6:10]); got != 1 {
		t.Fatalf("seq=%d want=1", got)
	}
	if got := frame[HeaderSize:]; !bytes.Equal(got, []byte{0x0a, 0x03, 'a', 'b', 'c'}) {
		t.Fatalf("payload=%x want=0a03616263", got)
	}
}

func TestDecodeRoundTripsSeqAndRejectsSixByteFrame(t *testing.T) {
	frame, err := Encode(MsgID_HeartbeatReq, 77, &protocolpb.HeartbeatReq{})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Seq != 77 {
		t.Fatalf("seq=%d want=77", decoded.Seq)
	}
	if _, err := Decode([]byte{6, 0, 0, 0, 0xeb, 0x03}); err == nil {
		t.Fatal("accepted 6-byte frame")
	}
}

func TestEncodeAcceptsExactlyMaxFrameSize(t *testing.T) {
	payload := &protocolpb.LoginReq{Code: strings.Repeat("x", MaxFrameSize-HeaderSize-4)}
	frame, err := Encode(MsgID_LoginReq, 1, payload)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if got := len(frame); got != MaxFrameSize {
		t.Fatalf("frame length=%d want=%d", got, MaxFrameSize)
	}
}

func TestEncodeRejectsFrameOneByteOverMaxFrameSize(t *testing.T) {
	payload := &protocolpb.LoginReq{Code: strings.Repeat("x", MaxFrameSize-HeaderSize-3)}
	if _, err := Encode(MsgID_LoginReq, 1, payload); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("Encode() error=%v want ErrFrameTooLarge", err)
	}
}

func TestDecodeRejectsDeclaredLengthMismatch(t *testing.T) {
	frame := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(frame[:4], HeaderSize+1)
	if _, err := Decode(frame); err == nil {
		t.Fatal("Decode() accepted a declared-length mismatch")
	}
}

func TestDecodeRejectsNineByteTruncatedHeader(t *testing.T) {
	if _, err := Decode(make([]byte, HeaderSize-1)); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("Decode() error=%v want ErrInvalidHeader", err)
	}
}

func TestEncodeRejectsNilProtobufPayload(t *testing.T) {
	var payload proto.Message
	if _, err := Encode(MsgID_LoginReq, 1, payload); err == nil {
		t.Fatal("Encode() accepted a nil protobuf payload")
	}
}
