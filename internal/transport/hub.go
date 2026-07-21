// Package transport —— 连接管理中心 (Hub)
//
// Hub 维护所有活跃连接，负责新连接注册、断开清理、广播、优雅关闭。
// 采用经典 Hub-Client 模式：Hub 在独立 goroutine 中通过 channel 收发注册/注销请求，
// 避免对连接 map 的并发访问。
package transport

import (
	"sync"

	"game-server/internal/protocol"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// Hub 管理所有活跃的客户端连接。
type Hub struct {
	connections map[*Connection]struct{}
	register    chan *Connection
	unregister  chan *Connection
	broadcast   chan broadcastRequest
	pushToUID   chan pushToUIDRequest
	count       chan chan int
	closeAll    chan struct{} // 优雅关闭信号：要求断开所有连接并结束 Run
	done        chan struct{} // Run 退出后关闭，供 Shutdown 等待
	startOnce   sync.Once
	stopOnce    sync.Once
}

type broadcastRequest struct {
	msgID uint16
	data  []byte
}
type pushToUIDRequest struct {
	uid       int64
	msgID     uint16
	data      []byte
	delivered chan bool
}

// NewHub 创建连接管理中心。
func NewHub() *Hub {
	return &Hub{
		connections: make(map[*Connection]struct{}),
		register:    make(chan *Connection),
		unregister:  make(chan *Connection),
		broadcast:   make(chan broadcastRequest),
		pushToUID:   make(chan pushToUIDRequest),
		count:       make(chan chan int),
		closeAll:    make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// Run 启动 Hub 事件循环，应在独立 goroutine 中运行。
//
// 所有对 connections map 的写操作都集中在此 goroutine，天然无锁安全。
func (h *Hub) Run() {
	h.start()
	<-h.done
}

func (h *Hub) start() {
	h.startOnce.Do(func() {
		go h.run()
	})
}

func (h *Hub) run() {
	defer close(h.done)
	zap.L().Info("Hub 启动")
	for {
		select {
		case conn := <-h.register:
			h.connections[conn] = struct{}{}
			zap.L().Info("新连接加入", zap.Int("total", len(h.connections)))

		case conn := <-h.unregister:
			if _, ok := h.connections[conn]; ok {
				delete(h.connections, conn)
				conn.stop()
				zap.L().Info("连接断开",
					zap.Int64("uid", conn.sess.UID()),
					zap.Int("total", len(h.connections)),
				)
			}

		case request := <-h.broadcast:
			for conn := range h.connections {
				if err := conn.enqueue(request.data); err != nil {
					if err == errSendBufferFull {
						zap.L().Warn("广播跳过：发送缓冲区已满",
							zap.Int64("uid", conn.sess.UID()),
							zap.Uint16("msgID", request.msgID),
						)
					}
				}
			}
		case request := <-h.pushToUID:
			delivered := false
			for conn := range h.connections {
				if conn.sess.UID() == request.uid && conn.enqueue(request.data) == nil {
					delivered = true
					break
				}
			}
			request.delivered <- delivered

		case response := <-h.count:
			response <- len(h.connections)

		case <-h.closeAll:
			// 优雅关闭：断开所有连接并结束事件循环。
			zap.L().Info("Hub 开始关闭所有连接", zap.Int("count", len(h.connections)))
			for conn := range h.connections {
				conn.stop()
				delete(h.connections, conn)
			}
			return
		}
	}
}

func (h *Hub) PushToUID(uid int64, msgID uint16, payload proto.Message) bool {
	data, err := protocol.Encode(msgID, 0, payload)
	if err != nil {
		return false
	}
	h.start()
	result := make(chan bool, 1)
	select {
	case h.pushToUID <- pushToUIDRequest{uid: uid, msgID: msgID, data: data, delivered: result}:
		return <-result
	case <-h.closeAll:
		return false
	case <-h.done:
		return false
	}
}

// Register 注册新连接（线程安全，经 channel 传递）。
func (h *Hub) Register(conn *Connection) bool {
	h.start()
	select {
	case h.register <- conn:
		return true
	case <-h.closeAll:
		return false
	case <-h.done:
		return false
	}
}

// Unregister 注销连接。
//
// 注意：readPump 退出时会调用它。若 Hub 已因 closeAll 退出，
// 这里用 select+done 兜底，避免向已停止的 Run 发送而永久阻塞。
func (h *Hub) Unregister(conn *Connection) {
	h.start()
	select {
	case h.unregister <- conn:
	case <-h.closeAll:
	case <-h.done:
		// Hub 已关闭，无需再注销
	}
}

// OnlineCount 返回当前在线连接数。查询经 Hub 事件循环串行化，不直接并发读取连接 map。
func (h *Hub) OnlineCount() int {
	h.start()
	response := make(chan int, 1)
	select {
	case h.count <- response:
		return <-response
	case <-h.closeAll:
		return 0
	case <-h.done:
		return 0
	}
}

// Broadcast 向所有在线连接广播消息（服务器公告、活动通知等）。
func (h *Hub) Broadcast(msgID uint16, payload proto.Message) {
	data, err := protocol.Encode(msgID, 0, payload)
	if err != nil {
		zap.L().Error("广播消息编码失败", zap.Error(err))
		return
	}
	h.start()
	select {
	case h.broadcast <- broadcastRequest{msgID: msgID, data: data}:
	case <-h.closeAll:
	case <-h.done:
	}
}

// Shutdown 优雅关闭：触发 closeAll 并等待 Run 结束。
func (h *Hub) Shutdown() {
	h.start()
	h.stopOnce.Do(func() {
		close(h.closeAll)
	})
	<-h.done
}
