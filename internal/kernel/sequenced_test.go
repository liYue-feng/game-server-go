package kernel

import (
	"context"
	"errors"
	"testing"

	"game-server/internal/protocol"
	"game-server/internal/session"

	"google.golang.org/protobuf/proto"
)

type seqConn struct {
	seq                   uint32
	replyCount, pushCount int
	msgID                 uint16
}

func (c *seqConn) Reply(seq uint32, id uint16, _ proto.Message) error {
	c.replyCount++
	c.seq = seq
	c.msgID = id
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
