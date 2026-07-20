# Task 7 报告 — middleware 转 pipeline 钩子

## 做了什么
- 新增 `internal/hooks/hooks.go`：`Auth(redis,k)` 与 `RateLimit(redis,k,limit,window)` 两个 `pipeline.BeforeHandler`。
  - 逻辑等价旧中间件：认证（免鉴权放行 / 未登录 ErrUnauthorized / 会话过期 ErrLoginTokenExpired / Redis 故障降级放行）、限流（按 uid、Redis 故障降级、超限 ErrTooFrequent）。
  - 免鉴权判断复用 `kernel.IsAuthFree` + `kernel.MsgIDFromContext`。
- 独立成 `hooks` 包避免依赖环（kernel 依赖 pipeline；hooks 依赖 kernel/store/session/pipeline，无人依赖 hooks）。
- 删除 `internal/middleware`（功能已迁移）。

## 验证
- `go build ./internal/hooks/...` 退出码 0；`internal/middleware` 目录已移除。

## 备注
- 钩子注册顺序在 main（Task 8）：先 Auth 后 RateLimit。
