package protocolpb

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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

func TestCombatResultReqSurvivalTimeUsesDoubleFixed64Wire(t *testing.T) {
	request := &CombatResultReq{}
	field := request.ProtoReflect().Descriptor().Fields().ByNumber(5)
	if field == nil {
		t.Fatal("CombatResultReq field 5 is missing")
	}
	if got := field.Name(); got != "survival_time" {
		t.Fatalf("field 5 name = %q, want survival_time", got)
	}
	if got := field.Kind(); got != protoreflect.DoubleKind {
		t.Fatalf("field 5 kind = %s, want double", got)
	}

	request.ProtoReflect().Set(field, protoreflect.ValueOfFloat64(12.5))
	body, err := proto.Marshal(request)
	if err != nil {
		t.Fatalf("marshal CombatResultReq: %v", err)
	}
	if got := hex.EncodeToString(body); got != "290000000000002940" {
		t.Fatalf("body = %s, want 290000000000002940", got)
	}
}
