package gm

import (
	"context"
	"game-server/internal/protocolpb"
	"game-server/internal/session"
	"google.golang.org/protobuf/proto"
	"testing"
)

type fakeBroadcaster struct {
	n  int
	id uint16
	p  proto.Message
}

func (f *fakeBroadcaster) Broadcast(id uint16, p proto.Message) { f.n++; f.id = id; f.p = p }
func (f *fakeBroadcaster) OnlineCount() int                     { return 0 }

type gmConn struct{}

func (gmConn) Reply(uint32, uint16, proto.Message) error { return nil }
func (gmConn) Push(uint16, proto.Message) error          { return nil }
func TestBroadcastCommandUsesBroadcaster(t *testing.T) {
	f := &fakeBroadcaster{}
	h := &Handler{hub: f, adminUIDs: map[int64]bool{1: true}}
	s := session.New(gmConn{})
	s.Bind(1)
	_, err := h.Command(session.WithSession(context.Background(), s), &protocolpb.GMCommandReq{Cmd: "broadcast", ArgsJson: []byte(`{"content":"hi"}`)})
	if err != nil || f.n != 1 {
		t.Fatalf("err=%v count=%d", err, f.n)
	}
	p := f.p.(*protocolpb.GMCommandResp)
	if p.Cmd != "broadcast" || p.Result != "hi" {
		t.Fatal(p)
	}
}
