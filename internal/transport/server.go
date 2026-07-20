// Package transport —— WebSocket 服务器
//
// Server 是传输层入口：启动 HTTP 服务监听 WebSocket 升级请求，
// 为每个新连接创建 Connection、注册到 Hub、启动读写 goroutine。
// 业务处理全部委托给注入的 kernel.Kernel。
package transport

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"game-server/internal/kernel"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var errServerAlreadyStarted = errors.New("transport server already started")

type serverState uint8

const (
	serverNew serverState = iota
	serverRunning
	serverStopping
	serverStopped
)

// Server WebSocket 网关服务器。
type Server struct {
	hub      *Hub
	kernel   *kernel.Kernel
	upgrader websocket.Upgrader

	mu           sync.Mutex
	state        serverState
	httpSrv      *http.Server
	connections  sync.WaitGroup
	shutdownOnce sync.Once
}

// NewServer 创建服务器，绑定消息处理内核。
func NewServer(k *kernel.Kernel) *Server {
	return &Server{
		hub:    NewHub(),
		kernel: k,
		upgrader: websocket.Upgrader{
			// 开发环境允许所有来源；生产应改为只允许你的游戏域名（CSRF 防护）。
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
}

// Start 启动服务器（阻塞，直到 HTTP 服务关闭或出错）。
//
// 流程：启动 Hub 事件循环 -> 注册 /ws 与 /health 路由 -> ListenAndServe。
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{Addr: addr, Handler: mux}

	s.mu.Lock()
	switch s.state {
	case serverNew:
		s.state = serverRunning
		s.httpSrv = httpSrv
		s.hub.start()
	case serverStopping, serverStopped:
		s.mu.Unlock()
		return http.ErrServerClosed
	default:
		s.mu.Unlock()
		return errServerAlreadyStarted
	}
	s.mu.Unlock()

	zap.L().Info("WebSocket 服务器启动", zap.String("addr", addr))
	err := httpSrv.ListenAndServe()
	s.Shutdown()
	return err
}

// handleWebSocket 处理 WebSocket 升级请求：升级 -> 建连 -> 注册 -> 启动读写泵。
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.reserveConnection() {
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	reserved := true
	defer func() {
		if reserved {
			s.connections.Done()
		}
	}()

	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.L().Error("WebSocket 升级失败", zap.String("remote", r.RemoteAddr), zap.Error(err))
		return
	}
	c := newConnection(s.hub, wsConn, s.kernel)
	if !s.hub.Register(c) {
		c.stop()
		return
	}
	zap.L().Info("新 WebSocket 连接", zap.String("remote", wsConn.RemoteAddr().String()))

	reserved = false
	go func() {
		defer s.connections.Done()
		c.run()
	}()
}

func (s *Server) reserveConnection() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != serverRunning {
		return false
	}
	s.connections.Add(1)
	return true
}

// Hub 返回 Hub 实例（供外部广播、统计在线数）。
func (s *Server) Hub() *Hub {
	return s.hub
}

// Shutdown 优雅关闭：先停 HTTP 停止收新连接，再断开所有活跃连接。
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		if s.state == serverStopped {
			s.mu.Unlock()
			return
		}
		s.state = serverStopping
		httpSrv := s.httpSrv
		s.mu.Unlock()

		if httpSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := httpSrv.Shutdown(ctx); err != nil {
				zap.L().Error("HTTP 服务关闭失败", zap.Error(err))
			}
			cancel()
		}

		s.hub.Shutdown()
		s.connections.Wait()

		s.mu.Lock()
		s.state = serverStopped
		s.mu.Unlock()
	})
}
