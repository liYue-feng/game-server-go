# Task 6 报告 — 迁移 payment / gm 组件

## 做了什么
- `internal/payment/handler.go`：`CreateOrder(ctx,*CreateOrderReq)(*CreateOrderResp,error)` 进 kernel；`HandlePayCallback([]byte)` 保持 HTTP 处理不变（微信主动回调）。
- `internal/gm/handler.go`：`Command(ctx,*GMCommandReq)(*GMCommandResp,error)`；`hub` 依赖由 `gateway.Hub` 改为 `transport.Hub`；管理员白名单校验保留在方法内，越权返回 BizError(ErrUnauthorized)；其余指令沿用"结果文本"语义。
- 两文件因原 GBK 中文注释无法精确 patch，故整体用 UTF-8 重写，逻辑保持等价。

## 验证
- `go build ./internal/gm/... ./internal/payment/...` → 0。

## 备注
- 依赖链 gm→transport→kernel→(pipeline/protocol/session)，无循环。
- main.go 仍待 Task 8 切换。
