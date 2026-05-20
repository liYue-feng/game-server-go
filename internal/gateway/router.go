// Package gateway — 消息路由器
//
// Router 负责将收到的消息按 MsgID 分发到对应的 handler 函数。
// 这是一种经典的"事件驱动"模式，类似于 Erlang/OTP 的 gen_server:handle_info。
//
// 支持中间件链：消息在到达 handler 之前，会依次经过所有注册的中间件。
// 中间件可以执行认证、限流、日志等横切逻辑，然后决定是否继续处理。
//
// 路由表在服务器启动时注册，运行时不可修改（无需加锁）。
// 每条消息在独立的 goroutine 中处理，handler 之间互不阻塞。
package gateway

import (
	"encoding/json"

	"game-server/internal/protocol"

	"go.uber.org/zap"
)

// HandlerFunc 消息处理函数类型
//
// 参数：
//   - conn: 发送消息的客户端连接，可以通过它回复消息或获取玩家信息
//   - body: 消息体的原始 JSON 字节，handler 内自行反序列化到具体类型
//
// 设计说明：body 传 json.RawMessage 而非 interface{}，是因为：
//   - 延迟解析：路由层不需要知道每种消息的具体类型
//   - 性能：避免无用的 JSON 反序列化
//   - 类型安全：handler 内按具体类型解析，编译期就能发现类型错误
type HandlerFunc func(conn *Connection, body json.RawMessage)

// MiddlewareFunc 中间件函数类型
//
// 中间件在 handler 之前执行，可以：
//   - 执行前置逻辑（认证、限流、日志）
//   - 调用 next(conn, body) 继续处理
//   - 不调用 next 则中断处理链（如认证失败）
type MiddlewareFunc func(conn *Connection, body json.RawMessage, next HandlerFunc)

// Router 消息路由器
type Router struct {
	handlers    map[uint16]HandlerFunc    // MsgID -> Handler 的映射表
	middlewares []MiddlewareFunc          // 全局中间件链
}

// NewRouter 创建一个新的路由器
func NewRouter() *Router {
	return &Router{
		handlers: make(map[uint16]HandlerFunc),
	}
}

// Use 注册全局中间件
// 中间件按注册顺序执行：先注册的先执行
// 必须在 Register 之前调用，否则不会应用到已注册的 handler
//
// 使用方式：
//
//	router.Use(middleware.AuthMiddleware(redis))
//	router.Use(middleware.RateLimitMiddleware(redis, 100, time.Second))
func (r *Router) Use(mw MiddlewareFunc) {
	r.middlewares = append(r.middlewares, mw)
	zap.L().Info("注册中间件", zap.Int("index", len(r.middlewares)))
}

// Register 注册消息处理函数
//
// 一个 MsgID 只能注册一个 handler，重复注册会 panic（这是编程错误，应尽早发现）
//
// 使用方式：
//
//	router.Register(protocol.MsgID_LoginReq, loginHandler.HandleLogin)
//	router.Register(protocol.MsgID_HeartbeatReq, handleHeartbeat)
func (r *Router) Register(msgID uint16, handler HandlerFunc) {
	if _, exists := r.handlers[msgID]; exists {
		zap.L().Panic("重复注册消息处理器",
			zap.Uint16("msgID", msgID),
		)
	}
	r.handlers[msgID] = handler
	zap.L().Info("注册消息处理器", zap.Uint16("msgID", msgID))
}

// Route 将消息路由到对应的 handler
//
// 执行流程：
//  1. 查找 handler，未找到则返回错误
//  2. 登录检查（内置守卫）
//  3. 执行中间件链
//  4. 执行 handler
//
// 中间件链的执行方式类似洋葱模型：
//
//	mw1(before) -> mw2(before) -> mw3(before) -> handler -> mw3(after) -> mw2(after) -> mw1(after)
//
// 但当前实现是前置模式（中间件只做 before，调用 next 后不关心 after），
// 如果需要 after 逻辑，中间件可以在 next 之后添加代码。
func (r *Router) Route(conn *Connection, msg *protocol.Message) {
	handler, exists := r.handlers[msg.MsgID]
	if !exists {
		zap.L().Warn("未注册的消息ID",
			zap.Uint16("msgID", msg.MsgID),
			zap.Int64("uid", conn.GetUID()),
		)
		conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
			Code: protocol.ErrInvalidParam,
			Msg:  "不支持的消息类型",
		})
		return
	}

	// 登录检查：除了登录和心跳，其他消息都需要先登录
	// 这是一个通用的鉴权守卫，避免在每个 handler 中重复检查
	if msg.MsgID != protocol.MsgID_LoginReq && msg.MsgID != protocol.MsgID_HeartbeatReq {
		if !conn.IsLoggedIn() {
			zap.L().Warn("未登录的请求",
				zap.Uint16("msgID", msg.MsgID),
			)
			conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrUnauthorized,
				Msg:  "请先登录",
			})
			return
		}
	}

	// 构建中间件链：从最后一个中间件开始，反向包裹 handler
	// 这样第一个注册的中间件最先执行，符合直觉
	finalHandler := handler
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		mw := r.middlewares[i]
		// 用闭包捕获当前的 finalHandler
		current := finalHandler
		finalHandler = func(conn *Connection, body json.RawMessage) {
			mw(conn, body, current)
		}
	}

	// 在独立 goroutine 中执行完整的中间件链 + handler
	go finalHandler(conn, msg.Body)
}
