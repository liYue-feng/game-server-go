package session

import (
	"testing"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

type sequencedSentMessage struct {
	msgID uint16
	seq   uint32
	p     proto.Message
}

type sequencedConn struct{ sent []sequencedSentMessage }

func (c *sequencedConn) Reply(seq uint32, msgID uint16, p proto.Message) error {
	c.sent = append(c.sent, sequencedSentMessage{msgID, seq, p})
	return nil
}
func (c *sequencedConn) Push(msgID uint16, p proto.Message) error {
	c.sent = append(c.sent, sequencedSentMessage{msgID, 0, p})
	return nil
}

func TestSessionPushUsesZeroSeq(t *testing.T) {
	conn := &sequencedConn{}
	s := New(conn)
	if err := s.Push(1004, &protocolpb.HeartbeatResp{}); err != nil {
		t.Fatal(err)
	}
	if len(conn.sent) != 1 || conn.sent[0].seq != 0 {
		t.Fatalf("push seq=%d want=0", conn.sent[0].seq)
	}
}
