// Package gateway — 连接管理中心 (Hub)
//
// Hub 维护所有活跃的 WebSocket 连接，负责：
//   - 新连接注册
//   - 断开连接清理
//   - 广播消息（如服务器公告、全服推送）
//
// 设计模式：这是经典的 Hub-Client 模式，出自 gorilla/websocket 的 chat 示例。
// Hub 在独立的 goroutine 中运行，通过 channel 接收注册/注销请求，
// 避免对连接 map 的并发访问。
package gateway

import (
	"game-server/internal/protocol"

	"go.uber.org/zap"
)

// Hub 管理所有活跃的客户端连接
type Hub struct {
	// 所有已注册的连接
	// key 是连接指针，value 无实际用途（用 struct{} 节省内存）
	connections map[*Connection]struct{}

	// 注册请求 channel
	// 新连接建立时，通过此 channel 通知 Hub
	register chan *Connection

	// 注销请求 channel
	// 连接断开时，通过此 channel 通知 Hub
	unregister chan *Connection

	// 消息路由器
	router *Router
}

// NewHub 创建一个新的连接管理中心
func NewHub(router *Router) *Hub {
	return &Hub{
		connections: make(map[*Connection]struct{}),
		register:   make(chan *Connection),
		unregister: make(chan *Connection),
		router:     router,
	}
}

// Run 启动 Hub 的事件循环
// 应该在独立的 goroutine 中运行，通常在服务器启动时调用：
//
//	go hub.Run()
func (h *Hub) Run() {
	zap.L().Info("Hub 启动")
	for {
		select {
		case conn := <-h.register:
			// 新连接加入
			h.connections[conn] = struct{}{}
			zap.L().Info("新连接加入",
				zap.Int("total", len(h.connections)),
			)

		case conn := <-h.unregister:
			// 连接断开，清理资源
			if _, ok := h.connections[conn]; ok {
				delete(h.connections, conn)
				// 关闭 send 通道，通知 writePump 退出
				close(conn.send)
				zap.L().Info("连接断开",
					zap.Int64("uid", conn.GetUID()),
					zap.Int("total", len(h.connections)),
				)
			}
		}
	}
}

// Register 注册一个新连接
// 线程安全：通过 channel 传递，Hub 在自己的 goroutine 中处理
func (h *Hub) Register(conn *Connection) {
	h.register <- conn
}

// Unregister 注销一个连接
func (h *Hub) Unregister(conn *Connection) {
	h.unregister <- conn
}

// OnlineCount 返回当前在线连接数
// 注意：此方法直接读取 map，在 Hub goroutine 之外调用可能存在竞态
// 如果需要精确计数，应通过 channel 请求 Hub 返回
func (h *Hub) OnlineCount() int {
	return len(h.connections)
}

// Broadcast 向所有在线连接广播消息
// 典型场景：服务器公告、活动开始通知、版本更新提示
func (h *Hub) Broadcast(msgID uint16, payload interface{}) {
	data, err := protocol.Encode(msgID, payload)
	if err != nil {
		zap.L().Error("广播消息编码失败", zap.Error(err))
		return
	}

	for conn := range h.connections {
		select {
		case conn.send <- data:
		default:
			// 发送缓冲区满，跳过此连接
			// 广播消息的重要性通常低于单播，不应因某个客户端卡住而影响其他客户端
			zap.L().Warn("广播跳过：发送缓冲区已满",
				zap.Int64("uid", conn.GetUID()),
			)
		}
	}
}

// CloseAllConnections 关闭所有活跃的客户端连接
// 用于服务器优雅关闭，确保所有连接干净退出
func (h *Hub) CloseAllConnections() {
	zap.L().Info("开始关闭所有客户端连接", zap.Int("count", len(h.connections)))
	// 直接遍历并关闭连接，避免通过 unregister channel 阻塞
	for conn := range h.connections {
		close(conn.send)
		delete(h.connections, conn)
	}
	zap.L().Info("所有客户端连接已关闭")
}
