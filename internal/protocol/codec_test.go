package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

func TestEncodeUsesProtobufPayloadAndPreservesLittleEndianFrame(t *testing.T) {
	payload := &protocolpb.LoginReq{Code: "protobuf-login"}
	wantBody, err := proto.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal expected protobuf body: %v", err)
	}

	frame, err := Encode(MsgID_LoginReq, payload)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if got := binary.LittleEndian.Uint32(frame[:4]); got != uint32(len(frame)) {
		t.Fatalf("frame length = %d, want %d", got, len(frame))
	}
	if got := binary.LittleEndian.Uint16(frame[4:6]); got != MsgID_LoginReq {
		t.Fatalf("message ID = %d, want %d", got, MsgID_LoginReq)
	}
	if !bytes.Equal(frame[HeaderSize:], wantBody) {
		t.Fatalf("payload = %x, want protobuf %x", frame[HeaderSize:], wantBody)
	}
}

func TestEncodeRejectsNonProtobufPayload(t *testing.T) {
	var payload proto.Message
	if _, err := Encode(MsgID_LoginReq, payload); err == nil {
		t.Fatal("Encode() accepted a nil protobuf payload")
	}
}
