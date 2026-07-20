# Pitaya 借鉴式改造设计（game-server-go）

## Context（背景）

`game-server-go` 是一个"吸血鬼幸存者"类微信小游戏的 Go 后端，定位为**学习参考项目**，采用单体模块化架构（`internal/` 分层）。当前网络层是手写的 `gorilla/websocket` 网关：`Hub` 管理连接、`Router` 按 `uint16` 消息 ID 分发、`Connection` 各自跑 `readPump/writePump`。业务模块 `login/game/rank/payment/combat/gm` 的 handler 签名是 `func(conn *gateway.Connection, body json.RawMessage)`，通过 `router.Register(MsgID, handler)` 注册；认证与限流通过 `router.Use(MiddlewareFunc)` 挂载。

`topfreegames/pitaya`（已克隆到 `F:\Own_project\pitaya`，仅作**只读架构参考**，不引入为依赖）是成熟的 Go 分布式游戏服务器框架。它的价值参考点是：

- **Component 模型**：业务是"组件+方法"，方法签名 `func(ctx, *Req) (*Resp, error)`，返回值即响应、error 即错误，框架统一编解码。
- **生命周期钩子**：`Init/AfterInit/BeforeShutdown/Shutdown`，组件自管理定时器与资源。
- **Session 抽象**：`session.Bind(uid)`、`session.Push`、`OnClose`，把"连接"与"玩家身份"解耦。
- **Pipeline（HandlerHooks）**：`BeforeHandler/AfterHandler` 责任链，取代散落的中间件。
- **Router/Handler 分层**：解码、路由、分发、编码由框架统一处理，业务只写纯函数。

**关键约束（来自用户）**：改造对象是 `game-server-go`；**pitaya 不改、不作为依赖引入，只借鉴其架构思想**；开发与查 bug 走 superpowers SDD 流程（先 spec、再 plan、执行产出 task 报告）。

**关键约束（来自代码现状）**：Unity 客户端（`game-client-unity/Assets/Scripts/Protocol/*`、`Network/*`）与当前**二进制帧协议**（`4B 长度 + 2B MsgID + JSON body`，小端）强耦合，且 `MsgID`/`ErrCode` 常量与服务器一一对应。因此本次改造**不改变线上协议与消息 ID**，Unity 客户端无需改动。

## Goals（目标）

- 把散落在 `gateway` 里的"路由 + 分发 + 中间件"重构成一套 **pitaya 风格的 Handler/Component 内核**，业务 handler 收敛为 `func(ctx, *Req) (*Resp, error)` 纯函数。
- 引入 **Session 抽象**，把玩家身份（uid/nickname/token）与底层 `Connection` 解耦，对齐 pitaya `session.Bind` 语义。
- 把认证、限流重构为 **Pipeline 钩子（BeforeHandler）**，对齐 pitaya `HandlerHooks`，取代 `router.Use` 的 `MiddlewareFunc`。
- 保持**线上二进制协议、消息 ID、错误码完全不变**，Unity 客户端零改动即可连通。
- 保持 `store/model/config/logger` 数据与基础设施层基本不动，改造聚焦网络/分发内核与业务 handler 形态。
- 全程可编译、可回归；为新内核补齐单元测试（codec/router/session/pipeline），当前项目无测试，属于"有逻辑落点"的新增。

## Non-Goals（非目标）

- 不引入 pitaya 作为 Go 依赖，不复制 pitaya 源码，不改动 `F:\Own_project\pitaya`。
- 不改变线上协议帧格式、消息 ID、错误码；不改动 Unity 客户端。
- 不引入 NATS/etcd/gRPC 等分布式集群组件（保持单体，符合项目既定架构决策）。
- 不改用 Protobuf 序列化（保持 JSON body）。
- 不实现待办清单里的其他独立特性（微信支付 V3 完整对接、配置热更新、Prometheus），除非与内核重构直接相关。
- 不重写 `store`/`model` 的数据访问逻辑。

## Selected Design（选定方案）

### 总体思路

在**保留二进制协议**的前提下，把 `gateway` 拆成两层，向 pitaya 对齐：

1. **传输层（transport）**：只负责 WebSocket 原始字节收发、心跳保活、连接生命周期。等价 pitaya 的 acceptor/agent。基本沿用现有 `Connection` 的 `readPump/writePump`，但剥离业务分发。
2. **服务层（service）**：`HandlerService` 负责"解码帧 → 定位 handler → 执行 Pipeline → 反射调用业务方法 → 编码响应"。等价 pitaya 的 `service.HandlerService` + `handlerPool`。

业务模块从"手动解析 body + 手动 SendMessage"升级为 **Component + 强类型方法**。

### Handler 签名与注册（对齐 pitaya component）

新的业务方法签名：

```go
// 有请求体、有响应
func (h *LoginComponent) Login(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error)
// 无响应（如心跳可回，也可 fire-and-forget）
func (h *LoginComponent) Heartbeat(ctx context.Context, req *protocol.HeartbeatReq) (*protocol.HeartbeatResp, error)
```

注册方式对齐 pitaya `app.Register(component)`，但因为线上用的是 `uint16 MsgID` 而非 route 字符串，注册表用**显式绑定**：

```go
kernel.Register(protocol.MsgID_LoginReq, protocol.MsgID_LoginResp, login.Login)
```

`kernel` 内部用反射把 `req` 从 `json.RawMessage` 解出、调用方法、把返回值编码为响应帧（响应帧的 MsgID 用注册时声明的 respID）。error 分两类：

- `*protocol.BizError`（新增，包裹 code+msg）→ 编码为 `MsgID_Error` 帧（对齐现有 `ErrorResp`）。
- 普通 error → 记 zap 日志 + 返回 `ErrInternal` 的 `MsgID_Error` 帧。

### Session 抽象（对齐 pitaya session）

新增 `internal/session`：

```go
type Session struct {
    conn     Conn          // 底层连接接口（WebSocket 发送能力）
    uid      int64
    data     map[string]any // nickname/token 等
    onClose  []func()
    mu       sync.RWMutex
}
func (s *Session) Bind(uid int64)          // 对齐 pitaya session.Bind
func (s *Session) UID() int64
func (s *Session) Set(k string, v any)
func (s *Session) Push(msgID uint16, payload any) error // 服务器主动推送
func (s *Session) OnClose(fn func())
```

`Conn` 是接口，生产实现由 WebSocket `Connection` 提供，测试可用 fake。ctx 里携带 `*Session`（`session.FromContext(ctx)`），对齐 pitaya `GetSessionFromCtx`。

### Pipeline（对齐 pitaya HandlerHooks）

新增 `internal/pipeline`：

```go
type BeforeHandler func(ctx context.Context, in any) (context.Context, any, error)
type AfterHandler  func(ctx context.Context, out any, err error) (any, error)
type Hooks struct { Before []BeforeHandler; After []AfterHandler }
```

认证与限流从 `middleware` 迁移为 Before 钩子：

- **AuthHook**：从 ctx 取 Session，校验 uid>0 且 Redis token 匹配；失败返回 `BizError(ErrUnauthorized)` 中断链。登录/心跳消息在 kernel 层标记为"免鉴权"（对齐现有 Router 的 skip 逻辑）。
- **RateLimitHook**：按 uid 走 `redis.CheckRateLimit`，超限返回 `BizError(ErrTooFrequent)`。

### 目录结构（改造后）

```
internal/
  transport/      # 新：WebSocket 原始收发 + 心跳（由现 gateway/connection.go+server.go 拆分而来）
  kernel/         # 新：HandlerService — 帧解码/路由/反射分发/编码（吸收 gateway/router.go）
  session/        # 新：Session 抽象
  pipeline/       # 新：BeforeHandler/AfterHandler 钩子链
  protocol/       # 基本不变；新增 BizError 类型
  login/ game/ rank/ payment/ combat/ gm/  # 改造为 Component，方法签名升级
  store/ model/ config/ middleware(迁移后移除)/
```

`gateway` 包在迁移完成后拆解：`hub.go`→`transport`，`router.go`→`kernel`，`connection.go`→`transport`+`session`。保留过渡期兼容说明写在报告里。

### 迁移策略（增量、可回归）

采用**内核先行 + 模块逐个迁移**，每步保持 `go build ./...` 通过：

1. 先建 `session`/`pipeline`/`kernel`/`transport` 骨架 + 单测（不接业务）。
2. 把 `login` 作为**样板模块**迁移，端到端跑通（登录+心跳），验证协议帧不变。
3. 依次迁移 `game`→`rank`→`combat`→`payment`→`gm`。
4. 迁移 `middleware`→`pipeline` 钩子，删除旧 `router.Use` 路径。
5. 删除 `gateway` 旧路由/分发死代码，`main.go` 切到新 `kernel` 启动。
6. 支付 HTTP 回调服务器（`:8081`）保持独立，不进内核。

### 兼容性验证

- 保留一份**协议帧金标准测试**：构造 `LoginReq` 帧 → 新 kernel 解码/分发/编码 → 断言响应帧字节结构与旧 `protocol.Encode` 一致（长度、MsgID、JSON body）。这是"Unity 不需要改"的机器可验证保证。

## Risks / Tradeoffs（风险与权衡）

- **反射调用开销**：pitaya 也用反射，吸血鬼幸存者类游戏消息量小，可接受；handler 注册在启动期完成、运行期只读，无锁。
- **一次性大改**：通过"内核先行 + 逐模块迁移 + 每步编译通过"降低风险，任何一步可独立回滚。
- **测试从零起步**：项目当前无测试；仅为**新增内核**补测，不给旧业务补测，避免超出改造范围。
- **协议不变的约束**：牺牲了直接采用 pitaya route 字符串的优雅，但换来 Unity 客户端零改动，符合用户"只改服务端"的要求。

## Open Questions（待确认）

- 是否希望最终**彻底删除** `gateway` 包，还是保留薄壳做过渡兼容？（默认：迁移完成后删除死代码。）
- 迁移完成后是否要顺带把待办里的"优雅关闭 WebSocket 连接"一并纳入（新 transport 天然更好做）？（默认：纳入，因与传输层重构强相关。）
