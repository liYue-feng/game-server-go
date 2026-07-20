# Phase A4 Development Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the server build and deliver an explicitly opt-in, in-process development backend for A4 WebSocket login, heartbeat, archive save, and archive load without MySQL, Redis, or WeChat credentials.

**Architecture:** Production continues to use MySQL, Redis, and WeChat, but its composition must return an error before opening the WebSocket listener when either data dependency cannot initialize. Player, Session, and Archive contracts live in internal/store; MySQL and Redis satisfy the production contracts, while one RWMutex-protected MemoryDevelopmentStore satisfies all development contracts. cmd/server/main.go is the composition root: its development branch creates only the four A4 message handlers and never creates middleware, rank, payment, combat, GM, or a payment callback server.

**Tech Stack:** Go 1.23, GORM/MySQL, go-redis, Viper, Gorilla WebSocket, Zap, standard-library sync and errors.

## Global Constraints

- Preserve the 4-byte little-endian length, 2-byte little-endian message ID, JSON protocol.
- A development credential succeeds only when both development.enabled and development.login_enabled are true.
- The production exchanger rejects every dev: code before any WeChat HTTP request.
- configs/config.dev.yaml contains no MySQL password, Redis password, WeChat AppID, or WeChat AppSecret. Production credentials enter only through GAME_ environment variables.
- Memory state ends when the process exits.
- Development registers only LoginReq, HeartbeatReq, SaveArchiveReq, and LoadArchiveReq.
- Follow red, green, refactor for every behavior change. Every task has one local commit; do not push.

## File Structure

- Modify: internal/model/player.go and internal/rank/handler.go - persistent and normalized player level; remove the missing GetPlayerStats call.
- Create: internal/model/player_test.go - level fallback regression.
- Modify: internal/config/config.go; create internal/config/config_test.go and configs/config.dev.yaml - explicit mode configuration without secrets.
- Create: internal/login/code_exchange.go and internal/login/code_exchange_test.go - production/development credential boundary.
- Create: internal/store/repository.go, internal/store/memory_development.go, internal/store/memory_development_test.go - small repository contracts and concurrent development store.
- Create: internal/login/service.go, internal/login/service_test.go, internal/game/service.go, internal/game/service_test.go; modify both handlers - thin transports over repository-backed services.
- Modify: internal/gateway/router.go; create its test - route registration inspection for composition tests.
- Modify: cmd/server/main.go; create cmd/server/main_test.go and cmd/devprobe/main.go - composition, production fail-fast, and a real WebSocket probe.

---

### Task 1: Repair The Rank And Player Compile Contract

**Files:**
- Create: internal/model/player_test.go
- Modify: internal/model/player.go
- Modify: internal/rank/handler.go:75-91
- Modify: internal/login/handler.go:145-152

**Interfaces:**
- Consumes: func (s *MySQLStore) GetPlayerByID(id int64) (*model.Player, error).
- Produces: func (p Player) EffectiveLevel() int and Player.Level int.

- [ ] **Step 1: Reproduce the proven root cause**

Run: `go test ./internal/rank`

Expected: exit code 1 with `h.mysql.GetPlayerStats undefined` at internal/rank/handler.go:82. This is the stale API call introduced after the rank level field, not an unavailable external service.

- [ ] **Step 2: Write the failing level contract**

Create internal/model/player_test.go:

~~~go
package model

import "testing"

func TestPlayerEffectiveLevelDefaultsInvalidValuesToOne(t *testing.T) {
	for _, tt := range []struct{ level, want int }{
		{0, 1}, {-4, 1}, {7, 7},
	} {
		if got := (Player{Level: tt.level}).EffectiveLevel(); got != tt.want {
			t.Fatalf("EffectiveLevel(%d) = %d, want %d", tt.level, got, tt.want)
		}
	}
}
~~~

- [ ] **Step 3: Verify red**

Run: `go test ./internal/model ./internal/rank -count=1`

Expected: exit code 1. model reports missing EffectiveLevel and rank still reports missing GetPlayerStats.

- [ ] **Step 4: Implement the smallest model/rank change**

Add after BestScore in Player:

~~~go
Level int `gorm:"not null;default:1" json:"level"`
~~~

Add to internal/model/player.go:

~~~go
func (p Player) EffectiveLevel() int {
	if p.Level < 1 {
		return 1
	}
	return p.Level
}
~~~

Add `Level: 1` to the existing new-player literal in internal/login/handler.go. Replace the rank block with:

~~~go
level := 1
if player, err := h.mysql.GetPlayerByID(uid); err == nil {
	level = player.EffectiveLevel()
}
~~~

- [ ] **Step 5: Verify green and commit**

Run: `gofmt -w internal/model/player.go internal/model/player_test.go internal/rank/handler.go internal/login/handler.go`

Run: `go test ./internal/model ./internal/rank -count=1`

Expected: exit code 0; the table test passes and rank compiles.

Run:

~~~powershell
git add internal/model/player.go internal/model/player_test.go internal/rank/handler.go internal/login/handler.go
git commit -m "fix: restore rank player level lookup"
~~~

Expected: one local commit containing only this repair.

### Task 2: Add Explicit Configuration And Credential Exchange

**Files:**
- Modify: internal/config/config.go
- Create: internal/config/config_test.go
- Create: configs/config.dev.yaml
- Create: internal/login/code_exchange.go
- Create: internal/login/code_exchange_test.go

**Interfaces:**
- Produces:

~~~go
type DevelopmentConfig struct {
	Enabled      bool `mapstructure:"enabled"`
	LoginEnabled bool `mapstructure:"login_enabled"`
}
type LoginIdentity struct { OpenID string }
type CodeExchanger interface { Exchange(code string) (LoginIdentity, error) }
func NewDevelopmentCodeExchanger(enabled bool) *DevelopmentCodeExchanger
func NewWechatCodeExchanger(client *WechatClient) *WechatCodeExchanger
~~~

- [ ] **Step 1: Write the failing configuration/exchanger tests**

Create internal/config/config_test.go. Write a temp YAML file whose exact content is:

~~~yaml
development:
  enabled: true
  login_enabled: true
~~~

Call Load(path), then assert both cfg.Development.Enabled and cfg.Development.LoginEnabled.

Add `TestLoadMapsNestedEnvironmentVariables`. Write a YAML file with `development.enabled: false`, use `t.Setenv("GAME_DEVELOPMENT_ENABLED", "true")`, call `Load(path)`, and require `cfg.Development.Enabled` true. This proves production secrets and deployment switches can use the documented `GAME_` underscore names.

Create internal/login/code_exchange_test.go:

~~~go
func TestDevelopmentCodeExchangerAcceptsConfiguredIdentity(t *testing.T) {
	identity, err := NewDevelopmentCodeExchanger(true).Exchange("dev:editor-001")
	if err != nil || identity.OpenID != "dev:editor-001" {
		t.Fatalf("Exchange() = %#v, %v", identity, err)
	}
}

func TestDevelopmentCodeExchangerRejectsDisabledOrMalformedCode(t *testing.T) {
	for _, tt := range []struct{ enabled bool; code string }{
		{false, "dev:editor-001"}, {true, "wechat-code"}, {true, "dev:"},
	} {
		if _, err := NewDevelopmentCodeExchanger(tt.enabled).Exchange(tt.code); err == nil {
			t.Fatalf("Exchange(%q) returned nil error", tt.code)
		}
	}
}

func TestWechatCodeExchangerRejectsDevelopmentCodeBeforeClientUse(t *testing.T) {
	if _, err := NewWechatCodeExchanger(nil).Exchange("dev:editor-001"); err == nil {
		t.Fatal("production exchanger accepted a development code")
	}
}
~~~

- [ ] **Step 2: Verify red**

Run: `go test ./internal/config ./internal/login -count=1`

Expected: exit code 1 because Config.Development and both exchanger constructors do not exist.

- [ ] **Step 3: Implement the explicit boundary**

Add this field to Config and add the DevelopmentConfig definition from this task's interface block:

~~~go
Development DevelopmentConfig `mapstructure:"development"`
~~~

Before `v.AutomaticEnv()` in `Load`, add:

~~~go
v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
~~~

Add the standard-library `strings` import to `internal/config/config.go`.

Create configs/config.dev.yaml:

~~~yaml
server:
  host: "127.0.0.1"
  port: 8080
  name: "game-server-dev"
development:
  enabled: true
  login_enabled: true
log:
  level: "debug"
  filename: "logs/server-dev.log"
  max_size: 20
  max_backups: 2
  max_age: 7
~~~

Create internal/login/code_exchange.go:

~~~go
var ErrDevelopmentCodeRejected = errors.New("development login code rejected")

type LoginIdentity struct{ OpenID string }
type CodeExchanger interface{ Exchange(code string) (LoginIdentity, error) }

type DevelopmentCodeExchanger struct{ enabled bool }

func NewDevelopmentCodeExchanger(enabled bool) *DevelopmentCodeExchanger {
	return &DevelopmentCodeExchanger{enabled: enabled}
}
func (e *DevelopmentCodeExchanger) Exchange(code string) (LoginIdentity, error) {
	identity := strings.TrimPrefix(code, "dev:")
	if !e.enabled || !strings.HasPrefix(code, "dev:") || identity == "" {
		return LoginIdentity{}, ErrDevelopmentCodeRejected
	}
	return LoginIdentity{OpenID: code}, nil
}

type WechatCodeExchanger struct{ client *WechatClient }

func NewWechatCodeExchanger(client *WechatClient) *WechatCodeExchanger {
	return &WechatCodeExchanger{client: client}
}
func (e *WechatCodeExchanger) Exchange(code string) (LoginIdentity, error) {
	if strings.HasPrefix(code, "dev:") {
		return LoginIdentity{}, ErrDevelopmentCodeRejected
	}
	result, err := e.client.Code2Session(code)
	if err != nil {
		return LoginIdentity{}, err
	}
	return LoginIdentity{OpenID: result.OpenID}, nil
}
~~~

Import only errors and strings in this new file.

- [ ] **Step 4: Verify green, secret boundary, and commit**

Run: `gofmt -w internal/config/config.go internal/config/config_test.go internal/login/code_exchange.go internal/login/code_exchange_test.go`

Run: `go test ./internal/config ./internal/login -count=1`

Expected: exit code 0.

Run: `rg -n -i 'app_secret:|app_id:|password:' configs/config.dev.yaml`

Expected: exit code 1 and no output.

Run:

~~~powershell
git add internal/config/config.go internal/config/config_test.go configs/config.dev.yaml internal/login/code_exchange.go internal/login/code_exchange_test.go
git commit -m "feat: add explicit development login mode"
~~~

Expected: one local commit.

### Task 3: Introduce Repository Contracts And The Concurrent Development Store

**Files:**
- Create: internal/store/repository.go
- Create: internal/store/memory_development.go
- Create: internal/store/memory_development_test.go

**Interfaces:**
- Produces:

~~~go
type PlayerRepository interface {
	GetPlayerByID(int64) (*Player, error)
	GetPlayerByOpenID(string) (*Player, error)
	CreatePlayer(*Player) error
	UpdatePlayer(*Player) error
}
type SessionRepository interface {
	SetSession(int64, *SessionData) error
	GetSession(int64) (*SessionData, error)
	DelSession(int64) error
}
type ArchiveRepository interface {
	GetArchive(int64) (*Archive, error)
	SaveArchive(*Archive) error
}
func NewMemoryDevelopmentStore() *MemoryDevelopmentStore
~~~

- [ ] **Step 1: Write failing memory-store tests**

Create internal/store/memory_development_test.go with these tests:

~~~go
func TestMemoryDevelopmentStoreCreatesIndependentPlayerCopies(t *testing.T)
func TestMemoryDevelopmentStoreStoresAndRefreshesSessions(t *testing.T)
func TestMemoryDevelopmentStoreSavesAndLoadsArchive(t *testing.T)
func TestMemoryDevelopmentStoreConcurrentPlayerCreation(t *testing.T)
~~~

The player test creates `&Player{OpenID: "dev:alice", Level: 1}`, asserts ID 1, mutates its nickname, fetches by OpenID, and asserts the stored nickname did not change. The session test stores `&SessionData{Uid: 1, Nickname: "玩家1", Token: "token-a"}`, mutates the local pointer, verifies the stored token remains token-a, deletes it, and asserts GetSession returns nil without error. The archive test saves `{"coins":3}` and requires exact round-trip text. The concurrency test starts 32 goroutines through sync.WaitGroup, each creating dev:user-%d, then asserts every returned ID is positive and unique.

- [ ] **Step 2: Verify red**

Run: `go test ./internal/store -run TestMemoryDevelopmentStore -count=1`

Expected: exit code 1 because NewMemoryDevelopmentStore is missing.

- [ ] **Step 3: Implement only A4 storage surfaces**

Create repository.go with the three interfaces above and:

~~~go
var ErrNotFound = errors.New("store: not found")

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound)
}
~~~

Create MemoryDevelopmentStore with:

~~~go
type MemoryDevelopmentStore struct {
	mu                sync.RWMutex
	nextPlayerID      int64
	playersByID       map[int64]Player
	playerIDByOpenID  map[string]int64
	sessionsByUID     map[int64]SessionData
	archivesByUID     map[int64]Archive
}
~~~

NewMemoryDevelopmentStore initializes every map and nextPlayerID to 1. CreatePlayer takes mu.Lock, rejects duplicate OpenID, writes the current ID to player, increments it, changes levels below 1 to 1, and stores a value copy. Every getter takes RLock and returns a new pointer to a value copy. UpdatePlayer, SetSession, DelSession, and SaveArchive take Lock. Missing player/archive returns an error wrapping ErrNotFound; missing session returns (nil, nil). This type has no rank, payment, combat, rate-limit, or persistence methods.

- [ ] **Step 4: Verify race safety and commit**

Run: `gofmt -w internal/store/repository.go internal/store/memory_development.go internal/store/memory_development_test.go`

Run: `go test -race ./internal/store -run TestMemoryDevelopmentStore -count=1`

Expected: exit code 0 with no DATA RACE report.

Run:

~~~powershell
git add internal/store/repository.go internal/store/memory_development.go internal/store/memory_development_test.go
git commit -m "feat: add memory development repositories"
~~~

Expected: one local commit.

### Task 4: Make Login And Archive Handlers Depend On The Small Interfaces

**Files:**
- Create: internal/login/service.go
- Create: internal/login/service_test.go
- Modify: internal/login/handler.go
- Create: internal/game/service.go
- Create: internal/game/service_test.go
- Modify: internal/game/handler.go
- Modify: cmd/server/main.go

**Interfaces:**
- Consumes: store.PlayerRepository, store.SessionRepository, store.ArchiveRepository, CodeExchanger, and store.IsNotFound.
- Produces:

~~~go
type TokenGenerator func() (string, error)
type LoginResult struct { UID int64; Nickname, Token string }
func NewLoginService(store.PlayerRepository, store.SessionRepository, CodeExchanger, TokenGenerator) *LoginService
func GenerateToken() (string, error)
func (s *LoginService) Login(code string) (LoginResult, error)
func (s *LoginService) RefreshSession(uid int64) error
func NewArchiveService(store.ArchiveRepository) *ArchiveService
func (s *ArchiveService) Save(uid int64, data string) error
func (s *ArchiveService) Load(uid int64) (string, error)
~~~

- [ ] **Step 1: Write failing service tests**

In internal/login/service_test.go, create a MemoryDevelopmentStore, enabled development exchanger, and generator returning `"fixed-token", nil`. Login dev:editor-001 twice; require both results UID 1, nonempty nickname, fixed token, and stored SessionData token fixed-token. Add a test whose generator returns errors.New("token entropy unavailable") and require Login to return that error with no session.

In internal/game/service_test.go, require Load(1) before a save to return empty data and nil error. Save `{"stage":2}` and require the next Load(1) to return that exact string.

- [ ] **Step 2: Verify red**

Run: `go test ./internal/login ./internal/game -count=1`

Expected: exit code 1 because both services and constructors are missing.

- [ ] **Step 3: Implement service behavior and thin handlers**

LoginService.Login performs this exact sequence:

~~~go
identity, err := s.exchanger.Exchange(code)
if err != nil { return LoginResult{}, err }
player, err := s.players.GetPlayerByOpenID(identity.OpenID)
if store.IsNotFound(err) {
	player = &store.Player{OpenID: identity.OpenID, Level: 1}
	if err = s.players.CreatePlayer(player); err != nil { return LoginResult{}, err }
	player.Nickname = fmt.Sprintf("玩家%d", player.ID)
	if err = s.players.UpdatePlayer(player); err != nil { return LoginResult{}, err }
} else if err != nil { return LoginResult{}, err }
token, err := s.generate()
if err != nil { return LoginResult{}, err }
player.Token = token
if err = s.players.UpdatePlayer(player); err != nil { return LoginResult{}, err }
if err = s.sessions.SetSession(player.ID, &store.SessionData{Uid: player.ID, Nickname: player.Nickname, Token: token}); err != nil { return LoginResult{}, err }
return LoginResult{UID: player.ID, Nickname: player.Nickname, Token: token}, nil
~~~

RefreshSession rewrites an existing session; for an absent session it writes `&store.SessionData{Uid: uid}`. Change login.Handler to hold `service *LoginService` and expose `func NewHandler(service *LoginService) *Handler`. Its transport methods retain request JSON parsing, conn.SetPlayerInfo, LoginResp, and timestamp echo; exchange errors send ErrLoginInvalidCode and all remaining service errors send ErrInternal.

Rename the existing package-private `generateToken` function in internal/login/handler.go to exported `GenerateToken`, keeping its crypto/rand and hex implementation unchanged. This is the production TokenGenerator supplied by the composition root; tests continue to inject their fixed generator.

ArchiveService.Save is `return s.archives.SaveArchive(&store.Archive{PlayerID: uid, Data: data})`. ArchiveService.Load returns empty/nil when store.IsNotFound(err), returns other errors unchanged, otherwise returns archive.Data. Change game.Handler to hold service *ArchiveService and remove its Redis dependency. Save retains ErrArchiveSaveFailed and Load sends LoadArchiveResp with service data.

Keep the repository buildable in this commit by replacing the existing constructor calls in `cmd/server/main.go` with:

~~~go
loginService := login.NewLoginService(
	mysqlStore,
	redisStore,
	login.NewWechatCodeExchanger(wechatClient),
	login.GenerateToken,
)
loginHandler := login.NewHandler(loginService)
archiveService := game.NewArchiveService(mysqlStore)
gameHandler := game.NewHandler(archiveService)
~~~

Task 5 will replace the nullable composition behavior; this step only migrates the constructor contract without adding development-mode branching.

- [ ] **Step 4: Verify green and commit**

Run: `gofmt -w internal/login/service.go internal/login/service_test.go internal/login/handler.go internal/game/service.go internal/game/service_test.go internal/game/handler.go cmd/server/main.go`

Run: `go test ./... -count=1`

Expected: exit code 0; no package reports build failed after the constructor migration.

Run:

~~~powershell
git add internal/login/service.go internal/login/service_test.go internal/login/handler.go internal/game/service.go internal/game/service_test.go internal/game/handler.go cmd/server/main.go
git commit -m "refactor: decouple a4 handlers from concrete stores"
~~~

Expected: one local commit.

### Task 5: Compose And Verify The Real Development Server

**Files:**
- Modify: internal/gateway/router.go
- Create: internal/gateway/router_test.go
- Modify: cmd/server/main.go
- Create: cmd/server/main_test.go
- Create: cmd/devprobe/main.go

**Interfaces:**
- Produces:

~~~go
func (r *Router) HasHandler(msgID uint16) bool
type runtime struct {
	router         *gateway.Router
	close          func() error
	development    bool
	paymentHandler *payment.Handler
}
func newRuntime(cfg *config.Config) (*runtime, error)
~~~

- [ ] **Step 1: Write failing route/composition tests**

router_test.go creates a Router, registers LoginReq with a no-op HandlerFunc, requires HasHandler(LoginReq) true and HasHandler(GetRankReq) false.

main_test.go defines:
- TestNewRuntimeDevelopmentRegistersOnlyA4Messages: build Config with Development.Enabled and LoginEnabled true, call newRuntime, defer close, require login/heartbeat/save/load true; require GetRankReq, SubmitScoreReq, CreateOrderReq, CombatResultReq, and GMCommandReq false; require paymentHandler nil.
- TestNewRuntimeProductionFailsBeforeServingWhenMySQLIsUnavailable: set Development.Enabled false and MySQL Host 127.0.0.1, Port 1, User test, DBName test; require newRuntime to return a non-nil error without starting a listener.

- [ ] **Step 2: Verify red**

Run: `go test ./internal/gateway ./cmd/server -count=1`

Expected: exit code 1 because HasHandler and newRuntime are absent.

- [ ] **Step 3: Implement composition and the executable WebSocket probe**

Add to Router:

~~~go
func (r *Router) HasHandler(msgID uint16) bool {
	_, ok := r.handlers[msgID]
	return ok
}
~~~

Add newRuntime in cmd/server/main.go. Its development branch creates exactly one memory store, one LoginService using NewDevelopmentCodeExchanger(cfg.Development.LoginEnabled) and login.GenerateToken, one ArchiveService, one router, one login handler, and one game handler. It registers only:

~~~go
router.Register(protocol.MsgID_LoginReq, loginHandler.HandleLogin)
router.Register(protocol.MsgID_HeartbeatReq, loginHandler.HandleHeartbeat)
router.Register(protocol.MsgID_SaveArchiveReq, gameHandler.HandleSaveArchive)
router.Register(protocol.MsgID_LoadArchiveReq, gameHandler.HandleLoadArchive)
~~~

Return runtime{router: router, development: true, close: func() error { return nil }}. Do not install either Redis middleware.

Its production branch initializes MySQL then Redis. Return `fmt.Errorf("initialize mysql: %w", err)` immediately on MySQL failure; on Redis failure close MySQL and return `fmt.Errorf("initialize redis: %w", err)`. Only after both succeed construct the WechatCodeExchanger, login/archive services, Redis middleware, and the current rank/payment/combat/GM handlers. The branch retains the full existing production registrations. runtime.close closes Redis and MySQL once and returns the first close error.

main calls newRuntime before gateway.NewServer and exits 1 on its error. It starts payment callback HTTP only when runtime.development is false. On SIGINT/SIGTERM it shuts down the gateway then calls runtime.close exactly once. Remove nullable store handling and its defers.

Create cmd/devprobe/main.go. Dial ws://127.0.0.1:8080/ws using websocket.DefaultDialer. Use protocol.Encode/Decode and json.Unmarshal to implement:

~~~go
send(protocol.MsgID_LoginReq, protocol.LoginReq{Code: "dev:process-probe"})
read(protocol.MsgID_LoginResp, &loginResp)
send(protocol.MsgID_SaveArchiveReq, protocol.SaveArchiveReq{Data: "{\"phase\":\"a4\"}"})
read(protocol.MsgID_SaveArchiveResp, &saveResp)
send(protocol.MsgID_LoadArchiveReq, protocol.LoadArchiveReq{})
read(protocol.MsgID_LoadArchiveResp, &loadResp)
~~~

The probe exits nonzero on an error, requires positive loginResp.Uid, nonempty token, saveResp.Success, and loadResp.Data exactly `{"phase":"a4"}`. It prints `development session probe passed` only after every assertion.

- [ ] **Step 4: Verify code quality**

Run: `gofmt -w internal/gateway/router.go internal/gateway/router_test.go cmd/server/main.go cmd/server/main_test.go cmd/devprobe/main.go`

Run: `go test ./...`

Expected: exit code 0 and no package reports build failed.

Run: `go build ./...`

Expected: exit code 0.

Run: `go vet ./...`

Expected: exit code 0 with no diagnostics.

Run: `gofmt -l .`

Expected: no output.

- [ ] **Step 5: Verify an actual server process and WebSocket session**

Run:

~~~powershell
Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue
~~~

Expected: no output. If a process is listening, identify and stop only the verified old development server before continuing.

Run:

~~~powershell
New-Item -ItemType Directory -Force 'logs' | Out-Null
go build -o 'logs/a4-dev-server.exe' ./cmd/server
if ($LASTEXITCODE -ne 0) { throw 'development server build failed' }
$server = Start-Process '.\logs\a4-dev-server.exe' -ArgumentList '-config','configs/config.dev.yaml' -PassThru -RedirectStandardOutput 'logs/a4-dev.stdout.log' -RedirectStandardError 'logs/a4-dev.stderr.log'
$deadline = (Get-Date).AddSeconds(20)
do {
  try { $health = Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:8080/health' -TimeoutSec 1; break } catch { Start-Sleep -Milliseconds 200 }
} while ((Get-Date) -lt $deadline)
if ($null -eq $health -or $health.StatusCode -ne 200 -or $health.Content -ne 'ok') { Stop-Process -Id $server.Id -Force; throw 'development server did not become healthy' }
go run ./cmd/devprobe
$probeExit = $LASTEXITCODE
Stop-Process -Id $server.Id -ErrorAction Stop
Wait-Process -Id $server.Id -ErrorAction SilentlyContinue
if ($probeExit -ne 0) { throw "devprobe exited $probeExit" }
~~~

Expected: health is 200/ok; devprobe prints development session probe passed and exits 0; stderr contains no MySQL, Redis, or WeChat initialization attempt.

- [ ] **Step 6: Commit the composition root**

Run:

~~~powershell
git add internal/gateway/router.go internal/gateway/router_test.go cmd/server/main.go cmd/server/main_test.go cmd/devprobe/main.go
git commit -m "feat: compose a4 development server mode"
~~~

Expected: one local commit. The implementation has five isolated commits and no push.

## Final Acceptance Checklist

- [ ] The Task 5 test, build, vet, and format commands have their stated successful output.
- [ ] config.dev.yaml contains only explicit development settings and no populated secret fields.
- [ ] Production dependency failure returns before any WebSocket listener starts.
- [ ] Development exposes exactly the four A4 messages and no payment callback.
- [ ] The real Go process and cmd/devprobe prove login plus archive save/load through ws://127.0.0.1:8080/ws.
- [ ] git log --oneline -5 shows one commit per task; git status --short is empty before the parent agent pushes.

## Execution Handoff

Plan complete and saved to docs/superpowers/plans/2026-07-18-phase-a4-development-backend.md. Two execution options:

1. **Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, review between tasks.
2. **Inline Execution** - Execute tasks in this session using executing-plans with review checkpoints.
