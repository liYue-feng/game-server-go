# Agent Guidelines for Go Game Server

This document contains build, test, and code style guidelines for the Go game server project. Agents should follow these conventions when working in this repository.

## Project Overview

A Go-based game server for "Vampire Survivors" style WeChat mini-games. Uses a modular monolith architecture. The network layer uses a pitaya-inspired core (kernel + session + pipeline + transport), retains the 6-byte binary envelope, and serializes every body with generated protobuf types. Business modules cover login, typed archives, ranking, combat settlement/configuration, payment, and GM.

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
cmd/devprobe/        # Real WebSocket development probe
configs/             # YAML configuration
proto/game/v1/       # Canonical messages.proto schema
internal/
  config/            # Config loading (Viper)
  session/           # Player session (Bind/UID/Set/Get/Push) + ctx accessors
  pipeline/          # Handler pipeline (Before/After hooks)
  kernel/            # Message kernel: Register + reflection-based Dispatch
  transport/         # WebSocket transport: connection, hub, server (/ws, /health)
  hooks/             # pipeline before-hooks: auth + rate limit
  login/             # WeChat login, heartbeat
  game/              # Save/load game archives
  combat/            # Combat settlement, config, styles, player stats
  rank/              # Leaderboard (Redis Sorted Set)
  model/             # Database models (GORM)
  store/             # MySQL/Redis, in-memory development store, settlement transaction
  protocol/          # Envelope codec, 32 IDs, canonical request/response routes
  protocolpb/        # Generated Go protobuf messages
pkg/logger/          # Logger wrapper (zap + lumberjack)
tools/protobuf/      # Pinned generation and generated-output drift checks
```

## Protocol

Binary framing with protobuf payload:
- 4 bytes (uint32 LE): total frame length (including self)
- 2 bytes (uint16 LE): message ID
- N bytes: protobuf-encoded body

`proto/game/v1/messages.proto` is the canonical schema. `internal/protocol/routes.go` maps request/response prototypes, and `internal/protocolpb/messages.pb.go` is generated rather than hand-written.

Message ID ranges:
- `1xxx`: Login module (1001=LoginReq, 1002=LoginResp, 1003=HeartbeatReq, 1004=HeartbeatResp)
- `2xxx`: Game archive module (2001=SaveArchiveReq, 2002=SaveArchiveResp, 2003=LoadArchiveReq, 2004=LoadArchiveResp)
- `3xxx`: Ranking module (3001=GetRankReq, 3002=GetRankResp, 3003=SubmitScoreReq, 3004=SubmitScoreResp)
- `4xxx`: Combat module (4001=CombatResultReq, 4002=CombatResultResp, 4003-4014=config/style/stats)
- `5xxx`: Payment module (5001-5003)
- `6xxx`: GM module (6001-6002)
- `9xxx`: System messages (9999=Error)

## Code Style Guidelines

### Go Conventions
- Follow standard Go project layout and `gofmt` formatting
- Comments should explain **WHY**, not just what — this is a learning project
- Use `internal/` for private packages, `pkg/` for reusable packages
- Error handling: return errors for system failures; the kernel sends the generated `protocolpb.ErrorResp` for business errors

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
- Archives store serialized `protocolpb.PlayerArchive` bytes, not JSON
- Combat settlement is exactly once per `(player_id, run_id)`; never bypass `CombatSettlementRepository`
- Redis for: session cache, leaderboard (ZSET), rate limiting

## Testing
```bash
go test ./...

# Generate Go and Unity protocol outputs, then verify committed outputs
powershell.exe -NoProfile -File tools/protobuf/Generate-Protocol.ps1 -ClientRoot ..\game-client-unity
powershell.exe -NoProfile -File tools/protobuf/Verify-Protocol.ps1 -ClientRoot ..\game-client-unity

# Start dependency-free development backend and run its protobuf probe
go run ./cmd/server -config configs/config.dev.yaml
go run ./cmd/devprobe
```

The Unity integration runner at `game-client-unity/tools/integration/Invoke-A4BackendIntegration.ps1` must report exactly 3/3 PlayMode cases: archive round trip, victory persistence, and defeat settlement.

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
