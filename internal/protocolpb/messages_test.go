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

	frame := make([]byte, 6+len(body))
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(frame)))
	binary.LittleEndian.PutUint16(frame[4:6], uint16(MessageId_MESSAGE_ID_LOGIN_REQ))
	copy(frame[6:], body)
	if got := hex.EncodeToString(frame); got != "0b000000e9030a03616263" {
		t.Fatalf("frame = %s, want 0b000000e9030a03616263", got)
	}
}
