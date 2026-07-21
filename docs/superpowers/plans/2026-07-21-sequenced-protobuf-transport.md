# Sequenced Protobuf Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Coordinate the Unity client and Go server migration from the existing 6-byte protobuf envelope to a 10-byte little-endian envelope with request `seq`, single-response correlation, real payment/GM push handling, and verified combat completion.

**Architecture:** Both repositories own a byte-identical `proto/game.proto` and generate only their local language output. Go replies echo the request `seq`, all server pushes use `seq=0`, and Unity `NetworkClient` is the sole allocator and pending-request owner. Existing coordinators retain timeout/retry ownership, while combat retries use a new transport `seq` and the same business `run_id`.

**Tech Stack:** Unity 2022.3 C#, NUnit EditMode/PlayMode, Google.Protobuf 3.35.1, PowerShell 5.1/Pester 3.4, Go 1.24, protobuf-go v1.36.11, protoc 35.0, Gorilla WebSocket.

## Global Constraints

- The repositories are `E:/Own_project/game-client-unity` and `E:/Own_project/game-server-go`. Before Task 1, create the same coordination branch, `feature/sequenced-protobuf-transport`, from each repository's local `master`. Tasks 1-8 are committed, reviewed, and tested on those coordination branches; Tasks 1-7 must never be pushed to `master` individually.
- After Task 8's complete cross-repository verification and reviews pass on both coordination branches, fast-forward merge each coordination branch into its local `master` and perform a coordinated release. Git cannot atomically update two repositories: record the expected client/server `master` SHAs, push the two masters sequentially, and poll both `origin/master` refs until each equals its expected SHA.
- The new protocol's deployment, process startup, and activation gate remains closed until both remote refs are verified. If the second push fails after the first succeeds, leave the first commit on its remote, keep the release gate closed, repair/retry the failed push, and verify both refs again; never deploy or activate a one-sided transport migration. The separate combat-resource plan may use its own task-by-task publishing policy after this transport release.
- Conflict resolution preserves the current local implementation semantics.
- Each repository owns `proto/game.proto`; the two files must be byte-identical and have the same raw SHA-256.
- Preserve the complete current business schema, package `game.protocol.v1`, Go package `game-server/internal/protocolpb;protocolpb`, C# namespace `Game.Protocol`, all 32 message IDs, typed archives, typed score metadata, combat `run_id`, outcome, archive snapshot, and duplicate settlement fields.
- `CombatResultReq.duration_ms` remains protobuf `int64`, Go `int64`, and C# `long`; do not replace it with desktop-proto `double survival_time`.
- The frame is little-endian `[Length uint32][MsgID uint16][Seq uint32][protobuf body]`; `Length = 10 + body length`, `HeaderSize = 10`, and `MaxFrameSize = 64 KiB` including the header.
- Ordinary requests require nonzero `seq`; responses echo it; server pushes use `seq=0`; a client request with `seq=0` closes the connection without an `ErrorResp`.
- No 6-byte or JSON-body compatibility layer is retained.
- `NetworkClient` exclusively allocates nonzero `uint32` request sequences starting at 1, wraps `uint.MaxValue` to 1, and skips live pending values.
- Pending registration happens before transport send. Protobuf serialization failure uses `out seq=0`, creates no pending entry, and does not consume a sequence; synchronous send failure removes the pending entry, calls failure once, returns `false`, and retains the allocated `out seq` for diagnostics.
- Retry cancels the old pending request before sending, allocates a new `seq`, reuses the same `run_id`, and ignores late old success/error frames.
- The network layer adds no request timer. Existing connection/session/battle coordinators keep timeout and retry ownership.
- Deterministic tests follow RED-GREEN-REFACTOR. Each task may be committed and reviewed on the coordination branches, but the transport migration is neither merged into local `master` nor pushed until Task 8 has passed its complete acceptance run.

## File Map

Server protocol ownership:

- `proto/game.proto`: complete shared schema.
- `internal/protocolpb/game.pb.go`: generated Go messages.
- `tools/protobuf/Generate-Protocol.ps1`: pinned local Go generation only.
- `tools/protobuf/Verify-Protocol.ps1`: local drift plus sibling-schema hash gate.
- `internal/protocol/codec.go`: 10-byte frame codec.
- `internal/session/session.go`: explicit `Reply` versus `Push` semantics.
- `internal/kernel/kernel.go`: request `seq` validation and response/error echo.
- `internal/transport/connection.go`, `hub.go`: WebSocket close, direct push, broadcast push.
- `internal/payment/pusher.go`: narrow payment-owned `UIDPusher` interface implemented in production by `*transport.Hub`.
- `internal/payment/order_store.go`: narrow payment-owned `OrderStore` interface implemented in production by `*store.MySQLStore`.

Client protocol ownership:

- `proto/game.proto`: byte-identical shared schema.
- `tools/protobuf/generated/Game.cs`: generated staging source.
- `Assets/Scripts/Protocol/Generated/Game.cs`: Unity runtime generated source.
- `tools/protobuf/Generate-Protocol.ps1`: pinned local C# generation.
- `tools/protobuf/Verify-GeneratedProtocol.ps1`: local drift plus sibling-schema hash gate.
- `Assets/Scripts/Protocol/Codec.cs`: 10-byte frame codec.
- `Assets/Scripts/Network/NetworkClient.cs`: sequence allocation, pending correlation, push dispatch.
- `Assets/Scripts/Online/*SessionService.cs`: single-request business APIs.
- `Assets/Scripts/Online/BattleSettlementCoordinator.cs`: retry/cancel and stable `run_id`.

---

### Task 1: Establish Byte-Identical Local Schemas and Generators

**Files:**
- Server create: `proto/game.proto`
- Server create: `internal/protocolpb/game.pb.go`
- Server modify: `tools/protobuf/Generate-Protocol.ps1`
- Server modify: `tools/protobuf/Generate-Protocol.Tests.ps1`
- Server modify: `tools/protobuf/Verify-Protocol.ps1`
- Server delete: `proto/game/v1/messages.proto`, `internal/protocolpb/messages.pb.go`
- Client create: `proto/game.proto`
- Client create: `tools/protobuf/Generate-Protocol.ps1`
- Client create: `tools/protobuf/generated/Game.cs`
- Client create: `Assets/Scripts/Protocol/Generated/Game.cs`
- Client rename: `Assets/Scripts/Protocol/Generated/Messages.cs.meta` to `Game.cs.meta`
- Client modify: `tools/protobuf/GeneratedProtocol.Tests.ps1`
- Client modify: `tools/protobuf/Verify-GeneratedProtocol.ps1`
- Client modify: `Assets/Scripts/Protocol/README.md`
- Client delete: `tools/protobuf/generated/Messages.cs`, `Assets/Scripts/Protocol/Generated/Messages.cs`
- Server modify: `AGENTS.md`

**Interfaces:**
- Consumes: current complete schema `game-server-go/proto/game/v1/messages.proto`.
- Produces: byte-identical `proto/game.proto`, server `game.pb.go`, and client `Game.cs` copies generated locally.

- [ ] **Step 1: Write failing generation-contract tests**

In both Pester suites, assert the exact new paths and absence of old sources. Add this raw-byte helper to both suites:

```powershell
function Get-RawSha256([string]$Path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash
}

It 'owns one local game.proto and rejects the old source names' {
    Test-Path (Join-Path $projectRoot 'proto\game.proto') | Should Be $true
    @(Get-ChildItem (Join-Path $projectRoot 'proto') -Recurse -Filter '*.proto').Count | Should Be 1
    Test-Path (Join-Path $projectRoot 'proto\game\v1\messages.proto') | Should Be $false
}
```

The server test must require `internal/protocolpb/game.pb.go`. The client test must require both `Game.cs` destinations and compare them with CRLF-only normalization. Both tests must resolve the sibling repository and assert raw schema SHA-256 equality.

- [ ] **Step 2: Run the Pester tests and verify RED**

```powershell
Set-Location E:\Own_project\game-server-go
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/Generate-Protocol.Tests.ps1 -EnableExit"
Set-Location E:\Own_project\game-client-unity
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/GeneratedProtocol.Tests.ps1 -EnableExit"
```

Expected: both commands fail because `proto/game.proto`, `game.pb.go`, `Game.cs`, and the client local generator do not exist.

- [ ] **Step 3: Move the complete schema and implement local generation**

Copy the current server schema byte-for-byte to both `proto/game.proto`, retaining this header and every existing declaration/tag below it:

```proto
syntax = "proto3";
package game.protocol.v1;
option go_package = "game-server/internal/protocolpb;protocolpb";
option csharp_namespace = "Game.Protocol";
```

Keep the existing 32 `MessageId` values and current typed business messages. Do not copy legacy desktop message bodies.

The server generator paths become:

```powershell
$schemaPath = Join-Path $backendRoot 'proto\game.proto'
$goOutputPath = Join-Path $backendRoot 'internal\protocolpb\game.pb.go'
& $protoc "--proto_path=$(Join-Path $backendRoot 'proto')" `
  "--go_out=$temporaryRoot" '--go_opt=module=game-server' 'game.proto'
```

The client generator pins `protoc 35.0` and Google.Protobuf `3.35.1`, checks the existing published SHA-256 values, and writes only C#:

```powershell
$schemaPath = Join-Path $clientRoot 'proto\game.proto'
$stagingPath = Join-Path $clientRoot 'tools\protobuf\generated\Game.cs'
$runtimePath = Join-Path $clientRoot 'Assets\Scripts\Protocol\Generated\Game.cs'
& $protoc "--proto_path=$(Join-Path $clientRoot 'proto')" `
  "--csharp_out=$temporaryRoot" 'game.proto'
Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'Game.cs') -Committed $stagingPath
Copy-OrCheckGeneratedFile -Candidate (Join-Path $temporaryRoot 'Game.cs') -Committed $runtimePath
```

Both verify scripts accept an optional sibling root, compare `proto/game.proto` raw hashes, run their own generator with `-Check`, and reject any second `.proto` or old generated filename.

- [ ] **Step 4: Generate, verify, and scan drift**

```powershell
Set-Location E:\Own_project\game-server-go
powershell.exe -NoProfile -File tools/protobuf/Generate-Protocol.ps1
powershell.exe -NoProfile -File tools/protobuf/Verify-Protocol.ps1 -ClientRoot E:\Own_project\game-client-unity
Set-Location E:\Own_project\game-client-unity
powershell.exe -NoProfile -File tools/protobuf/Generate-Protocol.ps1
powershell.exe -NoProfile -File tools/protobuf/Verify-GeneratedProtocol.ps1 -BackendRoot E:\Own_project\game-server-go
rg -n "messages\.proto|messages\.pb\.go|Generated[/\\]Messages\.cs" . -g '!docs/superpowers/**'
```

Expected: generators and verifiers pass, raw schema hashes match, and the final `rg` returns no production/tooling references.

- [ ] **Step 5: Commit both repositories on the coordination branches (do not merge or push)**

```powershell
git -C E:\Own_project\game-server-go add AGENTS.md proto internal/protocolpb tools/protobuf
git -C E:\Own_project\game-server-go commit -m "build: localize canonical game protobuf"
git -C E:\Own_project\game-client-unity add proto tools/protobuf Assets/Scripts/Protocol
git -C E:\Own_project\game-client-unity commit -m "build: generate Unity protocol locally"
```

### Task 2: Add the 10-Byte Sequenced Go Codec

**Files:**
- Modify: server `internal/protocol/codec.go`
- Modify: server `internal/protocol/codec_test.go`
- Modify: server `internal/protocolpb/messages_test.go`

**Interfaces:**
- Consumes: generated `protocolpb` messages from Task 1.
- Produces: `Encode(msgID uint16, seq uint32, payload proto.Message)` and `Message{MsgID, Seq, Body}`.

- [ ] **Step 1: Write failing codec tests**

Add tests with exact values:

```go
func TestLoginReqGoldenSequencedFrame(t *testing.T) {
    frame, err := Encode(MsgID_LoginReq, 1, &protocolpb.LoginReq{Code: "abc"})
    if err != nil { t.Fatal(err) }
    want := []byte{0x0f, 0, 0, 0, 0xe9, 0x03, 1, 0, 0, 0, 0x0a, 0x03, 'a', 'b', 'c'}
    if !bytes.Equal(frame, want) { t.Fatalf("frame=%x want=%x", frame, want) }
}

func TestDecodeRoundTripsSeqAndRejectsSixByteFrame(t *testing.T) {
    frame, _ := Encode(MsgID_HeartbeatReq, 77, &protocolpb.HeartbeatReq{})
    decoded, err := Decode(frame)
    if err != nil { t.Fatal(err) }
    if decoded.Seq != 77 { t.Fatalf("seq=%d want=77", decoded.Seq) }
    if _, err := Decode([]byte{6, 0, 0, 0, 0xeb, 0x03}); err == nil { t.Fatal("accepted 6-byte frame") }
}
```

Also cover exactly `MaxFrameSize`, one byte over it, declared-length mismatch, and a 9-byte truncated header.

- [ ] **Step 2: Run focused tests and verify RED**

```powershell
go test ./internal/protocol ./internal/protocolpb -run 'Test(LoginReqGoldenSequencedFrame|DecodeRoundTripsSeq)' -count=1
```

Expected: compile/test failure because `Encode` has no `seq` and `Message` has no `Seq`.

- [ ] **Step 3: Implement the codec**

Use these exact offsets:

```go
const (
    HeaderSize = 10
    MaxFrameSize = 64 * 1024
)

type Message struct {
    MsgID uint16
    Seq   uint32
    Body  []byte
}

binary.LittleEndian.PutUint32(frame[0:4], uint32(totalLen))
binary.LittleEndian.PutUint16(frame[4:6], msgID)
binary.LittleEndian.PutUint32(frame[6:10], seq)
```

`Decode` reads `Seq` from `[6:10]`, copies Body from offset 10, and keeps the existing nil payload, length mismatch, and 64 KiB rejection behavior.

- [ ] **Step 4: Run focused and package tests**

```powershell
go test ./internal/protocol ./internal/protocolpb -count=1
git diff --check
```

Expected: all selected packages pass and the golden bytes are exact.

- [ ] **Step 5: Commit on the server coordination branch (do not merge or push)**

```powershell
git add internal/protocol internal/protocolpb/messages_test.go
git commit -m "feat: add sequenced protobuf frame codec"
```

### Task 3: Echo Seq for Replies and Reserve Seq Zero for Pushes

**Files:**
- Modify: server `internal/session/session.go`
- Modify: server `internal/session/session_test.go`
- Modify: server `internal/kernel/kernel.go`
- Modify: server `internal/kernel/kernel_test.go`
- Modify: server `internal/transport/connection.go`
- Modify: server `internal/transport/hub.go`
- Modify: server `internal/transport/server_test.go`

**Interfaces:**
- Consumes: Task 2 `protocol.Encode(msgID, seq, payload)`.
- Produces: `Conn.Reply`, `Conn.Push`, `Session.Reply`, `Session.Push`, and `Kernel.Dispatch(...) error`.

- [ ] **Step 1: Write failing reply/push/protocol-violation tests**

Add fake connection captures with explicit sequence:

```go
type sentMessage struct { msgID uint16; seq uint32; payload proto.Message }
type fakeConn struct { sent []sentMessage }
func (f *fakeConn) Reply(seq uint32, msgID uint16, p proto.Message) error {
    f.sent = append(f.sent, sentMessage{msgID: msgID, seq: seq, payload: p}); return nil
}
func (f *fakeConn) Push(msgID uint16, p proto.Message) error {
    f.sent = append(f.sent, sentMessage{msgID: msgID, seq: 0, payload: p}); return nil
}
```

Required tests: `TestKernelEchoesRequestSeq`, `TestBizErrorEchoesRequestSeq`, `TestAuthErrorEchoesRequestSeq`, `TestMalformedBodyErrorEchoesRequestSeq`, `TestKernelRejectsZeroSeqWithoutErrorFrame`, `TestSessionPushUsesZeroSeq`, and `TestReadPumpClosesOnZeroSeqRequest`.

- [ ] **Step 2: Run tests and verify RED**

```powershell
go test ./internal/session ./internal/kernel ./internal/transport -count=1
```

Expected: compile failures for missing `Reply`, `Seq`, and `Dispatch` error result.

- [ ] **Step 3: Implement explicit response and push paths**

Use these interfaces:

```go
type Conn interface {
    Reply(seq uint32, msgID uint16, payload proto.Message) error
    Push(msgID uint16, payload proto.Message) error
}

func (s *Session) Reply(seq uint32, msgID uint16, payload proto.Message) error {
    return s.conn.Reply(seq, msgID, payload)
}
func (s *Session) Push(msgID uint16, payload proto.Message) error {
    return s.conn.Push(msgID, payload)
}
```

`Connection.Reply` calls `protocol.Encode(msgID, seq, payload)`; `Connection.Push` calls `protocol.Encode(msgID, 0, payload)`. `Kernel.Dispatch` returns a sentinel fatal protocol error for malformed headers and `frame.Seq == 0`; `readPump` breaks on that error. Valid nonzero frames with unknown ID, invalid protobuf, hook errors, auth errors, business errors, or internal errors call `Session.Reply(frame.Seq, MsgID_Error, ...)`. Normal results use the same `frame.Seq`.

- [ ] **Step 4: Run server transport tests**

```powershell
go test ./internal/session ./internal/kernel ./internal/transport -count=1
go test ./internal/hooks ./internal/pipeline -count=1
```

Expected: all pass; zero-seq and malformed headers close without any application response.

- [ ] **Step 5: Commit on the server coordination branch (do not merge or push)**

```powershell
git add internal/session internal/kernel internal/transport
git commit -m "feat: correlate replies and isolate pushes"
```

### Task 4: Sequence Devprobe and Deliver Payment/GM Pushes

**Files:**
- Modify: server `cmd/devprobe/main.go`
- Modify: server `cmd/devprobe/main_test.go`
- Modify: server `cmd/server/main.go`
- Modify: server `cmd/server/main_test.go`
- Modify: server `internal/transport/hub.go`
- Create: server `internal/payment/pusher.go`
- Create: server `internal/payment/order_store.go`
- Modify: server `internal/payment/handler.go`
- Create: server `internal/payment/handler_test.go`
- Modify: server `internal/gm/handler.go`
- Modify: server `internal/gm/handler_test.go`
- Modify: every remaining server test calling `protocol.Encode`

**Interfaces:**
- Consumes: Task 3 reply/push separation.
- Produces: probe-side sequence correlation plus payment-owned `OrderStore` and `UIDPusher` dependencies backed in production by `*store.MySQLStore` and `*transport.Hub`.

- [ ] **Step 1: Write failing end-to-end server tests**

Make probe sends return their sequence and reads require it:

```go
func (p *probe) send(msgID uint16, payload proto.Message) (uint32, error)
func (p *probe) read(expectedMsgID uint16, expectedSeq uint32, destination proto.Message) error
```

Add `TestRunProbeAssociatesEveryResponseBySeq`, `TestGMBroadcastHasZeroSeq`, `TestPaymentCallbackPushesPayResultNotifyWithZeroSeq`, `TestPaymentDeliveryFailureReturnsErrorAndDoesNotPush`, and `TestPaymentOfflineDeliveryStaysDeliveredAndReturnsSuccess`. Payment handler tests inject fake `OrderStore` and `UIDPusher` implementations. The failure fake returns an error specifically from `UpdateOrderStatus(orderNo, store.OrderStatusDelivered)` and the test requires the handler to return an error with no recorded push. Success tests require the paid player's `PayResultNotify{OrderNo, Status:"success", ProductId}` only after that delivered update succeeds.

- [ ] **Step 2: Run tests and verify RED**

```powershell
go test ./cmd/devprobe ./cmd/server ./internal/payment ./internal/gm ./internal/transport -count=1
```

Expected: failures because probe frames have no sequence checks and payment has no online push.

- [ ] **Step 3: Implement probe correlation and targeted push**

The probe starts at 1 and increments per request:

```go
func (p *probe) nextSequence() uint32 {
    p.seq++
    if p.seq == 0 { p.seq = 1 }
    return p.seq
}
```

Mock probe servers decode each request and reply with `decoded.Seq`. Keep `Hub.PushToUID(uid int64, msgID uint16, payload proto.Message) bool`; its Hub-loop branch finds a connection whose `Session().UID()` matches and enqueues a frame encoded with `seq=0`. Define the narrow payment-owned dependency in `internal/payment/pusher.go`:

```go
type UIDPusher interface {
    PushToUID(uid int64, msgID uint16, payload proto.Message) bool
}
```

Define the payment-owned store seam in `internal/payment/order_store.go`, matching the current `*store.MySQLStore` method signatures and their actual `internal/model` type:

```go
type OrderStore interface {
    CreateOrder(order *model.PaymentOrder) error
    GetOrderByOrderNo(orderNo string) (*model.PaymentOrder, error)
    UpdateOrderStatus(orderNo string, status int) error
}
```

Make `payment.Handler` depend on both `OrderStore` and `UIDPusher`. Inject `*store.MySQLStore` and `*transport.Hub` only in production wiring, where they satisfy those interfaces; tests inject fakes. Call `UpdateOrderStatus(orderNo, store.OrderStatusDelivered)` first. Only when that call succeeds, emit through the injected pusher:

```go
&protocolpb.PayResultNotify{
    OrderNo: notify.OrderNo,
    Status: "success",
    ProductId: int32(order.ProductID),
}
```

If the status update fails, return an HTTP error and do not push. If `PushToUID` returns `false` because no matching online target exists, keep the already delivered order unchanged and return HTTP success. GM broadcast continues through `Hub.Broadcast`, now always `seq=0`.

- [ ] **Step 4: Run all server checks**

```powershell
go test ./... -count=1
go vet ./...
go build ./...
gofmt -l cmd internal | ForEach-Object { if ($_){ throw "gofmt required: $_" } }
```

Expected: all checks pass and no source still calls the two-argument `protocol.Encode`.

- [ ] **Step 5: Commit on the server coordination branch (do not merge or push)**

```powershell
git add cmd internal
git commit -m "feat: sequence probes and server pushes"
```

### Task 5: Add the Unity 10-Byte Codec and Atomic Pending Correlation

**Files:**
- Modify: client `Assets/Scripts/Protocol/Codec.cs`
- Modify: client `Assets/Scripts/Network/NetworkClient.cs`
- Modify: client `Assets/Scripts/Network/NetworkConnectionController.cs`
- Modify: client `Assets/Tests/EditMode/Protocol/ProtobufGoldenFrameTests.cs`
- Modify: client `Assets/Tests/EditMode/Network/NetworkClientTests.cs`
- Modify: client `Assets/Tests/EditMode/Network/NetworkConnectionControllerTests.cs`
- Modify: client `Assets/Tests/EditMode/Network/TestDoubles/FakeWebSocketTransport.cs`

**Interfaces:**
- Consumes: Task 1 generated `Game.Protocol` messages.
- Produces: `Codec.Encode(msgID, seq, body)` plus an `IMessage` delegating overload, sequenced `TryDecode`, `Request<TRequest,TResponse>`, and `CancelRequest`.

- [ ] **Step 1: Write failing codec and pending tests**

Golden assertion:

```csharp
CollectionAssert.AreEqual(
    new byte[] { 0x0F, 0, 0, 0, 0xE9, 0x03, 1, 0, 0, 0, 0x0A, 0x03, 0x61, 0x62, 0x63 },
    Codec.Encode(MsgID.LoginReq, 1, new LoginReq { Code = "abc" }));
```

Required tests: `RequestRegistersPendingBeforeSynchronousResponseCanArrive`, `RequestSerializationFailureLeavesSeqZeroNoPendingAndDoesNotConsumeSequence`, `RequestSynchronousSendFailureCompletesOnceAndDoesNotLeakPending`, `SequencesIncrementWrapAndSkipPending`, `OutOfOrderSameResponseIdRequestsCompleteBySeq`, `ErrorResponseCompletesOnlyMatchingSeq`, `WrongResponseIdFailsOnce`, `MalformedBodyFailsOnce`, `UnknownAndDuplicateSeqAreDropped`, `ZeroSeqPushDoesNotCompletePending`, `CancelIgnoresLateSuccessAndError`, and `DisconnectAndDisposeFailPendingOnce`. The serialization-failure test makes the next successful request receive sequence `1`.

- [ ] **Step 2: Run focused Unity tests and verify RED**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testFilter 'Game.Tests.EditMode.Protocol.ProtobufGoldenFrameTests;Game.Tests.EditMode.Network.NetworkClientTests' `
  -testResults 'Logs\seq-client-red.xml' -logFile 'Logs\seq-client-red.log'
```

Expected: failures for the old codec and missing Request API.

- [ ] **Step 3: Implement codec and pending state machine**

Use exact codec signatures:

```csharp
public static byte[] Encode(ushort msgID, uint seq, byte[] body)
public static byte[] Encode(ushort msgID, uint seq, IMessage message)
    => Encode(msgID, seq, message.ToByteArray());
public static bool TryDecode(byte[] data, out ushort msgID, out uint seq, out byte[] body)
```

`NetworkClient` owns `private readonly object _pendingGate`, `Dictionary<uint, PendingRequest> _pending`, and `_nextSeq = 1`. Implement the approved API:

```csharp
public bool Request<TRequest, TResponse>(
    ushort requestId, ushort responseId, TRequest payload,
    Action<TResponse> onSuccess, Action<string> onFailure, out uint seq)
    where TRequest : class, IMessage<TRequest>
    where TResponse : class, IMessage<TResponse>;

public bool CancelRequest(uint seq);
```

The request ordering is fixed: first call `payload.ToByteArray()` before touching `_nextSeq` or `_pending`. A protobuf serialization exception calls failure once, returns `false` with `out seq=0`, creates no pending entry, and leaves the next sequence unchanged. Next verify an alive transport. Under `_pendingGate`, allocate a nonzero free sequence and register the parser/callback entry. Then build the frame with `Codec.Encode(requestId, seq, body)` using the already serialized body. Release `_pendingGate` before `transport.Send` so a synchronous response cannot deadlock. A framing or send failure atomically removes the entry and calls failure only if this path won completion; a synchronous send failure returns `false` while retaining the allocated `out seq` for diagnostics. `ReceiveFrame` routes `seq=0` only to `On<T>` push subscriptions; nonzero frames atomically remove matching pending, accept expected response ID or `MsgID.Error`, and never invoke callbacks while holding `_pendingGate`. `NotifyDisconnected`, `Disconnect`, and `Dispose` drain pending through one helper.

- [ ] **Step 4: Run focused network tests**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testFilter 'Game.Tests.EditMode.Protocol.ProtobufGoldenFrameTests;Game.Tests.EditMode.Network' `
  -testResults 'Logs\seq-client-green.xml' -logFile 'Logs\seq-client-green.log'
```

Expected: all selected tests pass with no unexpected Unity errors.

- [ ] **Step 5: Commit on the client coordination branch (do not merge or push)**

```powershell
git add Assets/Scripts/Protocol/Codec.cs Assets/Scripts/Network Assets/Tests/EditMode/Protocol Assets/Tests/EditMode/Network
git commit -m "feat: correlate Unity protobuf requests by seq"
```

### Task 6: Migrate Login, Archive, Heartbeat, and Combat Retry Owners

**Files:**
- Modify: client `Assets/Scripts/Online/LoginSessionService.cs`
- Modify: client `Assets/Scripts/Online/ArchiveSessionService.cs`
- Modify: client `Assets/Scripts/Online/BattleSettlementService.cs`
- Modify: client `Assets/Scripts/Online/BattleSettlementCoordinator.cs`
- Modify: client `Assets/Scripts/Network/NetworkConnectionController.cs`
- Modify: client `Assets/Tests/EditMode/Online/LoginAndArchiveSessionServiceTests.cs`
- Modify: client `Assets/Tests/EditMode/Online/BattleSettlementCoordinatorTests.cs`
- Modify: client `Assets/Tests/EditMode/Online/OnlineSessionCoordinatorTests.cs`
- Modify: client `Assets/Tests/EditMode/Online/OnlineSessionHostTests.cs`

**Interfaces:**
- Consumes: Task 5 `NetworkClient.Request` and `CancelRequest`.
- Produces: business services with one active transport sequence and no response-as-push subscriptions.

- [ ] **Step 1: Write failing service and retry tests**

Required cases: `LoginRequestCompletesOnlyItsOwnSeq`, `ArchiveSaveErrorCompletesOnlyActiveSeq`, `HeartbeatUsesNonZeroSeqWithoutRequestTimer`, and `RetryCancelsOldSeqReusesRunIdAndIgnoresLateOldSuccessOrError`.

The combat retry test decodes the first and second outgoing frames, asserts different nonzero sequences and equal `CombatResultReq.RunId`, injects old success and old `ErrorResp`, and asserts the coordinator remains pending until the second sequence completes.

- [ ] **Step 2: Run focused tests and verify RED**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testFilter 'Game.Tests.EditMode.Online' -testResults 'Logs\seq-online-red.xml' `
  -logFile 'Logs\seq-online-red.log'
```

Expected: old global response subscriptions allow unrelated frames or cannot compile against the new Codec.

- [ ] **Step 3: Replace response subscriptions with request callbacks**

Each service stores its active `uint` sequence. Its cancellation path sets business state inactive before calling `CancelRequest`, so the cancellation failure callback cannot revive or fail a newer operation.

Change battle send to:

```csharp
public bool Send(
    CombatResultReq request,
    Action<CombatResultResp> onSuccess,
    Action<string> onFailure,
    out uint seq)
```

`BattleSettlementCoordinator` stores `_combatSeq`. Before every forced resend/retry, set a cancellation guard, call `CancelRequest(_combatSeq)`, clear the guard, then call `Request` with the existing `_request`. On success/failure clear `_combatSeq` only for the active attempt. Keep `_request.RunId` unchanged and preserve the existing rule that an accepted combat response retries only archive save.

Heartbeat uses `Request<HeartbeatReq,HeartbeatResp>` and ignores the success payload; the connection controller remains the only timer/retry owner.

- [ ] **Step 4: Run online and host tests**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testFilter 'Game.Tests.EditMode.Online;Game.Tests.EditMode.Network.NetworkConnectionControllerTests' `
  -testResults 'Logs\seq-online-green.xml' -logFile 'Logs\seq-online-green.log'
```

Expected: all selected tests pass, including existing same-`run_id` recovery and archive-only retry cases.

- [ ] **Step 5: Commit on the client coordination branch (do not merge or push)**

```powershell
git add Assets/Scripts/Online Assets/Scripts/Network/NetworkConnectionController.cs Assets/Tests/EditMode/Online Assets/Tests/EditMode/Network/NetworkConnectionControllerTests.cs
git commit -m "feat: sequence online and combat requests"
```

### Task 7: Migrate Managers and Add Payment/GM Client Boundaries

**Files:**
- Modify: client `Assets/Scripts/Managers/LoginManager.cs`
- Modify: client `Assets/Scripts/Managers/ArchiveManager.cs`
- Modify: client `Assets/Scripts/Managers/RankManager.cs`
- Modify: client `Assets/Scripts/Managers/CombatManager.cs`
- Create: client `Assets/Scripts/Online/PaymentSessionService.cs`
- Create: client `Assets/Scripts/Online/GmCommandService.cs`
- Create: client `Assets/Tests/EditMode/Online/PaymentAndGmSessionServiceTests.cs`
- Modify: client `Assets/Tests/EditMode/Network/ManagerNetworkSubscriptionTests.cs`
- Modify: client every remaining test using old `Codec.Encode/TryDecode` signatures

**Interfaces:**
- Consumes: Task 5 request/push split.
- Produces: no production request through legacy `Send`; functional create-order, payment notification, and GM command APIs.

- [ ] **Step 1: Write failing manager/payment/GM tests**

Required cases: `ManagerRequestsUseNonZeroCorrelation`, `PaymentCreateOrderUsesRequestSeq`, `PaymentNotificationUsesZeroSeqPush`, `GmCommandResponseUsesRequestSeq`, `GmBroadcastUsesZeroSeqPush`, and `CombatManagerCanUpdatePlayerStats`.

- [ ] **Step 2: Run tests and verify RED**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testFilter 'Game.Tests.EditMode.Network.ManagerNetworkSubscriptionTests;Game.Tests.EditMode.Online.PaymentAndGmSessionServiceTests' `
  -testResults 'Logs\seq-managers-red.xml' -logFile 'Logs\seq-managers-red.log'
```

Expected: failures because payment/GM services and player-stats update request are absent.

- [ ] **Step 3: Implement all remaining request boundaries**

`PaymentSessionService` exposes:

```csharp
public bool CreateOrder(int productId, Action<CreateOrderResp> onSuccess, Action<string> onFailure, out uint seq);
public event Action<PayResultNotify> PaymentResult;
```

It registers only `On<PayResultNotify>(MsgID.PayResultNotify, ...)`, which can run only for `seq=0` frames. `GmCommandService` exposes:

```csharp
public bool Execute(string command, byte[] argsJson, Action<GMCommandResp> onSuccess, Action<string> onFailure, out uint seq);
public event Action<GMCommandResp> BroadcastReceived;
```

Its request callback handles nonzero `seq`; its `On<GMCommandResp>` subscription receives only `seq=0` GM broadcasts. Replace every manager `Send` plus response `On<T>` pair with `Request`; add `CombatManager.UpdatePlayerStats(PlayerStatsData stats)` using IDs `4013/4014`.

- [ ] **Step 4: Run EditMode and static legacy scans**

```powershell
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode `
  -projectPath 'E:\Own_project\game-client-unity' -runTests -testPlatform EditMode `
  -testResults 'Logs\seq-all-editmode.xml' -logFile 'Logs\seq-all-editmode.log'
rg -n "\.Send\(MsgID\.|Codec\.Encode\([^,]+,[^,]+\)|TryDecode\([^,]+,\s*out\s+[^,]+,\s*out\s+[^,]+\)" Assets/Scripts Assets/Tests -g '*.cs'
```

Expected: full EditMode passes; the scan has no production legacy request calls or old codec signatures.

- [ ] **Step 5: Commit on the client coordination branch (do not merge or push)**

```powershell
git add Assets/Scripts/Managers Assets/Scripts/Online Assets/Tests/EditMode
git commit -m "feat: sequence remaining online features"
```

### Task 8: Prove the Full Cross-Repository Contract

**Files:**
- Modify: server `tools/protobuf/Verify-Protocol.ps1`
- Modify: server `cmd/devprobe/main.go`
- Modify: client `tools/integration/Invoke-A4BackendIntegration.ps1`
- Modify: client `tools/integration/Invoke-A4BackendIntegration.Tests.ps1`
- Modify: client `Assets/Tests/PlayMode/RealBackendOnlineFlowTests.cs`
- Modify as required by new helper signatures: client PlayMode tests under `Assets/Tests/PlayMode`

**Interfaces:**
- Consumes: all prior transport and business tasks.
- Produces: repeatable 10-byte/seq verification, real-backend 3/3 evidence, and clean process ownership.

- [ ] **Step 1: Write failing verifier/runner assertions**

Pester must require these evidence strings from the runner:

```text
PROTO_SCHEMA_SHA256_MATCH=1
DEVPROBE_EVIDENCE=sequenced_protobuf_archive_and_combat:1
UNITY_RESULT=total=3 passed=3 failed=0 skipped=0
```

Add a devprobe assertion that every response `seq` equals its request and no response uses zero. Update `RealBackendOnlineFlowTests` helpers to decode the outgoing request sequence and encode the corresponding response with that sequence.

- [ ] **Step 2: Run integration Pester and verify RED**

```powershell
Set-Location E:\Own_project\game-client-unity
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/integration/Invoke-A4BackendIntegration.Tests.ps1 -EnableExit"
```

Expected: fixture/contract failure because current runner evidence does not name the sequenced contract.

- [ ] **Step 3: Update the verifier and owned integration runner**

The runner first executes both local generators in `-Check`, compares the raw proto hashes, then runs Go tests/build/devprobe and Unity. It must never stop or reuse an unowned process. Preserve the current `finally` cleanup for captured PIDs, `GAME_BACKEND_INTEGRATION`, temporary executables, and ports `8080/8081`.

The success evidence becomes:

```go
const probeSuccessEvidence = "development session probe passed: sequenced protobuf login found=false typed save typed reload combat duplicate"
```

- [ ] **Step 4: Run complete verification and three consecutive real-backend runs**

```powershell
Set-Location E:\Own_project\game-server-go
powershell.exe -NoProfile -File tools/protobuf/Verify-Protocol.ps1 -ClientRoot E:\Own_project\game-client-unity
go test ./... -count=1
go vet ./...
go build ./...

Set-Location E:\Own_project\game-client-unity
powershell.exe -NoProfile -File tools/protobuf/Verify-GeneratedProtocol.ps1 -BackendRoot E:\Own_project\game-server-go
powershell.exe -NoProfile -Command "Invoke-Pester -Script tools/protobuf/GeneratedProtocol.Tests.ps1,tools/integration/Invoke-A4BackendIntegration.Tests.ps1 -EnableExit"
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode -projectPath . -runTests -testPlatform EditMode -testResults Logs\seq-final-editmode.xml -logFile Logs\seq-final-editmode.log
& 'D:\Unity_Soft\2022\Editor\Unity.exe' -batchmode -projectPath . -runTests -testPlatform PlayMode -testResults Logs\seq-final-playmode.xml -logFile Logs\seq-final-playmode.log
1..3 | ForEach-Object { & .\tools\integration\Invoke-A4BackendIntegration.ps1 -BackendRoot E:\Own_project\game-server-go }
```

Expected: all commands pass; every integration run reports exactly 3/3 plus one archive, one victory, and one defeat marker; captured processes exit and ports 8080/8081 are free.

- [ ] **Step 5: Commit both repositories on the coordination branches (do not merge or push yet)**

```powershell
git -C E:\Own_project\game-server-go add tools/protobuf cmd/devprobe
git -C E:\Own_project\game-server-go commit -m "test: verify sequenced protobuf integration"
git -C E:\Own_project\game-client-unity add tools/integration Assets/Tests/PlayMode
git -C E:\Own_project\game-client-unity commit -m "test: prove sequenced backend combat flow"
```

- [ ] **Step 6: Fast-forward local masters and perform the coordinated release**

Keep the new-protocol deployment/startup/activation gate closed. Fast-forward both local masters, record their expected SHAs in the release record before either push, then push sequentially and poll both remote refs:

```powershell
git -C E:\Own_project\game-server-go checkout master
git -C E:\Own_project\game-server-go merge --ff-only feature/sequenced-protobuf-transport
git -C E:\Own_project\game-client-unity checkout master
git -C E:\Own_project\game-client-unity merge --ff-only feature/sequenced-protobuf-transport

$serverExpected = (git -C E:\Own_project\game-server-go rev-parse master).Trim()
$clientExpected = (git -C E:\Own_project\game-client-unity rev-parse master).Trim()
"PROTOCOL_RELEASE_GATE=CLOSED"
"SERVER_EXPECTED_SHA=$serverExpected"
"CLIENT_EXPECTED_SHA=$clientExpected"

git -C E:\Own_project\game-server-go push origin master
if ($LASTEXITCODE -ne 0) { throw "server master push failed; release gate remains closed" }
git -C E:\Own_project\game-client-unity push origin master
if ($LASTEXITCODE -ne 0) { throw "client master push failed; server may already be remote; release gate remains closed" }

$deadline = (Get-Date).AddMinutes(2)
do {
    $serverLine = @(git -C E:\Own_project\game-server-go ls-remote origin refs/heads/master)
    $serverRemote = if ($LASTEXITCODE -eq 0 -and $serverLine.Count -eq 1) { ($serverLine[0] -split '\s+')[0] } else { '' }
    $clientLine = @(git -C E:\Own_project\game-client-unity ls-remote origin refs/heads/master)
    $clientRemote = if ($LASTEXITCODE -eq 0 -and $clientLine.Count -eq 1) { ($clientLine[0] -split '\s+')[0] } else { '' }
    if ($serverRemote -eq $serverExpected -and $clientRemote -eq $clientExpected) { break }
    Start-Sleep -Seconds 2
} while ((Get-Date) -lt $deadline)

if ($serverRemote -ne $serverExpected -or $clientRemote -ne $clientExpected) {
    throw "remote masters do not match both expected SHAs; release gate remains closed"
}
"PROTOCOL_RELEASE_GATE=OPEN SERVER=$serverRemote CLIENT=$clientRemote"
```

Only after the final gate-open evidence may the new protocol be deployed, started, or activated. If the second push fails after the first succeeds, do not roll back or deploy the first repository alone: keep the gate closed, repair and retry the failed push, then rerun the two-ref poll against the recorded expected SHAs.

## Final Acceptance

- [ ] Both `proto/game.proto` files are byte-identical and each repository can regenerate its own output without drift.
- [ ] The old schema and old generated names are absent.
- [ ] The 15-byte LoginReq golden frame is exact on Go and C#.
- [ ] Go ordinary requests reject zero sequence by closing without a zero-sequence error.
- [ ] All response and error paths echo request sequence; all broadcast/payment/GM pushes use zero.
- [ ] Unity pending requests pass increment, wrap, skip, reentrant response, out-of-order, same-ID concurrency, malformed body, wrong ID, ErrorResp, cancel, disconnect, dispose, duplicate, and unknown-sequence tests.
- [ ] Login, heartbeat, archive, rank, combat configuration/stats, payment, GM, and combat settlement use correlated requests.
- [ ] Combat retry cancels the old sequence, reuses `run_id`, ignores late old responses, and does not duplicate settlement or archive save.
- [ ] Go test/vet/build, Pester, full EditMode, full PlayMode, and three owned real-backend 3/3 runs pass.
- [ ] Tasks 1-8 have clean spec and quality reviews on both coordination branches before either local `master` is fast-forwarded.
- [ ] The coordinated release records both expected master SHAs, sequentially pushes the repositories, and verifies both `origin/master` refs match; deployment, startup, and activation remain blocked until both are confirmed.
- [ ] A partial cross-repository push leaves the successful remote commit in place but keeps the release gate closed until the failed push is repaired and both remote refs are reverified; a one-sided protocol is never deployed.

## Self-Review

- Spec coverage: Tasks 1-8 cover schema ownership, local generation, 10-byte framing, sequence rules, pending atomicity, error/close behavior, all request owners, push semantics, combat retry, real-backend acceptance, and the cross-repository release gate.
- Placeholder scan: the plan contains no deferred implementation step or unspecified edge-case instruction.
- Type consistency: `seq` is `uint32`/`uint`; message IDs are `uint16`/`ushort`; `duration_ms` remains `int64`/`long`; business `run_id` remains `string`.
- Scope separation: combat resource generation and asset auditing are intentionally handled by the separate client plan `2026-07-21-combat-resource-engineering.md`.
