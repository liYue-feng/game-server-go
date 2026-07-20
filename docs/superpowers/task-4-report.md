# Task 4 报告 — 样板迁移：login 组件

## 做了什么
- 重写 `internal/login/handler.go`：`Login(ctx,*LoginReq)(*LoginResp,error)`、`Heartbeat(ctx,*HeartbeatReq)(*HeartbeatResp,error)`。
- 玩家身份改用 `session.FromContext(ctx)`：登录成功后 `s.Bind(uid)` + `s.Set(nickname/token)`（取代旧 `conn.SetPlayerInfo`）。
- 错误统一返回 `protocol.NewBizError(code,msg)`。
- 移除对 `gateway`/`encoding/json` 的依赖。

## 验证
- `go build ./internal/login/...` → 0。

## 备注
- 迁移改变了方法签名，`cmd/server/main.go`（旧 gateway 路由）暂时编译不过，属预期；Task 8 切换 main 后恢复 `go build ./...`。逐模块用分包构建验证。
