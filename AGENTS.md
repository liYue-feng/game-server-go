# Agent Guidelines for Go Game Server

This document contains build, test, and code style guidelines for the Go game server project. Agents should follow these conventions when working in this repository.

## Project Overview

A Go-based game server for "Vampire Survivors" style WeChat mini-games. Uses a modular monolith architecture. The network layer is refactored to a pitaya-inspired core (kernel + session + pipeline + transport) while keeping the original binary wire protocol; business modules cover login (WeChat), game archive, ranking, payment, and GM.

## Build & Run

```bash
# 编译
make build

# 运行（开发模式）
make run

# 运行测试
make test

# 整理依赖
make tidy

# 格式化代码
make fmt
```

## Project Structure

```
cmd/server/          # Entry point (main.go)
configs/             # YAML configuration
internal/
  config/            # Config loading (Viper)
  session/           # Player session (Bind/UID/Set/Get/Push) + ctx accessors
  pipeline/          # Handler pipeline (Before/After hooks)
  kernel/            # Message kernel: Register + reflection-based Dispatch
  transport/         # WebSocket transport: connection, hub, server (/ws, /health)
  hooks/             # pipeline before-hooks: auth + rate limit
  login/             # WeChat login, heartbeat
  game/              # Save/load game archives
  rank/              # Leaderboard (Redis Sorted Set)
  model/             # Database models (GORM)
  store/             # Data access layer (MySQL + Redis)
  protocol/          # Message protocol: IDs, codec, error codes
pkg/logger/          # Logger wrapper (zap + lumberjack)
```

## Protocol

Binary framing with JSON payload:
- 4 bytes (uint32 LE): total frame length (including self)
- 2 bytes (uint16 LE): message ID
- N bytes: JSON-encoded body

Message ID ranges:
- `1xxx`: Login module (1001=LoginReq, 1002=LoginResp, 1003=HeartbeatReq, 1004=HeartbeatResp)
- `2xxx`: Game archive module (2001=SaveArchiveReq, 2002=SaveArchiveResp, 2003=LoadArchiveReq, 2004=LoadArchiveResp)
- `3xxx`: Ranking module (3001=GetRankReq, 3002=GetRankResp, 3003=SubmitScoreReq, 3004=SubmitScoreResp)
- `9xxx`: System messages (9999=Error)

## Code Style Guidelines

### Go Conventions
- Follow standard Go project layout and `gofmt` formatting
- Comments should explain **WHY**, not just what — this is a learning project
- Use `internal/` for private packages, `pkg/` for reusable packages
- Error handling: return errors for system failures, use `protocol.ErrorResp` for business errors

### Naming Conventions
- Message IDs: `MsgID_ModuleAction` (e.g., `MsgID_LoginReq`, `MsgID_SaveArchiveResp`)
- Error codes: `ErrModule_Reason` (e.g., `ErrLoginInvalidCode`, `ErrArchiveSaveFailed`)
- Database models: PascalCase struct → GORM auto-converts to snake_case table
- Redis keys: namespace-prefixed (e.g., `session:`, `rank:`, `rate:`)

### File Organization
Each module follows the same pattern:
- `handler.go` — Message handler functions registered with the router
- Supporting files for complex logic (e.g., `wechat.go` for API client)

### Concurrency
- Each WebSocket connection has `readPump` + `writePump` goroutines
- Message handlers run in separate goroutines (dispatched by router)
- Shared state must use mutexes or channel-based Hub pattern
- Never write to WebSocket from multiple goroutines (use `send` channel)

### Database
- All DB operations go through `store` layer — business modules never use GORM directly
- `store.MySQLStore` re-exports `model` types for convenience
- Use GORM AutoMigrate in development; use migration tools in production
- Redis for: session cache, leaderboard (ZSET), rate limiting

## Testing
```bash
go test ./...
```

## General Principles

1. **Client-authoritative model** — Game logic runs on client, server handles persistence and social features
2. **Prefer existing utilities** — Use functions from `store`, `protocol`, `logger` packages
3. **Follow the naming conventions** — Ensures consistency across the codebase
4. **Keep handlers thin** — Handler parses request, delegates to store, sends response
5. **Log sparingly but informatively** — Use appropriate zap levels (Debug/Info/Warn/Error)
6. **No hard-coded secrets** — Use environment variables with `GAME_` prefix

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/gorilla/websocket` | WebSocket server |
| `github.com/spf13/viper` | Configuration management |
| `go.uber.org/zap` | Structured logging |
| `github.com/go-redis/redis/v8` | Redis client |
| `gorm.io/gorm` | MySQL ORM |
| `gopkg.in/natefinch/lumberjack.v2` | Log file rotation |
