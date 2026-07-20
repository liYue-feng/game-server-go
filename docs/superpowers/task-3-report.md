# Task 3 报告 — transport 层接入 kernel

## 做了什么
- 新增 `internal/transport/connection.go`：`Connection` 承载 WS 读写，持有 `*session.Session`，实现 `session.Conn.SendMessage`；readPump 解帧后调用 `kernel.Dispatch(ctxWithSession, data)`；readPump 退出时触发 `session.Close()`（OnClose 回调）。
- 新增 `internal/transport/hub.go`：`Hub` 事件循环 + 广播；新增**优雅关闭**（`closeAll`/`done` 信号，`Shutdown()` 等待 Run 退出；`Unregister` 用 select+done 兜底避免关闭后阻塞）。
- 新增 `internal/transport/server.go`：`NewServer(kernel)`，`/ws` 与 `/health` 路由，`http.Server` + `Shutdown(ctx)` 优雅停机。
- 身份从 Connection 剥离到 Session（对齐 pitaya：连接只管字节，Session 管身份）。

## 验证
- `go build ./internal/transport/...` → 0；`go vet ./internal/transport/...` → 0；`go build ./...` → 0。

## 备注
- 旧 `internal/gateway` 暂保留（过渡，Task 8 删除死代码）；此时 main.go 尚未切换到 transport。
- 顺带落地 spec Open Question 的"WebSocket 优雅关闭"。
