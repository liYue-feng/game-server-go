package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"game-server/internal/config"
	"game-server/internal/payment"
	"game-server/internal/protocol"
	"game-server/internal/session"
	"game-server/internal/store"
)

func TestPaymentCallbackRejectsOversizedBodyWithoutCallingHandler(t *testing.T) {
	callbackCalls := 0
	handler := newPaymentCallbackHandler(func([]byte) (*payment.CallbackResp, error) {
		callbackCalls++
		return &payment.CallbackResp{}, nil
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/pay/callback",
		strings.NewReader(strings.Repeat("x", int(maxPaymentCallbackBodySize)+1)),
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("callback status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	if callbackCalls != 0 {
		t.Fatalf("payment callback calls = %d, want 0", callbackCalls)
	}
}

func TestPaymentCallbackServerConfiguresTimeouts(t *testing.T) {
	server := newPaymentCallbackServer(&runtime{paymentHandler: &payment.Handler{}})
	if server == nil {
		t.Fatal("newPaymentCallbackServer() = nil for production runtime")
	}

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 10*time.Second {
		t.Errorf("ReadTimeout = %v, want 10s", server.ReadTimeout)
	}
	if server.WriteTimeout != 10*time.Second {
		t.Errorf("WriteTimeout = %v, want 10s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout = %v, want 60s", server.IdleTimeout)
	}
}

type runtimeCaptureConn struct {
	frames [][]byte
}

func (c *runtimeCaptureConn) SendMessage(msgID uint16, payload interface{}) error {
	frame, err := protocol.Encode(msgID, payload)
	if err != nil {
		return err
	}
	c.frames = append(c.frames, frame)
	return nil
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
	if appRuntime.paymentHandler != nil {
		t.Fatal("development runtime must not initialize payment")
	}

	present := []uint16{
		protocol.MsgID_LoginReq,
		protocol.MsgID_HeartbeatReq,
		protocol.MsgID_SaveArchiveReq,
		protocol.MsgID_LoadArchiveReq,
	}
	for _, msgID := range present {
		if !appRuntime.kernel.HasHandler(msgID) {
			t.Errorf("development kernel missing message %d", msgID)
		}
	}

	absent := []uint16{
		protocol.MsgID_GetRankReq,
		protocol.MsgID_SubmitScoreReq,
		protocol.MsgID_CreateOrderReq,
		protocol.MsgID_CombatResultReq,
		protocol.MsgID_GetEnemyConfigsReq,
		protocol.MsgID_GetDungeonConfigReq,
		protocol.MsgID_GetStyleConfigsReq,
		protocol.MsgID_UnlockStyleReq,
		protocol.MsgID_GetPlayerStatsReq,
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
	frame, err := protocol.Encode(protocol.MsgID_SaveArchiveReq, protocol.SaveArchiveReq{Data: "{}"})
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
	var response protocol.ErrorResp
	if err := json.Unmarshal(message.Body, &response); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if response.Code != protocol.ErrUnauthorized {
		t.Fatalf("error code = %d, want %d", response.Code, protocol.ErrUnauthorized)
	}
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
