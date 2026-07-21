// Package session 提供"玩家会话"抽象，把玩家身份与底层连接解耦。
//
// 借鉴 pitaya 的 session.Session：
//   - 底层连接（WebSocket）只负责收发字节，不关心"这是谁"
//   - Session 负责承载玩家身份（uid）与业务态（nickname/token 等）
//   - 业务 handler 通过 ctx 拿到 Session，调用 Bind/Push/OnClose
//
// 为什么用接口 Conn 而不是直接依赖 WebSocket 连接？
//   - 解耦：session 包不依赖 gorilla/websocket，便于单测（用 fake Conn）
//   - 对齐 pitaya：pitaya 的 session 也通过 NetworkEntity 接口发送
package session

import (
	"context"
	"sync"

	"google.golang.org/protobuf/proto"
)

// Conn 是 Session 依赖的底层连接能力（由 transport 层实现）。
// 只暴露"发送一帧消息"，其余连接细节对 session 不可见。
type Conn interface {
	// SendMessage 把 payload 按 msgID 编码为协议帧并发送给客户端。
	// 线程安全由实现方保证（生产实现走 send channel + writePump）。
	Reply(seq uint32, msgID uint16, payload proto.Message) error
	Push(msgID uint16, payload proto.Message) error
}

// Session 玩家会话。
//
// 生命周期：连接建立时创建（未绑定 uid），登录成功后 Bind(uid)，
// 连接关闭时触发 OnClose 回调（用于退组、清理缓存等）。
type Session struct {
	mu      sync.RWMutex
	conn    Conn
	uid     int64                  // 玩家 ID，0 表示未登录
	data    map[string]interface{} // 业务态：nickname、token 等
	onClose []func()               // 连接关闭时依次执行的回调
}

// New 创建一个绑定到底层连接的会话（初始未登录）。
func New(conn Conn) *Session {
	return &Session{
		conn: conn,
		data: make(map[string]interface{}),
	}
}

// Bind 绑定玩家 ID，标记该会话为已登录。
// 对齐 pitaya 的 session.Bind(uid)：登录成功后调用。
func (s *Session) Bind(uid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uid = uid
}

// UID 返回玩家 ID，0 表示未登录。
func (s *Session) UID() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uid
}

// IsBound 是否已登录（已绑定 uid）。
func (s *Session) IsBound() bool {
	return s.UID() > 0
}

// Set 写入业务态（如 nickname、token）。
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// Get 读取业务态；ok 表示 key 是否存在。
func (s *Session) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

// GetString 读取字符串型业务态，不存在或类型不符返回空串。
func (s *Session) GetString(key string) string {
	v, ok := s.Get(key)
	if !ok {
		return ""
	}
	str, _ := v.(string)
	return str
}

// Push 服务器主动向客户端推送一帧消息。
// 对齐 pitaya 的 session.Push：用于支付结果通知、广播等场景。
func (s *Session) Push(msgID uint16, payload proto.Message) error {
	return s.conn.Push(msgID, payload)
}

func (s *Session) Reply(seq uint32, msgID uint16, payload proto.Message) error {
	return s.conn.Reply(seq, msgID, payload)
}

// OnClose 注册连接关闭时的回调。
// 多次注册按注册顺序在 Close 时依次执行。
func (s *Session) OnClose(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = append(s.onClose, fn)
}

// Close 由 transport 层在连接断开时调用，依次触发 OnClose 回调。
func (s *Session) Close() {
	s.mu.Lock()
	callbacks := make([]func(), len(s.onClose))
	copy(callbacks, s.onClose)
	s.mu.Unlock()

	for _, fn := range callbacks {
		fn()
	}
}

// ---- context 存取 ----
//
// 业务 handler 通过 ctx 拿到 Session，对齐 pitaya 的 GetSessionFromCtx。

type ctxKey struct{}

// WithSession 把 Session 放入 context。
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext 从 context 取出 Session；不存在返回 nil。
func FromContext(ctx context.Context) *Session {
	s, _ := ctx.Value(ctxKey{}).(*Session)
	return s
}
