# Payment Fail-Closed Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the unsafe placeholder payment path and ship an explicit, correlated, side-effect-free disabled boundary until a complete WeChat Pay V3 entitlement contract exists.

**Architecture:** The protobuf surface remains stable. A dependency-free disabled payment handler returns a business error, both runtimes register it for deterministic client behavior, and startup rejects any attempt to enable payment. No HTTP callback listener or fulfillment code exists in the release candidate.

**Tech Stack:** Go 1.24, protobuf-go v1.36.11, Gorilla WebSocket, GORM, Pester 3.4, Unity 2022.3 documentation.

## Global Constraints

- Work only on the two `feature/sequenced-protobuf-transport` coordination worktrees.
- Do not merge, push, deploy, or start a persistent service before final review.
- Preserve all 32 protobuf IDs and both generated outputs byte-for-byte.
- Do not add a fake signature, fake entitlement, body-only callback, or development bypass.
- `wechat.payment_enabled` defaults to `false`; `true` is a startup error.
- Disabled order requests use the existing protobuf error path and echo the request sequence.
- Follow RED-GREEN-REFACTOR and commit each independently reviewed task.

---

### Task 1: Replace Placeholder Payment With A Disabled Boundary

**Files:**
- Rewrite: `internal/payment/handler.go`
- Rewrite: `internal/payment/handler_test.go`
- Delete: `internal/payment/order_store.go`
- Delete: `internal/payment/pusher.go`
- Delete: `internal/payment/constants.go`
- Modify: `internal/config/config.go`
- Modify: `internal/protocol/common.go`
- Modify: `configs/config.yaml`
- Modify: `cmd/server/main.go`
- Modify: `cmd/server/main_test.go`
- Modify: client `Assets/Tests/EditMode/Online/PaymentAndGmSessionServiceTests.cs`

**Interfaces:**
- Produces: `payment.NewDisabledHandler() *payment.Handler`.
- Produces: `(*payment.Handler).CreateOrder(context.Context, *protocolpb.CreateOrderReq) (*protocolpb.CreateOrderResp, error)` with no side effects.
- Produces: `WechatConfig.PaymentEnabled bool` mapped from `payment_enabled`.
- Produces: `protocol.ErrPaymentUnavailable = 60001` and message `payment is disabled`.

- [ ] **Step 1: Write failing disabled-boundary tests**

Add tests that prove `CreateOrder` returns code `60001`, both runtimes register the route, authenticated kernel dispatch echoes the nonzero request sequence, no callback server exists, and `payment_enabled: true` plus `GAME_WECHAT_PAYMENT_ENABLED=true` fail before the injected store openers run. Add a Unity payment test proving wrong-sequence error is ignored, matching `60001` error invokes failure exactly once, success remains unset, and `PaymentResult` does not fire.

- [ ] **Step 2: Run focused tests and verify RED**

```powershell
go test ./internal/payment ./cmd/server -count=1
```

Expected: failures because the placeholder handler still accepts callbacks and startup still wires the callback server.

- [ ] **Step 3: Implement the disabled boundary**

Replace the handler with a dependency-free implementation. Remove callback parsing, verification stubs, product definitions, order mutation, delivery, and pushing. Register the disabled handler for `MsgID_CreateOrderReq` in both runtime branches. Remove callback HTTP server construction and its request-body helper. Reject `PaymentEnabled` at the beginning of `newRuntime`.

Retain `model.PaymentOrder`, AutoMigrate, and MySQL order CRUD unchanged so existing databases and historical rows are not destructively migrated.

- [ ] **Step 4: Run focused and full server verification**

```powershell
go test ./internal/payment ./cmd/server -count=1
go test ./... -count=1
go vet ./...
go build ./...
```

Expected: all commands exit 0; no listener or callback code remains.

Run the owned development integration runner once and assert WebSocket login succeeds while port `8081` has no listener. This process-level check also covers the disabled route's coexistence with normal server startup.

- [ ] **Step 5: Commit**

```powershell
git add internal/payment internal/config/config.go internal/protocol/common.go configs/config.yaml cmd/server/main.go cmd/server/main_test.go docs/superpowers/specs/2026-07-21-payment-fail-closed-release-design.md docs/superpowers/plans/2026-07-21-payment-fail-closed-release.md
git commit -m "fix: disable incomplete payment boundary"
git -C E:/Own_project/game-client-unity/.worktrees/sequenced-protobuf-transport add Assets/Tests/EditMode/Online/PaymentAndGmSessionServiceTests.cs
git -C E:/Own_project/game-client-unity/.worktrees/sequenced-protobuf-transport commit -m "test: cover disabled payment response"
```

### Task 2: Correct Authoritative Transport Documentation

**Files:**
- Modify: server `AGENTS.md`
- Modify: server `CLAUDE.md`
- Modify: server `internal/transport/connection.go`
- Modify: server `README.md`
- Modify: server `docker-compose.yml`
- Modify: client `CLAUDE.md`
- Test: server `tools/protobuf/Generate-Protocol.Tests.ps1`
- Test: client `tools/protobuf/GeneratedProtocol.Tests.ps1`

**Interfaces:**
- Produces: authoritative documentation for `[Length uint32][MsgID uint16][Seq uint32]`, nonzero requests, echoed responses, and zero-sequence pushes.

- [ ] **Step 1: Add failing stale-documentation scans**

Extend each repository's protocol Pester suite to read its authoritative instruction files and reject the old 6-byte or `[Length][MsgID]`-only description. The server suite also asserts README marks payment disabled and Docker Compose does not publish `8081`.

- [ ] **Step 2: Run both Pester suites and verify RED**

```powershell
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/Generate-Protocol.Tests.ps1 -EnableExit"
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/GeneratedProtocol.Tests.ps1 -EnableExit"
```

Expected: failures naming the stale instruction/comment files.

- [ ] **Step 3: Update the authoritative text**

Document the exact 10-byte little-endian frame and sequence ownership in all current authoritative locations, including README. Mark payment IDs as reserved/disabled and remove the Docker `8081` publication. Do not rewrite historical design documents or delete existing payment database models.

- [ ] **Step 4: Run documentation, protocol, and diff gates**

```powershell
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/Generate-Protocol.Tests.ps1,tools/protobuf/PeerRootResolver.Tests.ps1 -EnableExit"
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/GeneratedProtocol.Tests.ps1,tools/protobuf/PeerRootResolver.Tests.ps1 -EnableExit"
git diff --check
```

Expected: all tests pass and no stale 6-byte description remains.

- [ ] **Step 5: Commit each repository locally**

```powershell
git -C E:/Own_project/game-server-go/.worktrees/sequenced-protobuf-transport add AGENTS.md CLAUDE.md README.md docker-compose.yml internal/transport/connection.go tools/protobuf/Generate-Protocol.Tests.ps1
git -C E:/Own_project/game-server-go/.worktrees/sequenced-protobuf-transport commit -m "docs: describe sequenced protobuf frames"
git -C E:/Own_project/game-client-unity/.worktrees/sequenced-protobuf-transport add CLAUDE.md tools/protobuf/GeneratedProtocol.Tests.ps1
git -C E:/Own_project/game-client-unity/.worktrees/sequenced-protobuf-transport commit -m "docs: describe sequenced protobuf frames"
```

## Final Verification

- [ ] Full Go test/vet/build passes.
- [ ] Server and client protocol/Pester/asset gates pass.
- [ ] Full graphical Unity EditMode and PlayMode pass.
- [ ] Three owned real-backend 3/3 runs pass with balanced raw-frame ledgers.
- [ ] A fresh final review reports no Critical or Important findings.
- [ ] Only then may both local masters fast-forward and the coordinated remote SHA gate open.
