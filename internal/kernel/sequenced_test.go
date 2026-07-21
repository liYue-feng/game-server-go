package kernel

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"

	"google.golang.org/protobuf/proto"
)

type seqConn struct {
	seq                   uint32
	replyCount, pushCount int
	msgID                 uint16
	payload               proto.Message
}

func TestMalformedBodyErrorEchoesRequestSeq(t *testing.T) {
	conn := &seqConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	frame := make([]byte, protocol.HeaderSize+1)
	binary.LittleEndian.PutUint32(frame[:4], uint32(len(frame)))
	binary.LittleEndian.PutUint16(frame[4:6], protocol.MsgID_HeartbeatReq)
	binary.LittleEndian.PutUint32(frame[6:10], 73)
	frame[10] = 0xff
	k := New(nil)
	k.RegisterRoute(protocol.MsgID_HeartbeatReq, func(context.Context, *protocol.HeartbeatReq) (*protocol.HeartbeatResp, error) {
		return &protocol.HeartbeatResp{}, nil
	}, AuthFree())
	if err := k.Dispatch(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if conn.replyCount != 1 || conn.seq != 73 || conn.msgID != protocol.MsgID_Error {
		t.Fatalf("reply count=%d seq=%d msg=%d", conn.replyCount, conn.seq, conn.msgID)
	}
	if got := conn.payload.(*protocolpb.ErrorResp).Msg; got != "invalid request payload" {
		t.Fatalf("error message=%q want invalid request payload", got)
	}
}

func (c *seqConn) Reply(seq uint32, id uint16, payload proto.Message) error {
	c.replyCount++
	c.seq = seq
	c.msgID = id
	c.payload = payload
	return nil
}
func (c *seqConn) Push(uint16, proto.Message) error { c.pushCount++; return nil }

func TestKernelEchoesRequestSeq(t *testing.T) {
	k := New(nil)
	k.RegisterRoute(protocol.MsgID_HeartbeatReq, func(context.Context, *protocol.HeartbeatReq) (*protocol.HeartbeatResp, error) {
		return &protocol.HeartbeatResp{}, nil
	}, AuthFree())
	conn := &seqConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	frame, _ := protocol.Encode(protocol.MsgID_HeartbeatReq, 91, &protocol.HeartbeatReq{})
	if err := k.Dispatch(ctx, frame); err != nil {
		t.Fatal(err)
	}
	if conn.seq != 91 {
		t.Fatalf("reply seq=%d want=91", conn.seq)
	}
}

func TestKernelRejectsZeroSeqWithoutErrorFrame(t *testing.T) {
	conn := &seqConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	frame, _ := protocol.Encode(protocol.MsgID_HeartbeatReq, 0, &protocol.HeartbeatReq{})
	if err := New(nil).Dispatch(ctx, frame); !errors.Is(err, ErrFatalProtocol) {
		t.Fatalf("Dispatch error=%v want fatal", err)
	}
	if conn.replyCount != 0 || conn.pushCount != 0 {
		t.Fatalf("zero-seq emitted reply=%d push=%d", conn.replyCount, conn.pushCount)
	}
}
