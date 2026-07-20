package session

import (
	"context"
	"testing"

	"game-server/internal/protocolpb"

	"google.golang.org/protobuf/proto"
)

// fakeConn 是 Conn 的测试替身：记录最后一次发送的 msgID 与 payload。
type fakeConn struct {
	lastMsgID   uint16
	lastPayload proto.Message
	sendCount   int
}

func (f *fakeConn) SendMessage(msgID uint16, payload proto.Message) error {
	f.lastMsgID = msgID
	f.lastPayload = payload
	f.sendCount++
	return nil
}

// TestBindAndUID 验证 Bind 后 UID/IsBound 生效。
func TestBindAndUID(t *testing.T) {
	s := New(&fakeConn{})
	if s.IsBound() {
		t.Fatal("新会话不应处于已绑定状态")
	}
	s.Bind(42)
	if got := s.UID(); got != 42 {
		t.Fatalf("UID = %d, want 42", got)
	}
	if !s.IsBound() {
		t.Fatal("Bind 后应为已绑定")
	}
}

// TestSetGet 验证业务态读写。
func TestSetGet(t *testing.T) {
	s := New(&fakeConn{})
	s.Set("nickname", "玩家1")
	if got := s.GetString("nickname"); got != "玩家1" {
		t.Fatalf("GetString = %q, want 玩家1", got)
	}
	if got := s.GetString("missing"); got != "" {
		t.Fatalf("缺失 key 应返回空串, got %q", got)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("缺失 key 的 ok 应为 false")
	}
}

// TestPush 验证 Push 经由底层 Conn 发送。
func TestPush(t *testing.T) {
	conn := &fakeConn{}
	s := New(conn)
	payload := &protocolpb.HeartbeatResp{Timestamp: 1}
	if err := s.Push(1002, payload); err != nil {
		t.Fatalf("Push 出错: %v", err)
	}
	if conn.lastMsgID != 1002 || conn.lastPayload != payload {
		t.Fatalf("Push 未正确转发到 Conn: msgID=%d payload=%v", conn.lastMsgID, conn.lastPayload)
	}
}

// TestOnCloseOrder 验证 OnClose 回调按注册顺序执行。
func TestOnCloseOrder(t *testing.T) {
	s := New(&fakeConn{})
	var order []int
	s.OnClose(func() { order = append(order, 1) })
	s.OnClose(func() { order = append(order, 2) })
	s.Close()
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("OnClose 执行顺序错误: %v", order)
	}
}

// TestContextRoundTrip 验证 ctx 存取。
func TestContextRoundTrip(t *testing.T) {
	s := New(&fakeConn{})
	ctx := WithSession(context.Background(), s)
	if got := FromContext(ctx); got != s {
		t.Fatal("FromContext 未返回原会话")
	}
	if got := FromContext(context.Background()); got != nil {
		t.Fatal("无会话的 ctx 应返回 nil")
	}
}
