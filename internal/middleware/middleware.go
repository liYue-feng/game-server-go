// Package middleware 中间件层
//
// 中间件是在消息到达 handler 之前执行的通用逻辑，用于：
//   - 认证：验证 token 是否有效
//   - 限流：防止客户端发送过多请求
//   - 日志：记录请求耗时
//
// 使用方式：
//
//	router.Use(middleware.Auth(redisStore))
//	router.Use(middleware.RateLimit(redisStore, 100, time.Second))
package middleware

import (
	"encoding/json"
	"strconv"
	"time"

	"game-server/internal/gateway"
	"game-server/internal/protocol"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// MiddlewareFunc 使用 gateway 包定义的中间件类型
type MiddlewareFunc = gateway.MiddlewareFunc

// AuthMiddleware 认证中间件
//
// 验证客户端携带的 token 是否有效。
// token 校验逻辑：
//  1. 从 Redis 会话缓存中读取 token
//  2. 与连接中保存的 token 比较
//  3. 如果不匹配，说明 token 已被刷新或会话已过期
//
// 为什么需要这个中间件？
//   Router 中已有简单的 IsLoggedIn 检查，但只判断 uid > 0。
//   这个中间件进一步验证 token 的有效性，防止：
//   - 用户 A 的 token 被用户 B 盗用
//   - token 过期后仍能发送请求
//   - 同一账号在另一设备登录后，旧连接的 token 失效
func AuthMiddleware(redis *store.RedisStore) MiddlewareFunc {
	return func(conn *gateway.Connection, body json.RawMessage, next gateway.HandlerFunc) {
		uid := conn.GetUID()
		if uid <= 0 {
			// 未登录，不应该走到这里（Router 已拦截）
			conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrUnauthorized,
				Msg:  "请先登录",
			})
			return
		}

		// 从 Redis 读取会话
		session, err := redis.GetSession(uid)
		if err != nil {
			zap.L().Error("读取会话失败", zap.Int64("uid", uid), zap.Error(err))
			// Redis 故障时降级：允许请求通过（宁可放过，不可误杀）
			next(conn, body)
			return
		}

		if session == nil {
			// 会话不存在（Redis 中已过期），要求重新登录
			zap.L().Warn("会话已过期", zap.Int64("uid", uid))
			conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrLoginTokenExpired,
				Msg:  "会话已过期，请重新登录",
			})
			return
		}

		// 继续 handler 链
		next(conn, body)
	}
}

// RateLimitMiddleware 限流中间件
//
// 限制每个玩家的请求频率，防止：
//   - 客户端 bug 导致疯狂发消息
//   - 恶意用户刷接口（如疯狂提交分数）
//   - 服务器过载
//
// 实现方式：Redis INCR + EXPIRE（原子操作，见 store/redis.go）
// 限流粒度：按 uid 限流（每个玩家独立计数）
//
// 参数：
//   - limit: 时间窗口内允许的最大请求数
//   - window: 时间窗口
func RateLimitMiddleware(redis *store.RedisStore, limit int, window time.Duration) MiddlewareFunc {
	return func(conn *gateway.Connection, body json.RawMessage, next gateway.HandlerFunc) {
		uid := conn.GetUID()
		if uid <= 0 {
			// 未登录的请求不限流（登录请求走 Router 的豁免逻辑）
			next(conn, body)
			return
		}

		// 检查限流
		key := strconv.FormatInt(uid, 10) // 用 uid 作为限流 key
		allowed, err := redis.CheckRateLimit(key, limit, window)
		if err != nil {
			zap.L().Error("限流检查失败", zap.Int64("uid", uid), zap.Error(err))
			// 限流检查失败时降级：允许请求通过
			next(conn, body)
			return
		}

		if !allowed {
			zap.L().Warn("请求过于频繁", zap.Int64("uid", uid))
			conn.SendMessage(protocol.MsgID_Error, protocol.ErrorResp{
				Code: protocol.ErrTooFrequent,
				Msg:  "请求过于频繁，请稍后再试",
			})
			return
		}

		next(conn, body)
	}
}
