package protocolpb

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestLoginReqGoldenWireFrame(t *testing.T) {
	body, err := proto.Marshal(&LoginReq{Code: "abc"})
	if err != nil {
		t.Fatalf("marshal LoginReq: %v", err)
	}
	if got := hex.EncodeToString(body); got != "0a03616263" {
		t.Fatalf("body = %s, want 0a03616263", got)
	}

	frame := make([]byte, 10+len(body))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(frame)))
	binary.LittleEndian.PutUint16(frame[4:6], uint16(MessageId_MESSAGE_ID_LOGIN_REQ))
	binary.LittleEndian.PutUint32(frame[6:10], 1)
	copy(frame[10:], body)
	if got := hex.EncodeToString(frame); got != "0f000000e903010000000a03616263" {
		t.Fatalf("frame = %s, want 0f000000e903010000000a03616263", got)
	}
}
