// Package hooks 提供认证与限流的 pipeline 前置钩子（Before）。
//
// 取代旧 internal/middleware 的 router.Use 中间件：
//   - 逻辑等价（认证、限流），但改为 pipeline.BeforeHandler 形态
//   - 是否放行由返回 error 决定（返回 BizError 即中断，由 kernel 编码为错误帧）
//   - 免鉴权消息（登录、心跳）通过 kernel.IsAuthFree 判断跳过
//
// 放在独立包（而非 pipeline 包）是为了避免依赖环：
//
//	kernel -> pipeline（kernel 依赖 pipeline 类型）
//	hooks  -> kernel/store/session/pipeline（hooks 依赖它们，但没人依赖 hooks）
package hooks

import (
	"context"
	"strconv"
	"time"

	"game-server/internal/kernel"
	"game-server/internal/pipeline"
	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"

	"go.uber.org/zap"
)

// Auth 返回认证前置钩子。
//
// 校验逻辑：
//  1. 免鉴权消息（登录、心跳）直接放行
//  2. 未绑定 uid（未登录）→ 中断，返回 ErrUnauthorized
//  3. Redis 会话不存在（已过期）→ 中断，返回 ErrLoginTokenExpired
//  4. Redis 故障 → 降级放行（宁可放过，不可误杀）
//
// k 用于判断某消息是否免鉴权（对齐旧 Router 的 skip 逻辑）。
func Auth(redis *store.RedisStore, k *kernel.Kernel) pipeline.BeforeHandler {
	return func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		if k.IsAuthFree(kernel.MsgIDFromContext(ctx)) {
			return ctx, in, nil
		}

		s := session.FromContext(ctx)
		uid := int64(0)
		if s != nil {
			uid = s.UID()
		}
		if uid <= 0 {
			return ctx, in, protocol.NewBizError(protocol.ErrUnauthorized, "请先登录")
		}

		sess, err := redis.GetSession(uid)
		if err != nil {
			// Redis 故障降级：允许请求通过。
			zap.L().Error("读取会话失败", zap.Int64("uid", uid), zap.Error(err))
			return ctx, in, nil
		}
		if sess == nil {
			zap.L().Warn("会话已过期", zap.Int64("uid", uid))
			return ctx, in, protocol.NewBizError(protocol.ErrLoginTokenExpired, "会话已过期，请重新登录")
		}
		return ctx, in, nil
	}
}

// RateLimit 返回限流前置钩子。
//
// 按 uid 限流（每个玩家独立计数）。免鉴权消息与未登录请求不在此限流
// （登录请求的频控可另行处理）。Redis 故障时降级放行。
//
// 参数：limit 为时间窗口内允许的最大请求数，window 为时间窗口。
func RateLimit(redis *store.RedisStore, k *kernel.Kernel, limit int, window time.Duration) pipeline.BeforeHandler {
	return func(ctx context.Context, in interface{}) (context.Context, interface{}, error) {
		if k.IsAuthFree(kernel.MsgIDFromContext(ctx)) {
			return ctx, in, nil
		}
		s := session.FromContext(ctx)
		if s == nil || s.UID() <= 0 {
			return ctx, in, nil
		}
		uid := s.UID()

		allowed, err := redis.CheckRateLimit(strconv.FormatInt(uid, 10), limit, window)
		if err != nil {
			zap.L().Error("限流检查失败", zap.Int64("uid", uid), zap.Error(err))
			return ctx, in, nil
		}
		if !allowed {
			zap.L().Warn("请求过于频繁", zap.Int64("uid", uid))
			return ctx, in, protocol.NewBizError(protocol.ErrTooFrequent, "请求过于频繁，请稍后再试")
		}
		return ctx, in, nil
	}
}
