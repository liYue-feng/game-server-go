# Task 1 报告 — 内核骨架 session + pipeline + BizError

## 做了什么
- 新增 `internal/protocol/errors.go`：`BizError{Code,Msg}` 实现 error，配 `NewBizError`。区分业务错误（回传具体码）与系统错误（回 ErrInternal）。
- 新增 `internal/session/session.go`：`Conn` 接口 + `Session`（Bind/UID/IsBound/Set/Get/GetString/Push/OnClose/Close）+ ctx 存取 `WithSession/FromContext`。对齐 pitaya session。
- 新增 `internal/pipeline/pipeline.go`：`BeforeHandler/AfterHandler/Hooks` + `ExecuteBefore/ExecuteAfter`。对齐 pitaya HandlerHooks。
- 新增测试 `session_test.go`、`pipeline_test.go`。

## 验证
- `go test ./internal/session/... ./internal/pipeline/...` → ok（均通过）。
- `go build ./...` → 退出码 0。

## 备注
- `internal/kernel`、`internal/transport` 目录已创建但暂无 .go 文件（Task 2/3 填充），不影响构建。
- 未改动 pitaya；未新增第三方依赖。
