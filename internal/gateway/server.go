// Package gateway — WebSocket 服务器
//
// Server 是网关层的入口，负责：
//   - 启动 HTTP 服务器，监听 WebSocket 升级请求
//   - 为每个新连接创建 Connection 并注册到 Hub
//   - 启动连接的读写 goroutine
package gateway

import (
	"net/http"

	"go.uber.org/zap"
	"github.com/gorilla/websocket"
)

// Server WebSocket 网关服务器
type Server struct {
	hub      *Hub                // 连接管理中心
	upgrader websocket.Upgrader  // HTTP -> WebSocket 升级器
}

// NewServer 创建 WebSocket 网关服务器
func NewServer(router *Router) *Server {
	return &Server{
		hub: NewHub(router),
		upgrader: websocket.Upgrader{
			// CheckOrigin 检查请求来源，用于 CSRF 防护
			// 生产环境应改为只允许你的游戏域名
			// 开发环境设为 true 允许所有来源（方便本地调试）
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			// 二进制消息最大长度
			// 超过此大小的消息会被拒绝（防止恶意大帧攻击）
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// Start 启动服务器
// 1. 启动 Hub 事件循环
// 2. 注册 HTTP 路由
// 3. 启动 HTTP 服务器（它会自动处理 WebSocket 升级）
func (s *Server) Start(addr string) error {
	// 启动 Hub
	go s.hub.Run()

	// 注册 WebSocket 路由
	// 客户端通过 ws://host:port/ws 连接
	http.HandleFunc("/ws", s.handleWebSocket)

	// 健康检查端点，用于 K8s/Docker 健康探针
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	zap.L().Info("WebSocket 服务器启动", zap.String("addr", addr))

	// ListenAndServe 会阻塞，直到服务器关闭或出错
	return http.ListenAndServe(addr, nil)
}

// handleWebSocket 处理 WebSocket 升级请求
//
// 流程：
//  1. 将 HTTP 连接升级为 WebSocket
//  2. 创建 Connection 对象
//  3. 注册到 Hub
//  4. 启动读写 goroutine
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级 HTTP 连接到 WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("WebSocket 升级失败",
			zap.String("remote", r.RemoteAddr),
			zap.Error(err),
		)
		return
	}

	// 创建连接对象
	c := newConnection(s.hub, conn)

	// 注册到 Hub
	s.hub.Register(c)

	zap.L().Info("新 WebSocket 连接",
		zap.String("remote", conn.RemoteAddr().String()),
	)

	// 启动读写 goroutine
	// 这两个 goroutine 的生命周期与连接相同，连接断开时自动退出
	// 注意：readPump 和 writePump 都有 defer 关闭连接的逻辑，
	// 所以任意一方退出都会导致连接关闭，另一方也会随之退出
	go c.writePump()
	go c.readPump()
}

// Hub 返回 Hub 实例（供外部使用，如广播消息）
func (s *Server) Hub() *Hub {
	return s.hub
}
