# 带序列号的 Protobuf 网络协议迁移设计

日期：2026-07-21

状态：已完成分段设计确认，等待书面规格复核

涉及仓库：

- `E:/Own_project/game-client-unity`
- `E:/Own_project/game-server-go`

## 1. 目标

客户端与服务端从 JSON Body 和 6 字节帧头一次性迁移到 Protobuf Body 和 10 字节帧头。客户端为每个请求分配递增 `seq`，服务端响应原样回传该 `seq`，客户端据此关联并完成对应请求。

迁移后必须保留现有完整业务协议，重点保证登录、心跳、存档、排行、配置、支付、GM、战斗结算、重复结算和通关流程可用。

## 2. 非目标

- 不保留旧 6 字节帧或 JSON Body 兼容层。
- 不用桌面旧版 `game.proto` 覆盖当前完整业务模型。
- 不在网络层增加第二套业务超时和重试计时器。
- 不让推送消息占用请求 `seq`。

## 3. 唯一协议源与生成物

两个仓库各自保存一份 `proto/game.proto`，文件内容和 SHA-256 必须完全一致。桌面文件 `C:/Users/23906/Desktop/game.proto` 只作为帧格式、`seq` 和消息编号迁移参考；最终协议以当前项目的完整业务字段为基础。

服务端：

- 输入：`proto/game.proto`
- 输出：`internal/protocolpb/game.pb.go`
- 工具：`protoc 35.0`、`protoc-gen-go v1.36.11`
- Go package：`game-server/internal/protocolpb;protocolpb`

Unity 客户端：

- 输入：`proto/game.proto`
- 暂存输出：`tools/protobuf/generated/Game.cs`
- 运行时输出：`Assets/Scripts/Protocol/Generated/Game.cs`
- 工具：`protoc 35.0`、`Google.Protobuf 3.35.1`
- C# namespace：`Game.Protocol`

新的生成链路验证通过后，删除旧 `messages.proto`、`messages.pb.go` 和 `Messages.cs`，避免双协议源。

## 4. 帧格式

所有多字节整数使用小端序：

```text
[0..3]  Length uint32 = 10 + BodyLength
[4..5]  MsgID  uint16
[6..9]  Seq    uint32
[10..]  Body   protobuf bytes
```

约束：

- `HeaderSize = 10`
- `Length` 是完整帧长，包含自身 4 字节以及整个帧头。
- `MaxFrameSize = 64 KiB`，包含帧头。
- 普通请求必须使用非零 `seq`。
- 服务端主动推送必须使用 `seq = 0`。

黄金帧：`LoginReq { code: "abc" }`、`MsgID = 1001`、`seq = 1` 的完整字节为：

```text
0F 00 00 00 E9 03 01 00 00 00 0A 03 61 62 63
```

## 5. 客户端请求关联

`NetworkClient` 是唯一 `seq` 分配者和 pending 请求所有者。建议公开接口：

```csharp
bool Request<TRequest, TResponse>(
    ushort requestId,
    ushort responseId,
    TRequest payload,
    Action<TResponse> onSuccess,
    Action<string> onFailure,
    out uint seq)

bool CancelRequest(uint seq)
```

分配规则：

- 首次请求使用 `seq = 1`，每次递增。
- `0` 永远保留给服务端推送。
- `uint.MaxValue` 后回绕到 `1`。
- 回绕时跳过 pending 表中仍占用的序列号。
- pending 表必须线程安全，记录期望响应 MsgID、解析器、成功回调和失败回调。

完成规则：

- 正常响应、`ErrorResp`、错误 MsgID、Body 解析失败、主动取消、断线和 `Dispose` 都只能完成一次。
- `seq != 0` 只进入 pending 响应路径；未知非零 `seq` 记录日志后丢弃。
- `seq = 0` 只进入现有 `On<T>` 推送分发路径。
- 重试由现有业务协调器负责，每次重试分配新 `seq`，但复用同一个业务 `run_id`。
- 迁移全部请求调用点：登录、心跳、存档、排行、战斗、配置、支付、GM。

## 6. 服务端响应与推送

协议层接口调整为：

```go
func Encode(msgID uint16, seq uint32, payload proto.Message) ([]byte, error)
```

解码结果必须包含 `Seq`。连接内核对普通请求要求非零 `seq`，并在以下响应中原样回传请求 `seq`：

- 正常业务响应
- `ErrorResp`
- 鉴权失败响应
- hook 或统一错误处理生成的响应

发送语义必须显式区分：

```go
Reply(seq uint32, msgID uint16, payload proto.Message)
Push(msgID uint16, payload proto.Message)
```

`Push` 始终编码为 `seq = 0`。Hub 广播、支付通知和 GM 推送都使用 `Push`。

结构不完整或非法长度的帧头直接关闭连接。帧结构有效但业务请求无效时，返回带该帧 `seq` 的 `ErrorResp`。

## 7. 原子迁移顺序

迁移按同一交付批次完成，不提供双栈兼容：

1. 固化并同步 `proto/game.proto`，完成双端生成和漂移检查。
2. 服务端编解码增加 `seq`，区分 `Reply` 与 `Push`。
3. 客户端编解码增加 `seq`，实现分配器和 pending 关联。
4. 迁移所有请求调用点和所有服务端响应/推送调用点。
5. 更新协议验证器、开发探针、真实后端联调脚本和文档。
6. 完成全量测试、战斗专项测试及资源完整性审计。

## 8. 错误与边界行为

- 重复响应：第一次完成 pending，后续同 `seq` 响应视为未知响应并丢弃。
- 错误 MsgID：移除 pending，并以协议错误调用失败回调。
- Body 解析失败：移除 pending，并以解析错误调用失败回调。
- 断线或销毁：一次性失败并清空全部 pending。
- 取消请求：移除指定 pending；迟到响应按未知 `seq` 丢弃。
- 乱序响应：只按 `seq` 关联，不依赖 MsgID 到达顺序。
- 相同 MsgID 并发请求：使用不同 `seq` 独立完成。

## 9. 验证策略

协议与生成链路：

- 比较两个 `proto/game.proto` 的原始 SHA-256。
- 重新生成并验证工作树无生成漂移。
- 双端黄金帧测试必须得到完全相同的 15 字节。
- 验证 `Length`、最大帧、半包、粘包、非法长度和非法 Body。

客户端网络测试：

- `seq` 递增、回绕、跳过 pending。
- 乱序响应、相同 MsgID 并发、`ErrorResp`、错误 MsgID。
- malformed Body、未知/重复 `seq`、断线、取消、销毁。
- `seq = 0` 推送不进入 pending。

服务端验证：

- `go test ./...`
- `go vet ./...`
- `go build ./...`
- 协议验证器使用 10 字节帧头。
- devprobe 证明登录、存档、战斗结算和重复结算流程。

Unity 全量验证：

- Protobuf Pester 测试。
- 资源完整性 Pester 测试与资源校验器。
- 全量 EditMode 测试。
- 全量 PlayMode 测试。
- 真实后端 runner 连续 3 次通过，覆盖存档往返、胜利持久化和失败结算。
- 每次测试清理进程、环境变量及 `8080`、`8081` 端口。

战斗专项至少覆盖：

- `BattleCombatLoopTests`
- `BattleEnemyExperienceTests`
- `OnlineBattleCompletionTests`
- `BattleSettlementUiTests`
- `RealBackendOnlineFlowTests`

## 10. 战斗资源工程化

资源审计覆盖 Scene、Prefab、Sprite、Texture、Material、AnimationClip、AnimatorController、AudioClip、Font 和 GUID 引用。

确定性、可程序生成的缺口由 `Assets/Editor/CombatAssetGenerator.cs` 补齐或扩展，统一命令为：

```powershell
D:/Unity_Soft/2022/Editor/Unity.exe `
  -batchmode -quit `
  -projectPath E:/Own_project/game-client-unity `
  -executeMethod Game.Editor.CombatAssetGenerator.GenerateAll
```

无法本地确定生成的美术、授权或风格资源，记录到 `docs/combat-resource-gap-report.md`。每项必须包含消费组件字段、目标路径、尺寸与导入设置、可复现生成提示或制作步骤、导入/生成命令以及覆盖测试。

## 11. 验收条件

- 双端协议文件字节一致，生成物可重复生成且无漂移。
- 所有帧使用 10 字节帧头和 Protobuf Body。
- 所有客户端请求使用非零递增 `seq`，响应按 `seq` 正确关联。
- 所有服务端主动推送使用 `seq = 0`。
- 登录、心跳、存档、排行、配置、支付、GM 和战斗流程通过自动化验证。
- 战斗可正常开始、循环、结算、胜利通关、失败结算并持久化。
- 真实后端联调连续 3 次通过。
- 战斗资源缺口已生成，或以可执行方式完整列出。
- 客户端和服务端最终提交进入 `master` 并推送；冲突时保留本地实现语义。
