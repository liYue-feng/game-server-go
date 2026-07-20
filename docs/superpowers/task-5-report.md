# Task 5 报告 — 迁移 game / rank / combat 组件

## 做了什么
- `internal/game/handler.go`：`SaveArchive`、`LoadArchive` → `func(ctx,*Req)(*Resp,error)`。
- `internal/rank/handler.go`：`GetRank`、`SubmitScore` 同上；保留 parseMember 与异步写 MySQL 历史。
- `internal/combat/handler.go`：7 个方法全部迁移（CombatResult / GetEnemyConfigs / GetDungeonConfig / GetStyleConfigs / UnlockStyle / GetPlayerStats / UpdatePlayerStats）。
- 三模块统一用 `session.FromContext(ctx).UID()` 取身份、`protocol.NewBizError` 返错。

## 验证
- `go build ./internal/combat/... ./internal/login/... ./internal/game/... ./internal/rank/...` → 0。

## 备注
- combat 保留 config.go/validator.go 不变；仅 handler.go 改造。
- main.go 仍待 Task 8 切换。
