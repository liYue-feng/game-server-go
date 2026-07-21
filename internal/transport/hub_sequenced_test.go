package transport

import (
	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"testing"
)

func testHubConn(uid int64) *Connection {
	c := &Connection{send: make(chan []byte, 2), done: make(chan struct{})}
	c.sess = session.New(c)
	c.sess.Bind(uid)
	return c
}
func TestHubPushFramesUseZeroSeq(t *testing.T) {
	h := NewHub()
	c := testHubConn(9)
	if !h.Register(c) {
		t.Fatal("register")
	}
	defer h.Shutdown()
	h.Broadcast(protocol.MsgID_GMCommandResp, &protocolpb.GMCommandResp{})
	frame := <-c.send
	m, err := protocol.Decode(frame)
	if err != nil || m.Seq != 0 || m.MsgID != protocol.MsgID_GMCommandResp {
		t.Fatalf("broadcast=%v err=%v", m, err)
	}
	if !h.PushToUID(9, protocol.MsgID_PayResultNotify, &protocolpb.PayResultNotify{}) {
		t.Fatal("online target not delivered")
	}
	m, err = protocol.Decode(<-c.send)
	if err != nil || m.Seq != 0 || m.MsgID != protocol.MsgID_PayResultNotify {
		t.Fatalf("push=%v err=%v", m, err)
	}
	if h.PushToUID(99, protocol.MsgID_PayResultNotify, &protocolpb.PayResultNotify{}) {
		t.Fatal("offline target delivered")
	}
}
