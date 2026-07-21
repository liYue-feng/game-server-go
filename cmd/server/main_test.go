package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"game-server/internal/combat"
	"game-server/internal/config"
	"game-server/internal/game"
	"game-server/internal/gm"
	"game-server/internal/login"
	"game-server/internal/payment"
	"game-server/internal/protocol"
	"game-server/internal/protocolpb"
	"game-server/internal/rank"
	"game-server/internal/session"
	"game-server/internal/store"

	"google.golang.org/protobuf/proto"
)

// These assignments make every production WebSocket route fail compilation if
// a handler drifts from its generated protobuf request or response type.
var (
	_ interface {
		Login(context.Context, *protocolpb.LoginReq) (*protocolpb.LoginResp, error)
		Heartbeat(context.Context, *protocolpb.HeartbeatReq) (*protocolpb.HeartbeatResp, error)
	} = (*login.Handler)(nil)
	_ interface {
		SaveArchive(context.Context, *protocolpb.SaveArchiveReq) (*protocolpb.SaveArchiveResp, error)
		LoadArchive(context.Context, *protocolpb.LoadArchiveReq) (*protocolpb.LoadArchiveResp, error)
	} = (*game.Handler)(nil)
	_ interface {
		GetRank(context.Context, *protocolpb.GetRankReq) (*protocolpb.GetRankResp, error)
		SubmitScore(context.Context, *protocolpb.SubmitScoreReq) (*protocolpb.SubmitScoreResp, error)
	} = (*rank.Handler)(nil)
	_ interface {
		CreateOrder(context.Context, *protocolpb.CreateOrderReq) (*protocolpb.CreateOrderResp, error)
	} = (*payment.Handler)(nil)
	_ interface {
		CombatResult(context.Context, *protocolpb.CombatResultReq) (*protocolpb.CombatResultResp, error)
		GetEnemyConfigs(context.Context, *protocolpb.GetEnemyConfigsReq) (*protocolpb.GetEnemyConfigsResp, error)
		GetDungeonConfig(context.Context, *protocolpb.GetDungeonConfigReq) (*protocolpb.GetDungeonConfigResp, error)
		GetStyleConfigs(context.Context, *protocolpb.GetStyleConfigsReq) (*protocolpb.GetStyleConfigsResp, error)
		UnlockStyle(context.Context, *protocolpb.UnlockStyleReq) (*protocolpb.UnlockStyleResp, error)
		GetPlayerStats(context.Context, *protocolpb.GetPlayerStatsReq) (*protocolpb.GetPlayerStatsResp, error)
		UpdatePlayerStats(context.Context, *protocolpb.UpdatePlayerStatsReq) (*protocolpb.UpdatePlayerStatsResp, error)
	} = (*combat.Handler)(nil)
	_ interface {
		Command(context.Context, *protocolpb.GMCommandReq) (*protocolpb.GMCommandResp, error)
	} = (*gm.Handler)(nil)
)

func TestProductionWebSocketRouteIDsCoverEveryRequest(t *testing.T) {
	routes := protocol.Routes()
	if len(routes) != 15 {
		t.Fatalf("route coverage = %d, want 15", len(routes))
	}
	for _, route := range routes {
		if route.RequestID == 0 || route.ResponseID == 0 || route.RequestPrototype == nil || route.ResponsePrototype == nil {
			t.Fatalf("invalid route %#v", route)
		}
	}
}

func TestServerHasNoPaymentCallbackSurface(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go) error = %v", err)
	}
	for _, forbidden := range []string{
		"newPaymentCallbackServer",
		"newPaymentCallbackHandler",
		"/pay/callback",
		":8081",
		"MaxBytesReader",
	} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("main.go still contains payment callback surface %q", forbidden)
		}
	}
}

type runtimeCaptureConn struct {
	frames [][]byte
}

func (c *runtimeCaptureConn) Reply(seq uint32, msgID uint16, payload proto.Message) error {
	frame, err := protocol.Encode(msgID, seq, payload)
	if err != nil {
		return err
	}
	c.frames = append(c.frames, frame)
	return nil
}
func (c *runtimeCaptureConn) Push(msgID uint16, payload proto.Message) error {
	return c.Reply(0, msgID, payload)
}

func TestNewRuntimeDevelopmentRegistersOnlyOnlineSessionMessages(t *testing.T) {
	cfg := &config.Config{Development: config.DevelopmentConfig{Enabled: true, LoginEnabled: true}}

	appRuntime, err := newRuntime(cfg)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = appRuntime.close() })

	if !appRuntime.development {
		t.Fatal("runtime.development = false, want true")
	}
	if appRuntime.kernel == nil || appRuntime.server == nil {
		t.Fatal("development runtime must own kernel and transport server")
	}

	present := []uint16{
		protocol.MsgID_LoginReq,
		protocol.MsgID_HeartbeatReq,
		protocol.MsgID_SaveArchiveReq,
		protocol.MsgID_LoadArchiveReq,
		protocol.MsgID_CombatResultReq,
		protocol.MsgID_GetPlayerStatsReq,
		protocol.MsgID_CreateOrderReq,
	}
	for _, msgID := range present {
		if !appRuntime.kernel.HasHandler(msgID) {
			t.Errorf("development kernel missing message %d", msgID)
		}
	}

	absent := []uint16{
		protocol.MsgID_GetRankReq,
		protocol.MsgID_SubmitScoreReq,
		protocol.MsgID_GetEnemyConfigsReq,
		protocol.MsgID_GetDungeonConfigReq,
		protocol.MsgID_GetStyleConfigsReq,
		protocol.MsgID_UnlockStyleReq,
		protocol.MsgID_UpdatePlayerStatsReq,
		protocol.MsgID_GMCommandReq,
	}
	for _, msgID := range absent {
		if appRuntime.kernel.HasHandler(msgID) {
			t.Errorf("development kernel unexpectedly registered message %d", msgID)
		}
	}

	for _, msgID := range []uint16{protocol.MsgID_LoginReq, protocol.MsgID_HeartbeatReq} {
		if !appRuntime.kernel.IsAuthFree(msgID) {
			t.Errorf("message %d must be auth free", msgID)
		}
	}
	if appRuntime.kernel.IsAuthFree(protocol.MsgID_SaveArchiveReq) {
		t.Fatal("archive save must require authentication")
	}
}

func TestNewRuntimeDevelopmentReturnsCorrelatedPaymentDisabledError(t *testing.T) {
	appRuntime, err := newRuntime(&config.Config{
		Development: config.DevelopmentConfig{Enabled: true, LoginEnabled: true},
	})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = appRuntime.close() })

	conn := &runtimeCaptureConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	dispatchRuntimeRequestSeq(t, appRuntime, ctx, protocol.MsgID_LoginReq, 1,
		&protocolpb.LoginReq{Code: "dev:payment-disabled"})
	dispatchRuntimeRequestSeq(t, appRuntime, ctx, protocol.MsgID_CreateOrderReq, 60001,
		&protocolpb.CreateOrderReq{ProductId: 1})

	if len(conn.frames) != 2 {
		t.Fatalf("response frames = %d, want 2", len(conn.frames))
	}
	message, err := protocol.Decode(conn.frames[1])
	if err != nil {
		t.Fatalf("decode payment response: %v", err)
	}
	if message.MsgID != protocol.MsgID_Error || message.Seq != 60001 {
		t.Fatalf("payment response = id %d seq %d, want id %d seq 60001",
			message.MsgID, message.Seq, protocol.MsgID_Error)
	}
	var response protocolpb.ErrorResp
	if err := proto.Unmarshal(message.Body, &response); err != nil {
		t.Fatalf("decode payment error: %v", err)
	}
	if response.Code != int32(protocol.ErrPaymentUnavailable) || response.Msg != "payment is disabled" {
		t.Fatalf("payment error = code %d message %q", response.Code, response.Msg)
	}
}

func TestNewRuntimeProductionRegistersDisabledPaymentRoute(t *testing.T) {
	storeCalls := installCountingStoreOpeners(t)

	appRuntime, err := newRuntime(&config.Config{})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = appRuntime.close() })
	if *storeCalls != 2 {
		t.Fatalf("store opener calls = %d, want 2", *storeCalls)
	}
	if !appRuntime.kernel.HasHandler(protocol.MsgID_CreateOrderReq) {
		t.Fatal("production kernel missing disabled payment route")
	}
}

func TestNewRuntimeRejectsPaymentEnabledBeforeOpeningStores(t *testing.T) {
	for _, development := range []bool{false, true} {
		t.Run(fmt.Sprintf("development=%t", development), func(t *testing.T) {
			storeCalls := installCountingStoreOpeners(t)
			appRuntime, err := newRuntime(&config.Config{
				Wechat:      config.WechatConfig{PaymentEnabled: true},
				Development: config.DevelopmentConfig{Enabled: development},
			})
			if err == nil || !strings.Contains(err.Error(), "secure payment provider is unavailable") {
				t.Fatalf("newRuntime() error = %v, want secure provider unavailable", err)
			}
			if appRuntime != nil {
				t.Fatal("newRuntime() returned runtime with payment enabled")
			}
			if *storeCalls != 0 {
				t.Fatalf("store opener calls = %d, want 0", *storeCalls)
			}
		})
	}
}

func TestNewRuntimeRejectsEnvironmentEnabledPaymentBeforeOpeningStores(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("wechat:\n  payment_enabled: false\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("GAME_WECHAT_PAYMENT_ENABLED", "true")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if !cfg.Wechat.PaymentEnabled {
		t.Fatal("Wechat.PaymentEnabled = false, want environment override true")
	}
	storeCalls := installCountingStoreOpeners(t)
	appRuntime, err := newRuntime(cfg)
	if err == nil || !strings.Contains(err.Error(), "secure payment provider is unavailable") {
		t.Fatalf("newRuntime() error = %v, want secure provider unavailable", err)
	}
	if appRuntime != nil || *storeCalls != 0 {
		t.Fatalf("runtime = %v, store opener calls = %d; want nil and 0", appRuntime, *storeCalls)
	}
}

func TestNewRuntimeDevelopmentInstallsAuthenticationHook(t *testing.T) {
	appRuntime, err := newRuntime(&config.Config{
		Development: config.DevelopmentConfig{Enabled: true, LoginEnabled: true},
	})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = appRuntime.close() })

	conn := &runtimeCaptureConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	frame, err := protocol.Encode(protocol.MsgID_SaveArchiveReq, 1, &protocolpb.SaveArchiveReq{})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	appRuntime.kernel.Dispatch(ctx, frame)

	if len(conn.frames) != 1 {
		t.Fatalf("response frames = %d, want 1", len(conn.frames))
	}
	message, err := protocol.Decode(conn.frames[0])
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if message.MsgID != protocol.MsgID_Error {
		t.Fatalf("response message = %d, want error", message.MsgID)
	}
	var response protocolpb.ErrorResp
	if err := proto.Unmarshal(message.Body, &response); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if response.Code != int32(protocol.ErrUnauthorized) {
		t.Fatalf("error code = %d, want %d", response.Code, protocol.ErrUnauthorized)
	}
}

func TestNewRuntimeDevelopmentRoutesSettlementLevelToStats(t *testing.T) {
	appRuntime, err := newRuntime(&config.Config{Development: config.DevelopmentConfig{Enabled: true, LoginEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = appRuntime.close() })
	conn := &runtimeCaptureConn{}
	ctx := session.WithSession(context.Background(), session.New(conn))
	dispatchRuntimeRequest(t, appRuntime, ctx, protocol.MsgID_LoginReq, &protocolpb.LoginReq{Code: "dev:level-route"})
	dispatchRuntimeRequest(t, appRuntime, ctx, protocol.MsgID_CombatResultReq, &protocolpb.CombatResultReq{RunId: "route-level-2", DungeonLevel: 2, Score: 10, Kills: 1, SurvivalTime: 1, StyleId: 1, Outcome: protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY, PlayerLevel: 2})
	dispatchRuntimeRequest(t, appRuntime, ctx, protocol.MsgID_CombatResultReq, &protocolpb.CombatResultReq{RunId: "route-level-1", DungeonLevel: 2, Score: 10, Kills: 1, SurvivalTime: 1, StyleId: 1, Outcome: protocolpb.BattleOutcome_BATTLE_OUTCOME_VICTORY, PlayerLevel: 1})
	dispatchRuntimeRequest(t, appRuntime, ctx, protocol.MsgID_GetPlayerStatsReq, &protocolpb.GetPlayerStatsReq{})
	if len(conn.frames) != 4 {
		t.Fatalf("response frames = %d, want 4", len(conn.frames))
	}
	message, err := protocol.Decode(conn.frames[3])
	if err != nil {
		t.Fatal(err)
	}
	if message.MsgID != protocol.MsgID_GetPlayerStatsResp {
		t.Fatalf("response ID = %d", message.MsgID)
	}
	stats := &protocolpb.GetPlayerStatsResp{}
	if err := proto.Unmarshal(message.Body, stats); err != nil {
		t.Fatal(err)
	}
	if stats.Level != 2 {
		t.Fatalf("stats level = %d, want 2", stats.Level)
	}
}

func dispatchRuntimeRequest(t *testing.T, appRuntime *runtime, ctx context.Context, id uint16, request proto.Message) {
	t.Helper()
	dispatchRuntimeRequestSeq(t, appRuntime, ctx, id, 1, request)
}

func dispatchRuntimeRequestSeq(t *testing.T, appRuntime *runtime, ctx context.Context, id uint16, seq uint32, request proto.Message) {
	t.Helper()
	frame, err := protocol.Encode(id, seq, request)
	if err != nil {
		t.Fatal(err)
	}
	appRuntime.kernel.Dispatch(ctx, frame)
}

func installCountingStoreOpeners(t *testing.T) *int {
	t.Helper()
	originalMySQL := openMySQLStore
	originalRedis := openRedisStore
	t.Cleanup(func() {
		openMySQLStore = originalMySQL
		openRedisStore = originalRedis
	})
	calls := 0
	openMySQLStore = func(*config.MySQLConfig) (*store.MySQLStore, func() error, error) {
		calls++
		return nil, func() error { return nil }, nil
	}
	openRedisStore = func(*config.RedisConfig) (*store.RedisStore, func() error, error) {
		calls++
		return nil, func() error { return nil }, nil
	}
	return &calls
}

func TestNewRuntimeProductionFailsBeforeServingWhenMySQLIsUnavailable(t *testing.T) {
	cfg := &config.Config{MySQL: config.MySQLConfig{
		Host: "127.0.0.1", Port: 1, User: "test", DBName: "test",
	}}

	appRuntime, err := newRuntime(cfg)
	if err == nil {
		t.Fatal("newRuntime() error = nil, want MySQL initialization failure")
	}
	if appRuntime != nil {
		t.Fatal("newRuntime() returned a runtime after MySQL initialization failure")
	}
	if !strings.Contains(err.Error(), "initialize mysql") {
		t.Fatalf("newRuntime() error = %q, want initialize mysql context", err)
	}
}

func TestNewRuntimeProductionClosesMySQLWhenRedisInitializationFails(t *testing.T) {
	originalMySQL := openMySQLStore
	originalRedis := openRedisStore
	t.Cleanup(func() {
		openMySQLStore = originalMySQL
		openRedisStore = originalRedis
	})

	mysqlCloseCalls := 0
	redisErr := errors.New("redis unavailable")
	openMySQLStore = func(*config.MySQLConfig) (*store.MySQLStore, func() error, error) {
		return nil, func() error {
			mysqlCloseCalls++
			return nil
		}, nil
	}
	openRedisStore = func(*config.RedisConfig) (*store.RedisStore, func() error, error) {
		return nil, nil, redisErr
	}

	appRuntime, err := newRuntime(&config.Config{})
	if !errors.Is(err, redisErr) {
		t.Fatalf("newRuntime() error = %v, want %v", err, redisErr)
	}
	if appRuntime != nil {
		t.Fatal("newRuntime() returned a runtime after Redis initialization failure")
	}
	if mysqlCloseCalls != 1 {
		t.Fatalf("MySQL close calls = %d, want 1", mysqlCloseCalls)
	}
}

func TestNewRuntimeCloseAttemptsAllResourcesOnceAndReturnsFirstError(t *testing.T) {
	firstErr := errors.New("first close failed")
	secondErr := errors.New("second close failed")
	firstCalls := 0
	secondCalls := 0

	closeRuntime := newRuntimeClose(
		nil,
		func() error { firstCalls++; return firstErr },
		nil,
		func() error { secondCalls++; return secondErr },
	)

	for attempt := 0; attempt < 2; attempt++ {
		if err := closeRuntime(); !errors.Is(err, firstErr) {
			t.Fatalf("close attempt %d error = %v, want %v", attempt+1, err, firstErr)
		}
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("close calls = first %d, second %d; want 1 each", firstCalls, secondCalls)
	}
}
