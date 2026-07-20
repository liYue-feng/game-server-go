// Package transport —— 连接管理中心 (Hub)
//
// Hub 维护所有活跃连接，负责新连接注册、断开清理、广播、优雅关闭。
// 采用经典 Hub-Client 模式：Hub 在独立 goroutine 中通过 channel 收发注册/注销请求，
// 避免对连接 map 的并发访问。
package transport

import (
	"game-server/internal/protocol"

	"go.uber.org/zap"
)

// Hub 管理所有活跃的客户端连接。
type Hub struct {
	connections map[*Connection]struct{}
	register    chan *Connection
	unregister  chan *Connection
	closeAll    chan struct{} // 优雅关闭信号：要求断开所有连接并结束 Run
	done        chan struct{} // Run 退出后关闭，供 Shutdown 等待
}

// NewHub 创建连接管理中心。
func NewHub() *Hub {
	return &Hub{
		connections: make(map[*Connection]struct{}),
		register:    make(chan *Connection),
		unregister:  make(chan *Connection),
		closeAll:    make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// Run 启动 Hub 事件循环，应在独立 goroutine 中运行。
//
// 所有对 connections map 的写操作都集中在此 goroutine，天然无锁安全。
func (h *Hub) Run() {
	zap.L().Info("Hub 启动")
	for {
		select {
		case conn := <-h.register:
			h.connections[conn] = struct{}{}
			zap.L().Info("新连接加入", zap.Int("total", len(h.connections)))

		case conn := <-h.unregister:
			if _, ok := h.connections[conn]; ok {
				delete(h.connections, conn)
				close(conn.send) // 通知 writePump 退出
				zap.L().Info("连接断开",
					zap.Int64("uid", conn.sess.UID()),
					zap.Int("total", len(h.connections)),
				)
			}

		case <-h.closeAll:
			// 优雅关闭：断开所有连接并结束事件循环。
			zap.L().Info("Hub 开始关闭所有连接", zap.Int("count", len(h.connections)))
			for conn := range h.connections {
				close(conn.send)
				delete(h.connections, conn)
			}
			close(h.done)
			return
		}
	}
}

// Register 注册新连接（线程安全，经 channel 传递）。
func (h *Hub) Register(conn *Connection) {
	h.register <- conn
}

// Unregister 注销连接。
//
// 注意：readPump 退出时会调用它。若 Hub 已因 closeAll 退出，
// 这里用 select+done 兜底，避免向已停止的 Run 发送而永久阻塞。
func (h *Hub) Unregister(conn *Connection) {
	select {
	case h.unregister <- conn:
	case <-h.done:
		// Hub 已关闭，无需再注销
	}
}

// OnlineCount 返回当前在线连接数（读 map，Hub goroutine 外调用可能有竞态，仅供近似统计）。
func (h *Hub) OnlineCount() int {
	return len(h.connections)
}

// Broadcast 向所有在线连接广播消息（服务器公告、活动通知等）。
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
			zap.L().Warn("广播跳过：发送缓冲区已满", zap.Int64("uid", conn.sess.UID()))
		}
	}
}

// Shutdown 优雅关闭：触发 closeAll 并等待 Run 结束。
func (h *Hub) Shutdown() {
	select {
	case h.closeAll <- struct{}{}:
		<-h.done
	case <-h.done:
		// 已经关闭
	}
}
