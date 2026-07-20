# Task 2 报告 — HandlerService 内核（反射分发 + 编解码）

## 做了什么
- 新增 `internal/kernel/kernel.go`：`Kernel` + `Register(reqID,respID,fn,opts...)` + `Dispatch(ctx,data)`。
  - 启动期反射校验 handler 签名 `func(context.Context,*Req)(*Resp,error)`，不符即 panic。
  - `Dispatch` 复用 `protocol.Decode/Encode`：解码 → 定位 → Before 钩子 → 反射调用 → After 钩子 → 编码响应。
  - 错误分流：`*protocol.BizError` 回具体 code；普通 error 记日志并归一为 `ErrInternal`。
  - `AuthFree()` 选项 + `IsAuthFree` 供 pipeline 判断免鉴权（登录/心跳）。
  - ctx 携带 msgID（`MsgIDFromContext`），供认证钩子判断。
- 新增 `internal/kernel/kernel_test.go`，含 6 个用例。

## 验证
- `go test ./internal/kernel/...` → ok。
  - **金标准测试** `TestGoldenWireCompatibility`：内核发出的响应帧字节与旧 `protocol.Encode` 完全一致 → Unity 客户端零改动的机器化保证。
  - 另含 BizError 编码、系统 error 归一、未注册消息、前置钩子中断、AuthFree 标记。
- `go build ./...` → 0；`go vet ./internal/kernel/... ./internal/session/... ./internal/pipeline/...` → 0。

## 备注
- handler 注册运行期只读、无锁；反射开销对挂机类小消息量可忽略。
- 未改动 pitaya；未新增依赖。
