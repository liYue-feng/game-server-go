# Pitaya 借鉴式改造实施计划（game-server-go）

> **配套设计**：`docs/superpowers/specs/2026-07-20-pitaya-inspired-refactor-design.md`
> **执行方式**：按任务逐个推进（`- [ ]` 勾选跟踪）。每个任务完成后 `go build ./...` 必须通过；有逻辑落点的任务用 RED-GREEN（先写失败测试→实现→测试通过）。

**目标**：在**不改变线上二进制协议/消息 ID/错误码**、**不引入 pitaya 依赖**的前提下，把 `game-server-go` 的网络分发内核重构成 pitaya 风格（Component 方法 + Session + Pipeline 钩子），Unity 客户端零改动。

**技术栈**：Go 1.25、gorilla/websocket、go-redis/v8、gorm、zap、viper、标准库 `testing`。

## Global Constraints（全局约束）

- 只改 `F:\Own_project\game-server-go`；**不改 `F:\Own_project\pitaya`**，pitaya 仅只读参考。
- 不引入任何第三方新依赖；不引入 pitaya 依赖。
- 不改变协议帧格式、`MsgID_*`、`ErrCode`；不改 Unity 客户端。
- 每个任务结束时 `go build ./...` 必须为 0；涉及行为的任务先写失败测试再实现。
- 包注释、导出函数注释、关键决策注释必须补齐（项目定位是学习参考，注释解释 WHY）。
- 遵循现有命名：`MsgID_ModuleAction`、`ErrModule_Reason`；业务不直接用 gorm，走 `store`。
- 每完成一个任务，在 `docs/superpowers/` 下产出 `task-N-report.md`（做了什么、验证命令、结果）。
- 迁移期间保持增量可回滚：新内核与旧 gateway 可短暂并存，直到 main.go 切换完成再删死代码。

---

## Tasks（任务）

### Task 1 — 内核骨架：session + pipeline + BizError
- [ ] 新增 `internal/protocol/errors.go`：`BizError{Code int; Msg string}` 实现 `error`，配 `NewBizError`。
- [ ] 新增 `internal/session/session.go`：`Conn` 接口、`Session`（Bind/UID/Set/Get/Push/OnClose）、`context` 存取 `FromContext/WithSession`。
- [ ] 新增 `internal/pipeline/pipeline.go`：`BeforeHandler`/`AfterHandler`/`Hooks` 及 `ExecuteBefore/ExecuteAfter`。
- [ ] RED-GREEN：`session_test.go`（Bind/Push 经 fake Conn、OnClose 触发）、`pipeline_test.go`（Before 链中断、After 顺序）。
- verify：`go test ./internal/session/... ./internal/pipeline/...` 通过；`go build ./...` 通过。

### Task 2 — HandlerService 内核（反射分发 + 编解码）
- [ ] 新增 `internal/kernel/kernel.go`：`Kernel` 持有 `map[uint16]handlerEntry`（reqType/respID/反射方法/是否免鉴权）。
- [ ] `Register(reqID, respID uint16, fn any, opts...)`：反射校验签名 `func(ctx, *Req)(*Resp, error)`。
- [ ] `Dispatch(ctx, frame []byte)`：复用 `protocol.Decode` → 解 body 到 reqType → 跑 pipeline Before → 反射调用 → BizError/error→`MsgID_Error` 帧，正常→respID 帧（复用 `protocol.Encode`）。
- [ ] RED-GREEN：`kernel_test.go` 金标准——注册一个回显 handler，喂 `LoginReq` 帧，断言输出帧字节结构与 `protocol.Encode(respID, resp)` 完全一致（长度/MsgID/body）。
- verify：`go test ./internal/kernel/...` 通过；`go build ./...` 通过。

### Task 3 — transport 层（WebSocket 收发接入 kernel）
- [ ] 新增 `internal/transport/`：从 `gateway/connection.go`+`server.go` 抽出 WS 读写/心跳；`Connection` 实现 `session.Conn`。
- [ ] readPump 解出帧后调用 `kernel.Dispatch(ctxWithSession, data)`，不再依赖旧 `Router`。
- [ ] 保留旧 `gateway` 包暂不删除（过渡）。
- verify：`go build ./...` 通过（transport 可被 main 引用但暂不切换）。

### Task 4 — 样板迁移：login 组件
- [ ] `internal/login/handler.go` 改造：`Login(ctx,*LoginReq)(*LoginResp,error)`、`Heartbeat(...)`；用 `session.FromContext` 取代 `conn.SetPlayerInfo`（改 `s.Bind`+`s.Set`）。
- [ ] 错误返回改用 `protocol.NewBizError(code,msg)`。
- [ ] 在临时 `main` 装配路径注册 login 到 kernel，`go build` 通过。
- verify：`go build ./...`；如可行，加 `login` 层针对纯逻辑（token 生成）的小测试。

### Task 5 — 迁移 game / rank / combat 组件
- [ ] 三个模块 handler 逐个改为 `func(ctx,*Req)(*Resp,error)`，错误用 BizError，注册到 kernel。
- verify：每个模块迁移后 `go build ./...` 通过。

### Task 6 — 迁移 payment / gm 组件
- [ ] payment 的 WS 类请求（CreateOrder）进 kernel；HTTP 回调 `/pay/callback` 保持独立 HTTP server 不进内核。
- [ ] gm 的 GMCommand 进 kernel。
- verify：`go build ./...` 通过。

### Task 7 — middleware → pipeline 钩子
- [ ] 把 `middleware.AuthMiddleware/RateLimitMiddleware` 重写为 `pipeline.BeforeHandler`（AuthHook/RateLimitHook）。
- [ ] kernel 支持"免鉴权消息集合"（login/heartbeat），对齐旧 Router skip 逻辑。
- [ ] 删除 `internal/middleware`（其功能已迁移），清理因此产生的孤儿引用。
- verify：`go test ./internal/pipeline/...`；`go build ./...` 通过。

### Task 8 — main.go 切换 + 删除 gateway 死代码
- [ ] `cmd/server/main.go` 改为：装配 kernel（注册全部组件 + Before 钩子）→ 启动 transport server → 保留支付回调 HTTP server。
- [ ] 删除 `internal/gateway` 中已被 transport/kernel 取代的死代码；移除孤儿 import。
- [ ] （可选，来自 spec Open Question）顺带落地 WebSocket 优雅关闭。
- verify：`go build ./...` 通过；`go vet ./...` 通过；手动 smoke（可选）：本地起服务、用现有协议帧模拟 LoginReq 走通。

### Task 9 — 文档与记忆更新
- [ ] 更新 `README.md`/`AGENTS.md` 的架构章节，反映 kernel/session/pipeline 新分层。
- [ ] 更新 `.claude/memory/project-overview.md`：网络层由手写 gateway 改为 pitaya 风格内核。
- verify：文档与代码一致；`go build ./...` 通过。

---

## 验收标准（Definition of Done）

- 全量 `go build ./...` 与 `go vet ./...` 通过；新内核单测（session/pipeline/kernel）通过。
- 协议金标准测试证明响应帧字节与旧 `protocol.Encode` 一致 → Unity 客户端零改动。
- 业务 handler 全部为 `func(ctx,*Req)(*Resp,error)` 形态；认证/限流为 pipeline 钩子；旧 `gateway` 死代码与 `middleware` 包移除。
- pitaya 目录未被改动；`go.mod` 无 pitaya 依赖。
