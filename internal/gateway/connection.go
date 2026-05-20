// Package gateway 网关层 —— 管理客户端 WebSocket 连接。
//
// 网关是游戏服务器的"前台"，负责：
//   - 维护所有客户端的 WebSocket 长连接
//   - 管理连接的生命周期（建立、读取、关闭）
//   - 将收到的消息路由到对应的业务 handler
//   - 向客户端推送消息
package gateway

import (
	"sync"
	"time"

	"game-server/internal/protocol"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ========== 连接管理 ==========

const (
	// 写超时：发送消息到客户端的最大等待时间
	// 超过此时间说明客户端网络异常，应断开连接
	writeWait = 10 * time.Second

	// Pong 超时：等待客户端 pong 响应的最大时间
	// 超过此时间说明客户端已掉线
	pongWait = 90 * time.Second

	// Ping 周期：服务器主动发送 ping 的间隔
	// 必须小于 pongWait，确保在超时前能收到响应
	pingPeriod = (pongWait * 9) / 10

	// 发送缓冲区大小
	// 缓冲区满后，新的发送操作会被阻塞或丢弃
	sendBufSize = 256
)

// Connection 封装一个客户端 WebSocket 连接
// 每个连接对应两个 goroutine：
//   - readPump: 从 WebSocket 读取消息，路由到 handler
//   - writePump: 从发送缓冲区读取消息，写入 WebSocket
//
// 这种读写分离的模式是 gorilla/websocket 推荐的做法，
// 原因：一个 WebSocket 连接同一时刻只允许一个写操作，多个 goroutine 并发写会 panic
type Connection struct {
	hub  *Hub            // 所属的连接管理中心
	conn *websocket.Conn // 底层 WebSocket 连接
	send chan []byte     // 发送缓冲区，writePump 从这里取数据发送

	// 玩家信息，登录成功后设置
	mu     sync.RWMutex
	uid    int64  // 已登录的玩家 ID，0 表示未登录
	token  string // 会话令牌
}

// newConnection 创建一个新的连接实例
func newConnection(hub *Hub, conn *websocket.Conn) *Connection {
	return &Connection{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, sendBufSize),
	}
}

// SetPlayerInfo 设置玩家信息（登录成功后调用）
func (c *Connection) SetPlayerInfo(uid int64, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uid = uid
	c.token = token
}

// GetUID 获取玩家 ID
func (c *Connection) GetUID() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uid
}

// IsLoggedIn 判断是否已登录
func (c *Connection) IsLoggedIn() bool {
	return c.GetUID() > 0
}

// SendMessage 向客户端发送消息
// 线程安全：实际写入 WebSocket 的操作由 writePump goroutine 完成
func (c *Connection) SendMessage(msgID uint16, payload interface{}) error {
	data, err := protocol.Encode(msgID, payload)
	if err != nil {
		return err
	}

	// 非阻塞发送：如果缓冲区满了，说明客户端处理不过来，丢弃消息
	// 为什么丢弃而不是阻塞？因为阻塞会导致服务器 goroutine 堆积，可能拖垮整个服务
	select {
	case c.send <- data:
		return nil
	default:
		zap.L().Warn("发送缓冲区已满，丢弃消息",
			zap.Int64("uid", c.GetUID()),
			zap.Uint16("msgID", msgID),
		)
		return nil
	}
}

// readPump 读取泵 —— 从 WebSocket 读取客户端消息
//
// 每个连接启动一个 readPump goroutine，负责：
//  1. 读取客户端发送的二进制消息
//  2. 解码为 protocol.Message
//  3. 交给 Router 分发到对应的 handler
//  4. 处理 pong 响应（刷新读超时）
//
// 退出条件：读取错误或连接关闭
func (c *Connection) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	// 设置读超时
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// 设置 pong 处理器：每次收到 pong，刷新读超时
	// 这是 WebSocket 心跳机制的标准做法
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		// 只处理二进制消息（我们的协议是二进制帧，不接受文本帧）
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			// 正常关闭（客户端主动断开）不算错误
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				zap.L().Error("WebSocket 读取异常",
					zap.Int64("uid", c.GetUID()),
					zap.Error(err),
				)
			}
			break
		}

		// 忽略非二进制消息
		if msgType != websocket.BinaryMessage {
			zap.L().Warn("收到非二进制消息，已忽略",
				zap.Int64("uid", c.GetUID()),
				zap.Int("msgType", msgType),
			)
			continue
		}

		// 解码消息
		msg, err := protocol.Decode(data)
		if err != nil {
			zap.L().Error("消息解码失败",
				zap.Int64("uid", c.GetUID()),
				zap.Error(err),
			)
			// 发送错误响应
			c.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrInvalidParam,
				Msg:  "消息格式错误",
			})
			continue
		}

		// 路由到对应的 handler
		c.hub.router.Route(c, msg)
	}
}

// writePump 写入泵 —— 将消息发送给客户端
//
// 每个连接启动一个 writePump goroutine，负责：
//  1. 从 send 通道取出待发送的消息
//  2. 写入 WebSocket
//  3. 定期发送 ping 保持连接活跃
//
// 退出条件：发送失败或连接关闭
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod) // 定时 ping
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// send 通道被关闭，说明 Hub 要求断开此连接
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// 写入消息
			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				zap.L().Error("WebSocket 写入失败",
					zap.Int64("uid", c.GetUID()),
					zap.Error(err),
				)
				return
			}

		case <-ticker.C:
			// 定期发送 ping
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
