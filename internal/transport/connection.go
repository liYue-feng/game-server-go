// Package transport 是改造后的传输层，只负责 WebSocket 原始字节收发与连接生命周期。
//
// 借鉴 pitaya 的 acceptor + agent 分工：传输层不认识业务，
// 只做"收到一帧就交给 kernel、要发一帧就写 WebSocket"。
// 业务分发、编解码、认证限流全部下沉到 kernel + pipeline。
//
// 与旧 gateway 的区别：
//   - Connection 不再持有 uid/token，身份由 session.Session 承载（解耦）
//   - readPump 解出帧后调用 kernel.Dispatch，而非旧 Router.Route
//   - Connection 实现 session.Conn 接口，供 Session.Push 发送消息
package transport

import (
	"context"
	"errors"
	"sync"
	"time"

	"game-server/internal/kernel"
	"game-server/internal/protocol"
	"game-server/internal/session"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var (
	ErrConnectionClosed = errors.New("transport connection closed")
	errSendBufferFull   = errors.New("transport send buffer full")
)

const (
	// WebSocket 单条消息上限为 4 MiB，包含完整应用层协议帧（6 字节帧头和消息体）。
	maxWebSocketMessageSize int64 = 4 * 1024 * 1024
	// 写超时：发送消息到客户端的最大等待时间
	writeWait = 10 * time.Second
	// Pong 超时：等待客户端 pong 响应的最大时间，超过视为掉线
	pongWait = 90 * time.Second
	// Ping 周期：服务器主动发 ping 的间隔，必须小于 pongWait
	pingPeriod = (pongWait * 9) / 10
	// 发送缓冲区大小
	sendBufSize = 256
)

// Connection 封装一个客户端 WebSocket 连接。
//
// 每个连接对应两个 goroutine：readPump（读并分发）、writePump（发送并保活）。
// 这是 gorilla/websocket 推荐的读写分离模式：同一连接同一时刻只允许一个写操作。
type Connection struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte      // 发送缓冲区，writePump 从这里取数据写出
	kernel *kernel.Kernel   // 消息处理内核
	sess   *session.Session // 该连接的玩家会话
	done   chan struct{}

	mu     sync.RWMutex
	closed bool
}

// newConnection 创建连接实例，并绑定一个未登录的会话。
func newConnection(hub *Hub, wsConn *websocket.Conn, k *kernel.Kernel) *Connection {
	c := &Connection{
		hub:    hub,
		conn:   wsConn,
		send:   make(chan []byte, sendBufSize),
		kernel: k,
		done:   make(chan struct{}),
	}
	// 会话以本连接作为底层 Conn（实现 session.Conn 的 SendMessage）
	c.sess = session.New(c)
	return c
}

// Session 返回该连接的会话（供外部按需访问，如广播、踢人）。
func (c *Connection) Session() *session.Session {
	return c.sess
}

// SendMessage 实现 session.Conn：把 payload 编码为协议帧并投递到发送缓冲区。
//
// 线程安全：真正写 WebSocket 的动作由 writePump goroutine 完成。
// 非阻塞投递：缓冲区满说明客户端处理不过来，丢弃该消息而非阻塞，
// 否则会拖垮服务器 goroutine。
func (c *Connection) Reply(seq uint32, msgID uint16, payload proto.Message) error {
	return c.sendMessage(seq, msgID, payload)
}

func (c *Connection) Push(msgID uint16, payload proto.Message) error {
	return c.sendMessage(0, msgID, payload)
}

func (c *Connection) sendMessage(seq uint32, msgID uint16, payload proto.Message) error {
	data, err := protocol.Encode(msgID, seq, payload)
	if err != nil {
		return err
	}
	if err := c.enqueue(data); err != nil {
		if err != errSendBufferFull {
			return err
		}
		zap.L().Warn("发送缓冲区已满，丢弃消息",
			zap.Int64("uid", c.sess.UID()),
			zap.Uint16("msgID", msgID),
		)
		return nil
	}
	return nil
}

func (c *Connection) enqueue(data []byte) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return ErrConnectionClosed
	}
	select {
	case c.send <- data:
		return nil
	default:
		return errSendBufferFull
	}
}

func (c *Connection) stop() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()
	_ = c.conn.Close()
}

func (c *Connection) run() {
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writePump()
	}()

	c.readPump()
	c.stop()
	<-writerDone
}

// readPump 读取泵：从 WebSocket 读取客户端消息并交给 kernel 分发。
//
// 退出条件：读取错误或连接关闭。退出时注销连接并触发会话 OnClose。
func (c *Connection) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.sess.Close() // 触发 OnClose 回调（退组、清缓存等）
		c.stop()
	}()

	c.conn.SetReadLimit(maxWebSocketMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	// 每次收到 pong 就刷新读超时，这是 WebSocket 心跳的标准做法。
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// ctx 携带会话，供 kernel/pipeline/业务 handler 使用。
	ctx := session.WithSession(context.Background(), c.sess)

	for {
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				zap.L().Error("WebSocket 读取异常", zap.Int64("uid", c.sess.UID()), zap.Error(err))
			}
			break
		}
		// 只处理二进制消息（协议是二进制帧）。
		if msgType != websocket.BinaryMessage {
			zap.L().Warn("收到非二进制消息，已忽略", zap.Int("msgType", msgType))
			continue
		}
		// 交给内核：解码 -> 认证/限流 -> 分发 -> 编码响应。
		if err := c.kernel.Dispatch(ctx, data); errors.Is(err, kernel.ErrFatalProtocol) {
			break
		}
	}
}

// writePump 写入泵：从发送缓冲区取数据写入 WebSocket，并定期发 ping 保活。
//
// 退出条件：发送失败、连接关闭，或 Hub 关闭连接的 done 信号到达。
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.stop()
	}()

	for {
		select {
		case <-c.done:
			return
		case message := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				zap.L().Error("WebSocket 写入失败", zap.Int64("uid", c.sess.UID()), zap.Error(err))
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
